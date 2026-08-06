package challenge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

// ReEvalContext carries current hard rules / contract / optional classifier inputs
// for re-evaluation of a justified retry. Justification is evidence only.
type ReEvalContext struct {
	// HardBlock forces Stage 2 BLOCK (e.g. still-denied tool/path).
	HardBlock  bool
	HardReason string
	// Irreversible forces HUMAN_REVIEW intervention + BLOCK decision.
	Irreversible bool
	// Contract/evidence revision for cache identity (optional).
	ContractRevision  int
	WorkspaceRevision string
	RulesetHash       string
	PolicyHash        string
	ModelID           string
	PromptHash        string
	// Optional Stage1 provider; if nil, uses deterministic defaults.
	Provider classifier.ClassifierProvider
	// FixtureName routes FakeClassifierProvider.
	FixtureName string
	// Threshold provisional (default 50).
	Threshold int
	// Exception flags (user/repo/flaky) — may flip BLOCK→ALLOW at Stage2 only.
	UserException       bool
	RepoPolicyException bool
	FlakyInvestigation  bool
	// RecentEventIDs for classifier evidence validation (legacy).
	RecentEventIDs []string
	// RelatedEventIDs legacy related evidence.
	RelatedEventIDs []string
	// Trajectory packet (wire §5) — binds TaskAnchor + digests into Stage-1 prompt.
	TaskAnchor    classifier.TaskAnchor
	RecentEvents  []classifier.EventDigest
	RelatedEvents []classifier.EventDigest
	// EvidenceRevision pairs with ContractRevision for cache identity.
	EvidenceRevision int
	// PolicyClass defaults to PRODUCTIVITY.
	PolicyClass string
	// Observer receives best-effort provider-call audits (nil-safe).
	Observer classifier.ProviderCallObserver
}

// ReEvalResult is the re-evaluation outcome for a retry.
type ReEvalResult struct {
	Stage2Decision string // ALLOW | BLOCK only
	Intervention   string // none | HUMAN_REVIEW (APPEALABLE already established)
	Reason         string
	RawSeverity    int
	// ProviderCall retains closed provider-call audit when Stage1 ran (may be nil).
	ProviderCall *classifier.ProviderCallAudit
}

// ReEvaluator re-runs hard rules → contract/evidence → optional classifier → Stage2.
type ReEvaluator interface {
	ReEvaluate(ctx context.Context, rec ChallengeRecord, proposed adapter.ProposedAction, just *Justification, in *ReEvalContext) (ReEvalResult, error)
}

// DefaultReEvaluator implements the #131 re-evaluation pipeline.
// Justification never automatically grants ALLOW.
type DefaultReEvaluator struct{}

// ReEvaluate runs the deterministic pipeline.
func (DefaultReEvaluator) ReEvaluate(ctx context.Context, rec ChallengeRecord, proposed adapter.ProposedAction, just *Justification, in *ReEvalContext) (ReEvalResult, error) {
	if in == nil {
		in = &ReEvalContext{}
	}
	if in.PolicyClass == "" {
		in.PolicyClass = rec.PolicyClass
	}
	in.PolicyClass = NormalizePolicyClass(in.PolicyClass)
	if in.Threshold <= 0 {
		in.Threshold = 50
	}

	// 1) Hard rules
	if in.HardBlock || IsHardDenyClass(rec.BlockClass) {
		return ReEvalResult{
			Stage2Decision: DecisionBlock,
			Intervention:   InterventionNone,
			Reason:         "hard_rule_block",
		}, nil
	}
	if LooksLikeSecretExfil(proposed) || LooksLikeCrossWorkspace(proposed) {
		return ReEvalResult{
			Stage2Decision: DecisionBlock,
			Intervention:   InterventionNone,
			Reason:         "hard_security_block",
		}, nil
	}

	// 2) Irreversible / high-impact → HUMAN_REVIEW + BLOCK (never self-allow)
	if in.Irreversible || IsIrreversibleClass(rec.BlockClass) {
		return ReEvalResult{
			Stage2Decision: DecisionBlock,
			Intervention:   InterventionHumanReview,
			Reason:         "human_review_required",
		}, nil
	}
	side, _, _ := classifySideEffect(proposed)
	switch side {
	case SideEffectDeploy, SideEffectPayment, SideEffectPermission:
		return ReEvalResult{
			Stage2Decision: DecisionBlock,
			Intervention:   InterventionHumanReview,
			Reason:         "irreversible_side_effect",
		}, nil
	}

	// 3) Optional classifier (Stage1) + 4) Stage2 resolver
	// Justification presence is NOT an exception flag — it is only new evidence IDs.
	if in.Provider != nil {
		pa := proposed
		cin := classifier.ClassifierInput{
			SchemaVersion:       classifier.SchemaClassifierInput,
			SessionID:           rec.SessionID,
			FixtureName:         in.FixtureName,
			PolicyClass:         in.PolicyClass,
			RulesetHash:         firstNonEmpty(in.RulesetHash, rec.RulesetHash),
			ProposedAction:      &pa,
			TaskAnchor:          in.TaskAnchor,
			ContractRevision:    in.ContractRevision,
			EvidenceRevision:    in.EvidenceRevision,
			RecentEvents:        append([]classifier.EventDigest(nil), in.RecentEvents...),
			RelatedEvents:       append([]classifier.EventDigest(nil), in.RelatedEvents...),
			RecentEventIDs:      append([]string(nil), in.RecentEventIDs...),
			RelatedEventIDs:     append([]string(nil), in.RelatedEventIDs...),
			UserException:       in.UserException,
			RepoPolicyException: in.RepoPolicyException,
			FlakyInvestigation:  in.FlakyInvestigation,
		}
		// Closed challenge/justification summary for model-backed re-eval (no private CoT).
		ch := &classifier.ChallengeContext{
			ChallengeID:       rec.ChallengeID,
			State:             string(rec.State),
			BlockClass:        rec.BlockClass,
			ReasonCode:        rec.ReasonCode,
			Appealability:     rec.Appealability,
			RequiredClaims:    append([]string(nil), rec.RequiredClaims...),
			RetryBudget:       rec.RetryBudget,
			ExpiresAtSequence: rec.ExpiresAtSequence,
			OriginalActionID:  rec.OriginalActionID,
			ActionFingerprint: rec.ActionFingerprint,
			// Claims remain closed claim names only; RequiredClaims is the closed checklist.
			Claims: append([]string(nil), rec.RequiredClaims...),
		}
		if just != nil {
			ch.ConcreteValue = just.ConcreteValue
			ch.PreventedFailureOrThreat = just.PreventedFailureOrThreat
			ch.EstimatedCost = just.EstimatedCost
			ch.AlternativesConsidered = just.AlternativesConsidered
			ch.ScopeLimit = just.ScopeLimit
			ch.VerificationPlan = just.VerificationPlan
			ch.RollbackPlan = just.RollbackPlan
			ch.EvidenceEventIDs = append([]string(nil), just.SupportingEvidenceEventIDs...)
			// Include justification evidence ids as related evidence (not auto-allow).
			cin.RelatedEventIDs = append([]string(nil), just.SupportingEvidenceEventIDs...)
		}
		cin.Challenge = ch
		if in.ContractRevision > 0 {
			cin.ContractRevision = in.ContractRevision
		}
		preq, perr := classifier.NewProviderRequest(cin)
		if perr != nil {
			if in.PolicyClass == PolicyClassSecurity {
				return ReEvalResult{Stage2Decision: DecisionBlock, Reason: "provider_fail_closed"}, nil
			}
			return ReEvalResult{Stage2Decision: DecisionAllow, Reason: "provider_fail_open", RawSeverity: 0}, nil
		}
		pres, err := in.Provider.Assess(ctx, preq)
		raw := pres.Assessment
		// Always retain closed provider-call audit (Usage/Meta); observer is best-effort.
		a := classifier.BuildProviderCallAudit(preq, pres, rec.SessionID, rec.ChallengeID, time.Now().UTC())
		if err != nil {
			var pe *classifier.ProviderError
			if errors.As(err, &pe) && pe != nil {
				a.ErrorClass = pe.Class
				a.FallbackReason = pe.Class
			} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				a.ErrorClass = "canceled"
				a.FallbackReason = "canceled"
			} else {
				a.ErrorClass = "transport"
				a.FallbackReason = "transport"
			}
			a.ParseStatus = classifier.ParseStatusError
		}
		auditPtr := &a
		if in.Observer != nil {
			_ = in.Observer.RecordProviderCall(ctx, a) // best-effort; never alters policy
		}
		if err != nil {
			// Parent cancel/deadline: raw identity (never fail-open / persist ALLOWED_ONCE).
			// Adapter-owned timeout is typed ProviderError{Class:"timeout"} — ordinary matrix.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ReEvalResult{Stage2Decision: DecisionBlock, Reason: "provider_context_canceled", ProviderCall: auditPtr}, err
			}
			if in.PolicyClass == PolicyClassSecurity {
				return ReEvalResult{Stage2Decision: DecisionBlock, Reason: "provider_fail_closed", ProviderCall: auditPtr}, nil
			}
			// Productivity fail-open on provider error (incl. adapter timeout) — not justification auto-allow.
			return ReEvalResult{Stage2Decision: DecisionAllow, Reason: "provider_fail_open", RawSeverity: 0, ProviderCall: auditPtr}, nil
		}
		// Stage 2 threshold
		if raw.ParseStatus != "ok" && raw.ParseStatus != classifier.ParseStatusOK {
			if in.PolicyClass == PolicyClassSecurity {
				return ReEvalResult{Stage2Decision: DecisionBlock, Reason: "parse_fail_closed", ProviderCall: auditPtr}, nil
			}
			return ReEvalResult{Stage2Decision: DecisionAllow, Reason: "parse_fail_open", ProviderCall: auditPtr}, nil
		}
		if !classifier.ValidateSeverity(raw.Severity) {
			if in.PolicyClass == PolicyClassSecurity {
				return ReEvalResult{Stage2Decision: DecisionBlock, Reason: "severity_fail_closed", ProviderCall: auditPtr}, nil
			}
			return ReEvalResult{Stage2Decision: DecisionAllow, Reason: "severity_fail_open", ProviderCall: auditPtr}, nil
		}
		dec := DecisionAllow
		reason := "below_threshold"
		if raw.Severity >= in.Threshold {
			dec = DecisionBlock
			reason = "above_threshold"
		}
		// Exceptions only (not justification auto-allow)
		if dec == DecisionBlock {
			if in.UserException {
				dec = DecisionAllow
				reason = "user_exception"
			} else if in.RepoPolicyException {
				dec = DecisionAllow
				reason = "repo_policy_exception"
			} else if in.FlakyInvestigation {
				dec = DecisionAllow
				reason = "flaky_investigation"
			}
		}
		return ReEvalResult{
			Stage2Decision: dec,
			Intervention:   InterventionNone,
			Reason:         reason,
			RawSeverity:    raw.Severity,
			ProviderCall:   auditPtr,
		}, nil
	}

	// No provider: deterministic default remains BLOCK for retry of originally blocked action
	// unless explicit exception flags. Justification alone does not flip.
	if in.UserException || in.RepoPolicyException || in.FlakyInvestigation {
		return ReEvalResult{
			Stage2Decision: DecisionAllow,
			Intervention:   InterventionNone,
			Reason:         "exception_without_provider",
		}, nil
	}
	return ReEvalResult{
		Stage2Decision: DecisionBlock,
		Intervention:   InterventionNone,
		Reason:         "default_block_no_auto_allow",
	}, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
