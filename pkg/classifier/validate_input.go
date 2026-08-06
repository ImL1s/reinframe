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

// ValidateClassifierInput enforces closed schema and trajectory/challenge bounds
// under default (non-strict fixture-compatible) options.
func ValidateClassifierInput(in ClassifierInput) error {
	return ValidateClassifierInputOpts(in, ProviderRequestOptions{AllowLegacyFixtureIDs: in.AllowLegacyFixtureIDs})
}

// ValidateClassifierInputOpts enforces closed schema; Production tightens enums/anchors.
func ValidateClassifierInputOpts(in ClassifierInput, opts ProviderRequestOptions) error {
	if in.SchemaVersion != SchemaClassifierInput {
		return fmt.Errorf("classifier: classifier_input schema required")
	}
	if opts.Production {
		switch in.PolicyClass {
		case PolicyClassProductivity, PolicyClassSecurity:
		default:
			return fmt.Errorf("classifier: production policy_class required")
		}
	} else {
		switch in.PolicyClass {
		case "", PolicyClassProductivity, PolicyClassSecurity:
		default:
			return fmt.Errorf("classifier: unknown policy_class")
		}
	}
	if in.ContractRevision < 0 || in.EvidenceRevision < 0 {
		return fmt.Errorf("classifier: negative revision")
	}
	if err := ValidateTaskAnchor(in.TaskAnchor); err != nil {
		return err
	}
	if opts.Production && (len(in.RecentEvents) > 0 || len(in.RelatedEvents) > 0) {
		if in.TaskAnchor.TaskID == "" || in.TaskAnchor.Objective == "" {
			return fmt.Errorf("classifier: production task_anchor required with trajectory")
		}
	}
	if err := ValidateEventDigests(in.RecentEvents, MaxRecentEvents); err != nil {
		return err
	}
	if err := ValidateEventDigests(in.RelatedEvents, MaxRelatedEvents); err != nil {
		return err
	}
	// Cross-list uniqueness.
	if err := noDupAcross(in.RecentEvents, in.RelatedEvents); err != nil {
		return err
	}
	if len(in.RecentEvents) > 0 || len(in.RelatedEvents) > 0 {
		if err := ValidateWindowMetaExact(in.Window, in.RecentEvents, in.RelatedEvents); err != nil {
			return err
		}
	} else {
		if err := ValidateWindowMeta(in.Window); err != nil {
			return err
		}
	}
	if in.ProposedAction != nil {
		if opts.Production {
			if err := ValidateProposedActionProduction(*in.ProposedAction); err != nil {
				return err
			}
		} else if err := ValidateProposedActionForModel(*in.ProposedAction); err != nil {
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

func noDupAcross(a, b []EventDigest) error {
	seen := map[string]struct{}{}
	for _, e := range a {
		if e.EventID == "" {
			continue
		}
		if _, ok := seen[e.EventID]; ok {
			return fmt.Errorf("classifier: duplicate event_id across lists")
		}
		seen[e.EventID] = struct{}{}
	}
	for _, e := range b {
		if e.EventID == "" {
			continue
		}
		if _, ok := seen[e.EventID]; ok {
			return fmt.Errorf("classifier: duplicate event_id across lists")
		}
		seen[e.EventID] = struct{}{}
	}
	return nil
}

// ValidateProposedActionForModel rejects truncated/lossy/unknown projections for model calls.
// Fixture-compatible: empty schema and partial parse_status allowed.
func ValidateProposedActionForModel(pa adapter.ProposedAction) error {
	if pa.SchemaVersion != "" && pa.SchemaVersion != "reinframe.proposed_action.v1" &&
		pa.SchemaVersion != adapter.ProposedActionSchemaVersion {
		return fmt.Errorf("classifier: unsupported proposed_action schema")
	}
	if pa.Truncated {
		return fmt.Errorf("classifier: truncated proposed_action rejected for model call")
	}
	if pa.ParseStatus != "" && pa.ParseStatus != "ok" && pa.ParseStatus != "partial" {
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

// ValidateProposedActionProduction requires exact schema and parse_status=ok.
func ValidateProposedActionProduction(pa adapter.ProposedAction) error {
	if pa.SchemaVersion != "reinframe.proposed_action.v1" && pa.SchemaVersion != adapter.ProposedActionSchemaVersion {
		return fmt.Errorf("classifier: proposed_action schema required")
	}
	if pa.Truncated {
		return fmt.Errorf("classifier: truncated proposed_action rejected")
	}
	if pa.ParseStatus != "ok" {
		return fmt.Errorf("classifier: proposed_action parse_status must be ok")
	}
	return ValidateProposedActionForModel(pa)
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
