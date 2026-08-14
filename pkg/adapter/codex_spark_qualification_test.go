package adapter_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestRunCodexSparkQualification_AllScenariosPass(t *testing.T) {
	t.Parallel()

	tmp, err := os.MkdirTemp("", "spark-qual-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	opts := adapter.CodexSparkQualificationOptions{
		SandboxDir:      tmp,
		ReinframeCommit: "testcommit187",
		AccountTier:     "chatgpt_pro",
	}

	report, invocations, err := adapter.RunCodexSparkQualification(context.Background(), opts)
	if err != nil {
		t.Fatalf("RunCodexSparkQualification failed: %v", err)
	}

	if report.SchemaVersion != adapter.CodexSparkLiveControlSchemaV1 {
		t.Errorf("expected schema %s, got %s", adapter.CodexSparkLiveControlSchemaV1, report.SchemaVersion)
	}
	if report.FinalDisposition != "GO" {
		t.Errorf("expected final disposition GO, got %s", report.FinalDisposition)
	}
	if report.Summary.Total != 5 || report.Summary.Passed != 5 || report.Summary.Failed != 0 {
		t.Errorf("unexpected summary counts: %+v", report.Summary)
	}
	if report.Provenance.Issue != 187 {
		t.Errorf("expected issue 187, got %d", report.Provenance.Issue)
	}
	if report.Provenance.Model != adapter.TargetModelGPT53CodexSpark {
		t.Errorf("expected model %s, got %s", adapter.TargetModelGPT53CodexSpark, report.Provenance.Model)
	}

	// Verify all 5 mandatory scenarios
	mandatoryScenarios := []string{
		adapter.SparkScenarioModelIdent,
		adapter.SparkScenarioFallbackDisabled,
		adapter.SparkScenarioTurnExec,
		adapter.SparkScenarioToolHook,
		adapter.SparkScenarioCleanup,
	}

	for _, id := range mandatoryScenarios {
		sc, exists := report.Scenarios[id]
		if !exists {
			t.Errorf("missing mandatory scenario: %s", id)
			continue
		}
		if sc.Status != "PASS" {
			t.Errorf("scenario %s status is %s (detail: %s)", id, sc.Status, sc.Detail)
		}
	}

	if len(invocations) < 5 {
		t.Errorf("expected at least 5 hook invocation records, got %d", len(invocations))
	}

	// Validate report with JSON Schema
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("failed to marshal report: %v", err)
	}
	var repMap map[string]any
	if err := json.Unmarshal(raw, &repMap); err != nil {
		t.Fatalf("failed to unmarshal report into map: %v", err)
	}
	if err := adapter.ValidateCodexSparkLiveControlReport(repMap); err != nil {
		t.Fatalf("schema validation error for generated report: %v", err)
	}
}

func TestValidateCodexSparkLiveControlReport_RejectsInvalid(t *testing.T) {
	t.Parallel()

	// Missing required fields
	invalidReport := map[string]any{
		"schema_version": "reinframe.codex_spark_live_control.v1",
		// missing provenance, scenarios, summary, final_disposition
	}
	if err := adapter.ValidateCodexSparkLiveControlReport(invalidReport); err == nil {
		t.Error("expected error validating incomplete report, got nil")
	}

	// Bad disposition
	invalidDisp := map[string]any{
		"schema_version": "reinframe.codex_spark_live_control.v1",
		"provenance": map[string]any{
			"issue":        187,
			"generated_at": "2026-08-15T02:00:00Z",
			"goos":         "windows",
			"goarch":       "amd64",
			"harness":      "reinframe.codex_spark_qualification.v1",
			"model":        "gpt-5.3-codex-spark",
			"account_tier": "chatgpt_pro",
		},
		"scenarios": map[string]any{},
		"summary": map[string]any{
			"total":        0,
			"passed":       0,
			"failed":       0,
			"inconclusive": 0,
		},
		"final_disposition": "INVALID_DISPOSITION",
	}
	if err := adapter.ValidateCodexSparkLiveControlReport(invalidDisp); err == nil {
		t.Error("expected error for invalid disposition, got nil")
	}
}

func TestCodexSparkPreflight_Valid(t *testing.T) {
	t.Parallel()

	pf := adapter.RunCodexSparkPreflight("testcommit")
	if !pf.Usable {
		t.Error("expected preflight to be usable")
	}
	if pf.TargetModel != adapter.TargetModelGPT53CodexSpark {
		t.Errorf("expected target model %s, got %s", adapter.TargetModelGPT53CodexSpark, pf.TargetModel)
	}
	if pf.AccountTier != "chatgpt_pro" {
		t.Errorf("expected account tier chatgpt_pro, got %s", pf.AccountTier)
	}
	if len(pf.Probes) < 3 {
		t.Errorf("expected at least 3 probes, got %d", len(pf.Probes))
	}
}

func TestRedactSparkEvidence(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"model":  "gpt-5.3-codex-spark",
		"detail": "test detail",
	}
	redacted := adapter.RedactSparkEvidence(input)
	if redacted == nil {
		t.Fatal("redacted output is nil")
	}
}
