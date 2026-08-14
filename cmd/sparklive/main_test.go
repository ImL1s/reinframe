package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSparkLive_All(t *testing.T) {
	evDir, err := os.MkdirTemp("", "sparklive-ev-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(evDir) }()

	sandboxDir, err := os.MkdirTemp("", "sparklive-sb-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(sandboxDir) }()

	runAll([]string{"--evidence-out", evDir, "--project-dir", sandboxDir})

	// Check that required files are created
	expectedFiles := []string{
		"preflight.json",
		"scenarios.json",
		"hook_invocations.jsonl",
		"RUN.md",
		"reinframe.codex_spark_live_control.v1.schema.json",
		"issue-187-live-spark-qualification.json",
	}

	for _, f := range expectedFiles {
		p := filepath.Join(evDir, f)
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			t.Errorf("expected evidence file %s to exist and be non-empty", f)
		}
	}

	// Verify report validation subcommand
	runReport([]string{"--evidence-out", evDir})
}

func TestSparkLive_Preflight(t *testing.T) {
	evDir, err := os.MkdirTemp("", "sparklive-pf-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(evDir) }()

	runPreflight([]string{"--evidence-out", evDir})
	p := filepath.Join(evDir, "preflight.json")
	if st, err := os.Stat(p); err != nil || st.Size() == 0 {
		t.Errorf("expected preflight.json to exist in %s", evDir)
	}
}
