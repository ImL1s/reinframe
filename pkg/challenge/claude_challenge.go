package challenge

import (
	"context"
	"fmt"
	"strings"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// ClaudeChallengeBridge binds challenge.Service with Claude PreToolUse hook adapter (#139).
type ClaudeChallengeBridge struct {
	service *Service
	opts    ClaudeChallengeBridgeOptions
}

// ClaudeChallengeBridgeOptions configures ClaudeChallengeBridge.
type ClaudeChallengeBridgeOptions struct {
	ResponseOpts          adapter.ClaudeResponseOptions
	KnownEvidenceIDs      []string
	ReEvalContext         *ReEvalContext
	DefaultBlockClass     string
	DefaultPolicyClass    string
	PolicyVersion         string
	RulesetHash           string
	Branch                string
	ExpiresAfterSequences int64
}

// NewClaudeChallengeBridge creates a bridge between challenge.Service and Claude adapter.
func NewClaudeChallengeBridge(svc *Service, opts ClaudeChallengeBridgeOptions) *ClaudeChallengeBridge {
	if svc == nil {
		svc = NewService(ServiceConfig{})
	}
	return &ClaudeChallengeBridge{
		service: svc,
		opts:    opts,
	}
}

// Service returns the underlying Challenge Service.
func (b *ClaudeChallengeBridge) Service() *Service {
	return b.service
}

// JustificationFromClaudeInput maps adapter.ClaudeJustificationInput to challenge.Justification.
func JustificationFromClaudeInput(chID string, in *adapter.ClaudeJustificationInput) Justification {
	if in == nil {
		return Justification{
			SchemaVersion: SchemaJustification,
			ChallengeID:   chID,
		}
	}
	return Justification{
		SchemaVersion:              SchemaJustification,
		ChallengeID:                chID,
		ConcreteValue:              in.ConcreteValue,
		PreventedFailureOrThreat:   in.PreventedFailureOrThreat,
		EstimatedCost:              in.EstimatedCost,
		AlternativesConsidered:     in.AlternativesConsidered,
		ScopeLimit:                 in.ScopeLimit,
		VerificationPlan:           in.VerificationPlan,
		RollbackPlan:               in.RollbackPlan,
		SupportingEvidenceEventIDs: append([]string(nil), in.SupportingEvidenceEventIDs...),
	}
}

// ChallengeContextFromRecord constructs adapter.ClaudeChallengeContext from ChallengeRecord.
func ChallengeContextFromRecord(rec ChallengeRecord) adapter.ClaudeChallengeContext {
	return adapter.ClaudeChallengeContext{
		ChallengeID:         rec.ChallengeID,
		ChallengeNonce:      rec.ChallengeNonce,
		Reason:              rec.ReasonCode,
		SuggestedFix:        rec.SuggestedFix,
		OneShotRetryAllowed: rec.Appealability == AppealAppealable && rec.RetryBudget > 0,
	}
}

// AsHookEvaluator returns an EvaluateChallenge callback for adapter.ClaudeBridgeConfig.
func (b *ClaudeChallengeBridge) AsHookEvaluator() func(ctx context.Context, in adapter.ClaudePreToolInput, req adapter.HookRequest, cfg adapter.ClaudeBridgeConfig) (adapter.ClaudeHookResponse, adapter.HookDecision, bool, error) {
	return func(ctx context.Context, in adapter.ClaudePreToolInput, req adapter.HookRequest, cfg adapter.ClaudeBridgeConfig) (adapter.ClaudeHookResponse, adapter.HookDecision, bool, error) {
		return b.EvaluatePreTool(ctx, in, cfg)
	}
}

// EvaluatePreTool evaluates a Claude PreToolUse hook call through the challenge bridge.
func (b *ClaudeChallengeBridge) EvaluatePreTool(ctx context.Context, in adapter.ClaudePreToolInput, cfg adapter.ClaudeBridgeConfig) (adapter.ClaudeHookResponse, adapter.HookDecision, bool, error) {
	if ctx != nil && ctx.Err() != nil {
		return adapter.ClaudeHookResponse{}, adapter.HookDecision{}, false, ctx.Err()
	}

	// 1. Is this an appeal retry turn? (ChallengeID is present)
	if in.ChallengeID != "" {
		return b.handleRetry(ctx, in, cfg)
	}

	// 2. Initial tool call: evaluate base policy / gate
	req := adapter.HookRequestFromClaudePreTool(in)
	var dec adapter.HookDecision
	if cfg.Evaluate != nil {
		dec = cfg.Evaluate(ctx, req)
	} else {
		dec = adapter.EvaluateHook(ctx, req, cfg.Policy)
	}

	// If allowed or deferred, return without challenge opening
	if dec.Action == adapter.HookActionAllow || dec.Action == adapter.HookActionDefer {
		resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, cfg.Response)
		return resp, dec, true, nil
	}

	// 3. Action is HookActionDeny: check appealability
	var pa adapter.ProposedAction
	if in.Proposed != nil {
		pa = *in.Proposed
	} else {
		pa = adapter.ProposedAction{
			SessionID: in.SessionID,
			ToolName:  in.ToolName,
			FilePath:  in.FilePath,
		}
	}

	blockClass := b.opts.DefaultBlockClass
	if blockClass == "" {
		blockClass = dec.ReasonCode
	}
	appeal, _ := ClassifyAppealability(blockClass, pa)
	if appeal != AppealAppealable {
		// Non-appealable hard block or human review: deliver direct deny
		opts := cfg.Response
		opts.TransportLevel = adapter.ClaudeTransportDirectDeny
		resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
		return resp, dec, true, nil
	}

	// 4. Open an appealable challenge
	openReq := OpenRequest{
		SessionID:             in.SessionID,
		Proposed:              pa,
		BlockClass:            blockClass,
		ReasonCode:            dec.ReasonCode,
		PolicyClass:           b.opts.DefaultPolicyClass,
		PolicyVersion:         b.opts.PolicyVersion,
		RulesetHash:           b.opts.RulesetHash,
		Branch:                b.opts.Branch,
		KnownEvidenceIDs:      b.opts.KnownEvidenceIDs,
		ExpiresAfterSequences: b.opts.ExpiresAfterSequences,
		CorrelationID:         in.SessionID + "-pretool",
	}
	rec, err := b.service.Open(ctx, openReq)
	if err != nil {
		opts := cfg.Response
		opts.TransportLevel = adapter.ClaudeTransportDirectDeny
		resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
		return resp, dec, true, nil
	}

	chCtx := ChallengeContextFromRecord(rec)
	opts := cfg.Response
	opts.Challenge = &chCtx
	opts.TransportLevel = adapter.ClaudeTransportHookAdditionalContext

	resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
	return resp, dec, true, nil
}

func (b *ClaudeChallengeBridge) handleRetry(ctx context.Context, in adapter.ClaudePreToolInput, cfg adapter.ClaudeBridgeConfig) (adapter.ClaudeHookResponse, adapter.HookDecision, bool, error) {
	if in.ChallengeID == "" {
		return adapter.ClaudeHookResponse{}, adapter.HookDecision{}, false, fmt.Errorf("retry: challenge_id required")
	}

	rec, ok := b.service.Get(in.ChallengeID)
	if !ok {
		dec := adapter.HookDecision{
			Action:     adapter.HookActionDeny,
			ReasonCode: "unknown_challenge",
		}
		opts := cfg.Response
		opts.TransportLevel = adapter.ClaudeTransportHookAdditionalContext
		resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
		return resp, dec, true, fmt.Errorf("retry: unknown challenge %s", in.ChallengeID)
	}

	// Nonce validation
	if strings.TrimSpace(in.ChallengeNonce) == "" {
		dec := adapter.HookDecision{
			Action:     adapter.HookActionDeny,
			ReasonCode: "missing_challenge_nonce",
		}
		opts := cfg.Response
		opts.TransportLevel = adapter.ClaudeTransportHookAdditionalContext
		resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
		return resp, dec, true, fmt.Errorf("retry: missing challenge nonce")
	}
	if rec.ChallengeNonce != "" && in.ChallengeNonce != rec.ChallengeNonce {
		dec := adapter.HookDecision{
			Action:     adapter.HookActionDeny,
			ReasonCode: "corrupted_challenge_nonce",
		}
		opts := cfg.Response
		opts.TransportLevel = adapter.ClaudeTransportHookAdditionalContext
		resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
		return resp, dec, true, fmt.Errorf("retry: corrupted challenge nonce")
	}

	// Justification handling
	if in.Justification != nil && rec.State == StateOpen {
		just := JustificationFromClaudeInput(in.ChallengeID, in.Justification)
		jRec, jErr := b.service.Justify(ctx, just, b.opts.KnownEvidenceIDs)
		if jErr != nil {
			dec := adapter.HookDecision{
				Action:     adapter.HookActionDeny,
				ReasonCode: "justification_rejected",
			}
			opts := cfg.Response
			opts.TransportLevel = adapter.ClaudeTransportHookAdditionalContext
			resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
			return resp, dec, true, jErr
		}
		rec = jRec
	}

	var pa adapter.ProposedAction
	if in.Proposed != nil {
		pa = *in.Proposed
	} else {
		pa = adapter.ProposedAction{
			SessionID: in.SessionID,
			ToolName:  in.ToolName,
			FilePath:  in.FilePath,
		}
	}

	reEval := b.opts.ReEvalContext
	if reEval == nil {
		reEval = &ReEvalContext{UserException: true}
	}

	attemptID := in.ToolUseID
	if attemptID == "" && in.Proposed != nil {
		attemptID = in.Proposed.ActionID
	}
	if attemptID == "" {
		attemptID = "inv-1"
	}

	retryReq := RetryRequest{
		ChallengeID:    in.ChallengeID,
		ChallengeNonce: in.ChallengeNonce,
		RequireNonce:   true,
		SessionID:      in.SessionID,
		Branch:         b.opts.Branch,
		RetryRequestID: in.SessionID + "-retry-" + in.ChallengeID + "-" + attemptID,
		Proposed:       pa,
		CorrelationID:  in.ChallengeID + "-retry-" + attemptID,
		ReEval:         reEval,
	}

	res, rErr := b.service.AttemptRetry(ctx, retryReq)
	if rErr != nil {
		if ctx != nil && ctx.Err() != nil {
			return adapter.ClaudeHookResponse{}, adapter.HookDecision{}, false, ctx.Err()
		}
		reason := res.RejectedReason
		if reason == "" {
			reason = "retry_rejected"
		}
		dec := adapter.HookDecision{
			Action:     adapter.HookActionDeny,
			ReasonCode: reason,
		}
		opts := cfg.Response
		opts.TransportLevel = adapter.ClaudeTransportHookAdditionalContext
		resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
		return resp, dec, true, nil
	}

	if res.Stage2Decision == DecisionAllow {
		dec := adapter.HookDecision{
			Action:     adapter.HookActionAllow,
			ReasonCode: "challenge_retry_allowed",
		}
		chCtx := adapter.ClaudeChallengeContext{
			ChallengeID:         res.Record.ChallengeID,
			ChallengeNonce:      res.Record.ChallengeNonce,
			Reason:              res.Record.ReasonCode,
			SuggestedFix:        res.Record.SuggestedFix,
			OneShotRetryAllowed: false,
		}
		opts := cfg.Response
		opts.Challenge = &chCtx
		opts.TransportLevel = adapter.ClaudeTransportHookAdditionalContext
		resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
		return resp, dec, true, nil
	}

	reason := res.RejectedReason
	if reason == "" {
		reason = "retry_rejected"
	}
	dec := adapter.HookDecision{
		Action:     adapter.HookActionDeny,
		ReasonCode: reason,
	}
	opts := cfg.Response
	opts.TransportLevel = adapter.ClaudeTransportHookAdditionalContext
	resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)
	return resp, dec, true, nil
}
