package detector

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// FailureModeVerificationChurn is emitted on redundant successful re-validation.
const FailureModeVerificationChurn = "verification_churn"

// DetectorNameVerificationChurn is the detector name on TunnelSignal.
const DetectorNameVerificationChurn = "VerificationChurnDetector"

// ValidationAttempt is one validation run considered for SOP-churn detection.
// Fingerprint inputs (normative): command + target scope + workspace revision
// + contract revision + purpose.
type ValidationAttempt struct {
	Command          string
	TargetScope      []string
	WorkspaceRev     string
	ContractRevision int
	Purpose          string
	Succeeded        bool

	// Counterexample / exemption flags (must NOT fire when true).
	// FlakyInvestigation: user asked for flaky investigation — re-runs allowed.
	FlakyInvestigation bool
	// PolicyRequiresRerun: repository policy requires this validation (e.g. docs lint).
	PolicyRequiresRerun bool
	// HighRiskIndependent: high-risk change demands independent re-validation.
	HighRiskIndependent bool
	// WorkspaceChanged: code/workspace changed since last success — re-test OK.
	// When true, fingerprint workspace component differs; also set explicitly.
	WorkspaceChanged bool
}

// VerificationChurnConfig configures VerificationChurnDetector.
type VerificationChurnConfig struct {
	Now func() time.Time
}

// VerificationChurnDetector detects re-running an equivalent successful validation
// without information gain (#85).
type VerificationChurnDetector struct {
	now func() time.Time

	mu       sync.Mutex
	sessions map[string]*churnSession
	seq      uint64
}

type churnSession struct {
	// lastSuccess[fingerprint] = true after a successful attempt with that fp.
	lastSuccess map[string]bool
}

// NewVerificationChurnDetector builds a churn detector.
func NewVerificationChurnDetector(cfg VerificationChurnConfig) *VerificationChurnDetector {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &VerificationChurnDetector{
		now:      now,
		sessions: make(map[string]*churnSession),
	}
}

// ValidationFingerprint builds the multi-part fingerprint string.
func ValidationFingerprint(a ValidationAttempt) string {
	cmd := NormalizeFingerprint(a.Command)
	purpose := NormalizeFingerprint(a.Purpose)
	ws := strings.TrimSpace(a.WorkspaceRev)
	scope := normalizeScope(a.TargetScope)
	return fmt.Sprintf("cmd=%s|scope=%s|ws=%s|crev=%d|purpose=%s",
		cmd, scope, ws, a.ContractRevision, purpose)
}

// Observe records a validation attempt. Fires when an equivalent fingerprint
// already succeeded and none of the counterexample exemptions apply.
func (d *VerificationChurnDetector) Observe(sessionID string, attempt ValidationAttempt) (*protocol.TunnelSignal, bool) {
	if sessionID == "" {
		return nil, false
	}
	fp := ValidationFingerprint(attempt)
	if strings.HasPrefix(fp, "cmd=|") || attempt.Command == "" {
		// empty command is not a validation
		if NormalizeFingerprint(attempt.Command) == "" {
			return nil, false
		}
	}

	// Exemptions: never fire.
	if attempt.FlakyInvestigation || attempt.PolicyRequiresRerun || attempt.HighRiskIndependent {
		d.recordSuccessIfNeeded(sessionID, fp, attempt)
		return nil, false
	}
	if attempt.WorkspaceChanged {
		// Treat as new workspace identity for bookkeeping.
		d.recordSuccessIfNeeded(sessionID, fp, attempt)
		return nil, false
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		ss = &churnSession{lastSuccess: make(map[string]bool)}
		d.sessions[sessionID] = ss
	}

	// Fire only when re-running after a prior *success* of the same fingerprint.
	if attempt.Succeeded && ss.lastSuccess[fp] {
		d.seq++
		sig := &protocol.TunnelSignal{
			SignalID:     fmt.Sprintf("sig-vc-%d", d.seq),
			SessionID:    sessionID,
			DetectorName: DetectorNameVerificationChurn,
			FailureMode:  FailureModeVerificationChurn,
			Weight:       0.3,
			Score:        1.0,
			Details: map[string]string{
				"fingerprint":       fp,
				"command":           NormalizeFingerprint(attempt.Command),
				"workspace_rev":     strings.TrimSpace(attempt.WorkspaceRev),
				"contract_revision": fmt.Sprintf("%d", attempt.ContractRevision),
				"purpose":           NormalizeFingerprint(attempt.Purpose),
			},
			TriggeredAt: d.now(),
		}
		return sig, true
	}

	if attempt.Succeeded {
		ss.lastSuccess[fp] = true
	}
	// Failed attempts do not mark success; a later success is first success (no fire).
	return nil, false
}

func (d *VerificationChurnDetector) recordSuccessIfNeeded(sessionID, fp string, attempt ValidationAttempt) {
	if !attempt.Succeeded {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		ss = &churnSession{lastSuccess: make(map[string]bool)}
		d.sessions[sessionID] = ss
	}
	// Exemptions that change workspace should not poison fingerprint as churn.
	if attempt.WorkspaceChanged {
		// Clear old success for this session on workspace change — simple model:
		// drop all prior success marks for the session when workspace explicitly changed.
		ss.lastSuccess = make(map[string]bool)
	}
	ss.lastSuccess[fp] = true
}

// ResetSession clears churn history for a session.
func (d *VerificationChurnDetector) ResetSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sessions, sessionID)
}

func normalizeScope(scope []string) string {
	if len(scope) == 0 {
		return ""
	}
	parts := make([]string, 0, len(scope))
	for _, s := range scope {
		s = NormalizeFingerprint(s)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}
