package evaluation_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func TestLoadAndRunSyntheticDataset(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "testdata", "evaluation")
	cases, err := evaluation.LoadCasesDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 12 {
		t.Fatalf("want >=12 cases, got %d", len(cases))
	}
	r := &evaluation.Runner{Commit: "test", DatasetVersion: "synthetic-m3-v1"}
	rep, err := r.Run(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HardGateEnabled {
		t.Fatal("hard_gate must be false")
	}
	if rep.SchemaVersion != evaluation.ReportSchemaVersion {
		t.Fatal(rep.SchemaVersion)
	}
	if rep.DatasetHash == "" {
		t.Fatal("dataset hash required")
	}
	if rep.Disposition != "MORE-DATA" && rep.Disposition != "NO-GO" && rep.Disposition != "LIMITED-GO" {
		t.Fatalf("disposition=%s", rep.Disposition)
	}
	// Classifier cases must never enforce
	for _, c := range rep.Cases {
		if c.Kind == evaluation.KindClassifierShadow && c.Enforced {
			t.Fatalf("%s enforced", c.CaseID)
		}
		if c.Error != "" && c.Kind != evaluation.KindClassifierShadow {
			// detector paths should be clean
			t.Fatalf("%s error=%s", c.CaseID, c.Error)
		}
	}
	// Healthy suite present
	if rep.Metrics.HealthyCases < 3 {
		t.Fatalf("healthy cases=%d", rep.Metrics.HealthyCases)
	}
	// Separate detector kinds scored
	if len(rep.Metrics.DetectorByKind) < 3 {
		t.Fatalf("detector kinds=%v", rep.Metrics.DetectorByKind)
	}
	// Determinism: second run same hash and disposition
	rep2, err := r.Run(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DatasetHash != rep2.DatasetHash {
		t.Fatal("hash nondeterministic")
	}
	if rep.Disposition != rep2.Disposition {
		t.Fatal("disposition nondeterministic")
	}
}

func TestDatasetSchemaRejectsBadVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := map[string]any{"schema_version": "nope", "case_id": "x", "kind": "repeated_failure"}
	b, _ := json.Marshal(bad)
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := evaluation.LoadCasesDir(dir); err == nil {
		t.Fatal("expected schema reject")
	}
}

func TestRepeatedFailurePositiveAndHealthy(t *testing.T) {
	t.Parallel()
	// Drive shipped runner path with in-memory cases (still real Runner.Run)
	trueV := true
	falseV := false
	cases := []evaluation.Case{
		{
			SchemaVersion: evaluation.DatasetSchemaVersion, CaseID: "rf-pos",
			ScenarioClass: evaluation.ClassPositiveDeviation, Kind: evaluation.KindRepeatedFailure,
			Source: "synthetic", Failures: []string{"error: boom", "error: boom", "error: boom"},
			ExpectDetectorFire: &trueV,
		},
		{
			SchemaVersion: evaluation.DatasetSchemaVersion, CaseID: "rf-healthy",
			ScenarioClass: evaluation.ClassHealthy, Kind: evaluation.KindRepeatedFailure,
			Source: "synthetic", Failures: []string{"error: a", "error: b"},
			ExpectDetectorFire: &falseV,
		},
	}
	rep, err := (&evaluation.Runner{}).Run(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	var pos, healthy evaluation.CaseResult
	for _, c := range rep.Cases {
		if c.CaseID == "rf-pos" {
			pos = c
		}
		if c.CaseID == "rf-healthy" {
			healthy = c
		}
	}
	if !pos.DetectorTP {
		t.Fatalf("pos=%+v", pos)
	}
	if !healthy.DetectorTN {
		t.Fatalf("healthy=%+v", healthy)
	}
}

func TestClassifierFalseBlockMetricPath(t *testing.T) {
	t.Parallel()
	cases := []evaluation.Case{
		{
			SchemaVersion: evaluation.DatasetSchemaVersion, CaseID: "cls-block",
			ScenarioClass: evaluation.ClassPositiveDeviation, Kind: evaluation.KindClassifierShadow,
			Source: "synthetic", ClassifierFixture: "clear_block", ExpectStage2Decision: "BLOCK",
		},
		{
			SchemaVersion: evaluation.DatasetSchemaVersion, CaseID: "cls-allow",
			ScenarioClass: evaluation.ClassHealthy, Kind: evaluation.KindClassifierShadow,
			Source: "synthetic", ClassifierFixture: "clear_allow", ExpectStage2Decision: "ALLOW",
		},
		{
			SchemaVersion: evaluation.DatasetSchemaVersion, CaseID: "cls-exc",
			ScenarioClass: evaluation.ClassHealthy, Kind: evaluation.KindClassifierShadow,
			Source: "synthetic", ClassifierFixture: "user_exception", UserException: true,
			ExpectStage2Decision: "ALLOW",
		},
	}
	rep, err := (&evaluation.Runner{}).Run(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HardGateEnabled {
		t.Fatal()
	}
	for _, c := range rep.Cases {
		if c.Stage2Correct == nil || !*c.Stage2Correct {
			t.Fatalf("%+v", c)
		}
	}
}
