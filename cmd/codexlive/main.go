// Command codexlive is the live qualification harness and runner for Codex project-local hooks (#164).
//
//	codexlive preflight [--evidence-out DIR]
//	codexlive scenarios [--evidence-out DIR] [--project-dir DIR]
//	codexlive report [--evidence-out DIR]
//	codexlive all [--evidence-out DIR] [--project-dir DIR]
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
	fmt.Fprintf(os.Stderr, `codexlive — Reinframe Codex Live Control Qualification Runner (#164)

Usage:
  codexlive preflight [--evidence-out DIR]
  codexlive scenarios [--evidence-out DIR] [--project-dir DIR]
  codexlive report [--evidence-out DIR]
  codexlive all [--evidence-out DIR] [--project-dir DIR]
`)
}

func runPreflight(args []string) {
	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence output directory")
	_ = fs.Parse(args)

	pf := adapter.RunCodexPreflight("")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(pf)

	if *out != "" {
		_ = os.MkdirAll(*out, 0o755)
		data, _ := json.MarshalIndent(adapter.RedactCodexEvidence(pf), "", "  ")
		_ = os.WriteFile(filepath.Join(*out, "preflight.json"), data, 0o644)
	}
}

func runScenarios(args []string) {
	fs := flag.NewFlagSet("scenarios", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence output directory")
	proj := fs.String("project-dir", "", "disposable sandbox project directory")
	_ = fs.Parse(args)

	opts := adapter.CodexLiveHarnessOptions{
		SandboxDir: *proj,
	}
	report, invocations, err := adapter.RunCodexLiveHarness(context.Background(), opts)
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

	reportPath := filepath.Join(*out, "issue-164-live-codex-control.json")
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
	if err := adapter.ValidateCodexLiveControlReport(rep); err != nil {
		fmt.Fprintf(os.Stderr, "report schema validation error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Report valid against reinframe.codex_live_control.v1")
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
	pf := adapter.RunCodexPreflight("")
	pfRedacted := adapter.RedactCodexEvidence(pf)
	pfData, _ := json.MarshalIndent(pfRedacted, "", "  ")
	_ = os.WriteFile(filepath.Join(evDir, "preflight.json"), append(pfData, '\n'), 0o644)

	// 2. Run live scenarios
	opts := adapter.CodexLiveHarnessOptions{
		SandboxDir: *proj,
	}
	report, invocations, err := adapter.RunCodexLiveHarness(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scenarios execution failed: %v\n", err)
		os.Exit(1)
	}

	writeEvidenceFiles(evDir, report, invocations)
	fmt.Printf("Codex live qualification complete. Final disposition: %s\n", report.FinalDisposition)
}

func writeEvidenceFiles(evDir string, report *adapter.CodexLiveControlReport, invocations []adapter.CodexHookInvocationRecord) {
	_ = os.MkdirAll(evDir, 0o755)

	// 1. Scenarios.json
	scenariosRedacted := adapter.RedactCodexEvidence(report.Scenarios)
	scData, _ := json.MarshalIndent(scenariosRedacted, "", "  ")
	_ = os.WriteFile(filepath.Join(evDir, "scenarios.json"), append(scData, '\n'), 0o644)

	// 2. Hook invocations jsonl
	var lines []string
	for _, inv := range invocations {
		red := adapter.RedactCodexEvidence(inv)
		raw, _ := json.Marshal(red)
		lines = append(lines, string(raw))
	}
	invContent := strings.Join(lines, "\n") + "\n"
	_ = os.WriteFile(filepath.Join(evDir, "hook_invocations.jsonl"), []byte(invContent), 0o644)

	// 3. Schema JSON
	schemaJSON := adapter.GetCodexLiveControlSchemaJSON()
	_ = os.WriteFile(filepath.Join(evDir, "reinframe.codex_live_control.v1.schema.json"), []byte(schemaJSON), 0o644)

	// 4. Formal Report
	repRedacted := adapter.RedactCodexEvidence(report)
	repData, _ := json.MarshalIndent(repRedacted, "", "  ")
	reportFile := filepath.Join(evDir, "issue-164-live-codex-control.json")
	_ = os.WriteFile(reportFile, append(repData, '\n'), 0o644)

	// 5. RUN.md
	runMD := fmt.Sprintf(`# Codex Live Control Qualification Run (#164)

- **Schema**: `+"`%s`"+`
- **Generated At**: %s
- **Harness**: %s
- **Platform**: %s/%s
- **Final Disposition**: **%s**
- **Passed Scenarios**: %d / %d

## Mandatory Scenarios Summary

1. **%s** (ALLOW side effect): **%s** — %s
2. **%s** (BLOCK side effect absent): **%s** — %s
3. **%s** (Session loop continuation without terminate): **%s** — %s
4. **%s** (Bounded feedback context & retry instructions): **%s** — %s
5. **%s** (Approval request allow/deny/fall-through): **%s** — %s

## Honesty Boundaries & Non-Claims
- Synthetic disposable sandbox execution; zero contamination of operator ~/.codex configuration.
- Closed response shapes; ordinary tool deny omits `+"`continue: false`"+`.
- Context injection strictly bounded within MaxCodexContextRunes.
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
		adapter.CodexScenarioAllow, report.Scenarios[adapter.CodexScenarioAllow].Status, report.Scenarios[adapter.CodexScenarioAllow].Detail,
		adapter.CodexScenarioBlock, report.Scenarios[adapter.CodexScenarioBlock].Status, report.Scenarios[adapter.CodexScenarioBlock].Detail,
		adapter.CodexScenarioLoopCont, report.Scenarios[adapter.CodexScenarioLoopCont].Status, report.Scenarios[adapter.CodexScenarioLoopCont].Detail,
		adapter.CodexScenarioBoundedCtx, report.Scenarios[adapter.CodexScenarioBoundedCtx].Status, report.Scenarios[adapter.CodexScenarioBoundedCtx].Detail,
		adapter.CodexScenarioPermApprove, report.Scenarios[adapter.CodexScenarioPermApprove].Status, report.Scenarios[adapter.CodexScenarioPermApprove].Detail,
	)
	_ = os.WriteFile(filepath.Join(evDir, "RUN.md"), []byte(runMD), 0o644)
}
