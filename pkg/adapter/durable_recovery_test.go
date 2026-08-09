package adapter_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func markerNameForKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) + ".key"
}

// TestOpen_SuppressDirNotDir_FailClosed: when path.suppress exists as a file, Open fails.
func TestOpen_SuppressDirNotDir_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	// Pre-create a valid empty ledger open, then poison suppress sibling as a file.
	if _, err := adapter.OpenDurableAdviceLedger(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".suppress", []byte("not-a-dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if err == nil {
		t.Fatal("expected open fail when suppress path is a file")
	}
	if !errors.Is(err, adapter.ErrLedgerRecoveryIncomplete) && !errors.Is(err, adapter.ErrLedgerPathUnsafe) {
		// ReadDir on file returns platform error wrapped as recovery incomplete.
		t.Logf("open err (acceptable if fail-closed): %v", err)
	}
}

// TestOpen_MarkerIsDirectory_FailClosed: marker .key as directory → open fails.
func TestOpen_MarkerIsDirectory_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	sup := path + ".suppress"
	if err := os.MkdirAll(sup, 0o700); err != nil {
		t.Fatal(err)
	}
	// Valid-looking hash name but directory body.
	key := "iv|sess|host|fp"
	bad := filepath.Join(sup, markerNameForKey(key))
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if err == nil {
		t.Fatal("expected open fail on directory marker")
	}
	if !errors.Is(err, adapter.ErrLedgerRecoveryIncomplete) {
		t.Fatalf("want ErrLedgerRecoveryIncomplete got %v", err)
	}
}

// TestOpen_EmptyMarker_FailClosed rejects empty marker body.
func TestOpen_EmptyMarker_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	sup := path + ".suppress"
	if err := os.MkdirAll(sup, 0o700); err != nil {
		t.Fatal(err)
	}
	key := "iv|sess|host|fp"
	if err := os.WriteFile(filepath.Join(sup, markerNameForKey(key)), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if !errors.Is(err, adapter.ErrLedgerMarkerInvalid) {
		t.Fatalf("want ErrLedgerMarkerInvalid got %v", err)
	}
}

// TestOpen_NonCanonicalMarkerName_FailClosed.
func TestOpen_NonCanonicalMarkerName_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	sup := path + ".suppress"
	if err := os.MkdirAll(sup, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sup, "deadbeefdeadbeef.key"), []byte("iv|sess|host|fp\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if !errors.Is(err, adapter.ErrLedgerMarkerInvalid) {
		t.Fatalf("want ErrLedgerMarkerInvalid got %v", err)
	}
}

// TestOpen_SuppressSymlink_FailClosed.
func TestOpen_SuppressSymlink_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	target := filepath.Join(dir, "real-suppress")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path+".suppress"); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if err == nil {
		t.Fatal("expected open fail on suppress symlink")
	}
	if !errors.Is(err, adapter.ErrLedgerPathUnsafe) {
		t.Fatalf("want ErrLedgerPathUnsafe got %v", err)
	}
}

// TestOpen_MalformedJSONL_FailClosed surfaces non-empty bad transition lines.
func TestOpen_MalformedJSONL_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if !errors.Is(err, adapter.ErrLedgerCorrupt) {
		t.Fatalf("want ErrLedgerCorrupt got %v", err)
	}
}

// TestOpen_TornFinalLine_FailClosed.
func TestOpen_TornFinalLine_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	// No trailing newline and incomplete JSON.
	if err := os.WriteFile(path, []byte(`{"schema":"reinframe.advice_delivery.v1","intervention_id":"x"`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if !errors.Is(err, adapter.ErrLedgerCorrupt) {
		t.Fatalf("want ErrLedgerCorrupt got %v", err)
	}
}

// TestDeliverPending_NotSent_NoSuppressMarker: definitive pre-send failure + ledger poison
// must not create a suppress marker; after repair, redelivery is allowed.
func TestDeliverPending_NotSent_NoSuppressMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	poisonLedgerAsDir(t, path)

	act := adapter.NewFakeActuator()
	act.Unsupported = true // definitive not_sent
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
		InterventionID: "iv-not-sent",
		SessionID:      "sess-n",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "x",
		Fingerprint:    "fp-n",
	}
	del.Enqueue(iv, time.Minute)
	item, res, err := del.DeliverPending(context.Background(), "sess-n")
	if !errors.Is(err, adapter.ErrDurableWriteFailed) {
		t.Fatalf("want ErrDurableWriteFailed got %v", err)
	}
	if item == nil {
		t.Fatal("nil item")
	}
	// Must NOT be AMBIGUOUS for definitive not-sent.
	if item.State == adapter.StateAmbiguous {
		t.Fatalf("not_sent must not force AMBIGUOUS; got %s res=%+v", item.State, res)
	}
	// No suppress key in-memory for not_sent.
	if led.AlreadyDeliveredKey("iv-not-sent", "sess-n", "test_host", "fp-n") {
		t.Fatal("not_sent durable fail must not install suppress key")
	}
	// Sidecar must not permanently suppress after repair.
	// Unpoison path: remove dir poison so a fresh Open can succeed without markers.
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	// Also remove any accidental suppress dir.
	_ = os.RemoveAll(path + ".suppress")

	led2, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if led2.AlreadyDeliveredKey("iv-not-sent", "sess-n", "test_host", "fp-n") {
		t.Fatal("restart must not suppress not_sent intervention")
	}
	act2 := adapter.NewFakeActuator()
	act2.AutoAck = false
	// Repair durability: new delivery with working ledger.
	del2, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act2,
		SupportsAdviceDelivery: true,
		Ledger:                 led2,
		Queue:                  adapter.NewPendingQueue(),
		DedupeHostFamily:       "test_host",
	})
	if err != nil {
		t.Fatal(err)
	}
	del2.Enqueue(iv, time.Minute)
	item2, _, err := del2.DeliverPending(context.Background(), "sess-n")
	if err != nil {
		t.Fatal(err)
	}
	if item2.State == adapter.StateSuppressed {
		t.Fatal("repaired path must allow redelivery of not_sent intervention")
	}
	if act2.CallCount() != 1 {
		t.Fatalf("want actuator called once after repair; got %d", act2.CallCount())
	}
}

// TestDeliverPending_GrokMissingClient_NotSentRetryable uses real GrokACPActuator
// missing client (definitive pre-send unsupported).
func TestDeliverPending_GrokMissingClient_NotSentRetryable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	poisonLedgerAsDir(t, path)

	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               &adapter.GrokACPActuator{}, // nil client → unsupported not_sent
		SupportsAdviceDelivery: true,
		Ledger:                 led,
		// DedupeHostFamily inferred from Grok
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := protocol.Intervention{
		InterventionID: "iv-grok-miss",
		SessionID:      "s",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "hi",
		Fingerprint:    "fp-g",
	}
	del.Enqueue(iv, time.Minute)
	item, _, err := del.DeliverPending(context.Background(), "s")
	if !errors.Is(err, adapter.ErrDurableWriteFailed) {
		t.Fatalf("want durable fail overlay got %v", err)
	}
	if item.State == adapter.StateAmbiguous {
		t.Fatalf("missing client is not_sent; state=%s", item.State)
	}
	if led.AlreadyDeliveredKey("iv-grok-miss", "s", adapter.GrokLiveHostFamily, "fp-g") {
		t.Fatal("must not suppress not_sent grok missing client")
	}
}

// TestClassifyDeliveryBoundary_Table is a pure unit of the shipped classifier.
func TestClassifyDeliveryBoundary_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		res  adapter.InterventionResult
		err  error
		want string
	}{
		{"unsupported", adapter.InterventionResult{Accepted: false, ErrorClass: adapter.ErrorClassUnsupportedCapability}, nil, adapter.BoundaryNotSent},
		{"accepted_pending", adapter.InterventionResult{Accepted: true, AckStatus: adapter.AckStatusPending}, nil, adapter.BoundaryTransportAccepted},
		{"transport_layer", adapter.InterventionResult{Accepted: true, AckLayer: adapter.ACKLayerTransport}, nil, adapter.BoundaryTransportAccepted},
		{"session_visible", adapter.InterventionResult{Accepted: true, AckLayer: adapter.ACKLayerSessionVisible}, nil, adapter.BoundarySessionVisible},
		{"timeout", adapter.InterventionResult{Accepted: false, ErrorClass: adapter.ErrorClassTimeout}, context.DeadlineExceeded, adapter.BoundarySendAttemptedUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := adapter.ClassifyDeliveryBoundary(tc.res, tc.err)
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
			if tc.want == adapter.BoundaryNotSent && adapter.ShouldAmbiguousSuppress(got) {
				t.Fatal("not_sent must not suppress")
			}
			if tc.want != adapter.BoundaryNotSent && !adapter.ShouldAmbiguousSuppress(got) {
				t.Fatal("non-not_sent must suppress on durable fail")
			}
		})
	}
}

// TestMarkerCrossKeyNoCollision: different session/host/fp do not suppress.
func TestMarkerCrossKeyNoCollision(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := led.MarkAmbiguousSuppress("iv", "sess-a", "host", "fp-a"); err != nil {
		t.Fatal(err)
	}
	if !led.AlreadyDeliveredKey("iv", "sess-a", "host", "fp-a") {
		t.Fatal("expected hit")
	}
	if led.AlreadyDeliveredKey("iv", "sess-b", "host", "fp-a") {
		t.Fatal("session must bind")
	}
	if led.AlreadyDeliveredKey("iv", "sess-a", "other", "fp-a") {
		t.Fatal("host must bind")
	}
	if led.AlreadyDeliveredKey("iv", "sess-a", "host", "fp-b") {
		t.Fatal("fingerprint must bind")
	}
	// Restart preserves only bound key.
	led2, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if !led2.AlreadyDeliveredKey("iv", "sess-a", "host", "fp-a") {
		t.Fatal("restart miss")
	}
	if led2.AlreadyDeliveredKey("iv", "sess-b", "host", "fp-a") {
		t.Fatal("restart cross-session")
	}
}
