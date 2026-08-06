// Command codexhooks is the Codex project-local hooks bridge for Reinframe (#163).
//
//	codexhooks pretool          # read PreToolUse/PermissionRequest JSON on stdin → stdout
//	codexhooks plan|install|uninstall|doctor -project ROOT [-command CMD]
//
// Does not modify Codex rollout/transcript files. Hook trust remains a Codex operator step.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	switch cmd {
	case "pretool", "hook":
		runHook(os.Stdin, os.Stdout)
	case "plan", "install", "uninstall", "doctor":
		runInstall(cmd, os.Args[2:])
	case "manifest":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(adapter.CodexHooksFoundationManifest())
	default:
		usage()
		os.Exit(2)
	}
}

func runHook(r io.Reader, w io.Writer) {
	raw, err := io.ReadAll(io.LimitReader(r, adapter.MaxCodexHookStdinBytes+1))
	if err != nil {
		fail(err)
	}
	in, err := adapter.ParseCodexHookStdin(raw)
	if err != nil {
		// Fail closed for control: deny PreToolUse when parse fails and event looks tool-like.
		out, _ := adapter.EncodeCodexHookResponse(map[string]any{
			"decision": "block",
			"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "deny",
				"permissionDecisionReason": "parse_fail_closed",
			},
			"systemMessage": "reinframe: hook input rejected",
		})
		_, _ = w.Write(out)
		os.Exit(0)
	}
	switch in.HookEventName {
	case adapter.CodexEventPreToolUse:
		pa, _ := adapter.ProposedActionFromCodexHook(in, adapter.ProposedActionOptions{})
		req := adapter.HookRequestFromCodexHook(in, &pa)
		// Default policy: allow (foundation binary is a mapper; real policy is injected by supervisor later).
		// For standalone CLI smoke, deny only when REINFRAME_CODEX_DENY_TOOLS matches.
		pol := adapter.HookPolicy{FailOpen: true}
		if deny := os.Getenv("REINFRAME_CODEX_DENY_TOOLS"); deny != "" {
			pol.DeniedTools = map[string]struct{}{deny: {}}
			pol.FailOpen = false
		}
		dec := adapter.EvaluateHook(context.Background(), req, pol)
		challengeID := os.Getenv("REINFRAME_CHALLENGE_ID")
		resp := adapter.CodexPreToolResponseFromDecision(in, dec, challengeID, "")
		out, err := adapter.EncodeCodexHookResponse(resp)
		if err != nil {
			fail(err)
		}
		_, _ = w.Write(out)
	case adapter.CodexEventPermissionRequest:
		// Default fall-through (empty decision) — host surfaces approval.
		out, _ := adapter.EncodeCodexHookResponse(map[string]any{})
		_, _ = w.Write(out)
	case adapter.CodexEventSessionStart, adapter.CodexEventUserPromptSubmit:
		ctx := "Reinframe Codex hooks foundation active (" + adapter.CodexHooksProfileV1 + "). Observe+gate foundation only; not live smoke."
		out, err := adapter.EncodeCodexHookResponse(adapter.CodexSessionContextResponse(in.HookEventName, ctx))
		if err != nil {
			fail(err)
		}
		_, _ = w.Write(out)
	default:
		// PostToolUse / Stop / SessionEnd: no-op success
		_, _ = w.Write([]byte("{}\n"))
	}
}

func runInstall(cmd string, args []string) {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	project := fs.String("project", "", "project root containing .codex/")
	bridge := fs.String("command", "", "hook command (default: absolute path to this binary + pretool)")
	_ = fs.Parse(args)
	if *project == "" {
		fmt.Fprintln(os.Stderr, "-project required")
		os.Exit(2)
	}
	root, err := filepath.Abs(*project)
	if err != nil {
		fail(err)
	}
	cmdStr := *bridge
	if cmdStr == "" {
		self, err := os.Executable()
		if err != nil {
			fail(err)
		}
		cmdStr = self + " pretool"
	}
	m := &adapter.CodexHooksManager{
		HooksPath:     filepath.Join(root, ".codex", "hooks.json"),
		BridgeCommand: cmdStr,
		ProjectRoot:   root,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	switch cmd {
	case "plan":
		p, err := m.PlanInstall()
		if err != nil {
			fail(err)
		}
		_ = enc.Encode(p)
	case "install":
		if err := m.Install(); err != nil {
			fail(err)
		}
		fmt.Println(`{"ok":true,"action":"install","note":"re-trust via Codex /hooks after install"}`)
	case "uninstall":
		if err := m.Uninstall(); err != nil {
			fail(err)
		}
		fmt.Println(`{"ok":true,"action":"uninstall"}`)
	case "doctor":
		d, err := m.Doctor()
		if err != nil {
			fail(err)
		}
		_ = enc.Encode(d)
		if !d.OK {
			os.Exit(1)
		}
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  codexhooks pretool
  codexhooks plan|install|uninstall|doctor -project ROOT [-command CMD]
  codexhooks manifest

Profile: %s
Hook-control foundation only; live Codex proof is separate (#164).
`, adapter.CodexHooksProfileV1)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}
