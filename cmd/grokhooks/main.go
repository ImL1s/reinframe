// Command grokhooks is the Grok Build native hooks bridge for Reinframe (#165).
//
//	grokhooks pretool
//	grokhooks plan|install|uninstall|doctor -project ROOT [-command CMD]
//	grokhooks manifest
//
// Host timeout/crash/malformed remain fail-open. Only explicit valid deny blocks.
// Never writes ~/.grok/auth.json or session history.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "pretool", "hook":
		runPretool(os.Stdin, os.Stdout)
	case "plan", "install", "uninstall", "doctor":
		runInstall(os.Args[1], os.Args[2:])
	case "manifest":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(adapter.NewGrokHooksFoundationManifest())
	default:
		usage()
		os.Exit(2)
	}
}

func runPretool(r io.Reader, w io.Writer) {
	raw, err := io.ReadAll(io.LimitReader(r, adapter.MaxGrokHookStdinBytes+1))
	if err != nil {
		fail(err)
	}
	in, err := adapter.ParseGrokHookStdin(raw)
	if err != nil {
		// PreToolUse-shaped peeks: explicit deny JSON (fail-closed adapter record).
		// Other events: empty object (host fail-open / no-op).
		var peek map[string]any
		n := ""
		if json.Unmarshal(raw, &peek) == nil {
			if s, ok := peek["hookEventName"].(string); ok {
				n = s
			} else if s, ok := peek["hook_event_name"].(string); ok {
				n = s
			}
		}
		if n != "" && (containsFold(n, "pretool") || containsFold(n, "pre_tool")) {
			out, _ := adapter.EncodeGrokHookResponse(adapter.GrokPreToolResponse{Decision: "deny", Reason: "parse_fail_closed"})
			_, _ = w.Write(out)
			return
		}
		_, _ = w.Write([]byte("{}\n"))
		return
	}
	if in.HookEventName != adapter.GrokEventPreToolUse {
		_, _ = w.Write([]byte("{}\n"))
		return
	}
	pa, _ := adapter.ProposedActionFromGrokHook(in, adapter.ProposedActionOptions{})
	req := adapter.HookRequestFromGrokHook(in, &pa)
	pol := adapter.HookPolicy{FailOpen: true}
	if deny := os.Getenv("REINFRAME_GROK_DENY_TOOLS"); deny != "" {
		// Comma-separated exact tool names (live hosts may advertise aliases).
		pol.DeniedTools = map[string]struct{}{}
		for _, n := range strings.Split(deny, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				pol.DeniedTools[n] = struct{}{}
			}
		}
		if len(pol.DeniedTools) > 0 {
			pol.FailOpen = false
		}
	}
	dec := adapter.EvaluateHook(context.Background(), req, pol)
	resp := adapter.GrokPreToolResponseFromDecision(dec, os.Getenv("REINFRAME_CHALLENGE_ID"))
	out, err := adapter.EncodeGrokHookResponse(resp)
	if err != nil {
		fail(err)
	}
	_, _ = w.Write(out)
}

func runInstall(cmd string, args []string) {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	project := fs.String("project", "", "project root")
	bridge := fs.String("command", "", "hook command")
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
	m := &adapter.GrokHooksManager{
		HooksFile:     filepath.Join(root, ".grok", "hooks", "reinframe-pretool.json"),
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
		fmt.Println(`{"ok":true,"action":"install","note":"trust project via /hooks-trust or --trust; host fail-open on hook crash"}`)
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
  grokhooks pretool
  grokhooks plan|install|uninstall|doctor -project ROOT [-command CMD]
  grokhooks manifest

Profile: %s
Native hook foundation; failures remain host fail-open. Live proof is #167.
`, adapter.GrokHooksProfileV1)
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (indexFold(s, sub) >= 0))
}

func indexFold(s, sub string) int {
	// simple case-insensitive substring
	ls, lsub := make([]byte, len(s)), make([]byte, len(sub))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		ls[i] = c
	}
	for i := 0; i < len(sub); i++ {
		c := sub[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		lsub[i] = c
	}
	return indexBytes(ls, lsub)
}

func indexBytes(s, sub []byte) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		ok := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}
