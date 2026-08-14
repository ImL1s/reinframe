package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCodexLive_All(t *testing.T) {
	evDir, err := os.MkdirTemp("", "codexlive-ev-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(evDir) }()

	sandboxDir, err := os.MkdirTemp("", "codexlive-sb-*")
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
		"reinframe.codex_live_control.v1.schema.json",
		"issue-164-live-codex-control.json",
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
