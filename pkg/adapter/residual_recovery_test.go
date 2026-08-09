package adapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// TestOpen_TmpMarker_PromotesToKey: crash-window .tmp with valid body is promoted and suppresses.
func TestOpen_TmpMarker_PromotesToKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	sup := path + ".suppress"
	if err := os.MkdirAll(sup, 0o700); err != nil {
		t.Fatal(err)
	}
	key := boundKey("iv-tmp", "sess-t", "host-t", "fp-t")
	tmp := filepath.Join(sup, markerNameForKey(key)+".tmp")
	if err := os.WriteFile(tmp, []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatalf("open should promote valid .tmp: %v", err)
	}
	if !led.AlreadyDeliveredKey("iv-tmp", "sess-t", "host-t", "fp-t") {
		t.Fatal("promoted .tmp must restore suppress key")
	}
	// Final .key must exist; .tmp must be gone.
	if _, err := os.Stat(filepath.Join(sup, markerNameForKey(key))); err != nil {
		t.Fatalf("final .key missing after promote: %v", err)
	}
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Fatalf(".tmp should be renamed away; err=%v", err)
	}
}

// TestOpen_MalformedTmp_FailClosed.
func TestOpen_MalformedTmp_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	sup := path + ".suppress"
	if err := os.MkdirAll(sup, 0o700); err != nil {
		t.Fatal(err)
	}
	// Valid-looking temp name for a real key but garbage body.
	key := boundKey("iv", "s", "h", "f")
	tmp := filepath.Join(sup, markerNameForKey(key)+".tmp")
	if err := os.WriteFile(tmp, []byte("not-a-valid-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if err == nil {
		t.Fatal("expected fail-closed on malformed .tmp")
	}
	if !errors.Is(err, adapter.ErrLedgerRecoveryIncomplete) && !errors.Is(err, adapter.ErrLedgerMarkerInvalid) {
		t.Fatalf("want recovery incomplete or marker invalid; got %v", err)
	}
}

// TestOpen_JSONL_UnknownToState_FailClosed.
func TestOpen_JSONL_UnknownToState_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	line, _ := json.Marshal(map[string]any{
		"schema":          "reinframe.advice_delivery.v1",
		"intervention_id": "iv-x",
		"session_id":      "s",
		"to_state":        "NOT_A_REAL_STATE",
		"host_family":     "h",
		"fingerprint":     "f",
	})
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if !errors.Is(err, adapter.ErrLedgerCorrupt) {
		t.Fatalf("want ErrLedgerCorrupt got %v", err)
	}
}

// TestOpen_JSONL_UnknownSchema_FailClosed.
func TestOpen_JSONL_UnknownSchema_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	line, _ := json.Marshal(map[string]any{
		"schema":          "reinframe.advice_delivery.v999",
		"intervention_id": "iv-x",
		"session_id":      "s",
		"to_state":        "TRANSPORT_ACCEPTED",
	})
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if !errors.Is(err, adapter.ErrLedgerCorrupt) {
		t.Fatalf("want ErrLedgerCorrupt got %v", err)
	}
}

// TestOpen_JSONL_EmptyOrMissingSchema_FailClosed: empty/omitted schema cannot
// silently load suppress knowledge (AC3 / skeptic residual after #212).
func TestOpen_JSONL_EmptyOrMissingSchema_FailClosed(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"empty", "omitted"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "advice.jsonl")
			rec := map[string]any{
				"intervention_id": "iv-suppress",
				"session_id":      "sess-s",
				"to_state":        "TRANSPORT_ACCEPTED",
				"host_family":     "h",
				"fingerprint":     "fp",
			}
			if name == "empty" {
				rec["schema"] = ""
			}
			// omitted: no schema key at all
			line, err := json.Marshal(rec)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			led, err := adapter.OpenDurableAdviceLedger(path)
			if err == nil {
				// Must not open successfully with suppress knowledge from schemaless line.
				if led != nil && led.AlreadyDeliveredKey("iv-suppress", "sess-s", "h", "fp") {
					t.Fatal("empty/omitted schema must not restore suppress key")
				}
				t.Fatal("expected Open fail-closed on empty/omitted schema")
			}
			if !errors.Is(err, adapter.ErrLedgerCorrupt) {
				t.Fatalf("want ErrLedgerCorrupt got %v", err)
			}
		})
	}
}

// TestOpen_JSONL_SuppressMissingSession_FailClosed.
func TestOpen_JSONL_SuppressMissingSession_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	line, _ := json.Marshal(map[string]any{
		"schema":          "reinframe.advice_delivery.v1",
		"intervention_id": "iv-x",
		"to_state":        "AMBIGUOUS",
	})
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if !errors.Is(err, adapter.ErrLedgerCorrupt) {
		t.Fatalf("want ErrLedgerCorrupt got %v", err)
	}
}

// TestDedupeKey_PipeInComponent_RoundTrip: components may contain '|'.
func TestDedupeKey_PipeInComponent_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := led.MarkAmbiguousSuppress("iv|weird", "sess|a", "host", "fp|x"); err != nil {
		t.Fatal(err)
	}
	led2, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if !led2.AlreadyDeliveredKey("iv|weird", "sess|a", "host", "fp|x") {
		t.Fatal("pipe-containing components must survive restart")
	}
	if led2.AlreadyDeliveredKey("iv", "weird", "sess|a", "host") {
		t.Fatal("must not collide across pipe boundaries")
	}
}

// TestDeliverPending_FileActuator_EmptyPath_NotSent: pre-send local fail stays retryable.
func TestDeliverPending_FileActuator_EmptyPath_NotSent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	poisonLedgerAsDir(t, path)

	act := &adapter.FileActuator{Path: ""} // definitive pre-send
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led,
		DedupeHostFamily:       "file_host",
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := protocol.Intervention{
		InterventionID: "iv-file-empty",
		SessionID:      "sf",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "hi",
		Fingerprint:    "fp-f",
	}
	del.Enqueue(iv, time.Minute)
	item, res, err := del.DeliverPending(context.Background(), "sf")
	if !errors.Is(err, adapter.ErrDurableWriteFailed) {
		t.Fatalf("want durable fail overlay got %v res=%+v", err, res)
	}
	if item.State == adapter.StateAmbiguous {
		t.Fatalf("pre-send file path empty must not force AMBIGUOUS; state=%s boundary=%s", item.State, res.DeliveryBoundary)
	}
	if led.AlreadyDeliveredKey("iv-file-empty", "sf", "file_host", "fp-f") {
		t.Fatal("must not install suppress for FileActuator pre-send")
	}
	// Repair and retry.
	_ = os.RemoveAll(path)
	led2, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "channel.jsonl")
	act2 := &adapter.FileActuator{Path: outPath}
	del2, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act2,
		SupportsAdviceDelivery: true,
		Ledger:                 led2,
		Queue:                  adapter.NewPendingQueue(),
		DedupeHostFamily:       "file_host",
	})
	if err != nil {
		t.Fatal(err)
	}
	del2.Enqueue(iv, time.Minute)
	item2, _, err := del2.DeliverPending(context.Background(), "sf")
	if err != nil {
		t.Fatal(err)
	}
	if item2.State == adapter.StateSuppressed {
		t.Fatal("repaired file path must allow redelivery")
	}
	if act2.CallCount() != 1 {
		t.Fatalf("want successful write after repair; calls=%d", act2.CallCount())
	}
}
