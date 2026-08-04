package challenge

import (
	"strings"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

// ClassifyAppealability maps a block class (+ optional action) to appeal behavior.
// Hard security denials are never self-appealable. Irreversible/high-impact route to HUMAN_REVIEW.
// Unknown block classes fail closed to HUMAN_REVIEW (no self-appeal).
func ClassifyAppealability(blockClass string, pa adapter.ProposedAction) (appeal string, intervention string) {
	bc := strings.ToUpper(strings.TrimSpace(blockClass))
	// Infer from action when block class empty/unknown.
	if bc == "" || bc == BlockClassUnknownSecurity {
		if LooksLikeSecretExfil(pa) {
			bc = BlockClassSecretExfiltration
		} else if LooksLikeCrossWorkspace(pa) {
			bc = BlockClassCrossWorkspace
		} else {
			side, _ := classifySideEffect(pa)
			switch side {
			case SideEffectDeploy:
				bc = BlockClassProductionDeploy
			case SideEffectPayment:
				bc = BlockClassPayment
			case SideEffectPermission:
				bc = BlockClassPermissionChange
			case SideEffectDeleteTree:
				// Remote/high-impact deletion may be human review when path looks remote.
				if strings.Contains(pa.Command, "://") || strings.Contains(pa.Command, "s3://") {
					bc = BlockClassRemoteDeletion
				}
			default:
				if bc == BlockClassUnknownSecurity {
					// keep unknown security
				} else if bc == "" {
					// empty class with no strong security signal stays unknown → fail closed below
					bc = BlockClassUnknownSecurity
				}
			}
		}
	}

	switch bc {
	case BlockClassSecretExfiltration, BlockClassExplicitDeny, BlockClassCrossWorkspace:
		return AppealNonAppealable, InterventionNone
	case BlockClassProductionDeploy, BlockClassPayment, BlockClassRemoteDeletion, BlockClassPermissionChange:
		return AppealHumanReview, InterventionHumanReview
	case BlockClassUnknownSecurity:
		// Fail closed: human review; no self-appeal.
		return AppealHumanReview, InterventionHumanReview
	case BlockClassScopeDrift, BlockClassOverSOP, BlockClassExpensiveHardening,
		BlockClassRepeatedExploration, BlockClassEvidenceGap, BlockClassProductivityGeneric:
		return AppealAppealable, InterventionAppealableChallenge
	default:
		// Unknown codes fail closed — do not invent self-appeal.
		return AppealHumanReview, InterventionHumanReview
	}
}

// IsHardDenyClass reports non-appealable security classes.
func IsHardDenyClass(blockClass string) bool {
	switch strings.ToUpper(strings.TrimSpace(blockClass)) {
	case BlockClassSecretExfiltration, BlockClassExplicitDeny, BlockClassCrossWorkspace:
		return true
	default:
		return false
	}
}

// IsIrreversibleClass reports classes that must route to HUMAN_REVIEW.
func IsIrreversibleClass(blockClass string) bool {
	switch strings.ToUpper(strings.TrimSpace(blockClass)) {
	case BlockClassProductionDeploy, BlockClassPayment, BlockClassRemoteDeletion,
		BlockClassPermissionChange, BlockClassUnknownSecurity:
		return true
	default:
		return false
	}
}

// DefaultRequiredClaims for appealable productivity blocks.
func DefaultRequiredClaims(blockClass string) []string {
	base := []string{
		"concrete_value",
		"prevented_failure_or_threat",
		"estimated_cost",
		"scope_limit",
		"verification_plan",
		"rollback_plan",
	}
	if strings.EqualFold(blockClass, BlockClassEvidenceGap) {
		return append(base, "supporting_evidence_event_ids")
	}
	return base
}

// Ensure productivity policy class constant available without import cycles in tests.
const PolicyClassProductivity = classifier.PolicyClassProductivity
const PolicyClassSecurity = classifier.PolicyClassSecurity
