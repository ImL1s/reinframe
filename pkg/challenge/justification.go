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
// knownEvidence must contain every referenced ID; duplicates are rejected.
// Returns sanitized justification (fields trimmed/bounded) or error.
// Justification never auto-grants permission; this only validates form.
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

	// Reject private CoT field names if smuggled via unknown JSON — we only accept closed fields.
	// (Unmarshal into struct already drops unknown JSON keys when using encoding/json into struct.)

	fields := map[string]string{
		"concrete_value":              j.ConcreteValue,
		"prevented_failure_or_threat": j.PreventedFailureOrThreat,
		"estimated_cost":              j.EstimatedCost,
		"alternatives_considered":     j.AlternativesConsidered,
		"scope_limit":                 j.ScopeLimit,
		"verification_plan":           j.VerificationPlan,
		"rollback_plan":               j.RollbackPlan,
	}
	for name, v := range fields {
		if err := checkField(name, v); err != nil {
			return Justification{}, err
		}
	}

	// Required claims (non-empty after trim).
	for _, claim := range requiredClaims {
		switch claim {
		case "concrete_value":
			if strings.TrimSpace(j.ConcreteValue) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim concrete_value")
			}
		case "prevented_failure_or_threat":
			if strings.TrimSpace(j.PreventedFailureOrThreat) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim prevented_failure_or_threat")
			}
		case "estimated_cost":
			if strings.TrimSpace(j.EstimatedCost) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim estimated_cost")
			}
		case "scope_limit":
			if strings.TrimSpace(j.ScopeLimit) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim scope_limit")
			}
		case "verification_plan":
			if strings.TrimSpace(j.VerificationPlan) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim verification_plan")
			}
		case "rollback_plan":
			if strings.TrimSpace(j.RollbackPlan) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim rollback_plan")
			}
		case "supporting_evidence_event_ids":
			if len(j.SupportingEvidenceEventIDs) == 0 {
				return Justification{}, fmt.Errorf("justification: missing required claim supporting_evidence_event_ids")
			}
		case "alternatives_considered":
			if strings.TrimSpace(j.AlternativesConsidered) == "" {
				return Justification{}, fmt.Errorf("justification: missing required claim alternatives_considered")
			}
		}
	}

	// Evidence IDs: known, unique, bounded.
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
		// Always validate against the known set (empty known ⇒ any ID is unknown).
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
			// Injection text is rejected as invalid justification content.
			// It never alters policy; we refuse the submission.
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

// HashJustification returns a stable content hash (no secrets expected in schema).
func HashJustification(j Justification) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%s|%s|%s|%v",
		j.SchemaVersion, j.ChallengeID, j.ConcreteValue, j.PreventedFailureOrThreat,
		j.EstimatedCost, j.AlternativesConsidered, j.ScopeLimit, j.VerificationPlan,
		j.RollbackPlan, j.SupportingEvidenceEventIDs)
	return hex.EncodeToString(h.Sum(nil))[:32]
}
