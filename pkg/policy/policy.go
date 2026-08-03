package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/reviewer"
)

// Action types produced by slow path.
const (
	ActionNone           = "none"
	ActionZoomOutPrompt  = "ZOOM_OUT_PROMPT"
	ActionEscalateHuman  = "ESCALATE_TO_HUMAN"
	ActionNoIntervention = "no_intervention"
)

// DefaultZoomOutAdvice is the provisional advice prompt for repeated-error loops.
const DefaultZoomOutAdvice = "ZOOM_OUT: you are repeating the same failure. Pause, re-diagnose the root cause, and replan before retrying the same edit/test loop."

// Engine is the fast/slow policy engine for the M2.0 slice.
type Engine struct {
	reviewer reviewer.ReviewerProvider // optional; only for uncertain slow path
	now      func() time.Time
	idSeq    uint64
}

// EngineConfig configures Engine.
type EngineConfig struct {
	// Reviewer is optional. Used only on uncertain slow-path branches.
	Reviewer reviewer.ReviewerProvider
	Now      func() time.Time
}

// NewEngine builds a policy engine.
func NewEngine(cfg EngineConfig) *Engine {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Engine{reviewer: cfg.Reviewer, now: now}
}

// FastInput is the pure deterministic PreTool/PreCommand evaluation input.
// No Reviewer field is present by design.
type FastInput struct {
	Request adapter.HookRequest
	Policy  adapter.HookPolicy
}

// EvaluateFast runs the synchronous fast path. It never calls a Reviewer.
func (e *Engine) EvaluateFast(ctx context.Context, in FastInput) adapter.HookDecision {
	return adapter.EvaluateHook(ctx, in.Request, in.Policy)
}

// SlowInput is slow-path adjudication input.
// Contract and Ledger may be nil on the first M2.0 slice.
type SlowInput struct {
	Signal   *protocol.TunnelSignal
	Contract *protocol.TaskContract  // optional
	Ledger   *protocol.EvidenceLedger // optional
	// Uncertain forces the optional Reviewer branch even for known failure modes.
	Uncertain bool
	// AdvicePrompt overrides default zoom-out text when non-empty.
	AdvicePrompt string
}

// SlowResult is the outcome of EvaluateSlow.
type SlowResult struct {
	Action         string
	Intervention   *protocol.Intervention
	UsedReviewer   bool
	ReviewDecision *protocol.ReviewDecision
	Reason         string
}

// EvaluateSlow maps a detector signal to an optional intervention.
//
// High-confidence deterministic repeated_error_loop → ZOOM_OUT_PROMPT without Reviewer.
// Uncertain path may call Reviewer when configured.
// Nil signal → no intervention.
func (e *Engine) EvaluateSlow(ctx context.Context, in SlowInput) (SlowResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if in.Signal == nil {
		return SlowResult{Action: ActionNone, Reason: "nil_signal"}, nil
	}
	sig := in.Signal

	// Optional fields reserved for later effort-calibration; first slice ignores nil.
	_ = in.Contract
	_ = in.Ledger

	// Deterministic high-confidence path (no Reviewer).
	if !in.Uncertain && isHighConfidenceRepeatedFailure(sig) {
		iv := e.buildZoomOut(sig, in.AdvicePrompt)
		return SlowResult{
			Action:       ActionZoomOutPrompt,
			Intervention: iv,
			UsedReviewer: false,
			Reason:       "deterministic_repeated_error_loop",
		}, nil
	}

	// Uncertain / forced reviewer branch.
	if e.reviewer == nil {
		// Without reviewer, still act on known repeated_error_loop deterministically.
		if isHighConfidenceRepeatedFailure(sig) {
			iv := e.buildZoomOut(sig, in.AdvicePrompt)
			return SlowResult{
				Action:       ActionZoomOutPrompt,
				Intervention: iv,
				UsedReviewer: false,
				Reason:       "uncertain_but_no_reviewer_fallback_deterministic",
			}, nil
		}
		return SlowResult{Action: ActionNoIntervention, Reason: "uncertain_no_reviewer"}, nil
	}

	req := protocol.ReviewRequest{
		RequestID:    fmt.Sprintf("rev-%s-%d", sig.SignalID, e.nextID()),
		ReviewerRole: "TunnelClassifier",
		Model:        "policy-optional",
		Prompt:       fmt.Sprintf("session=%s failure_mode=%s score=%g details=%v", sig.SessionID, sig.FailureMode, sig.Score, sig.Details),
		RequestedAt:  e.now(),
	}
	dec, err := e.reviewer.Generate(ctx, req)
	if err != nil {
		return SlowResult{Action: ActionNone, UsedReviewer: true, Reason: "reviewer_error"}, err
	}
	used := true
	if dec.Classification == "TUNNEL_VISION" || dec.TunnelConfidence >= 0.85 ||
		isHighConfidenceRepeatedFailure(sig) {
		advice := in.AdvicePrompt
		if advice == "" {
			advice = dec.SuggestedAdvice
		}
		if advice == "" {
			advice = DefaultZoomOutAdvice
		}
		iv := e.buildZoomOut(sig, advice)
		return SlowResult{
			Action:         ActionZoomOutPrompt,
			Intervention:   iv,
			UsedReviewer:   used,
			ReviewDecision: &dec,
			Reason:         "reviewer_tunnel",
		}, nil
	}
	return SlowResult{
		Action:         ActionNoIntervention,
		UsedReviewer:   used,
		ReviewDecision: &dec,
		Reason:         "reviewer_normal_progress",
	}, nil
}

func isHighConfidenceRepeatedFailure(sig *protocol.TunnelSignal) bool {
	if sig == nil {
		return false
	}
	if sig.FailureMode == detector.FailureModeRepeatedErrorLoop {
		return true
	}
	if sig.DetectorName == detector.DetectorNameRepeatedFailure && sig.Score >= 1.0 {
		return true
	}
	return false
}

func (e *Engine) buildZoomOut(sig *protocol.TunnelSignal, advice string) *protocol.Intervention {
	if advice == "" {
		advice = DefaultZoomOutAdvice
	}
	e.idSeq++
	id := fmt.Sprintf("iv-zoom-%s-%d", sig.SignalID, e.idSeq)
	fp := ""
	if sig.Details != nil {
		fp = sig.Details["fingerprint"]
	}
	now := e.now()
	return &protocol.Intervention{
		InterventionID: id,
		SessionID:      sig.SessionID,
		Level:          1,
		ActionType:     ActionZoomOutPrompt,
		AdvicePrompt:   advice,
		Status:         "PENDING",
		ExecutedAt:     now,
		RequiresAck:    true,
		SafeBoundary:   string(protocol.BoundaryBeforeTool),
		Fingerprint:    fp,
		Priority:       10,
	}
}

func (e *Engine) nextID() uint64 {
	e.idSeq++
	return e.idSeq
}
