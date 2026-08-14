package adapter_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestRunClaudeLiveHarness_AllScenariosPass(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "claude-test-harness-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()
	opts := adapter.ClaudeLiveHarnessOptions{
		SandboxDir:      tmpDir,
		ReinframeCommit: "test-commit-sha",
	}

	report, invocations, err := adapter.RunClaudeLiveHarness(ctx, opts)
	if err != nil {
		t.Fatalf("RunClaudeLiveHarness failed: %v", err)
	}

	if report.SchemaVersion != adapter.ClaudeLiveControlSchemaV1 {
		t.Errorf("schema version mismatch: got %s, want %s", report.SchemaVersion, adapter.ClaudeLiveControlSchemaV1)
	}

	if report.FinalDisposition != "GO" {
		t.Errorf("expected final disposition GO, got %s", report.FinalDisposition)
	}

	if report.Summary.Total != 4 || report.Summary.Passed != 4 || report.Summary.Failed != 0 {
		t.Errorf("unexpected summary stats: %+v", report.Summary)
	}

	// Verify the 4 scenarios
	expectedScenarios := []string{
		adapter.ClaudeScenarioAllow,
		adapter.ClaudeScenarioBlock,
		adapter.ClaudeScenarioLoopCont,
		adapter.ClaudeScenarioBoundedCtx,
	}

	for _, id := range expectedScenarios {
		sc, ok := report.Scenarios[id]
		if !ok {
			t.Errorf("scenario %s missing from report", id)
			continue
		}
		if sc.Status != "PASS" {
			t.Errorf("scenario %s status is %s (detail: %s)", id, sc.Status, sc.Detail)
		}
	}

	if len(invocations) < 4 {
		t.Errorf("expected at least 4 invocation logs, got %d", len(invocations))
	}

	// Test schema validation against report
	repRaw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var repMap map[string]any
	if err := json.Unmarshal(repRaw, &repMap); err != nil {
		t.Fatal(err)
	}

	if err := adapter.ValidateClaudeLiveControlReport(repMap); err != nil {
		t.Fatalf("schema validation failed: %v", err)
	}
}

func TestValidateClaudeLiveControlReport_RejectsInvalid(t *testing.T) {
	t.Parallel()

	invalidDoc := map[string]any{
		"schema_version": "invalid.schema",
		"scenarios":      map[string]any{},
	}
	if err := adapter.ValidateClaudeLiveControlReport(invalidDoc); err == nil {
		t.Fatal("expected schema validation error for invalid document")
	}
}

func TestClaudePreflight_Valid(t *testing.T) {
	t.Parallel()

	pf := adapter.RunClaudePreflight("test-sha")
	if !pf.Usable {
		t.Fatal("preflight should be usable")
	}
	if len(pf.Probes) == 0 {
		t.Fatal("preflight probes empty")
	}
	if pf.Harness != "reinframe.claude_live_harness.v1" {
		t.Fatalf("unexpected harness: %s", pf.Harness)
	}
}

func TestRedactClaudeEvidence(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"path": os.Getenv("USERPROFILE"),
		"user": os.Getenv("USERNAME"),
		"host": "localhost",
	}
	redacted := adapter.RedactClaudeEvidence(input)
	b, _ := json.Marshal(redacted)
	s := string(b)
	_ = s
}
