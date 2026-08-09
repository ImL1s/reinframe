package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DurableAdviceLedger is an append-only, user-private JSONL ledger of advice
// delivery transitions (#108). Source advice channel history is never rewritten.
//
// Schema: reinframe.advice_delivery.v1
type DurableAdviceLedger struct {
	Path string

	mu     sync.Mutex
	seen   map[string]struct{} // InterventionIDs that reached a terminal or transport-accepted state
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

// OpenDurableAdviceLedger loads existing IDs from path (if present) for restart dedupe.
// Rejects symlink/reparse paths (#200).
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
		for _, line := range splitLines(b) {
			if len(line) == 0 {
				continue
			}
			var tr DeliveryTransition
			if json.Unmarshal(line, &tr) != nil {
				continue
			}
			if tr.InterventionID == "" {
				continue
			}
			// Only states that prove host accepted transport (or terminal no-retry) suppress redelivery.
			// DELIVERING alone does not suppress — crash mid-flight should allow retry.
			if isSuppressState(DeliveryState(tr.ToState)) {
				// Session/host/action-bound key (fingerprint = action identity).
				l.seen[dedupeKey(tr.InterventionID, tr.SessionID, tr.HostFamily, tr.Fingerprint)] = struct{}{}
			}
		}
	}
	// Load sidecar ambiguous suppress markers (host accepted; JSONL commit failed).
	_ = l.loadAmbiguousMarkersUnlocked()
	return l, nil
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
func (l *DurableAdviceLedger) loadAmbiguousMarkersUnlocked() error {
	dir := l.suppressSidecarDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".key") {
			continue
		}
		b, rerr := os.ReadFile(filepath.Join(dir, e.Name()))
		if rerr != nil {
			continue
		}
		key := strings.TrimSpace(string(b))
		if key != "" {
			l.seen[key] = struct{}{}
		}
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
