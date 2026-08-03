package detector

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// FailureModeHypothesisLoop is emitted when the same hypothesis fingerprint
// repeats without new evidence identifiers.
const FailureModeHypothesisLoop = "hypothesis_loop"

// DetectorNameHypothesisLoop is the DetectorName on TunnelSignal.
const DetectorNameHypothesisLoop = "HypothesisLoopDetector"

// DefaultHypothesisLoopThreshold is provisional N for repeated similar conclusions.
const DefaultHypothesisLoopThreshold = 3

// HypothesisLoopConfig configures HypothesisLoopDetector.
type HypothesisLoopConfig struct {
	// Threshold is minimum identical fingerprint observations without new evidence.
	// Zero uses DefaultHypothesisLoopThreshold.
	Threshold int
	Now       func() time.Time
}

// HypothesisObservation is one stated conclusion / security hypothesis probe.
type HypothesisObservation struct {
	// Text is free-form rationale or conclusion (fingerprinted after normalize).
	Text string
	// EvidenceIDs are file paths, criterion IDs, or evidence item IDs supporting the claim.
	// When any ID is new for this fingerprint, the loop counter resets.
	EvidenceIDs []string
}

// HypothesisLoopDetector detects repeated similar conclusion fingerprints
// without new evidence IDs (#98 minimal). Healthy deep work that keeps
// attaching new evidence IDs does not fire.
type HypothesisLoopDetector struct {
	threshold int
	now       func() time.Time

	mu       sync.Mutex
	sessions map[string]*hypothesisSession
	seq      uint64
}

type hypothesisSession struct {
	// counts[fp] = consecutive observations without new evidence
	counts map[string]int
	// seenEvidence[fp] = set of evidence IDs already attached to this fingerprint
	seenEvidence map[string]map[string]struct{}
}

// NewHypothesisLoopDetector builds a detector with defaults.
func NewHypothesisLoopDetector(cfg HypothesisLoopConfig) *HypothesisLoopDetector {
	th := cfg.Threshold
	if th <= 0 {
		th = DefaultHypothesisLoopThreshold
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &HypothesisLoopDetector{
		threshold: th,
		now:       now,
		sessions:  make(map[string]*hypothesisSession),
	}
}

// Threshold returns the fire threshold.
func (d *HypothesisLoopDetector) Threshold() int {
	return d.threshold
}

// Observe records a hypothesis/conclusion for sessionID.
// Fires when the same fingerprint reaches threshold without any new evidence ID
// on the observations that built that count.
func (d *HypothesisLoopDetector) Observe(sessionID string, obs HypothesisObservation) (*protocol.TunnelSignal, bool) {
	if sessionID == "" {
		return nil, false
	}
	fp := NormalizeFingerprint(obs.Text)
	if fp == "" {
		return nil, false
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		ss = &hypothesisSession{
			counts:       make(map[string]int),
			seenEvidence: make(map[string]map[string]struct{}),
		}
		d.sessions[sessionID] = ss
	}
	if ss.seenEvidence[fp] == nil {
		ss.seenEvidence[fp] = make(map[string]struct{})
	}

	newEvidence := false
	for _, id := range obs.EvidenceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := ss.seenEvidence[fp][id]; !ok {
			ss.seenEvidence[fp][id] = struct{}{}
			newEvidence = true
		}
	}

	if newEvidence {
		// New workspace/evidence attachment — not a pure loop.
		ss.counts[fp] = 1
		return nil, false
	}

	ss.counts[fp]++
	count := ss.counts[fp]
	if count < d.threshold {
		return nil, false
	}

	d.seq++
	sig := &protocol.TunnelSignal{
		SignalID:     fmt.Sprintf("sig-hl-%d", d.seq),
		SessionID:    sessionID,
		DetectorName: DetectorNameHypothesisLoop,
		FailureMode:  FailureModeHypothesisLoop,
		Weight:       0.3,
		Score:        1.0,
		Details: map[string]string{
			"fingerprint":   fp,
			"count":         fmt.Sprintf("%d", count),
			"threshold":     fmt.Sprintf("%d", d.threshold),
			"evidence_seen": fmt.Sprintf("%d", len(ss.seenEvidence[fp])),
		},
		TriggeredAt: d.now(),
	}
	return sig, true
}

// Count returns the current no-new-evidence streak for a fingerprint text.
func (d *HypothesisLoopDetector) Count(sessionID, text string) int {
	fp := NormalizeFingerprint(text)
	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		return 0
	}
	return ss.counts[fp]
}

// ResetSession clears hypothesis history for a session.
func (d *HypothesisLoopDetector) ResetSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sessions, sessionID)
}
