package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestEvaluateSlow_ToolBudgetChurn_ZoomOut(t *testing.T) {
	t.Parallel()
	eng := policy.NewEngine(policy.EngineConfig{})
	sig := &protocol.TunnelSignal{
		SignalID:     "sig-tb-1",
		SessionID:    "s",
		DetectorName: detector.DetectorNameToolBudgetChurn,
		FailureMode:  detector.FailureModeToolBudgetChurn,
		Score:        1.0,
		Weight:       0.32,
		Details:      map[string]string{"max_tool_calls": "5"},
		TriggeredAt:  time.Now().UTC(),
	}
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{Signal: sig})
	if err != nil {
		t.Fatal(err)
	}
	if res.Intervention == nil || res.Intervention.ActionType != "ZOOM_OUT_PROMPT" {
		t.Fatalf("res=%+v", res)
	}
}

func TestEvaluateSlow_HypothesisLoop_ZoomOut(t *testing.T) {
	t.Parallel()
	eng := policy.NewEngine(policy.EngineConfig{})
	sig := &protocol.TunnelSignal{
		SignalID:     "sig-hl-1",
		SessionID:    "s",
		DetectorName: detector.DetectorNameHypothesisLoop,
		FailureMode:  detector.FailureModeHypothesisLoop,
		Score:        1.0,
		TriggeredAt:  time.Now().UTC(),
	}
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{Signal: sig})
	if err != nil {
		t.Fatal(err)
	}
	if res.Intervention == nil {
		t.Fatalf("expected zoom out, res=%+v", res)
	}
}
