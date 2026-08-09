// Command groklive is the opt-in live Grok Build host proof harness (#167).
//
//	groklive preflight [--grok-executable PATH]
//	groklive hooks --live --grok-executable PATH --project DIR --evidence-out DIR
//	groklive acp --live --grok-executable PATH --project DIR --evidence-out DIR
//	groklive report --evidence-out DIR
//
// Without --live, hooks/acp never launch Grok. Never reads ~/.grok/auth.json.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "preflight":
		runPreflight(os.Args[2:])
	case "hooks":
		runHooks(os.Args[2:])
	case "acp":
		runACP(os.Args[2:])
	case "report":
		runReport(os.Args[2:])
	case "all":
		runAll(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  groklive preflight [--grok-executable PATH]
  groklive hooks --live --grok-executable PATH --project DIR --evidence-out DIR [--grokhooks PATH]
  groklive acp --live --grok-executable PATH --project DIR --evidence-out DIR
  groklive report --evidence-out DIR
  groklive all --live --grok-executable PATH --project DIR --evidence-out DIR [--grokhooks PATH]

Without --live, hooks/acp never launch Grok. Does not read ~/.grok/auth.json.`)
}

func runAll(args []string) {
	fs := flag.NewFlagSet("all", flag.ExitOnError)
	live := fs.Bool("live", false, "opt-in: launch real Grok")
	exe := fs.String("grok-executable", "", "absolute path to grok")
	project := fs.String("project", "", "disposable project root")
	out := fs.String("evidence-out", "", "evidence directory")
	hooksBin := fs.String("grokhooks", "", "path to grokhooks binary")
	_ = fs.Parse(args)
	if !*live {
		fail(fmt.Errorf("groklive all: --live required"))
	}
	if strings.TrimSpace(*out) == "" {
		fail(fmt.Errorf("groklive all: --evidence-out required"))
	}
	// Sequential phases; preflight must write into evidence-out for report provenance.
	runPreflight([]string{"--grok-executable", *exe, "--evidence-out", *out})
	runHooks([]string{"--live", "--grok-executable", *exe, "--project", *project, "--evidence-out", *out, "--grokhooks", *hooksBin})
	runACP([]string{"--live", "--grok-executable", *exe, "--project", *project, "--evidence-out", *out})
	runReport([]string{"--evidence-out", *out})
}

func mustAbs(p, name string) string {
	if strings.TrimSpace(p) == "" {
		fail(fmt.Errorf("groklive: %s required", name))
	}
	a, err := filepath.Abs(p)
	if err != nil {
		fail(err)
	}
	return a
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err.Error())
	os.Exit(1)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func loadScenarioMap(dir string) map[string]ScenarioResult {
	path := filepath.Join(dir, "scenarios.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]ScenarioResult{}
	}
	var m map[string]ScenarioResult
	if json.Unmarshal(b, &m) != nil {
		return map[string]ScenarioResult{}
	}
	if m == nil {
		m = map[string]ScenarioResult{}
	}
	return m
}

func saveScenarioMap(dir string, m map[string]ScenarioResult) error {
	return writeJSON(filepath.Join(dir, "scenarios.json"), m)
}

// ScenarioResult is one scenario outcome for #167 evidence (v1 and v2).
type ScenarioResult struct {
	ID          string `json:"id"`
	Status      string `json:"status"` // PASS|FAIL|NOT_RUN|INCONCLUSIVE
	Detail      string `json:"detail,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	ACKLayer    string `json:"ack_layer,omitempty"`
	HostOutcome string `json:"host_outcome,omitempty"`
	At          string `json:"at,omitempty"`
	// v2 correlation / proof fields (#199)
	// DenyDirectProof: true only when deny JSON or exit-2 for the exact tool attempt was observed.
	DenyDirectProof bool `json:"deny_direct_proof,omitempty"`
	// FailOpenInvoked: true only when the broken hook process was positively invoked.
	FailOpenInvoked bool `json:"fail_open_invoked,omitempty"`
	// SessionCorrelated: true only when session/update matched target session + this prompt turn.
	SessionCorrelated bool `json:"session_correlated,omitempty"`
	// InterventionID bound into the scenario when relevant.
	InterventionID string `json:"intervention_id,omitempty"`
	// TargetSessionID for ACP scenarios.
	TargetSessionID string `json:"target_session_id,omitempty"`
	// DedupSuppressed: second same InterventionID was suppressed at business layer.
	DedupSuppressed bool `json:"dedup_suppressed,omitempty"`
}

func stamp() string { return time.Now().UTC().Format(time.RFC3339) }

func ctxTimeout(sec int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Duration(sec)*time.Second)
}
