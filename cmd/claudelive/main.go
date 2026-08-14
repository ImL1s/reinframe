// Command claudelive is the live qualification harness and runner for Claude Code hooks (#120).
//
//	claudelive preflight [--evidence-out DIR]
//	claudelive scenarios [--evidence-out DIR] [--project-dir DIR]
//	claudelive report [--evidence-out DIR]
//	claudelive all [--evidence-out DIR] [--project-dir DIR]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
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

	cmd := os.Args[1]
	switch cmd {
	case "preflight":
		runPreflight(os.Args[2:])
	case "scenarios":
		runScenarios(os.Args[2:])
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
	fmt.Fprintf(os.Stderr, `claudelive — Reinframe Claude Live Control Qualification Runner (#120)

Usage:
  claudelive preflight [--evidence-out DIR]
  claudelive scenarios [--evidence-out DIR] [--project-dir DIR]
  claudelive report [--evidence-out DIR]
  claudelive all [--evidence-out DIR] [--project-dir DIR]
`)
}

func runPreflight(args []string) {
	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence output directory")
	_ = fs.Parse(args)

	pf := adapter.RunClaudePreflight("")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(pf)

	if *out != "" {
		_ = os.MkdirAll(*out, 0o755)
		data, _ := json.MarshalIndent(adapter.RedactClaudeEvidence(pf), "", "  ")
		_ = os.WriteFile(filepath.Join(*out, "preflight.json"), data, 0o644)
	}
}

func runScenarios(args []string) {
	fs := flag.NewFlagSet("scenarios", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence output directory")
	proj := fs.String("project-dir", "", "disposable sandbox project directory")
	_ = fs.Parse(args)

	opts := adapter.ClaudeLiveHarnessOptions{
		SandboxDir: *proj,
	}
	report, invocations, err := adapter.RunClaudeLiveHarness(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running scenarios: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report.Scenarios)

	if *out != "" {
		writeEvidenceFiles(*out, report, invocations)
	}
}

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence directory containing run artifacts")
	_ = fs.Parse(args)

	if *out == "" {
		fmt.Fprintln(os.Stderr, "--evidence-out required for report")
		os.Exit(2)
	}

	reportPath := filepath.Join(*out, "issue-120-live-claude-control.json")
	data, err := os.ReadFile(reportPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read report file: %v\n", err)
		os.Exit(1)
	}
	var rep map[string]any
	if err := json.Unmarshal(data, &rep); err != nil {
		fmt.Fprintf(os.Stderr, "parse report file: %v\n", err)
		os.Exit(1)
	}
	if err := adapter.ValidateClaudeLiveControlReport(rep); err != nil {
		fmt.Fprintf(os.Stderr, "report schema validation error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Report valid against reinframe.claude_live_control.v1")
}

func runAll(args []string) {
	fs := flag.NewFlagSet("all", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence output directory")
	proj := fs.String("project-dir", "", "disposable sandbox directory")
	_ = fs.Parse(args)

	if *out == "" {
		fmt.Fprintln(os.Stderr, "--evidence-out required")
		os.Exit(2)
	}

	evDir := *out
	_ = os.MkdirAll(evDir, 0o755)

	// 1. Preflight
	pf := adapter.RunClaudePreflight("")
	pfRedacted := adapter.RedactClaudeEvidence(pf)
	pfData, _ := json.MarshalIndent(pfRedacted, "", "  ")
	_ = os.WriteFile(filepath.Join(evDir, "preflight.json"), append(pfData, '\n'), 0o644)

	// 2. Run live scenarios
	opts := adapter.ClaudeLiveHarnessOptions{
		SandboxDir: *proj,
	}
	report, invocations, err := adapter.RunClaudeLiveHarness(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scenarios execution failed: %v\n", err)
		os.Exit(1)
	}

	writeEvidenceFiles(evDir, report, invocations)
	fmt.Printf("Claude live qualification complete. Final disposition: %s\n", report.FinalDisposition)
}

func writeEvidenceFiles(evDir string, report *adapter.ClaudeLiveControlReport, invocations []adapter.ClaudeHookInvocationRecord) {
	_ = os.MkdirAll(evDir, 0o755)

	// 1. Scenarios.json
	scenariosRedacted := adapter.RedactClaudeEvidence(report.Scenarios)
	scData, _ := json.MarshalIndent(scenariosRedacted, "", "  ")
	_ = os.WriteFile(filepath.Join(evDir, "scenarios.json"), append(scData, '\n'), 0o644)

	// 2. Hook invocations jsonl
	var lines []string
	for _, inv := range invocations {
		red := adapter.RedactClaudeEvidence(inv)
		raw, _ := json.Marshal(red)
		lines = append(lines, string(raw))
	}
	invContent := strings.Join(lines, "\n") + "\n"
	_ = os.WriteFile(filepath.Join(evDir, "hook_invocations.jsonl"), []byte(invContent), 0o644)

	// 3. Schema JSON
	schemaJSON := adapter.GetClaudeLiveControlSchemaJSON()
	_ = os.WriteFile(filepath.Join(evDir, "reinframe.claude_live_control.v1.schema.json"), []byte(schemaJSON), 0o644)

	// 4. Formal Report
	repRedacted := adapter.RedactClaudeEvidence(report)
	repData, _ := json.MarshalIndent(repRedacted, "", "  ")
	reportFile := filepath.Join(evDir, "issue-120-live-claude-control.json")
	_ = os.WriteFile(reportFile, append(repData, '\n'), 0o644)

	// 5. RUN.md
	runMD := fmt.Sprintf(`# Claude Live Control Qualification Run (#120)

- **Schema**: `+"`%s`"+`
- **Generated At**: %s
- **Harness**: %s
- **Platform**: %s/%s
- **Final Disposition**: **%s**
- **Passed Scenarios**: %d / %d

## Mandatory Scenarios Summary

1. **%s** (ALLOW verifiable side effect): **%s** — %s
2. **%s** (BLOCK side effect absent without terminate): **%s** — %s
3. **%s** (Session loop continuity after deny): **%s** — %s
4. **%s** (Bounded reason/context transport): **%s** — %s

## Honesty Boundaries & Non-Claims
- Synthetic disposable sandbox execution; zero contamination of operator ~/.claude configuration.
- Closed response shapes; ordinary tool deny omits `+"`continue: false`"+`.
- Context injection strictly bounded within MaxHookReasonRunes.
- All local user identifiers and home directory paths redacted.
`,
		report.SchemaVersion,
		report.Provenance.GeneratedAt,
		report.Provenance.Harness,
		report.Provenance.GOOS,
		report.Provenance.GOARCH,
		report.FinalDisposition,
		report.Summary.Passed,
		report.Summary.Total,
		adapter.ClaudeScenarioAllow, report.Scenarios[adapter.ClaudeScenarioAllow].Status, report.Scenarios[adapter.ClaudeScenarioAllow].Detail,
		adapter.ClaudeScenarioBlock, report.Scenarios[adapter.ClaudeScenarioBlock].Status, report.Scenarios[adapter.ClaudeScenarioBlock].Detail,
		adapter.ClaudeScenarioLoopCont, report.Scenarios[adapter.ClaudeScenarioLoopCont].Status, report.Scenarios[adapter.ClaudeScenarioLoopCont].Detail,
		adapter.ClaudeScenarioBoundedCtx, report.Scenarios[adapter.ClaudeScenarioBoundedCtx].Status, report.Scenarios[adapter.ClaudeScenarioBoundedCtx].Detail,
	)
	_ = os.WriteFile(filepath.Join(evDir, "RUN.md"), []byte(runMD), 0o644)
}
