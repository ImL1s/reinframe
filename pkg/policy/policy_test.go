package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/reviewer"
)

// panicReviewer panics if Generate is called — used to prove fast path never touches Reviewer.
type panicReviewer struct{}

func (panicReviewer) Generate(context.Context, protocol.ReviewRequest) (protocol.ReviewDecision, error) {
	panic("Reviewer must not be called on fast path or deterministic slow path")
}

func sampleSignal() *protocol.TunnelSignal {
	return &protocol.TunnelSignal{
		SignalID:     "sig-1",
		SessionID:    "sess-p",
		DetectorName: detector.DetectorNameRepeatedFailure,
		FailureMode:  detector.FailureModeRepeatedErrorLoop,
		Weight:       0.35,
		Score:        1.0,
		Details:      map[string]string{"fingerprint": "err x", "count": "3"},
		TriggeredAt:  time.Now().UTC(),
	}
}

func TestEvaluateFast_NeverInvokesReviewer(t *testing.T) {
	t.Parallel()
	// Even if someone wires a panic reviewer into the engine, EvaluateFast must not touch it.
	eng := policy.NewEngine(policy.EngineConfig{Reviewer: panicReviewer{}})
	dec := eng.EvaluateFast(context.Background(), policy.FastInput{
		Request: adapter.HookRequest{SessionID: "s", Phase: "PreTool", ToolName: "Bash"},
		Policy:  adapter.HookPolicy{},
	})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("action=%s", dec.Action)
	}
}

func TestEvaluateFast_DeferPendingAdvisory(t *testing.T) {
	t.Parallel()
	eng := policy.NewEngine(policy.EngineConfig{Reviewer: panicReviewer{}})
	dec := eng.EvaluateFast(context.Background(), policy.FastInput{
		Request: adapter.HookRequest{SessionID: "s", ToolName: "Edit"},
		Policy: adapter.HookPolicy{
			PendingAdvisoryInterventionID: "iv-1",
		},
	})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("action=%s want defer", dec.Action)
	}
	if dec.ReasonCode != adapter.ReasonDeferPendingAdvisory {
		t.Fatalf("reason=%s", dec.ReasonCode)
	}
	if dec.InterventionID != "iv-1" {
		t.Fatalf("id=%s", dec.InterventionID)
	}
}

func TestEvaluateSlow_DeterministicZoomOutWithoutReviewer(t *testing.T) {
	t.Parallel()
	eng := policy.NewEngine(policy.EngineConfig{Reviewer: panicReviewer{}})
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{
		Signal:   sampleSignal(),
		Contract: nil,
		Ledger:   nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.UsedReviewer {
		t.Fatal("deterministic path must not use Reviewer")
	}
	if res.Action != policy.ActionZoomOutPrompt {
		t.Fatalf("action=%s", res.Action)
	}
	if res.Intervention == nil {
		t.Fatal("expected intervention")
	}
	if res.Intervention.ActionType != "ZOOM_OUT_PROMPT" {
		t.Fatalf("ActionType=%s", res.Intervention.ActionType)
	}
	if res.Intervention.AdvicePrompt == "" {
		t.Fatal("AdvicePrompt empty")
	}
	if !res.Intervention.RequiresAck {
		t.Fatal("RequiresAck should be true for slice")
	}
	if res.Intervention.SessionID != "sess-p" {
		t.Fatalf("session=%s", res.Intervention.SessionID)
	}
}

func TestEvaluateSlow_NilSignal(t *testing.T) {
	t.Parallel()
	eng := policy.NewEngine(policy.EngineConfig{})
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != policy.ActionNone {
		t.Fatalf("action=%s", res.Action)
	}
}

func TestEvaluateSlow_UncertainUsesOptionalReviewer(t *testing.T) {
	t.Parallel()
	fp := reviewer.NewFakeProvider()
	fp.Decision = protocol.ReviewDecision{
		Classification:   "TUNNEL_VISION",
		TunnelConfidence: 0.9,
		SuggestedAdvice:  "custom zoom advice",
	}
	eng := policy.NewEngine(policy.EngineConfig{Reviewer: fp})
	// Force uncertain branch (still same signal shape).
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{
		Signal:    sampleSignal(),
		Uncertain: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.UsedReviewer {
		t.Fatal("expected reviewer use")
	}
	if fp.CallCount() != 1 {
		t.Fatalf("reviewer calls=%d", fp.CallCount())
	}
	if res.Action != policy.ActionZoomOutPrompt {
		t.Fatalf("action=%s", res.Action)
	}
	if res.Intervention.AdvicePrompt != "custom zoom advice" {
		t.Fatalf("advice=%q", res.Intervention.AdvicePrompt)
	}
}
