package supervisor_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/supervisor"
)

func failureEvent(session, id string, seq int64, msg string) protocol.AgentEvent {
	payload, _ := json.Marshal(map[string]string{"error": msg})
	return protocol.AgentEvent{
		EventID:     id,
		SessionID:   session,
		SequenceNum: seq,
		EventType:   "error",
		Timestamp:   time.Now().UTC(),
		Payload:     payload,
	}
}

func newTestOrchestrator(t *testing.T, act *adapter.FakeActuator, alerter adapter.HumanAlerter, supportsAdvice bool) *supervisor.Orchestrator {
	t.Helper()
	if act == nil {
		act = adapter.NewFakeActuator()
		act.AutoAck = false // explicit ACK for control loop
	}
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		Alerter:                alerter,
		SupportsAdviceDelivery: supportsAdvice,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	o, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3}),
		Policy:   policy.NewEngine(policy.EngineConfig{}),
		Delivery: del,
	})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOrchestrator_DetectPolicyEnqueueLatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	o := newTestOrchestrator(t, nil, nil, true)
	sess := "orch-1"
	msg := "cannot import cycle not allowed"

	for i := 1; i <= 2; i++ {
		sig, iv, err := o.HandleEvent(ctx, failureEvent(sess, "e"+itoa(i), int64(i), msg))
		if err != nil {
			t.Fatal(err)
		}
		if sig != nil || iv != nil {
			t.Fatalf("early fire at %d sig=%v iv=%v", i, sig, iv)
		}
		dec := o.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
		if dec.Action != adapter.HookActionAllow {
			t.Fatalf("pretool before fire action=%s", dec.Action)
		}
	}

	sig, iv, err := o.HandleEvent(ctx, failureEvent(sess, "e3", 3, msg))
	if err != nil {
		t.Fatal(err)
	}
	if sig == nil || iv == nil {
		t.Fatal("expected signal and intervention on third failure")
	}
	if o.PendingInterventionID(sess) == "" {
		t.Fatal("expected pending latch")
	}

	dec := o.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Bash"})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("want defer after enqueue, got %s (%s)", dec.Action, dec.ReasonCode)
	}
	if dec.InterventionID != iv.InterventionID {
		t.Fatalf("defer id=%s want %s", dec.InterventionID, iv.InterventionID)
	}
}

func TestOrchestrator_DeliverAckAllow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	act := adapter.NewFakeActuator()
	act.AutoAck = false
	o := newTestOrchestrator(t, act, nil, true)
	sess := "orch-2"
	msg := "undefined: Foo"

	for i := 1; i <= 3; i++ {
		if _, _, err := o.HandleEvent(ctx, failureEvent(sess, "f"+itoa(i), int64(i), msg)); err != nil {
			t.Fatal(err)
		}
	}
	if o.PendingInterventionID(sess) == "" {
		t.Fatal("no latch")
	}

	item, res, err := o.DeliverAtSafeBoundary(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if act.CallCount() != 1 {
		t.Fatalf("Deliver calls=%d", act.CallCount())
	}
	if item.State != adapter.StateDelivering {
		t.Fatalf("state=%s want DELIVERING", item.State)
	}
	if res.AckStatus != adapter.AckStatusPending {
		t.Fatalf("ack=%s", res.AckStatus)
	}

	// Still deferred until ACK.
	dec := o.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("before ack action=%s", dec.Action)
	}

	if err := o.Acknowledge(item.Intervention.InterventionID, adapter.AckStatusAcked); err != nil {
		t.Fatal(err)
	}
	if o.PendingInterventionID(sess) != "" {
		t.Fatal("latch should clear after ACK")
	}
	dec = o.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("after ack action=%s", dec.Action)
	}

	audit := o.AuditSnapshot()
	if len(audit) < 3 {
		t.Fatalf("audit entries=%d", len(audit))
	}
}

func itoa(i int) string {
	return string(rune('0' + i))
}
