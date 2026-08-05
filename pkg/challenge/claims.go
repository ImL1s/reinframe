package challenge

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// Closed allowlist of RequiredClaims / justification claim names (#131).
const (
	ClaimConcreteValue            = "concrete_value"
	ClaimPreventedFailureOrThreat = "prevented_failure_or_threat"
	ClaimEstimatedCost            = "estimated_cost"
	ClaimAlternativesConsidered   = "alternatives_considered"
	ClaimScopeLimit               = "scope_limit"
	ClaimVerificationPlan         = "verification_plan"
	ClaimRollbackPlan             = "rollback_plan"
	ClaimSupportingEvidenceIDs    = "supporting_evidence_event_ids"
)

// ValidClaimNames is the single canonical allowlist (closed).
var ValidClaimNames = map[string]struct{}{
	ClaimConcreteValue:            {},
	ClaimPreventedFailureOrThreat: {},
	ClaimEstimatedCost:            {},
	ClaimAlternativesConsidered:   {},
	ClaimScopeLimit:               {},
	ClaimVerificationPlan:         {},
	ClaimRollbackPlan:             {},
	ClaimSupportingEvidenceIDs:    {},
}

const maxClaimNameRunes = 64
const maxRequiredClaims = 16

// ValidateRequiredClaims rejects unknown, misspelled, duplicate, empty, or oversized names.
// Returns canonical sorted unique list (stable for fingerprint/policy reuse).
func ValidateRequiredClaims(claims []string) ([]string, error) {
	if len(claims) == 0 {
		return nil, nil
	}
	if len(claims) > maxRequiredClaims {
		return nil, fmt.Errorf("required_claims: too many claims")
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(claims))
	for _, c := range claims {
		c = strings.TrimSpace(c)
		if c == "" {
			return nil, fmt.Errorf("required_claims: empty claim name")
		}
		if utf8.RuneCountInString(c) > maxClaimNameRunes {
			return nil, fmt.Errorf("required_claims: claim name too long")
		}
		if _, ok := ValidClaimNames[c]; !ok {
			return nil, fmt.Errorf("required_claims: unknown claim %q", c)
		}
		if _, dup := seen[c]; dup {
			return nil, fmt.Errorf("required_claims: duplicate claim %q", c)
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

// IsValidClaim reports whether name is on the closed allowlist.
func IsValidClaim(name string) bool {
	_, ok := ValidClaimNames[strings.TrimSpace(name)]
	return ok
}
