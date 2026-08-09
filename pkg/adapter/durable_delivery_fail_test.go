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

// TestDeliverPending_LedgerWriteFailure_ReturnsAmbiguous drives the real
// AdvisoryDelivery.DeliverPending path with a ledger path that cannot be written
// after the host actuator has already succeeded.
func TestDeliverPending_LedgerWriteFailure_ReturnsAmbiguous(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Parent path will be a regular file so OpenFile on .../file/ledger.jsonl fails.
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	badLedgerPath := filepath.Join(blocker, "ledger.jsonl")

	// Open succeeds when path does not exist yet (no symlink). Then we poison via Path.
	// Use a valid open first, then redirect Path to the unwritable location.
	goodPath := filepath.Join(dir, "good.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(goodPath)
	if err != nil {
		t.Fatal(err)
	}
	// Redirect subsequent Append writes to a path whose parent is a file → write fails.
	// Access Path via re-open pattern: construct delivery with poisoned ledger by
	// writing through a ledger that we replace after open.
	// Direct field: Open returns *DurableAdviceLedger with Path; tests in package adapter_test
	// cannot set unexported fields — use a path that Append will fail on:
	// OpenDurableAdviceLedger does not require parent to exist; Append does MkdirAll then OpenFile.
	// If parent component is a file, MkdirAll fails / OpenFile fails.
	led2, err := adapter.OpenDurableAdviceLedger(badLedgerPath)
	if err != nil {
		// Open may succeed (path absent). Append must fail.
		t.Logf("open err (ok if nil): %v", err)
	}
	if led2 == nil {
		// If open rejects, still prove DeliverPending surfaces durable failure via led with bad path.
		// Re-open good then we need exported way — use badLedgerPath open that succeeds with non-existing parents.
		// MkdirAll(".../not-a-dir") fails when not-a-dir is a file.
		led2, err = adapter.OpenDurableAdviceLedger(filepath.Join(dir, "nested", "l.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		// Point to bad path by recording through a custom approach:
		// Write once to establish, then replace parent with file before second write.
		_ = led
	}

	// Canonical portable approach: ledger under a directory we later replace with a file.
	base := filepath.Join(dir, "ledger-root")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "advice.jsonl")
	led3, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	// Remove directory and put a file in its place so next Append fails.
	if err := os.RemoveAll(base); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}

	act := adapter.NewFakeActuator()
	act.AutoAck = false
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led3,
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
	base := filepath.Join(dir, "ack-root")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "ack.jsonl")
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
	// Poison ledger path.
	if err := os.RemoveAll(base); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
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
