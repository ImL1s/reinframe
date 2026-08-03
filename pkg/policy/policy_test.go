package policy_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
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

func TestEvaluateSlow_ConcurrentUniqueInterventionIDs(t *testing.T) {
	t.Parallel()
	eng := policy.NewEngine(policy.EngineConfig{})
	const n = 64
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sig := &protocol.TunnelSignal{
				SignalID:     fmt.Sprintf("sig-%d", i),
				SessionID:    fmt.Sprintf("s-%d", i),
				DetectorName: detector.DetectorNameRepeatedFailure,
				FailureMode:  detector.FailureModeRepeatedErrorLoop,
				Score:        1.0,
				Details:      map[string]string{"fingerprint": "x"},
				TriggeredAt:  time.Now().UTC(),
			}
			res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{Signal: sig})
			if err != nil || res.Intervention == nil {
				t.Errorf("EvaluateSlow: err=%v res=%#v", err, res)
				return
			}
			ids <- res.Intervention.InterventionID
		}(i)
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]struct{}, n)
	for id := range ids {
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate InterventionID %q", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != n {
		t.Fatalf("unique ids=%d want %d", len(seen), n)
	}
}

func TestEvaluateSlow_ContractEnrichesAdvice(t *testing.T) {
	t.Parallel()
	eng := policy.NewEngine(policy.EngineConfig{Reviewer: panicReviewer{}})
	c := protocol.BuildContractFromSubmitted(protocol.TaskSubmitted{
		TaskID: "t1", SessionID: "s1", Prompt: "fix typo",
		SubmittedAt: time.Now().UTC(),
	}, protocol.BuildContractOptions{})
	led := protocol.NewEvidenceLedger(c.TaskID, c.Revision)
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{
		Signal:   sampleSignal(),
		Contract: &c,
		Ledger:   &led,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Intervention == nil {
		t.Fatal("expected intervention")
	}
	if !strings.Contains(res.Intervention.AdvicePrompt, "contract rev=") {
		t.Fatalf("advice should mention contract: %q", res.Intervention.AdvicePrompt)
	}
	if !strings.Contains(res.Intervention.AdvicePrompt, "ledger validations=") {
		t.Fatalf("advice should mention ledger: %q", res.Intervention.AdvicePrompt)
	}
}

func TestEvaluateSlow_HighRiskContractUsesReviewerWhenPresent(t *testing.T) {
	t.Parallel()
	fp := reviewer.NewFakeProvider()
	fp.Decision = protocol.ReviewDecision{
		Classification:   "TUNNEL_VISION",
		TunnelConfidence: 0.9,
		SuggestedAdvice:  "high-risk zoom",
	}
	eng := policy.NewEngine(policy.EngineConfig{Reviewer: fp})
	c := protocol.BuildContractFromSubmitted(protocol.TaskSubmitted{
		TaskID: "t-sec", SessionID: "s", Prompt: "fix production auth security bug",
		SubmittedAt: time.Now().UTC(),
	}, protocol.BuildContractOptions{})
	// Force high risk if heuristic differed
	c.Risk = protocol.RiskHigh
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{
		Signal:   sampleSignal(),
		Contract: &c,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.UsedReviewer {
		t.Fatal("high-risk contract with reviewer should use reviewer path")
	}
	if fp.CallCount() != 1 {
		t.Fatalf("calls=%d", fp.CallCount())
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
