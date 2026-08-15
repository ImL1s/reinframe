// Command claudebridge is the documented Claude Code PreToolUse / UserPrompt / Appeal
// entrypoint for Reinframe (#96, #139).
//
// Usage (PreToolUse hook — experimental):
//
//	claudebridge pretool < fixture.json
//	echo '{...}' | claudebridge pretool -deny-tool Bash
//	claudebridge appeal -challenge-id ch-123 -nonce <nonce> -value "Fix compilation error"
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
	"path/filepath"
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
	case "appeal":
		os.Exit(runAppeal(os.Args[2:]))
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

func defaultStorePath() string {
	if p := os.Getenv("REINFRAME_CHALLENGE_STORE"); p != "" {
		return p
	}
	return filepath.Join(".reinframe", "challenge_store.json")
}

func usage() {
	fmt.Fprintf(os.Stderr, `claudebridge — Reinframe Claude Code hook bridge (#96, #139)

  claudebridge pretool [flags]   # stdin: PreToolUse JSON → decision JSON
  claudebridge appeal [flags]    # submit appeal justification for a challenge
  claudebridge prompt            # stdin: UserPromptSubmit JSON → TaskSubmitted JSON

pretool flags:
  -deny-tool name     deny listed tool (repeatable via comma list)
  -pending-iv id      set pending advisory latch (defer)
  -timeout duration   hook timeout (default 50ms)
  -store-path path    persistent challenge store JSON path

appeal flags:
  -challenge-id id    challenge identifier (required)
  -nonce string       16-byte hex cryptographic nonce from BLOCK challenge (optional if in store)
  -value string       concrete value / reason for appeal justification (required)
  -prevented string   prevented threat or failure justification
  -cost string        estimated cost justification
  -store-path path    persistent challenge store JSON path
`)
}

func runPretool(args []string) int {
	fs := flag.NewFlagSet("pretool", flag.ContinueOnError)
	denyTools := fs.String("deny-tool", "", "comma-separated tool names to deny")
	pendingIV := fs.String("pending-iv", "", "pending intervention id → defer")
	timeout := fs.Duration("timeout", adapter.DefaultHookTimeout, "hook evaluation timeout")
	storePath := fs.String("store-path", defaultStorePath(), "path to persistent challenge store")
	blockClass := fs.String("block-class", challenge.BlockClassProductivityGeneric, "block class for challenges (e.g. PRODUCTIVITY_GENERIC, SCOPE_DRIFT, SECURITY)")
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

	st := challenge.NewStore()
	if *storePath != "" {
		_ = st.LoadFromFile(*storePath)
	}

	chSvc := challenge.NewService(challenge.ServiceConfig{
		Store: st,
	})
	chBridge := challenge.NewClaudeChallengeBridge(chSvc, challenge.ClaudeChallengeBridgeOptions{
		DefaultBlockClass: *blockClass,
	})

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

	if *storePath != "" {
		_ = st.SaveToFile(*storePath)
	}

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

func runAppeal(args []string) int {
	fs := flag.NewFlagSet("appeal", flag.ContinueOnError)
	chID := fs.String("challenge-id", "", "challenge identifier")
	nonce := fs.String("nonce", "", "challenge nonce")
	val := fs.String("value", "", "concrete value justification")
	prevented := fs.String("prevented", "prevents operation failure or outage", "prevented failure justification")
	cost := fs.String("cost", "0 USD negligible compute", "estimated cost justification")
	scope := fs.String("scope", "current workspace task", "scope limit justification")
	verification := fs.String("verification", "run automated test suite", "verification plan justification")
	rollback := fs.String("rollback", "git revert or workspace rollback", "rollback plan justification")
	storePath := fs.String("store-path", defaultStorePath(), "path to persistent challenge store")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *chID == "" {
		fmt.Fprintf(os.Stderr, "appeal error: -challenge-id required\n")
		return 2
	}
	if *val == "" {
		fmt.Fprintf(os.Stderr, "appeal error: -value required\n")
		return 2
	}

	st := challenge.NewStore()
	if *storePath != "" {
		if err := st.LoadFromFile(*storePath); err != nil {
			fmt.Fprintf(os.Stderr, "load challenge store: %v\n", err)
			return 1
		}
	}

	chSvc := challenge.NewService(challenge.ServiceConfig{Store: st})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rec, ok := st.Get(*chID)
	if !ok {
		fmt.Fprintf(os.Stderr, "appeal error: unknown challenge %q\n", *chID)
		return 1
	}
	if *nonce != "" && rec.ChallengeNonce != "" && *nonce != rec.ChallengeNonce {
		fmt.Fprintf(os.Stderr, "appeal error: challenge nonce mismatch\n")
		return 1
	}

	just := challenge.Justification{
		SchemaVersion:            challenge.SchemaJustification,
		ChallengeID:              *chID,
		ConcreteValue:            *val,
		PreventedFailureOrThreat: *prevented,
		EstimatedCost:            *cost,
		ScopeLimit:               *scope,
		VerificationPlan:         *verification,
		RollbackPlan:             *rollback,
	}

	updatedRec, err := chSvc.Justify(ctx, just, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "appeal failed: %v\n", err)
		return 1
	}
	rec = updatedRec

	if *storePath != "" {
		if err := st.SaveToFile(*storePath); err != nil {
			fmt.Fprintf(os.Stderr, "save challenge store: %v\n", err)
			return 1
		}
	}

	out := map[string]any{
		"status":         "justified",
		"challenge_id":   rec.ChallengeID,
		"session_id":     rec.SessionID,
		"state":          rec.State,
		"retry_budget":   rec.RetryBudget,
		"challenge_nonce": rec.ChallengeNonce,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
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
