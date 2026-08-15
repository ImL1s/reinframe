// Command claudebridge is the documented Claude Code PreToolUse / UserPrompt
// entrypoint for Reinframe (#96).
//
// Usage (PreToolUse hook — experimental):
//
//	claudebridge pretool < fixture.json
//	echo '{...}' | claudebridge pretool -deny-tool Bash
//
// Prints Claude-compatible decision JSON on stdout. Does not install itself
// into ~/.claude/settings.json (document-only; optional host smoke).
//
// Not production dual-host supervision; FileActuator / live Codex attach are separate.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/challenge"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "pretool":
		os.Exit(runPretool(os.Args[2:]))
	case "prompt":
		os.Exit(runPrompt(os.Args[2:]))
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `claudebridge — Reinframe Claude Code hook bridge (#96 experimental)

  claudebridge pretool [flags]   # stdin: PreToolUse JSON → decision JSON
  claudebridge prompt            # stdin: UserPromptSubmit JSON → TaskSubmitted JSON

pretool flags:
  -deny-tool name     deny listed tool (repeatable via comma list)
  -pending-iv id      set pending advisory latch (defer)
  -timeout duration   hook timeout (default 50ms)
`)
}

func runPretool(args []string) int {
	fs := flag.NewFlagSet("pretool", flag.ContinueOnError)
	denyTools := fs.String("deny-tool", "", "comma-separated tool names to deny")
	pendingIV := fs.String("pending-iv", "", "pending intervention id → defer")
	timeout := fs.Duration("timeout", adapter.DefaultHookTimeout, "hook evaluation timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		return 1
	}
	pol := adapter.HookPolicy{Timeout: *timeout}
	if *denyTools != "" {
		pol.DeniedTools = map[string]struct{}{}
		for _, n := range strings.Split(*denyTools, ",") {
			n = strings.TrimSpace(n)
			if n != "" {
				pol.DeniedTools[n] = struct{}{}
			}
		}
	}
	if *pendingIV != "" {
		pol.PendingAdvisoryInterventionID = *pendingIV
	}
	chSvc := challenge.NewService(challenge.ServiceConfig{})
	chBridge := challenge.NewClaudeChallengeBridge(chSvc, challenge.ClaudeChallengeBridgeOptions{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, dec, err := adapter.EvaluateClaudePreToolJSON(ctx, raw, adapter.ClaudeBridgeConfig{
		Policy:            pol,
		EvaluateChallenge: chBridge.AsHookEvaluator(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "evaluate: %v\n", err)
		return 1
	}
	_ = dec
	// Closed-schema encode (#116): rejects continue:false and unknown enums.
	b, err := adapter.MarshalClaudeHookResponseJSON(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		return 1
	}
	if _, err := os.Stdout.Write(append(b, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "write: %v\n", err)
		return 1
	}
	return 0
}

func runPrompt(args []string) int {
	_ = flag.NewFlagSet("prompt", flag.ContinueOnError).Parse(args)
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		return 1
	}
	out, err := adapter.MapClaudeUserPromptBridge(raw, adapter.TaskIntakeOptions{BuildContract: true})
	if err != nil {
		fmt.Fprintf(os.Stderr, "map: %v\n", err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return 1
	}
	return 0
}
