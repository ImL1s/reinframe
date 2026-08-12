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
	"bytes"
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
	ctxOut := fs.String("scan-context-out", "", "optional external live_scan_context JSON path (outside evidence-out)")
	ctxIn := fs.String("scan-context-in", "", "optional external live_scan_context JSON for report (outside evidence-out)")
	_ = fs.Parse(args)
	if !*live {
		fail(fmt.Errorf("groklive all: --live required"))
	}
	if strings.TrimSpace(*out) == "" {
		fail(fmt.Errorf("groklive all: --evidence-out required"))
	}
	// Sequential phases; preflight must write into evidence-out for report provenance.
	// live_identity.json captures the live executor binary (distinct from a later report re-run).
	// Write failure is fatal — report must not fall back to the generator identity.
	evDir := mustAbs(*out, "--evidence-out")
	if s := strings.TrimSpace(*ctxOut); s != "" {
		abs, err := filepath.Abs(s)
		if err != nil {
			fail(fmt.Errorf("groklive all: --scan-context-out: %w", err))
		}
		scanContextOutPath = abs
	}
	// --scan-context-in must be bound before ensureLiveIdentity so resume of a
	// copied campaign (no private cache) can import the transferable sidecar
	// during identity validation (Pro R31/R32 P2).
	if s := strings.TrimSpace(*ctxIn); s != "" {
		abs, err := filepath.Abs(s)
		if err != nil {
			fail(fmt.Errorf("groklive all: --scan-context-in: %w", err))
		}
		scanContextInPath = abs
	} else if s := strings.TrimSpace(*ctxOut); s != "" {
		// Same-host resume after private-cache loss can re-import the export path.
		abs, err := filepath.Abs(s)
		if err != nil {
			fail(fmt.Errorf("groklive all: --scan-context-out as import: %w", err))
		}
		scanContextInPath = abs
	}
	if err := ensureLiveIdentity(evDir); err != nil {
		fail(fmt.Errorf("groklive all: live_identity: %w", err))
	}
	runPreflight([]string{"--grok-executable", *exe, "--evidence-out", *out})
	runHooks([]string{"--live", "--grok-executable", *exe, "--project", *project, "--evidence-out", *out, "--grokhooks", *hooksBin})
	runACP([]string{"--live", "--grok-executable", *exe, "--project", *project, "--evidence-out", *out})
	reportArgs := []string{"--evidence-out", *out}
	if s := strings.TrimSpace(*ctxIn); s != "" {
		reportArgs = append(reportArgs, "--scan-context-in", s)
	} else if s := strings.TrimSpace(*ctxOut); s != "" {
		// Same campaign re-report on another host can use the export path.
		reportArgs = append(reportArgs, "--scan-context-in", s)
	}
	runReport(reportArgs)
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
	// Normalize typed maps/structs/slices into map[string]any / []any via JSON
	// (Pro R29 P1): map[string]ScenarioResult and []string otherwise bypass the walker.
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var normalized any
	if err := dec.Decode(&normalized); err != nil {
		return err
	}
	// Field-aware identity redaction: structured enum/platform keys keep values;
	// free-text strings always get path+hostname redact (incl. unsafe hostnames).
	normalized = redactIdentityInValue(normalized)
	b, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	// Path/account redaction only on serialized form (no whole-document hostname pass).
	s := redactPathsAndAccounts(string(b))
	if err := validatePostRedactPlatformFields(s); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), 0o600)
}

// closedStructuredJSONKey reports keys whose string values must not be hostname-redacted.
func closedStructuredJSONKey(k string) bool {
	switch k {
	case "goos", "goarch", "live_goos", "live_goarch",
		"report_generator_goos", "report_generator_goarch",
		"final_disposition", "disposition", "strongest_proven", "status", "class",
		"src", "live_binary_commit_src", "report_generator_commit_src", "schema",
		"scan_context_id", "live_binary_commit", "report_generator_commit",
		"grok_executable_sha256", "grokhooks_executable_sha256",
		"grok_executable_path_sha256", "grokhooks_executable_path_sha256",
		"grok_executable_basename", "grokhooks_executable_basename":
		return true
	default:
		return false
	}
}

// redactIdentityInValue walks maps/slices and redacts free-text strings only.
func redactIdentityInValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if closedStructuredJSONKey(k) {
				out[k] = val
				continue
			}
			out[k] = redactIdentityInValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redactIdentityInValue(val)
		}
		return out
	case string:
		return redactLocalIdentityAlways(x)
	default:
		return v
	}
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
	SessionCorrelated bool `json:"session_correlated"`
	// InterventionID bound into the scenario when relevant.
	InterventionID string `json:"intervention_id,omitempty"`
	// TargetSessionID is the SHA-256 hex of the host session id (never plaintext UUID).
	TargetSessionID string `json:"target_session_id,omitempty"`
	// DedupSuppressed: second same InterventionID was suppressed at business layer.
	DedupSuppressed bool `json:"dedup_suppressed,omitempty"`
}

func stamp() string { return time.Now().UTC().Format(time.RFC3339) }

func ctxTimeout(sec int) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Duration(sec)*time.Second)
}
