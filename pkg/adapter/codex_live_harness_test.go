package adapter_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestRunCodexLiveHarness_AllScenariosPass(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "codex-test-harness-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	ctx := context.Background()
	opts := adapter.CodexLiveHarnessOptions{
		SandboxDir:      tmpDir,
		ReinframeCommit: "test-commit-sha",
	}

	report, invocations, err := adapter.RunCodexLiveHarness(ctx, opts)
	if err != nil {
		t.Fatalf("RunCodexLiveHarness failed: %v", err)
	}

	if report.SchemaVersion != adapter.CodexLiveControlSchemaV1 {
		t.Errorf("schema version mismatch: got %s, want %s", report.SchemaVersion, adapter.CodexLiveControlSchemaV1)
	}

	if report.FinalDisposition != "GO" {
		t.Errorf("expected final disposition GO, got %s", report.FinalDisposition)
	}

	if report.Summary.Total != 5 || report.Summary.Passed != 5 || report.Summary.Failed != 0 {
		t.Errorf("unexpected summary stats: %+v", report.Summary)
	}

	// Verify the 5 scenarios
	expectedScenarios := []string{
		adapter.CodexScenarioAllow,
		adapter.CodexScenarioBlock,
		adapter.CodexScenarioLoopCont,
		adapter.CodexScenarioBoundedCtx,
		adapter.CodexScenarioPermApprove,
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

	if len(invocations) < 5 {
		t.Errorf("expected at least 5 invocation logs, got %d", len(invocations))
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

	if err := adapter.ValidateCodexLiveControlReport(repMap); err != nil {
		t.Fatalf("schema validation failed: %v", err)
	}
}

func TestValidateCodexLiveControlReport_RejectsInvalid(t *testing.T) {
	t.Parallel()

	invalidDoc := map[string]any{
		"schema_version": "invalid.schema",
		"scenarios":      map[string]any{},
	}
	if err := adapter.ValidateCodexLiveControlReport(invalidDoc); err == nil {
		t.Fatal("expected schema validation error for invalid document")
	}
}

func TestCodexPreflight_Valid(t *testing.T) {
	t.Parallel()

	pf := adapter.RunCodexPreflight("test-sha")
	if !pf.Usable {
		t.Fatal("preflight should be usable")
	}
	if len(pf.Probes) == 0 {
		t.Fatal("preflight probes empty")
	}
	if pf.Harness != "reinframe.codex_live_harness.v1" {
		t.Fatalf("unexpected harness: %s", pf.Harness)
	}
}

func TestRedactCodexEvidence(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"path": os.Getenv("USERPROFILE"),
		"user": os.Getenv("USERNAME"),
		"host": "localhost",
	}
	redacted := adapter.RedactCodexEvidence(input)
	b, _ := json.Marshal(redacted)
	s := string(b)

	if user := os.Getenv("USERNAME"); user != "" && len(user) > 3 {
		if strings.Contains(s, `"`+user+`"`) {
			t.Errorf("expected username %q to be redacted from JSON output", user)
		}
	}
}
