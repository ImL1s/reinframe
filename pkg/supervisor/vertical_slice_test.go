package supervisor_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/supervisor"
)

// TestVerticalSlice_RepeatedFailureToAckAllow is the #71 mandatory scenario:
// three identical failures → detector → policy pending ZOOM_OUT → PreTool defer →
// Actuator.Deliver → explicit ACK → PreTool allow.
//
// Anti-theater: Deliver must be observed; hand-inserted Store rows alone are not used.
func TestVerticalSlice_RepeatedFailureToAckAllow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	act := adapter.NewFakeActuator()
	act.AutoAck = false

	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	orch, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3}),
		Policy:   policy.NewEngine(policy.EngineConfig{}), // no Reviewer
		Delivery: del,
	})
	if err != nil {
		t.Fatal(err)
	}

	const sess = "vs-happy"
	const failMsg = "exit status 1: TestAuth failed: want 200 got 500"

	// 1–3: emit failures through the real detector path (not hand-built Intervention).
	var lastIV *protocol.Intervention
	for i := 1; i <= 3; i++ {
		payload, _ := json.Marshal(protocol.TestResultEvent{
			TestRunID:     "tr-" + itoa(i),
			Command:       "go test ./...",
			FailedCount:   1,
			FailureOutput: failMsg,
		})
		ev := protocol.AgentEvent{
			EventID:     "vs-e" + itoa(i),
			SessionID:   sess,
			SequenceNum: int64(i),
			EventType:   "test_result",
			Timestamp:   time.Now().UTC(),
			Payload:     payload,
		}
		sig, iv, err := orch.HandleEvent(ctx, ev)
		if err != nil {
			t.Fatalf("HandleEvent %d: %v", i, err)
		}
		if i < 3 {
			if sig != nil || iv != nil {
				t.Fatalf("step %d: unexpected fire", i)
			}
			continue
		}
		if sig == nil || iv == nil {
			t.Fatal("step 3: expected detector+policy intervention (not hand-inserted)")
		}
		if iv.ActionType != "ZOOM_OUT_PROMPT" {
			t.Fatalf("ActionType=%s", iv.ActionType)
		}
		lastIV = iv
	}

	// 4: next PreTool → defer
	dec := orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, Phase: "PreTool", ToolName: "Edit"})
	if dec.Action != adapter.HookActionDefer || dec.ReasonCode != adapter.ReasonDeferPendingAdvisory {
		t.Fatalf("pre-deliver PreTool: action=%s reason=%s", dec.Action, dec.ReasonCode)
	}
	if dec.InterventionID != lastIV.InterventionID {
		t.Fatalf("defer id=%s want %s", dec.InterventionID, lastIV.InterventionID)
	}

	// 5: safe-boundary deliver — must observe Actuator.Deliver
	if act.CallCount() != 0 {
		t.Fatal("Deliver must not run before DeliverAtSafeBoundary")
	}
	item, _, err := orch.DeliverAtSafeBoundary(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if act.CallCount() != 1 {
		t.Fatalf("anti-theater: Actuator.Deliver calls=%d want 1", act.CallCount())
	}
	got, ok := act.LastCall()
	if !ok || got.InterventionID != lastIV.InterventionID {
		t.Fatalf("LastCall=%#v", got)
	}
	if got.AdvicePrompt == "" {
		t.Fatal("advice prompt not delivered")
	}
	if item.State != adapter.StateDelivering {
		t.Fatalf("state=%s want DELIVERING (explicit ACK required)", item.State)
	}

	// 6: PreTool still defer until ACK (anti-theater: allow-before-ack must not pass)
	dec = orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Bash"})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("anti-theater: PreTool allow before ACK would pass incorrectly; got %s", dec.Action)
	}

	// 7: explicit ACK from fake agent
	if err := orch.Acknowledge(lastIV.InterventionID, adapter.AckStatusAcked); err != nil {
		t.Fatal(err)
	}

	// 8: subsequent PreTool allow
	dec = orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Bash"})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("after ACK action=%s reason=%s", dec.Action, dec.ReasonCode)
	}
}

// TestVerticalSlice_ObserveOnlyRequiresNonNopAlerter covers missing CapAdviceDelivery.
func TestVerticalSlice_ObserveOnlyRequiresNonNopAlerter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	act := adapter.NewFakeActuator()
	act.AutoAck = false
	alerter := &adapter.RecordingAlerter{}

	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		Alerter:                alerter,
		SupportsAdviceDelivery: false, // observe-only
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	orch, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3}),
		Policy:   policy.NewEngine(policy.EngineConfig{}),
		Delivery: del,
	})
	if err != nil {
		t.Fatal(err)
	}

	const sess = "vs-observe"
	const msg = "connection refused 127.0.0.1:5432"
	for i := 1; i <= 3; i++ {
		payload, _ := json.Marshal(map[string]string{"error": msg})
		_, _, err := orch.HandleEvent(ctx, protocol.AgentEvent{
			EventID: "o" + itoa(i), SessionID: sess, SequenceNum: int64(i),
			EventType: "error", Timestamp: time.Now().UTC(), Payload: payload,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	item, res, err := orch.DeliverAtSafeBoundary(ctx, sess)
	if err != nil {
		t.Fatalf("human escalation path err=%v", err)
	}
	if act.CallCount() != 0 {
		t.Fatal("observe-only must not call Actuator.Deliver")
	}
	if alerter.Len() != 1 {
		t.Fatalf("HumanAlerter calls=%d want 1", alerter.Len())
	}
	if res.DeliveryMode != adapter.DeliveryModeHumanEscalation {
		t.Fatalf("mode=%s", res.DeliveryMode)
	}
	if item == nil {
		t.Fatal("nil item")
	}
}

// TestVerticalSlice_NopAlerterObserveOnlyFails ensures silent success is rejected.
func TestVerticalSlice_NopAlerterObserveOnlyFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	act := adapter.NewFakeActuator()
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		Alerter:                adapter.NopHumanAlerter{},
		SupportsAdviceDelivery: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	orch, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3}),
		Policy:   policy.NewEngine(policy.EngineConfig{}),
		Delivery: del,
	})
	if err != nil {
		t.Fatal(err)
	}
	const sess = "vs-nop"
	for i := 1; i <= 3; i++ {
		payload, _ := json.Marshal(map[string]string{"error": "same err"})
		if _, _, err := orch.HandleEvent(ctx, protocol.AgentEvent{
			EventID: "n" + itoa(i), SessionID: sess, SequenceNum: int64(i),
			EventType: "error", Timestamp: time.Now().UTC(), Payload: payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, _, err = orch.DeliverAtSafeBoundary(ctx, sess)
	if !errors.Is(err, adapter.ErrNopHumanAlerter) {
		t.Fatalf("want ErrNopHumanAlerter, got %v", err)
	}
}

// TestVerticalSlice_AntiTheater_NoDeliverMeansStillDeferred documents that
// enqueue alone without Deliver keeps PreTool deferred — the happy-path test
// requires CallCount>=1, so skipping Deliver cannot satisfy #71.
func TestVerticalSlice_AntiTheater_NoDeliverMeansStillDeferred(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	act := adapter.NewFakeActuator()
	act.AutoAck = false
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator: act, SupportsAdviceDelivery: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	orch, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3}),
		Policy:   policy.NewEngine(policy.EngineConfig{}),
		Delivery: del,
	})
	if err != nil {
		t.Fatal(err)
	}
	const sess = "vs-no-del"
	for i := 1; i <= 3; i++ {
		payload, _ := json.Marshal(map[string]string{"error": "x"})
		if _, _, err := orch.HandleEvent(ctx, protocol.AgentEvent{
			EventID: "d" + itoa(i), SessionID: sess, SequenceNum: int64(i),
			EventType: "error", Timestamp: time.Now().UTC(), Payload: payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Intentionally skip DeliverAtSafeBoundary.
	if act.CallCount() != 0 {
		t.Fatal("setup broken")
	}
	dec := orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("without Deliver, PreTool must remain deferred, got %s", dec.Action)
	}
	// #71 anti-theater assertion: a passing vertical slice requires Deliver observed.
	if act.CallCount() == 0 {
		// This is the required failure mode for "only enqueue" — count stays 0.
		t.Log("confirmed: no Deliver observed (would fail happy-path assert CallCount==1)")
	}
}
