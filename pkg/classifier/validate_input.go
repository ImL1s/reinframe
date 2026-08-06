package classifier

import (
	"encoding/json"
	"fmt"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// Closed challenge state/appealability allowlists (subset of #131).
var validChallengeStates = map[string]struct{}{
	"": {}, "OPEN": {}, "JUSTIFIED": {}, "RETRY_PENDING": {}, "ALLOWED_ONCE": {},
	"REJECTED": {}, "HUMAN_REVIEW": {}, "ABANDONED": {}, "EXPIRED": {},
}

var validAppealability = map[string]struct{}{
	"": {}, "APPEALABLE": {}, "NON_APPEALABLE": {}, "HUMAN_REVIEW": {},
}

// ValidateClassifierInput enforces closed schema and trajectory/challenge bounds.
func ValidateClassifierInput(in ClassifierInput) error {
	if in.SchemaVersion != SchemaClassifierInput {
		return fmt.Errorf("classifier: classifier_input schema required")
	}
	switch in.PolicyClass {
	case "", PolicyClassProductivity, PolicyClassSecurity:
	default:
		return fmt.Errorf("classifier: unknown policy_class")
	}
	if in.ContractRevision < 0 || in.EvidenceRevision < 0 {
		return fmt.Errorf("classifier: negative revision")
	}
	if err := ValidateTaskAnchor(in.TaskAnchor); err != nil {
		return err
	}
	if err := ValidateEventDigests(in.RecentEvents, MaxRecentEvents); err != nil {
		return err
	}
	if err := ValidateEventDigests(in.RelatedEvents, MaxRelatedEvents); err != nil {
		return err
	}
	if err := ValidateWindowMeta(in.Window); err != nil {
		return err
	}
	if in.ProposedAction != nil {
		if err := ValidateProposedActionForModel(*in.ProposedAction); err != nil {
			return err
		}
	}
	if in.Challenge != nil {
		if err := ValidateChallengeContext(*in.Challenge); err != nil {
			return err
		}
	}
	return nil
}

// ValidateProposedActionForModel rejects truncated/lossy/unknown projections for model calls.
func ValidateProposedActionForModel(pa adapter.ProposedAction) error {
	if pa.SchemaVersion != "" && pa.SchemaVersion != "reinframe.proposed_action.v1" {
		// Allow empty for synthetic fixtures that only set command/tool.
		if pa.SchemaVersion != "" {
			return fmt.Errorf("classifier: unsupported proposed_action schema")
		}
	}
	if pa.Truncated {
		return fmt.Errorf("classifier: truncated proposed_action rejected for model call")
	}
	if pa.ParseStatus != "" && pa.ParseStatus != "ok" && pa.ParseStatus != "partial" {
		// fail_closed / unknown_shape rejected for model-backed assessment
		if pa.ParseStatus == "fail_closed" || pa.ParseStatus == "unknown_shape" {
			return fmt.Errorf("classifier: proposed_action parse_status not model-safe")
		}
	}
	if len(pa.RedactedPayload) > 0 {
		if !json.Valid(pa.RedactedPayload) {
			return fmt.Errorf("classifier: malformed redacted_payload")
		}
	}
	if len(pa.Arguments) > 64 {
		return fmt.Errorf("classifier: too many arguments")
	}
	if len(pa.TargetScope) > 64 {
		return fmt.Errorf("classifier: too many target_scope entries")
	}
	return nil
}

// ValidateChallengeContext enforces closed enums and bounds.
func ValidateChallengeContext(c ChallengeContext) error {
	if _, ok := validChallengeStates[c.State]; !ok {
		return fmt.Errorf("classifier: unknown challenge state")
	}
	if _, ok := validAppealability[c.Appealability]; !ok {
		return fmt.Errorf("classifier: unknown appealability")
	}
	if c.RetryBudget < 0 || c.RetryBudget > 8 {
		return fmt.Errorf("classifier: invalid retry_budget")
	}
	if c.ExpiresAtSequence < 0 {
		return fmt.Errorf("classifier: invalid expires_at_sequence")
	}
	const maxStr = 2048
	for _, s := range []string{
		c.ChallengeID, c.BlockClass, c.ReasonCode, c.ConcreteValue,
		c.PreventedFailureOrThreat, c.EstimatedCost, c.AlternativesConsidered,
		c.ScopeLimit, c.VerificationPlan, c.RollbackPlan, c.OriginalActionID, c.ActionFingerprint,
	} {
		if len(s) > maxStr {
			return fmt.Errorf("classifier: challenge field too long")
		}
	}
	if len(c.Claims) > 16 || len(c.RequiredClaims) > 16 || len(c.EvidenceEventIDs) > MaxEvidenceIDs {
		return fmt.Errorf("classifier: challenge list too long")
	}
	if err := noDupStrings(c.Claims); err != nil {
		return err
	}
	if err := noDupStrings(c.RequiredClaims); err != nil {
		return err
	}
	if err := noDupStrings(c.EvidenceEventIDs); err != nil {
		return err
	}
	return nil
}

func noDupStrings(xs []string) error {
	seen := map[string]struct{}{}
	for _, x := range xs {
		if x == "" {
			continue
		}
		if _, ok := seen[x]; ok {
			return fmt.Errorf("classifier: duplicate list entry")
		}
		seen[x] = struct{}{}
	}
	return nil
}
