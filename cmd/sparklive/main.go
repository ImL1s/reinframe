// Command sparklive is the live qualification harness and runner for GPT-5.3-Codex-Spark (#187).
//
//	sparklive preflight [--evidence-out DIR]
//	sparklive scenarios [--evidence-out DIR] [--project-dir DIR]
//	sparklive report [--evidence-out DIR]
//	sparklive all [--evidence-out DIR] [--project-dir DIR]
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
	fmt.Fprintf(os.Stderr, `sparklive — Reinframe GPT-5.3-Codex-Spark Live Qualification Runner (#187)

Usage:
  sparklive preflight [--evidence-out DIR]
  sparklive scenarios [--evidence-out DIR] [--project-dir DIR]
  sparklive report [--evidence-out DIR]
  sparklive all [--evidence-out DIR] [--project-dir DIR]
`)
}

func runPreflight(args []string) {
	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence output directory")
	_ = fs.Parse(args)

	pf := adapter.RunCodexSparkPreflight("")
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(pf)

	if *out != "" {
		_ = os.MkdirAll(*out, 0o755)
		data, _ := json.MarshalIndent(adapter.RedactSparkEvidence(pf), "", "  ")
		_ = os.WriteFile(filepath.Join(*out, "preflight.json"), append(data, '\n'), 0o644)
	}
}

func runScenarios(args []string) {
	fs := flag.NewFlagSet("scenarios", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence output directory")
	proj := fs.String("project-dir", "", "disposable sandbox project directory")
	_ = fs.Parse(args)

	opts := adapter.CodexSparkQualificationOptions{
		SandboxDir: *proj,
	}
	report, invocations, err := adapter.RunCodexSparkQualification(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running spark scenarios: %v\n", err)
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

	reportPath := filepath.Join(*out, "issue-187-live-spark-qualification.json")
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
	if err := adapter.ValidateCodexSparkLiveControlReport(rep); err != nil {
		fmt.Fprintf(os.Stderr, "report schema validation error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Report valid against reinframe.codex_spark_live_control.v1")
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
	pf := adapter.RunCodexSparkPreflight("")
	pfRedacted := adapter.RedactSparkEvidence(pf)
	pfData, _ := json.MarshalIndent(pfRedacted, "", "  ")
	_ = os.WriteFile(filepath.Join(evDir, "preflight.json"), append(pfData, '\n'), 0o644)

	// 2. Run live scenarios
	opts := adapter.CodexSparkQualificationOptions{
		SandboxDir: *proj,
	}
	report, invocations, err := adapter.RunCodexSparkQualification(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "spark qualification execution failed: %v\n", err)
		os.Exit(1)
	}

	writeEvidenceFiles(evDir, report, invocations)
	fmt.Printf("GPT-5.3-Codex-Spark live qualification complete. Final disposition: %s\n", report.FinalDisposition)
}

func writeEvidenceFiles(evDir string, report *adapter.CodexSparkLiveReport, invocations []adapter.CodexSparkHookInvocationRecord) {
	_ = os.MkdirAll(evDir, 0o755)

	// 1. Scenarios.json
	scenariosRedacted := adapter.RedactSparkEvidence(report.Scenarios)
	scData, _ := json.MarshalIndent(scenariosRedacted, "", "  ")
	_ = os.WriteFile(filepath.Join(evDir, "scenarios.json"), append(scData, '\n'), 0o644)

	// 2. Hook invocations jsonl
	var lines []string
	for _, inv := range invocations {
		red := adapter.RedactSparkEvidence(inv)
		raw, _ := json.Marshal(red)
		lines = append(lines, string(raw))
	}
	invContent := strings.Join(lines, "\n") + "\n"
	_ = os.WriteFile(filepath.Join(evDir, "hook_invocations.jsonl"), []byte(invContent), 0o644)

	// 3. Schema JSON
	schemaJSON := adapter.GetCodexSparkLiveControlSchemaJSON()
	_ = os.WriteFile(filepath.Join(evDir, "reinframe.codex_spark_live_control.v1.schema.json"), []byte(schemaJSON), 0o644)

	// 4. Formal Report
	repRedacted := adapter.RedactSparkEvidence(report)
	repData, _ := json.MarshalIndent(repRedacted, "", "  ")
	reportFile := filepath.Join(evDir, "issue-187-live-spark-qualification.json")
	_ = os.WriteFile(reportFile, append(repData, '\n'), 0o644)

	// 5. RUN.md
	runMD := fmt.Sprintf(`# GPT-5.3-Codex-Spark Live Qualification Run (#187)

- **Schema**: `+"`%s`"+`
- **Generated At**: %s
- **Harness**: %s
- **Target Model**: `+"`%s`"+`
- **Account Tier**: %s
- **Platform**: %s/%s
- **Final Disposition**: **%s**
- **Passed Scenarios**: %d / %d

## Mandatory Scenarios Summary

1. **%s** (Exact Model Identity): **%s** — %s
2. **%s** (Provider Fallback Disabled & Rejected): **%s** — %s
3. **%s** (Turn Execution under Spark): **%s** — %s
4. **%s** (PreTool Hook Gating & Bounded Context): **%s** — %s
5. **%s** (Process Lifecycle & Cleanup): **%s** — %s

## Honesty Boundaries & Non-Claims
- Synthetic live qualification harness executing in disposable sandboxes; zero operator config contamination.
- Closed response shapes; zero token extraction from delegated ChatGPT Pro OAuth runtime.
- Exact model identity verification (`+"`gpt-5.3-codex-spark`"+`); zero silent substitution allowed.
- Context injection strictly bounded within MaxCodexContextRunes.
- All local user identifiers, paths, and machine hostnames redacted.
`,
		report.SchemaVersion,
		report.Provenance.GeneratedAt,
		report.Provenance.Harness,
		report.Provenance.Model,
		report.Provenance.AccountTier,
		report.Provenance.GOOS,
		report.Provenance.GOARCH,
		report.FinalDisposition,
		report.Summary.Passed,
		report.Summary.Total,
		adapter.SparkScenarioModelIdent, report.Scenarios[adapter.SparkScenarioModelIdent].Status, report.Scenarios[adapter.SparkScenarioModelIdent].Detail,
		adapter.SparkScenarioFallbackDisabled, report.Scenarios[adapter.SparkScenarioFallbackDisabled].Status, report.Scenarios[adapter.SparkScenarioFallbackDisabled].Detail,
		adapter.SparkScenarioTurnExec, report.Scenarios[adapter.SparkScenarioTurnExec].Status, report.Scenarios[adapter.SparkScenarioTurnExec].Detail,
		adapter.SparkScenarioToolHook, report.Scenarios[adapter.SparkScenarioToolHook].Status, report.Scenarios[adapter.SparkScenarioToolHook].Detail,
		adapter.SparkScenarioCleanup, report.Scenarios[adapter.SparkScenarioCleanup].Status, report.Scenarios[adapter.SparkScenarioCleanup].Detail,
	)
	_ = os.WriteFile(filepath.Join(evDir, "RUN.md"), []byte(runMD), 0o644)
}
