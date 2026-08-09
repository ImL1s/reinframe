package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// DurableAdviceLedger is an append-only, user-private JSONL ledger of advice
// delivery transitions (#108). Source advice channel history is never rewritten.
//
// Schema: reinframe.advice_delivery.v1
type DurableAdviceLedger struct {
	Path string

	mu     sync.Mutex
	seen   map[string]struct{} // bound dedupe keys that suppress redelivery
	cursor int64               // bytes written (restart: file size)
}

// DeliveryTransition is one closed, versioned append-only record.
type DeliveryTransition struct {
	Schema         string    `json:"schema"` // reinframe.advice_delivery.v1
	At             time.Time `json:"at"`
	InterventionID string    `json:"intervention_id"`
	SessionID      string    `json:"session_id"`
	FromState      string    `json:"from_state,omitempty"`
	ToState        string    `json:"to_state"`
	AckLayer       string    `json:"ack_layer,omitempty"`
	AckStatus      string    `json:"ack_status,omitempty"`
	HostFamily     string    `json:"host_family,omitempty"`
	HostVersion    string    `json:"host_version,omitempty"`
	Profile        string    `json:"profile,omitempty"`
	SafeBoundary   string    `json:"safe_boundary,omitempty"`
	TargetSession  string    `json:"target_session_id,omitempty"`
	CapsDigest     string    `json:"caps_digest,omitempty"`
	Message        string    `json:"message,omitempty"`
	SourceKind     string    `json:"source_kind,omitempty"`
	SourceEventID  string    `json:"source_event_id,omitempty"`
	CorrelationID  string    `json:"correlation_id,omitempty"`
	Fingerprint    string    `json:"fingerprint,omitempty"`
}

// Recovery / integrity errors (#208).
var (
	ErrLedgerRecoveryIncomplete = errors.New("durable advice ledger: suppress recovery incomplete")
	ErrLedgerCorrupt            = errors.New("durable advice ledger: corrupt transition record")
	ErrLedgerMarkerInvalid      = errors.New("durable advice ledger: invalid suppress marker")
	ErrLedgerPathUnsafe         = errors.New("durable advice ledger: unsafe path")
)

// maxSuppressMarkerBytes bounds a single sidecar marker body.
const maxSuppressMarkerBytes = 4096

// OpenDurableAdviceLedger loads existing IDs from path (if present) for restart dedupe.
// Rejects symlink/reparse paths (#200). Fail-closed on incomplete suppress recovery (#208).
func OpenDurableAdviceLedger(path string) (*DurableAdviceLedger, error) {
	if path == "" {
		return nil, fmt.Errorf("durable advice ledger: path required")
	}
	if err := rejectLedgerSymlink(path); err != nil {
		return nil, err
	}
	l := &DurableAdviceLedger{
		Path: path,
		seen: make(map[string]struct{}),
	}
	b, err := os.ReadFile(path)
	if err != nil {
		// Missing JSONL is fine (first run). Path-as-directory (poisoned write path)
		// still loads sidecar suppress markers so restart does not silently redeliver.
		if !os.IsNotExist(err) {
			if fi, se := os.Lstat(path); se != nil || !fi.IsDir() {
				return nil, err
			}
		}
	} else {
		l.cursor = int64(len(b))
		if err := l.ingestJSONL(b); err != nil {
			return nil, err
		}
	}
	// Fail-closed: incomplete suppress recovery must not yield a usable ledger.
	if err := l.loadAmbiguousMarkersUnlocked(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *DurableAdviceLedger) ingestJSONL(b []byte) error {
	// Torn tail: non-empty file without trailing newline on last non-empty byte → incomplete write.
	if len(b) > 0 && b[len(b)-1] != '\n' {
		// Allow pure empty; otherwise last line is torn if it does not parse as full transition.
		lines := splitLines(b)
		if len(lines) > 0 && len(lines[len(lines)-1]) > 0 {
			var tr DeliveryTransition
			if json.Unmarshal(lines[len(lines)-1], &tr) != nil {
				return fmt.Errorf("%w: torn final line", ErrLedgerCorrupt)
			}
		}
	}
	for _, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var tr DeliveryTransition
		if err := json.Unmarshal(line, &tr); err != nil {
			return fmt.Errorf("%w: malformed transition line", ErrLedgerCorrupt)
		}
		if tr.InterventionID == "" {
			// Empty id on a parseable line cannot affect suppress state — skip.
			continue
		}
		// Only states that prove host accepted transport (or terminal no-retry) suppress redelivery.
		// DELIVERING alone does not suppress — crash mid-flight should allow retry.
		if isSuppressState(DeliveryState(tr.ToState)) {
			l.seen[dedupeKey(tr.InterventionID, tr.SessionID, tr.HostFamily, tr.Fingerprint)] = struct{}{}
		}
	}
	return nil
}

// suppressSidecarDir is a sibling of the JSONL path used when Append cannot commit
// after host acceptance. Must not share a poisoned parent with the main file when
// the main path itself is the failure mode (e.g. path is a directory).
func (l *DurableAdviceLedger) suppressSidecarDir() string {
	return l.Path + ".suppress"
}

func suppressMarkerFilename(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) + ".key"
}

// loadAmbiguousMarkersUnlocked reads sidecar suppress keys into seen.
// Open is single-threaded before the ledger is returned to callers.
// Returns error on incomplete recovery (fail-closed #208).
func (l *DurableAdviceLedger) loadAmbiguousMarkersUnlocked() error {
	dir := l.suppressSidecarDir()
	if err := rejectLedgerSymlink(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrLedgerPathUnsafe, err)
	}
	// Explicit type check: a non-directory suppress path is incomplete recovery
	// (portable across platforms where ReadDir(file) errors differ).
	if fi, err := os.Lstat(dir); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("%w: suppress path is not a directory", ErrLedgerRecoveryIncomplete)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("%w: suppress path: %v", ErrLedgerRecoveryIncomplete, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%w: suppress dir: %v", ErrLedgerRecoveryIncomplete, err)
	}
	for _, e := range entries {
		name := e.Name()
		// Ignore temp files from atomic write.
		if strings.HasSuffix(name, ".tmp") {
			continue
		}
		if e.IsDir() {
			// Directory where a marker file is required → incomplete recovery.
			if strings.HasSuffix(name, ".key") {
				return fmt.Errorf("%w: marker %s is a directory", ErrLedgerRecoveryIncomplete, name)
			}
			continue
		}
		if !strings.HasSuffix(name, ".key") {
			continue
		}
		full := filepath.Join(dir, name)
		if err := rejectLedgerSymlink(full); err != nil {
			return fmt.Errorf("%w: %v", ErrLedgerPathUnsafe, err)
		}
		b, rerr := os.ReadFile(full)
		if rerr != nil {
			return fmt.Errorf("%w: marker %s: %v", ErrLedgerRecoveryIncomplete, name, rerr)
		}
		if len(b) == 0 || len(b) > maxSuppressMarkerBytes {
			return fmt.Errorf("%w: marker size", ErrLedgerMarkerInvalid)
		}
		if !utf8.Valid(b) {
			return fmt.Errorf("%w: marker encoding", ErrLedgerMarkerInvalid)
		}
		key := strings.TrimSpace(string(b))
		if key == "" || strings.ContainsRune(key, 0) {
			return fmt.Errorf("%w: empty or null key", ErrLedgerMarkerInvalid)
		}
		// Canonical name must match hash of key (prevents rename/collision games).
		if name != suppressMarkerFilename(key) {
			return fmt.Errorf("%w: marker name not canonical for key", ErrLedgerMarkerInvalid)
		}
		// Bound key shape: intervention|session|host|fingerprint (exactly 3 pipes).
		if strings.Count(key, "|") != 3 {
			return fmt.Errorf("%w: key shape", ErrLedgerMarkerInvalid)
		}
		l.seen[key] = struct{}{}
	}
	return nil
}

// MarkAmbiguousSuppress records a bound suppress key when the JSONL path cannot be
// written after the host may have accepted delivery. Survives process restart via sidecar.
func (l *DurableAdviceLedger) MarkAmbiguousSuppress(interventionID, sessionID, hostFamily, fingerprint string) error {
	if l == nil {
		return fmt.Errorf("nil ledger")
	}
	if interventionID == "" {
		return fmt.Errorf("intervention id required")
	}
	key := dedupeKey(interventionID, sessionID, hostFamily, fingerprint)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen[key] = struct{}{}
	dir := l.suppressSidecarDir()
	if err := rejectLedgerSymlink(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrLedgerPathUnsafe, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, suppressMarkerFilename(key))
	// Atomic-ish: write temp then rename within same dir.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// AlreadyDelivered reports whether InterventionID has a durable delivery transition.
// Prefer AlreadyDeliveredKey for session/host/action-bound dedupe (#200).
func (l *DurableAdviceLedger) AlreadyDelivered(interventionID string) bool {
	return l.AlreadyDeliveredKey(interventionID, "", "", "")
}

// AlreadyDeliveredKey checks a bound dedupe key (intervention|session|host|fingerprint).
// Fingerprint is the action identity; empty fingerprint is a distinct key from non-empty.
func (l *DurableAdviceLedger) AlreadyDeliveredKey(interventionID, sessionID, hostFamily, fingerprint string) bool {
	if l == nil || interventionID == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.seen[dedupeKey(interventionID, sessionID, hostFamily, fingerprint)]
	return ok
}

func dedupeKey(interventionID, sessionID, hostFamily, fingerprint string) string {
	return interventionID + "|" + sessionID + "|" + hostFamily + "|" + fingerprint
}

func rejectLedgerSymlink(path string) error {
	// If the leaf exists and is a symlink, reject.
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("durable advice ledger: path is a symlink")
		}
	}
	// Also reject if any parent is a symlink (best-effort).
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("durable advice ledger: parent path is a symlink")
		}
	}
	return nil
}

// Append records a transition and marks the InterventionID as seen for dedupe.
func (l *DurableAdviceLedger) Append(tr DeliveryTransition) error {
	if l == nil {
		return fmt.Errorf("nil ledger")
	}
	if tr.Schema == "" {
		tr.Schema = "reinframe.advice_delivery.v1"
	}
	if tr.At.IsZero() {
		tr.At = time.Now().UTC()
	}
	line, err := json.Marshal(tr)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil && !os.IsExist(err) {
		if filepath.Dir(l.Path) != "." {
			return err
		}
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	n, err := f.Write(line)
	if err != nil {
		return err
	}
	l.cursor += int64(n)
	if tr.InterventionID != "" && isSuppressState(DeliveryState(tr.ToState)) {
		// Bind intervention + session + host + action fingerprint (not bare ID alone).
		key := dedupeKey(tr.InterventionID, tr.SessionID, tr.HostFamily, tr.Fingerprint)
		l.seen[key] = struct{}{}
	}
	return nil
}

func isSuppressState(st DeliveryState) bool {
	switch st {
	case StateTransportAccepted, StateSessionVisible, StateExplicitACK, StateBehavioralACK,
		StateAcked, StateRejected, StateTimedOut, StateExpired, StateSuppressed,
		StateAmbiguous: // host may have accepted; never auto-redeliver
		// Intentionally exclude StateFailed / StateUnsupported so transient pre-send failures can retry.
		return true
	default:
		return false
	}
}

// Cursor returns the durable byte offset (for tests / recovery diagnostics).
func (l *DurableAdviceLedger) Cursor() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cursor
}

// RecordResult appends a transition from a delivery result snapshot.
// Fingerprint should be set on res via RecordResultWithSource when action-bound dedupe is required.
func (l *DurableAdviceLedger) RecordResult(from DeliveryState, sessionID string, res InterventionResult, to DeliveryState) error {
	return l.RecordResultWithSource(from, sessionID, res, to, "", "", "", "")
}

// RecordResultWithSource appends with source-bound ACK identity (#200).
func (l *DurableAdviceLedger) RecordResultWithSource(from DeliveryState, sessionID string, res InterventionResult, to DeliveryState, srcKind, srcEvent, corr, fingerprint string) error {
	return l.Append(DeliveryTransition{
		InterventionID: res.InterventionID,
		SessionID:      sessionID,
		FromState:      string(from),
		ToState:        string(to),
		AckLayer:       res.AckLayer,
		AckStatus:      res.AckStatus,
		HostFamily:     res.HostFamily,
		HostVersion:    res.HostVersion,
		Profile:        res.Profile,
		SafeBoundary:   res.SafeBoundary,
		TargetSession:  res.TargetSessionID,
		CapsDigest:     res.CapsDigest,
		Message:        boundRunes(res.Message, 240),
		SourceKind:     srcKind,
		SourceEventID:  srcEvent,
		CorrelationID:  corr,
		Fingerprint:    fingerprint,
	})
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
