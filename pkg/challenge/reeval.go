package challenge

import (
	"context"
	"strings"

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
	// RecentEventIDs for classifier evidence validation.
	RecentEventIDs []string
	// PolicyClass defaults to PRODUCTIVITY.
	PolicyClass string
}

// ReEvalResult is the re-evaluation outcome for a retry.
type ReEvalResult struct {
	Stage2Decision string // ALLOW | BLOCK only
	Intervention   string // none | HUMAN_REVIEW (APPEALABLE already established)
	Reason         string
	RawSeverity    int
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
	if in.PolicyClass == "" {
		in.PolicyClass = PolicyClassProductivity
	}
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
	side, _ := classifySideEffect(proposed)
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
			RecentEventIDs:      append([]string(nil), in.RecentEventIDs...),
			UserException:       in.UserException,
			RepoPolicyException: in.RepoPolicyException,
			FlakyInvestigation:  in.FlakyInvestigation,
		}
		// Include justification evidence ids as related evidence (not auto-allow).
		if just != nil {
			cin.RelatedEventIDs = append([]string(nil), just.SupportingEvidenceEventIDs...)
		}
		raw, err := in.Provider.Assess(ctx, cin)
		if err != nil {
			if in.PolicyClass == PolicyClassSecurity {
				return ReEvalResult{Stage2Decision: DecisionBlock, Reason: "provider_fail_closed"}, nil
			}
			// Productivity fail-open on provider error — still not "auto allow from justification".
			return ReEvalResult{Stage2Decision: DecisionAllow, Reason: "provider_fail_open", RawSeverity: 0}, nil
		}
		// Stage 2 threshold
		if raw.ParseStatus != "ok" {
			if in.PolicyClass == PolicyClassSecurity {
				return ReEvalResult{Stage2Decision: DecisionBlock, Reason: "parse_fail_closed"}, nil
			}
			return ReEvalResult{Stage2Decision: DecisionAllow, Reason: "parse_fail_open"}, nil
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
