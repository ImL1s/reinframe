package adapter_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// poisonLedgerAsDir makes the ledger JSONL path a directory so Append OpenFile fails,
// while a sibling sidecar path (path+".suppress") remains writable under the parent.
func poisonLedgerAsDir(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

// TestDeliverPending_LedgerWriteFailure_ReturnsAmbiguous drives the real
// AdvisoryDelivery.DeliverPending path with a ledger path that cannot be written
// after the host actuator has already succeeded.
func TestDeliverPending_LedgerWriteFailure_ReturnsAmbiguous(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	poisonLedgerAsDir(t, path)

	act := adapter.NewFakeActuator()
	act.AutoAck = false
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led,
		DedupeHostFamily:       "test_host",
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := protocol.Intervention{
		InterventionID: "iv-dur-fail",
		SessionID:      "sess-d",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "hi",
		Fingerprint:    "fp-action-1",
	}
	del.Enqueue(iv, time.Minute)
	item, res, err := del.DeliverPending(context.Background(), "sess-d")
	if err == nil {
		t.Fatal("expected durable write error after host deliver")
	}
	if !errors.Is(err, adapter.ErrDurableWriteFailed) {
		t.Fatalf("want ErrDurableWriteFailed got %v", err)
	}
	if item == nil || item.State != adapter.StateAmbiguous {
		t.Fatalf("want StateAmbiguous got %+v", item)
	}
	if res.ErrorClass == "" {
		t.Fatalf("expected error class on result: %+v", res)
	}
	// Host actuator was still invoked (not a silent skip).
	if act.CallCount() != 1 {
		t.Fatalf("actuator calls=%d want 1", act.CallCount())
	}
}

// TestDeliverPending_AmbiguousMarker_RestartSuppress proves that when the JSONL
// path cannot commit after host accept, the sidecar suppress marker prevents
// redelivery after process-sim restart (new Open + new queue).
func TestDeliverPending_AmbiguousMarker_RestartSuppress(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	poisonLedgerAsDir(t, path)

	act := adapter.NewFakeActuator()
	act.AutoAck = false
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led,
		DedupeHostFamily:       "test_host",
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := protocol.Intervention{
		InterventionID: "iv-amb-restart",
		SessionID:      "sess-r",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "once",
		Fingerprint:    "fp-restart",
	}
	del.Enqueue(iv, time.Minute)
	item, _, err := del.DeliverPending(context.Background(), "sess-r")
	if !errors.Is(err, adapter.ErrDurableWriteFailed) {
		t.Fatalf("want ErrDurableWriteFailed got %v", err)
	}
	if item == nil || item.State != adapter.StateAmbiguous {
		t.Fatalf("want AMBIGUOUS got %+v", item)
	}
	if act.CallCount() != 1 {
		t.Fatalf("calls=%d", act.CallCount())
	}

	// Process-sim restart: reopen ledger (loads sidecar markers), fresh queue.
	led2, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if !led2.AlreadyDeliveredKey("iv-amb-restart", "sess-r", "test_host", "fp-restart") {
		t.Fatal("restart Open must load ambiguous suppress marker for bound key")
	}
	del2, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led2,
		Queue:                  adapter.NewPendingQueue(),
		DedupeHostFamily:       "test_host",
	})
	if err != nil {
		t.Fatal(err)
	}
	del2.Enqueue(iv, time.Minute)
	item2, res2, err := del2.DeliverPending(context.Background(), "sess-r")
	if err != nil {
		t.Fatal(err)
	}
	if item2.State != adapter.StateSuppressed {
		t.Fatalf("want SUPPRESSED after restart got %s res=%+v", item2.State, res2)
	}
	if act.CallCount() != 1 {
		t.Fatalf("actuator must not redeliver after restart; calls=%d", act.CallCount())
	}
}

// TestNewAdvisoryDelivery_LedgerRequiresHostFamily fails closed when Ledger is set
// without DedupeHostFamily and the actuator does not advertise a host pin.
func TestNewAdvisoryDelivery_LedgerRequiresHostFamily(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	led, err := adapter.OpenDurableAdviceLedger(filepath.Join(dir, "l.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               adapter.NewFakeActuator(),
		SupportsAdviceDelivery: true,
		Ledger:                 led,
		// DedupeHostFamily intentionally empty; FakeActuator has no HostFamily().
	})
	if err == nil {
		t.Fatal("expected error when Ledger set without host family")
	}
}

// TestNewAdvisoryDelivery_GrokInfersDedupeHostFamily defaults host from Grok actuator.
func TestNewAdvisoryDelivery_GrokInfersDedupeHostFamily(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "l.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-seed a suppress key using Grok host family (what Grok actuator writes).
	if err := led.RecordResultWithSource(adapter.StateDelivering, "s", adapter.InterventionResult{
		InterventionID: "iv-g",
		HostFamily:     adapter.GrokLiveHostFamily,
		AckLayer:       adapter.ACKLayerTransport,
		AckStatus:      adapter.AckStatusPending,
	}, adapter.StateTransportAccepted, "", "", "", "fp-g"); err != nil {
		t.Fatal(err)
	}
	// Empty DedupeHostFamily — must infer grok_build from *GrokACPActuator.
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               &adapter.GrokACPActuator{}, // nil client; only used for host pin + we suppress before Deliver
		SupportsAdviceDelivery: true,
		Ledger:                 led,
	})
	if err != nil {
		t.Fatalf("Grok actuator should infer DedupeHostFamily: %v", err)
	}
	del.Enqueue(protocol.Intervention{
		InterventionID: "iv-g",
		SessionID:      "s",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "x",
		Fingerprint:    "fp-g",
	}, time.Minute)
	item, _, err := del.DeliverPending(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if item.State != adapter.StateSuppressed {
		t.Fatalf("want SUPPRESSED via inferred host family; got %s", item.State)
	}
}

// TestLedgerDedupe_ActionFingerprintBound proves restart suppress uses fingerprint,
// not bare InterventionID alone.
func TestLedgerDedupe_ActionFingerprintBound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "l.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	res := adapter.InterventionResult{
		InterventionID: "iv-same",
		HostFamily:     "test_host",
		AckStatus:      adapter.AckStatusPending,
		AckLayer:       adapter.ACKLayerTransport,
	}
	if err := led.RecordResultWithSource(adapter.StateDelivering, "sess-a", res, adapter.StateTransportAccepted, "", "", "", "fp-A"); err != nil {
		t.Fatal(err)
	}
	// Same intervention + session + host + fingerprint → suppressed.
	if !led.AlreadyDeliveredKey("iv-same", "sess-a", "test_host", "fp-A") {
		t.Fatal("expected bound key hit")
	}
	// Different fingerprint (different action) → not suppressed.
	if led.AlreadyDeliveredKey("iv-same", "sess-a", "test_host", "fp-B") {
		t.Fatal("different action fingerprint must not suppress")
	}
	// Bare AlreadyDelivered without matching bound key should not hit (no bare-ID store).
	if led.AlreadyDelivered("iv-same") {
		// AlreadyDelivered uses empty session/host/fp — only hits if that exact key was stored.
		// We stored fingerprint fp-A, so empty fp key should miss.
		t.Fatal("bare AlreadyDelivered must not match action-bound key")
	}

	// Restart load preserves bound key.
	led2, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if !led2.AlreadyDeliveredKey("iv-same", "sess-a", "test_host", "fp-A") {
		t.Fatal("restart must restore bound dedupe key")
	}
	if led2.AlreadyDeliveredKey("iv-same", "sess-a", "test_host", "fp-B") {
		t.Fatal("restart must not suppress different fingerprint")
	}
}

// TestAcknowledgeSource_LedgerWriteFailure does not advance memory without durable commit.
func TestAcknowledgeSource_LedgerWriteFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ack.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	act := adapter.NewFakeActuator()
	act.AutoAck = false
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led,
		DedupeHostFamily:       "test_host",
	})
	if err != nil {
		t.Fatal(err)
	}
	del.Enqueue(protocol.Intervention{
		InterventionID: "iv-ack-fail",
		SessionID:      "s",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "a",
		Fingerprint:    "fp1",
	}, time.Minute)
	item, _, err := del.DeliverPending(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if item.State != adapter.StateDelivering && item.State != adapter.StateTransportAccepted && item.State != adapter.StateSessionVisible {
		// Fake pending → DELIVERING
		if item.State != adapter.StateDelivering {
			t.Fatalf("state=%s", item.State)
		}
	}
	// Poison ledger path after successful first write.
	poisonLedgerAsDir(t, path)
	err = del.AcknowledgeSource(adapter.AcknowledgeRequest{
		InterventionID: "iv-ack-fail",
		HostFamily:     "test_host",
		SourceKind:     "test",
		SourceEventID:  "e1",
		Status:         adapter.AckStatusAcked,
		AckLayer:       adapter.ACKLayerSessionVisible,
	})
	if !errors.Is(err, adapter.ErrDurableWriteFailed) {
		t.Fatalf("want ErrDurableWriteFailed got %v", err)
	}
	// Memory must not show successful source-bound ACK state (durable-first).
	got, _ := del.Get("iv-ack-fail")
	if got != nil && got.State == adapter.StateSessionVisible {
		t.Fatalf("memory must not advance to SESSION_VISIBLE when ledger write fails; got %s", got.State)
	}
}
