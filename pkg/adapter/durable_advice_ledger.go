package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		if os.IsNotExist(err) {
			return l, nil
		}
		return nil, err
	}
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
			l.seen[tr.InterventionID] = struct{}{}
			l.seen[dedupeKey(tr.InterventionID, tr.SessionID, tr.HostFamily, "")] = struct{}{}
		}
	}
	return l, nil
}

// AlreadyDelivered reports whether InterventionID has a durable delivery transition.
// Prefer AlreadyDeliveredKey for session/host/action-bound dedupe (#200).
func (l *DurableAdviceLedger) AlreadyDelivered(interventionID string) bool {
	return l.AlreadyDeliveredKey(interventionID, "", "", "")
}

// AlreadyDeliveredKey checks a bound dedupe key (intervention|session|host|fingerprint).
func (l *DurableAdviceLedger) AlreadyDeliveredKey(interventionID, sessionID, hostFamily, fingerprint string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.seen[dedupeKey(interventionID, sessionID, hostFamily, fingerprint)]; ok {
		return true
	}
	// Backward compatible: bare intervention id.
	_, ok := l.seen[interventionID]
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
		// FAILED/UNSUPPORTED are NOT permanent suppress — allow policy retry (#200).
		key := dedupeKey(tr.InterventionID, tr.SessionID, tr.HostFamily, "")
		l.seen[key] = struct{}{}
		l.seen[tr.InterventionID] = struct{}{}
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
func (l *DurableAdviceLedger) RecordResult(from DeliveryState, sessionID string, res InterventionResult, to DeliveryState) error {
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
	})
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
