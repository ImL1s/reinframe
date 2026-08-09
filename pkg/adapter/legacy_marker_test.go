package adapter_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func legacyMarkerName(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:16]) + ".key"
}

// TestOpen_LegacyPipeMarker_MigratesAndSuppresses proves #207–#211 pipe markers
// with exactly three separators migrate to JSON-array keys and remain suppress hits.
func TestOpen_LegacyPipeMarker_MigratesAndSuppresses(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	sup := path + ".suppress"
	if err := os.MkdirAll(sup, 0o700); err != nil {
		t.Fatal(err)
	}
	// Old body form: intervention|session|host|fingerprint
	legacy := "iv-legacy|sess-a|test_host|fp-a"
	name := legacyMarkerName(legacy)
	if err := os.WriteFile(filepath.Join(sup, name), []byte(legacy+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	led, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatalf("open with legacy marker: %v", err)
	}
	if !led.AlreadyDeliveredKey("iv-legacy", "sess-a", "test_host", "fp-a") {
		t.Fatal("migrated legacy key must suppress bound intervention")
	}
	// New canonical marker file should exist; legacy filename may be removed.
	newKeyBody, _ := jsonArrayKey("iv-legacy", "sess-a", "test_host", "fp-a")
	newName := legacyMarkerName(newKeyBody)
	if _, err := os.Stat(filepath.Join(sup, newName)); err != nil {
		t.Fatalf("expected migrated marker file %s: %v", newName, err)
	}
}

// TestOpen_LegacyPipeMarker_AmbiguousPipes_FailClosed: fingerprint containing |
// yields more than 3 separators → fail closed (no silent load).
func TestOpen_LegacyPipeMarker_AmbiguousPipes_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	sup := path + ".suppress"
	if err := os.MkdirAll(sup, 0o700); err != nil {
		t.Fatal(err)
	}
	// 4 pipes → ambiguous (fingerprint has |).
	legacy := "iv|sess|host|fp|extra"
	name := legacyMarkerName(legacy)
	if err := os.WriteFile(filepath.Join(sup, name), []byte(legacy+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if err == nil {
		t.Fatal("expected fail closed on ambiguous pipe marker")
	}
	if !errors.Is(err, adapter.ErrLedgerMarkerInvalid) && !errors.Is(err, adapter.ErrLedgerRecoveryIncomplete) {
		t.Logf("got err=%v (acceptable if fail-closed)", err)
	}
}

// TestOpen_MissingTrailingNewline_FailClosed even when last record parses.
func TestOpen_MissingTrailingNewline_FailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	// Valid JSON transition but no trailing newline.
	line := `{"schema":"reinframe.advice_delivery.v1","intervention_id":"iv","session_id":"s","to_state":"TRANSPORT_ACCEPTED","host_family":"h","fingerprint":"f"}`
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := adapter.OpenDurableAdviceLedger(path)
	if !errors.Is(err, adapter.ErrLedgerCorrupt) {
		t.Fatalf("want ErrLedgerCorrupt for missing newline got %v", err)
	}
}

func jsonArrayKey(a, b, c, d string) (string, error) {
	raw, err := json.Marshal([4]string{a, b, c, d})
	return string(raw), err
}
