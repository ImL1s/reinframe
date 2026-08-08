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
}

// OpenDurableAdviceLedger loads existing IDs from path (if present) for restart dedupe.
func OpenDurableAdviceLedger(path string) (*DurableAdviceLedger, error) {
	if path == "" {
		return nil, fmt.Errorf("durable advice ledger: path required")
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
		}
	}
	return l, nil
}

// AlreadyDelivered reports whether InterventionID has a durable delivery transition.
func (l *DurableAdviceLedger) AlreadyDelivered(interventionID string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.seen[interventionID]
	return ok
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
		l.seen[tr.InterventionID] = struct{}{}
	}
	return nil
}

func isSuppressState(st DeliveryState) bool {
	switch st {
	case StateTransportAccepted, StateSessionVisible, StateExplicitACK, StateBehavioralACK,
		StateAcked, StateRejected, StateTimedOut, StateExpired, StateSuppressed,
		StateFailed, StateUnsupported:
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
