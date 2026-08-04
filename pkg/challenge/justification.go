package challenge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Justification field bounds (closed, fail closed).
const (
	MaxJustificationFieldRunes = 500
	MaxEvidenceIDs             = 16
	MaxEvidenceIDRunes         = 128
)

// injection patterns that must never alter policy/provider/rules configuration.
var injectionMarkers = []string{
	"ignore previous",
	"ignore all previous",
	"system prompt",
	"override policy",
	"set policy",
	"ruleset=",
	"provider=",
	"model=",
	"api_key",
	"authorization:",
	"BEGIN PRIVATE",
	"chain-of-thought",
	"chain of thought",
	"<policy>",
	"</policy>",
	"allow unconditionally",
	"force allow",
	"decision=allow",
	"stage2=allow",
}

// ValidateJustification checks closed schema + bounds + evidence IDs + injection resistance.
// requiredClaims must already be validated via ValidateRequiredClaims (allowlist).
func ValidateJustification(j Justification, knownEvidence []string, requiredClaims []string) (Justification, error) {
	if strings.TrimSpace(j.SchemaVersion) == "" {
		j.SchemaVersion = SchemaJustification
	}
	if j.SchemaVersion != SchemaJustification {
		return Justification{}, fmt.Errorf("justification: unsupported schema_version %q", j.SchemaVersion)
	}
	if strings.TrimSpace(j.ChallengeID) == "" {
		return Justification{}, fmt.Errorf("justification: challenge_id required")
	}

	fields := map[string]string{
		ClaimConcreteValue:            j.ConcreteValue,
		ClaimPreventedFailureOrThreat: j.PreventedFailureOrThreat,
		ClaimEstimatedCost:            j.EstimatedCost,
		ClaimAlternativesConsidered:   j.AlternativesConsidered,
		ClaimScopeLimit:               j.ScopeLimit,
		ClaimVerificationPlan:         j.VerificationPlan,
		ClaimRollbackPlan:             j.RollbackPlan,
	}
	for name, v := range fields {
		if err := checkField(name, v); err != nil {
			return Justification{}, err
		}
	}

	// Required claims — every name must be allowlisted; default error branch for unknown.
	for _, claim := range requiredClaims {
		claim = strings.TrimSpace(claim)
		if !IsValidClaim(claim) {
			return Justification{}, fmt.Errorf("justification: unsupported required claim %q", claim)
		}
		switch claim {
		case ClaimConcreteValue:
			if strings.TrimSpace(j.ConcreteValue) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim %s", claim)
			}
		case ClaimPreventedFailureOrThreat:
			if strings.TrimSpace(j.PreventedFailureOrThreat) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim %s", claim)
			}
		case ClaimEstimatedCost:
			if strings.TrimSpace(j.EstimatedCost) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim %s", claim)
			}
		case ClaimScopeLimit:
			if strings.TrimSpace(j.ScopeLimit) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim %s", claim)
			}
		case ClaimVerificationPlan:
			if strings.TrimSpace(j.VerificationPlan) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim %s", claim)
			}
		case ClaimRollbackPlan:
			if strings.TrimSpace(j.RollbackPlan) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim %s", claim)
			}
		case ClaimSupportingEvidenceIDs:
			if len(j.SupportingEvidenceEventIDs) == 0 {
				return Justification{}, fmt.Errorf("justification: missing required claim %s", claim)
			}
		case ClaimAlternativesConsidered:
			if strings.TrimSpace(j.AlternativesConsidered) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim %s", claim)
			}
		default:
			// Explicit default — never silently drop a policy-required field.
			return Justification{}, fmt.Errorf("justification: unhandled required claim %q", claim)
		}
	}

	known := map[string]struct{}{}
	for _, id := range knownEvidence {
		known[id] = struct{}{}
	}
	if len(j.SupportingEvidenceEventIDs) > MaxEvidenceIDs {
		return Justification{}, fmt.Errorf("justification: too many evidence ids")
	}
	seen := map[string]struct{}{}
	cleanIDs := make([]string, 0, len(j.SupportingEvidenceEventIDs))
	for _, id := range j.SupportingEvidenceEventIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			return Justification{}, fmt.Errorf("justification: empty evidence id")
		}
		if utf8.RuneCountInString(id) > MaxEvidenceIDRunes {
			return Justification{}, fmt.Errorf("justification: evidence id too long")
		}
		if _, dup := seen[id]; dup {
			return Justification{}, fmt.Errorf("justification: duplicate evidence id %q", id)
		}
		seen[id] = struct{}{}
		if _, ok := known[id]; !ok {
			return Justification{}, fmt.Errorf("justification: unknown evidence id %q", id)
		}
		if err := checkInjection("evidence_id", id); err != nil {
			return Justification{}, err
		}
		cleanIDs = append(cleanIDs, id)
	}

	out := Justification{
		SchemaVersion:              SchemaJustification,
		ChallengeID:                strings.TrimSpace(j.ChallengeID),
		ConcreteValue:              boundField(j.ConcreteValue),
		PreventedFailureOrThreat:   boundField(j.PreventedFailureOrThreat),
		EstimatedCost:              boundField(j.EstimatedCost),
		AlternativesConsidered:     boundField(j.AlternativesConsidered),
		ScopeLimit:                 boundField(j.ScopeLimit),
		VerificationPlan:           boundField(j.VerificationPlan),
		RollbackPlan:               boundField(j.RollbackPlan),
		SupportingEvidenceEventIDs: cleanIDs,
	}
	return out, nil
}

func checkField(name, v string) error {
	if utf8.RuneCountInString(v) > MaxJustificationFieldRunes {
		return fmt.Errorf("justification: field %s exceeds max runes", name)
	}
	return checkInjection(name, v)
}

func checkInjection(name, v string) error {
	low := strings.ToLower(v)
	for _, m := range injectionMarkers {
		if strings.Contains(low, strings.ToLower(m)) {
			return fmt.Errorf("justification: field %s contains disallowed control text", name)
		}
	}
	return nil
}

func boundField(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= MaxJustificationFieldRunes {
		return s
	}
	r := []rune(s)
	return string(r[:MaxJustificationFieldRunes])
}

// HashJustification returns a collision-free content hash (length-prefixed fields).
// Distinguishes | in values, empty vs absent, evidence delimiter/order.
func HashJustification(j Justification) string {
	h := sha256.New()
	fields := []struct {
		k, v string
	}{
		{"schema", j.SchemaVersion},
		{"challenge_id", j.ChallengeID},
		{"concrete_value", j.ConcreteValue},
		{"prevented_failure_or_threat", j.PreventedFailureOrThreat},
		{"estimated_cost", j.EstimatedCost},
		{"alternatives_considered", j.AlternativesConsidered},
		{"scope_limit", j.ScopeLimit},
		{"verification_plan", j.VerificationPlan},
		{"rollback_plan", j.RollbackPlan},
		{"evidence", encodeStringList(j.SupportingEvidenceEventIDs)},
	}
	_, _ = fmt.Fprintf(h, "n=%d", len(fields))
	for _, f := range fields {
		_, _ = fmt.Fprintf(h, ";k=%d:%s;v=%d:%s", len(f.k), f.k, len(f.v), f.v)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
