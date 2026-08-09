package adapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func sampleIntervention(id, session string) protocol.Intervention {
	return protocol.Intervention{
		InterventionID: id,
		SessionID:      session,
		Level:          1,
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "Please replan with wider context.",
		Status:         "PENDING",
		ExecutedAt:     time.Now().UTC(),
	}
}

func TestAdvisory_QueueDeliverOnceAndAck(t *testing.T) {
	act := adapter.NewFakeActuator()
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAdvisoryDelivery: %v", err)
	}

	iv := sampleIntervention("iv-1", "sess-a")
	enq := del.Enqueue(iv, 0)
	if enq.Suppressed || enq.Expired {
		t.Fatalf("enqueue flags: suppressed=%v expired=%v", enq.Suppressed, enq.Expired)
	}
	if enq.Item.State != adapter.StatePending {
		t.Fatalf("state=%s want PENDING", enq.Item.State)
	}

	item, res, err := del.DeliverPending(context.Background(), "sess-a")
	if err != nil {
		t.Fatalf("DeliverPending: %v", err)
	}
	if act.CallCount() != 1 {
		t.Fatalf("actuator calls=%d want 1", act.CallCount())
	}
	if !res.Accepted || res.AckStatus != adapter.AckStatusAcked {
		t.Fatalf("result=%+v", res)
	}
	if item.State != adapter.StateAcked {
		t.Fatalf("item.State=%s want ACKED", item.State)
	}

	got, ok := del.Get("iv-1")
	if !ok {
		t.Fatal("missing item after deliver")
	}
	if got.State != adapter.StateAcked {
		t.Fatalf("stored state=%s", got.State)
	}

	// Second deliver should find nothing.
	if _, _, err := del.DeliverPending(context.Background(), "sess-a"); err == nil {
		t.Fatal("expected error when no pending left")
	}
	if act.CallCount() != 1 {
		t.Fatalf("duplicate deliver must not call actuator again; calls=%d", act.CallCount())
	}
}

func TestAdvisory_DuplicateSuppressed(t *testing.T) {
	act := adapter.NewFakeActuator()
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
	})
	if err != nil {
		t.Fatalf("NewAdvisoryDelivery: %v", err)
	}

	iv := sampleIntervention("iv-dup", "sess-b")
	first := del.Enqueue(iv, time.Minute)
	if first.Suppressed {
		t.Fatal("first enqueue should not be suppressed")
	}
	second := del.Enqueue(iv, time.Minute)
	if !second.Suppressed {
		t.Fatal("duplicate InterventionID must be suppressed")
	}
	if second.Item.State != adapter.StateSuppressed {
		t.Fatalf("state=%s want SUPPRESSED", second.Item.State)
	}

	// Original remains pending and is deliverable once.
	stored, ok := del.Get("iv-dup")
	if !ok || stored.State != adapter.StatePending {
		t.Fatalf("original state=%v ok=%v", stored, ok)
	}

	if _, _, err := del.DeliverPending(context.Background(), "sess-b"); err != nil {
		t.Fatalf("DeliverPending: %v", err)
	}
	if act.CallCount() != 1 {
		t.Fatalf("calls=%d want 1", act.CallCount())
	}
}

func TestAdvisory_ExpiredNotDelivered(t *testing.T) {
	act := adapter.NewFakeActuator()
	q := adapter.NewPendingQueue()
	// Fixed clock so TTL in the past is immediately expired.
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	q.SetClock(func() time.Time { return base })

	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Queue:                  q,
	})
	if err != nil {
		t.Fatalf("NewAdvisoryDelivery: %v", err)
	}

	// Enqueue with zero-duration TTL after we force ExpiresAt <= now via tiny TTL
	// and clock: ttl that ends at base (not After(base)) marks EXPIRED.
	// Use negative approach: enqueue with 1ns then advance clock before deliver.
	q.SetClock(func() time.Time { return base })
	iv := sampleIntervention("iv-exp", "sess-c")
	enq := del.Enqueue(iv, time.Nanosecond)
	// After enqueue at base with 1ns TTL, expires = base+1ns which is After(base).
	// Advance clock past expiry.
	q.SetClock(func() time.Time { return base.Add(time.Second) })

	if enq.Expired {
		// Accept either immediate expire or later expire path.
		if act.CallCount() != 0 {
			t.Fatalf("expired item must not deliver; calls=%d", act.CallCount())
		}
		return
	}

	if _, _, err := del.DeliverPending(context.Background(), "sess-c"); err == nil {
		t.Fatal("expected no deliverable pending after expiry")
	}
	if act.CallCount() != 0 {
		t.Fatalf("actuator must not be called for expired; calls=%d", act.CallCount())
	}

	got, ok := del.Get("iv-exp")
	if !ok {
		t.Fatal("item should still be stored")
	}
	if got.State != adapter.StateExpired {
		t.Fatalf("state=%s want EXPIRED", got.State)
	}
}

func TestAdvisory_ObserveOnlyUnsupportedHumanAlert(t *testing.T) {
	act := adapter.NewFakeActuator()
	alerter := &adapter.RecordingAlerter{}
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		Alerter:                alerter,
		SupportsAdviceDelivery: false, // simulate missing CapAdviceDelivery
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAdvisoryDelivery: %v", err)
	}

	iv := sampleIntervention("iv-obs", "sess-d")
	if enq := del.Enqueue(iv, 0); enq.Suppressed || enq.Expired {
		t.Fatalf("bad enqueue: %+v", enq)
	}

	item, res, err := del.DeliverPending(context.Background(), "sess-d")
	if err != nil {
		t.Fatalf("DeliverPending: %v", err)
	}
	if act.CallCount() != 0 {
		t.Fatalf("observe-only must not call actuator; calls=%d", act.CallCount())
	}
	if res.ErrorClass != adapter.ErrorClassUnsupportedCapability {
		t.Fatalf("ErrorClass=%s", res.ErrorClass)
	}
	if res.AckStatus != adapter.AckStatusUnsupported {
		t.Fatalf("AckStatus=%s", res.AckStatus)
	}
	if res.DeliveryMode != adapter.DeliveryModeHumanEscalation {
		t.Fatalf("DeliveryMode=%s", res.DeliveryMode)
	}
	if item.State != adapter.StateFailed {
		t.Fatalf("state=%s want FAILED", item.State)
	}
	alerts := alerter.Snapshot()
	if len(alerts) != 1 {
		t.Fatalf("alerter calls=%d want 1", len(alerts))
	}
	if alerts[0].SessionID != "sess-d" {
		t.Fatalf("alert session=%s", alerts[0].SessionID)
	}
	if alerts[0].Intervention.InterventionID != "iv-obs" {
		t.Fatalf("alert intervention=%s", alerts[0].Intervention.InterventionID)
	}
}

func TestAdvisory_AcknowledgePaths(t *testing.T) {
	act := adapter.NewFakeActuator()
	act.AutoAck = false
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
	})
	if err != nil {
		t.Fatalf("NewAdvisoryDelivery: %v", err)
	}

	iv := sampleIntervention("iv-ack", "sess-e")
	del.Enqueue(iv, time.Minute)

	// ACK before deliver must fail (PENDING is not DELIVERING).
	if err := del.Acknowledge("iv-ack", adapter.AckStatusAcked); err == nil {
		t.Fatal("expected Acknowledge to reject PENDING pre-delivery")
	}

	item, res, err := del.DeliverPending(context.Background(), "sess-e")
	if err != nil {
		t.Fatalf("DeliverPending: %v", err)
	}
	if res.AckStatus != adapter.AckStatusPending {
		t.Fatalf("AckStatus=%s want pending", res.AckStatus)
	}
	if item.State != adapter.StateDelivering {
		t.Fatalf("state=%s want DELIVERING", item.State)
	}

	if err := del.AcknowledgeSource(adapter.AcknowledgeRequest{
		InterventionID: "iv-ack", SourceKind: "test", SourceEventID: "evt-1",
		Status: adapter.AckStatusAcked, AckLayer: adapter.ACKLayerSessionVisible,
	}); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	got, _ := del.Get("iv-ack")
	if got.State != adapter.StateSessionVisible {
		t.Fatalf("state=%s want SESSION_VISIBLE (profile ceiling)", got.State)
	}
	if got.Result == nil || got.Result.AckStatus != adapter.AckStatusAcked || got.Result.AckLayer != adapter.ACKLayerSessionVisible {
		t.Fatalf("result=%+v", got.Result)
	}
	// Bare acked must fail.
	if err := del.Acknowledge("iv-ack", adapter.AckStatusAcked); err == nil {
		t.Fatal("bare Acknowledge(acked) must refuse explicit mint")
	}
}

func TestAdvisory_ObserveOnlyNopAlerterDoesNotSilentSucceed(t *testing.T) {
	act := adapter.NewFakeActuator()
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		Alerter:                adapter.NopHumanAlerter{},
		SupportsAdviceDelivery: false,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatalf("NewAdvisoryDelivery: %v", err)
	}
	del.Enqueue(sampleIntervention("iv-nop", "sess-nop"), 0)
	_, res, err := del.DeliverPending(context.Background(), "sess-nop")
	if err == nil {
		t.Fatal("expected error when escalating with NopHumanAlerter")
	}
	if !errors.Is(err, adapter.ErrNopHumanAlerter) {
		t.Fatalf("err=%v want ErrNopHumanAlerter", err)
	}
	if res.ErrorClass != adapter.ErrorClassTransport {
		t.Fatalf("ErrorClass=%s want transport", res.ErrorClass)
	}
	if act.CallCount() != 0 {
		t.Fatalf("actuator must not be called")
	}
}

func TestAdvisory_PendingAdvisoryIDForHookDefer(t *testing.T) {
	act := adapter.NewFakeActuator()
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
	})
	if err != nil {
		t.Fatalf("NewAdvisoryDelivery: %v", err)
	}
	del.Enqueue(sampleIntervention("iv-hook", "sess-f"), time.Minute)

	id := del.Queue().PendingAdvisoryID("sess-f")
	if id != "iv-hook" {
		t.Fatalf("PendingAdvisoryID=%s", id)
	}

	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "sess-f",
		ToolName:  "write_file",
		FilePath:  "/workspace/x.go",
	}, adapter.HookPolicy{
		ScopeWhitelist:                []string{"/workspace"},
		PendingAdvisoryInterventionID: id,
	})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("expected defer while advisory pending, got %+v", dec)
	}
}
