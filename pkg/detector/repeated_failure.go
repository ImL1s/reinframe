package detector

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// Config configures RepeatedFailureDetector.
type Config struct {
	// Threshold is the minimum identical fingerprint count to fire (default 3).
	Threshold int
	// Now, if set, overrides time source (tests).
	Now func() time.Time
}

// RepeatedFailureDetector tracks per-session failure fingerprints.
// Thread-safe for concurrent Observe calls on different sessions.
type RepeatedFailureDetector struct {
	threshold int
	now       func() time.Time

	mu       sync.Mutex
	sessions map[string]*sessionFingerprints
	seq      uint64
}

type sessionFingerprints struct {
	// counts maps normalized fingerprint → occurrence count
	counts map[string]int
	// firstSeen / lastSeen per fingerprint
	first map[string]time.Time
	last  map[string]time.Time
	// fired tracks fingerprints already emitted so we re-fire only on new hits
	// after threshold (every observation at/above threshold emits a signal so
	// policy can re-evaluate; orchestrator dedupes interventions by ID strategy).
	// For the M2.0 slice we emit on each observation when count >= threshold.
}

// NewRepeatedFailureDetector builds a detector with config defaults applied.
func NewRepeatedFailureDetector(cfg Config) *RepeatedFailureDetector {
	th := cfg.Threshold
	if th <= 0 {
		th = DefaultThreshold
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &RepeatedFailureDetector{
		threshold: th,
		now:       now,
		sessions:  make(map[string]*sessionFingerprints),
	}
}

// Threshold returns the configured fire threshold.
func (d *RepeatedFailureDetector) Threshold() int {
	return d.threshold
}

// Observe processes one AgentEvent. If a failure fingerprint is extracted and
// the session count for that fingerprint reaches threshold, it returns a
// TunnelSignal and true. Otherwise returns (nil, false).
//
// Never calls a Reviewer or network service.
func (d *RepeatedFailureDetector) Observe(event protocol.AgentEvent) (*protocol.TunnelSignal, bool) {
	raw := extractFailureText(event)
	if raw == "" {
		return nil, false
	}
	return d.ObserveRaw(event.SessionID, raw, event.EventID)
}

// ObserveRaw records a raw failure string for sessionID (tests and adapters).
// sourceID is optional correlation (event_id); may be empty.
func (d *RepeatedFailureDetector) ObserveRaw(sessionID, raw, sourceID string) (*protocol.TunnelSignal, bool) {
	fp := NormalizeFingerprint(raw)
	if fp == "" || sessionID == "" {
		return nil, false
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	ss := d.sessions[sessionID]
	if ss == nil {
		ss = &sessionFingerprints{
			counts: make(map[string]int),
			first:  make(map[string]time.Time),
			last:   make(map[string]time.Time),
		}
		d.sessions[sessionID] = ss
	}

	now := d.now()
	ss.counts[fp]++
	if _, ok := ss.first[fp]; !ok {
		ss.first[fp] = now
	}
	ss.last[fp] = now
	count := ss.counts[fp]

	if count < d.threshold {
		return nil, false
	}

	d.seq++
	sigID := fmt.Sprintf("sig-rf-%d", d.seq)
	if sourceID != "" {
		sigID = fmt.Sprintf("sig-rf-%s-%d", sourceID, d.seq)
	}

	sig := &protocol.TunnelSignal{
		SignalID:     sigID,
		SessionID:    sessionID,
		DetectorName: DetectorNameRepeatedFailure,
		FailureMode:  FailureModeRepeatedErrorLoop,
		Weight:       0.35, // provisional threat-model default
		Score:        1.0,
		Details: map[string]string{
			"fingerprint": fp,
			"count":       fmt.Sprintf("%d", count),
			"threshold":   fmt.Sprintf("%d", d.threshold),
			"source_id":   sourceID,
		},
		TriggeredAt: now,
	}
	return sig, true
}

// Count returns the current count for a session fingerprint (normalized).
func (d *RepeatedFailureDetector) Count(sessionID, raw string) int {
	fp := NormalizeFingerprint(raw)
	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		return 0
	}
	return ss.counts[fp]
}

// ResetSession clears all fingerprints for a session.
func (d *RepeatedFailureDetector) ResetSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sessions, sessionID)
}

// extractFailureText pulls a failure string from known event shapes.
func extractFailureText(event protocol.AgentEvent) string {
	if len(event.Payload) == 0 {
		return ""
	}

	switch event.EventType {
	case "test_result", "TestResult":
		var tr protocol.TestResultEvent
		if err := json.Unmarshal(event.Payload, &tr); err == nil {
			if tr.FailedCount > 0 || tr.FailureOutput != "" {
				if tr.FailureOutput != "" {
					return tr.FailureOutput
				}
				return fmt.Sprintf("test_failed command=%s failed=%d", tr.Command, tr.FailedCount)
			}
		}
	case "tool_call", "ToolCall":
		var tc protocol.ToolCallEvent
		if err := json.Unmarshal(event.Payload, &tc); err == nil {
			if tc.Error != "" {
				return tc.Error
			}
			if tc.ExitCode != nil && *tc.ExitCode != 0 {
				return fmt.Sprintf("tool %s exit=%d output=%s", tc.ToolName, *tc.ExitCode, tc.Output)
			}
		}
	case "error", "failure", "agent_error":
		// Prefer structured fields; fall back to raw payload string.
		var m map[string]any
		if err := json.Unmarshal(event.Payload, &m); err == nil {
			for _, k := range []string{"error", "failure_output", "normalized_text", "message", "raw_error"} {
				if v, ok := m[k].(string); ok && v != "" {
					return v
				}
			}
		}
		return string(event.Payload)
	}

	// Generic payload fallback: common failure keys.
	var m map[string]any
	if err := json.Unmarshal(event.Payload, &m); err == nil {
		for _, k := range []string{"failure_output", "error", "normalized_text", "raw_error"} {
			if v, ok := m[k].(string); ok && v != "" {
				return v
			}
		}
		// test_result-shaped without matching EventType
		if fc, ok := m["failed_count"].(float64); ok && fc > 0 {
			if fo, ok := m["failure_output"].(string); ok && fo != "" {
				return fo
			}
		}
	}
	return ""
}
