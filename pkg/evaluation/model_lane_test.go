package evaluation_test

import (
	"context"
	"testing"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func TestModelLaneB_OfflineFake(t *testing.T) {
	t.Parallel()
	rep, err := evaluation.RunModelLaneB(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if rep.HardGateEnabled {
		t.Fatal("hard gate")
	}
	if rep.Disposition != "MORE-DATA" {
		t.Fatalf("disposition %s", rep.Disposition)
	}
	if !rep.ChallengeReEvalOK {
		t.Fatal("challenge reeval")
	}
	if !rep.OpenAIAssessOK || rep.OpenAIProviderCalls != 1 {
		t.Fatalf("openai assess %+v", rep)
	}
	if !rep.ExactCacheHitOK || rep.ExactCacheProviderCalls != 1 {
		t.Fatalf("exact cache %+v", rep)
	}
	if !rep.Stage2InvariantOK {
		t.Fatal("stage2 invariant")
	}
	if !rep.MalformedRejected {
		t.Fatal("malformed")
	}
	if !rep.AuthNotRetried {
		t.Fatal("auth retry")
	}
}
