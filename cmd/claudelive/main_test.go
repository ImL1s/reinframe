package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeLive_All(t *testing.T) {
	evDir, err := os.MkdirTemp("", "claudelive-ev-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(evDir) }()

	sandboxDir, err := os.MkdirTemp("", "claudelive-sb-*")
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
		"reinframe.claude_live_control.v1.schema.json",
		"issue-120-live-claude-control.json",
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
