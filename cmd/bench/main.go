// Command bench runs offline M3 synthetic/FP benchmarks (#100).
//
//	go run ./cmd/bench -dataset testdata/evaluation -out reports/latest.json
//
// Never enables product hard-gates. Disposition is printed to stdout.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func main() {
	dataset := flag.String("dataset", "testdata/evaluation", "directory of case JSON files")
	out := flag.String("out", "", "write machine-readable report JSON (optional)")
	commit := flag.String("commit", "", "reinframe commit (default: git rev-parse HEAD)")
	flag.Parse()

	c := *commit
	if c == "" {
		c = gitHEAD()
	}
	cases, err := evaluation.LoadCasesDir(*dataset)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load dataset: %v\n", err)
		os.Exit(1)
	}
	r := &evaluation.Runner{
		Commit:           c,
		DatasetVersion:   "synthetic-m3-v1",
		RulesetID:        "provisional-default",
		RulesetHash:      "synthetic",
		ThresholdProfile: "provisional-50",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rep, err := r.Run(ctx, cases)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}
	// Human summary
	fmt.Printf("reinframe_commit=%s\n", rep.ReinframeCommit)
	fmt.Printf("dataset=%s hash=%s cases=%d\n", rep.DatasetVersion, rep.DatasetHash[:12], rep.Metrics.SampleSize)
	fmt.Printf("hard_gate_enabled=%v (must be false)\n", rep.HardGateEnabled)
	fmt.Printf("false_block_rate=%.3f false_allow_rate=%.3f\n", rep.Metrics.FalseBlockRate, rep.Metrics.FalseAllowRate)
	fmt.Printf("disposition=%s\n", rep.Disposition)
	fmt.Printf("note=%s\n", rep.DispositionNote)
	for k, m := range rep.Metrics.DetectorByKind {
		fmt.Printf("detector[%s] P=%.2f R=%.2f F1=%.2f tp=%d fp=%d fn=%d tn=%d\n",
			k, m.Precision, m.Recall, m.F1, m.TP, m.FP, m.FN, m.TN)
	}
	cs := rep.Metrics.ClassifierShadow
	fmt.Printf("classifier_shadow P=%.2f R=%.2f F1=%.2f\n", cs.Precision, cs.Recall, cs.F1)

	if *out != "" {
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
			os.Exit(1)
		}
		// Do not overwrite historical reports when path ends with versioned name;
		// for "latest.json" overwrite is explicit operator choice.
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal: %v\n", err)
			os.Exit(1)
		}
		b = append(b, '\n')
		if err := os.WriteFile(*out, b, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "write: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", *out)
	}
}

func gitHEAD() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
