package evaluation_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func challengeFixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Join(filepath.Dir(file), "testdata", "challenge_lane_a")
}

func TestChallengeLaneA_SchemaRejectUnknownKind(t *testing.T) {
	t.Parallel()
	err := evaluation.ValidateChallengeCase(evaluation.ChallengeCase{
		SchemaVersion: evaluation.ChallengeDatasetSchemaVersion,
		CaseID:        "x", Kind: "not_a_kind", SessionID: "s",
		Proposed:            evaluation.ProposedActionFixture{ToolName: "Bash"},
		ExpectAppealability: "APPEALABLE",
	})
	if err == nil {
		t.Fatal("want unknown kind error")
	}
}

func TestChallengeLaneA_RunFixtures(t *testing.T) {
	t.Parallel()
	cases, err := evaluation.LoadChallengeCasesDir(challengeFixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 6 {
		t.Fatalf("want fixtures got %d", len(cases))
	}
	r := evaluation.ChallengeRunner{Commit: "test", DatasetVersion: "challenge-lane-a-v1"}
	rep1, err := r.RunLaneA(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	if rep1.HardGateEnabled {
		t.Fatal("hard_gate must be false")
	}
	if rep1.Lane != evaluation.ChallengeLaneDeterministic {
		t.Fatal(rep1.Lane)
	}
	if rep1.Disposition != "MORE-DATA" && rep1.Disposition != "NO-GO" {
		t.Fatalf("disposition %s", rep1.Disposition)
	}
	// Deterministic replay.
	rep2, err := r.RunLaneA(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := json.Marshal(rep1.Results)
	b2, _ := json.Marshal(rep2.Results)
	if string(b1) != string(b2) {
		t.Fatal("replay must be deterministic")
	}
	if rep1.DatasetHash != rep2.DatasetHash || rep1.DatasetHash == "" {
		t.Fatal("dataset hash")
	}
	// Every case must pass.
	for _, res := range rep1.Results {
		if !res.PassCase {
			t.Fatalf("case %s failed: %+v", res.CaseID, res)
		}
	}
	m := rep1.Metrics
	if m.NonAppealableCases < 1 || m.NonAppealableRoutedOK < 1 {
		t.Fatalf("non-appealable metrics %+v", m)
	}
	if m.InvalidAppealAttempts < 1 || m.InvalidAppealRejected < 1 {
		t.Fatalf("invalid appeal metrics %+v", m)
	}
	if m.ValidAppealAccepted < 1 {
		t.Fatalf("valid appeal metrics %+v", m)
	}
	if m.HumanReviewRoutedOK < 1 {
		t.Fatalf("human review metrics %+v", m)
	}
	// Denominators: non-appealable not counted as valid appeal opportunities.
	if m.ValidAppealAttempts > m.AppealableCases+m.BypassAttempts {
		t.Fatalf("denominator inconsistency %+v", m)
	}
}

func TestChallengeLaneA_Race(t *testing.T) {
	t.Parallel()
	cases, err := evaluation.LoadChallengeCasesDir(challengeFixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	r := evaluation.ChallengeRunner{DatasetVersion: "challenge-lane-a-v1"}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.RunLaneA(context.Background(), cases)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
