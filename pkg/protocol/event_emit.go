package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// Canonical AgentEvent.EventType strings for task-model persistence.
// Payload is the JSON object of the corresponding protocol type.
const (
	EventTypeTaskSubmitted  = "task_submitted"
	EventTypeTaskContract   = "task_contract"
	EventTypeEvidenceLedger = "evidence_ledger"
)

// EmitOptions configures sequence/id fields when wrapping task types as AgentEvent.
type EmitOptions struct {
	EventID     string
	SequenceNum int64
	// Timestamp defaults to time.Now().UTC() when zero.
	Timestamp time.Time
}

// AgentEventFromTaskSubmitted wraps TaskSubmitted as a store-ready AgentEvent.
// Validates the payload against the task_submitted JSON schema.
func AgentEventFromTaskSubmitted(sub TaskSubmitted, opts EmitOptions) (AgentEvent, error) {
	return wrapAsAgentEvent(sub.SessionID, EventTypeTaskSubmitted, sub, opts)
}

// AgentEventFromTaskContract wraps TaskContract as AgentEvent (event type task_contract).
// SessionID must be supplied (contracts do not embed session_id).
func AgentEventFromTaskContract(sessionID string, c TaskContract, opts EmitOptions) (AgentEvent, error) {
	if sessionID == "" {
		return AgentEvent{}, fmt.Errorf("session_id is required for task_contract event")
	}
	return wrapAsAgentEvent(sessionID, EventTypeTaskContract, c, opts)
}

// AgentEventFromEvidenceLedger wraps EvidenceLedger as AgentEvent.
func AgentEventFromEvidenceLedger(sessionID string, led EvidenceLedger, opts EmitOptions) (AgentEvent, error) {
	if sessionID == "" {
		return AgentEvent{}, fmt.Errorf("session_id is required for evidence_ledger event")
	}
	return wrapAsAgentEvent(sessionID, EventTypeEvidenceLedger, led, opts)
}

func wrapAsAgentEvent(sessionID, eventType string, payload any, opts EmitOptions) (AgentEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return AgentEvent{}, err
	}
	if err := ValidateEvent(raw, eventType); err != nil {
		return AgentEvent{}, fmt.Errorf("validate %s: %w", eventType, err)
	}
	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	id := opts.EventID
	if id == "" {
		id = fmt.Sprintf("%s-%s-%d", eventType, sessionID, opts.SequenceNum)
	}
	seq := opts.SequenceNum
	if seq <= 0 {
		seq = 1
	}
	return AgentEvent{
		EventID:     id,
		SessionID:   sessionID,
		SequenceNum: seq,
		EventType:   eventType,
		Timestamp:   ts,
		Payload:     raw,
	}, nil
}
