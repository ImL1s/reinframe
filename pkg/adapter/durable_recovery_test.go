package adapter_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	// Assert no suppress marker was written on disk before cleanup.
	if entries, err := os.ReadDir(path + ".suppress"); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".key") {
				t.Fatalf("not_sent must not write suppress marker; found %s", e.Name())
			}
		}
	}
	// Unpoison JSONL path for repair path; leave .suppress as-is if empty.
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}

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
		{"agent_rejected", adapter.InterventionResult{Accepted: false, ErrorClass: adapter.ErrorClassAgentRejected, AckStatus: adapter.AckStatusRejected}, nil, adapter.BoundaryTransportAccepted},
		// Grok SessionPrompt failure shape (shipped grok_advice_actuator.go):
		// ErrorClassTransport + AckStatusRejected + Accepted=false → send_attempted_unknown.
		{"grok_session_prompt_fail", adapter.InterventionResult{
			Accepted: false, ErrorClass: adapter.ErrorClassTransport, AckStatus: adapter.AckStatusRejected, AckLayer: adapter.ACKLayerNone,
		}, errors.New("session/prompt: boom"), adapter.BoundarySendAttemptedUnknown},
		{"post_send_transport_fail", adapter.InterventionResult{Accepted: false, ErrorClass: adapter.ErrorClassTransport, AckStatus: adapter.AckStatusPending}, errors.New("rpc"), adapter.BoundarySendAttemptedUnknown},
		// Pre-send Grok locals use unsupported_capability only.
		{"grok_privacy_style", adapter.InterventionResult{Accepted: false, ErrorClass: adapter.ErrorClassUnsupportedCapability, AckStatus: adapter.AckStatusRejected}, errors.New("privacy"), adapter.BoundaryNotSent},
		{"grok_session_mismatch", adapter.InterventionResult{Accepted: false, ErrorClass: adapter.ErrorClassUnsupportedCapability, AckStatus: adapter.AckStatusRejected}, errors.New("session mismatch"), adapter.BoundaryNotSent},
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

// TestDeliverPending_SessionPromptFail_PoisonLedger_SuppressesRestart drives the
// real DeliverPending path with the exact Grok SessionPrompt-failure result shape
// (Transport + Rejected + Accepted=false). Durable poison must install AMBIGUOUS
// suppress marker; restart must not redeliver.
func TestDeliverPending_SessionPromptFail_PoisonLedger_SuppressesRestart(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	poisonLedgerAsDir(t, path)

	// Mirror grok_advice_actuator SessionPrompt failure result exactly.
	act := adapter.NewFakeActuator()
	act.ResultHook = func(_ context.Context, intervention protocol.Intervention) (adapter.InterventionResult, error) {
		return adapter.InterventionResult{
			InterventionID: intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   adapter.DefaultDeliveryMode(intervention.ActionType),
			AckStatus:      adapter.AckStatusRejected,
			ErrorClass:     adapter.ErrorClassTransport,
			AckLayer:       adapter.ACKLayerNone,
			HostFamily:     adapter.GrokLiveHostFamily,
			Message:        "session/prompt: simulated host failure",
		}, errors.New("session/prompt: simulated host failure")
	}

	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led,
		DedupeHostFamily:       adapter.GrokLiveHostFamily,
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := protocol.Intervention{
		InterventionID: "iv-prompt-fail",
		SessionID:      "sess-pf",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "replan",
		Fingerprint:    "fp-prompt-fail",
	}
	// Boundary must classify Grok prompt-fail shape as send_attempted_unknown.
	boundary := adapter.ClassifyDeliveryBoundary(adapter.InterventionResult{
		Accepted: false, ErrorClass: adapter.ErrorClassTransport, AckStatus: adapter.AckStatusRejected, AckLayer: adapter.ACKLayerNone,
	}, errors.New("session/prompt"))
	if boundary != adapter.BoundarySendAttemptedUnknown {
		t.Fatalf("SessionPrompt-fail shape want send_attempted_unknown got %s", boundary)
	}
	if !adapter.ShouldAmbiguousSuppress(boundary) {
		t.Fatal("send_attempted_unknown must suppress on durable fail")
	}

	del.Enqueue(iv, time.Minute)
	item, res, err := del.DeliverPending(context.Background(), "sess-pf")
	if !errors.Is(err, adapter.ErrDurableWriteFailed) {
		t.Fatalf("want ErrDurableWriteFailed got %v", err)
	}
	if item == nil || item.State != adapter.StateAmbiguous {
		t.Fatalf("post-send durable fail want AMBIGUOUS got %+v res=%+v", item, res)
	}
	if !led.AlreadyDeliveredKey("iv-prompt-fail", "sess-pf", adapter.GrokLiveHostFamily, "fp-prompt-fail") {
		t.Fatal("in-memory suppress key required after SessionPrompt-fail + durable fail")
	}
	// Sidecar marker must exist on disk for restart.
	sup := path + ".suppress"
	entries, err := os.ReadDir(sup)
	if err != nil {
		t.Fatalf("suppress dir: %v", err)
	}
	foundKey := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".key") {
			foundKey = true
			break
		}
	}
	if !foundKey {
		t.Fatal("expected on-disk suppress marker after SessionPrompt-fail + durable fail")
	}
	if act.CallCount() != 1 {
		t.Fatalf("calls=%d", act.CallCount())
	}

	// Process-sim restart: Open loads marker; redelivery suppressed.
	led2, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if !led2.AlreadyDeliveredKey("iv-prompt-fail", "sess-pf", adapter.GrokLiveHostFamily, "fp-prompt-fail") {
		t.Fatal("restart Open must load suppress marker for SessionPrompt-fail path")
	}
	del2, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led2,
		Queue:                  adapter.NewPendingQueue(),
		DedupeHostFamily:       adapter.GrokLiveHostFamily,
	})
	if err != nil {
		t.Fatal(err)
	}
	del2.Enqueue(iv, time.Minute)
	item2, res2, err := del2.DeliverPending(context.Background(), "sess-pf")
	if err != nil {
		t.Fatal(err)
	}
	if item2.State != adapter.StateSuppressed {
		t.Fatalf("want SUPPRESSED after restart got %s res=%+v", item2.State, res2)
	}
	if act.CallCount() != 1 {
		t.Fatalf("must not redeliver after restart; calls=%d", act.CallCount())
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
