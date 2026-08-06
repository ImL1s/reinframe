package classifier

import (
	"fmt"
	"sort"
)

// ValidateProviderResultForRequest validates a nil-error ProviderResult against
// the exact request before any Stage-2 threshold scoring.
//
// Only a fully valid ParseStatus=ok assessment may enter genuine scoring.
// invalid/error assessments are allowed as closed representations but must not
// be treated as scored success by callers (they check ParseStatus separately).
func ValidateProviderResultForRequest(req ProviderRequest, res ProviderResult) error {
	if err := ValidateProviderResult(res); err != nil {
		return newProviderError("parse", err.Error(), false, 0)
	}
	a := res.Assessment
	switch a.ParseStatus {
	case ParseStatusOK, ParseStatusInvalid, ParseStatusError:
	default:
		return newProviderError("parse", "unknown parse_status", false, 0)
	}
	okStatus := a.ParseStatus == ParseStatusOK
	invalidStatus := a.ParseStatus == ParseStatusInvalid

	if a.SchemaVersion != SchemaRawAssessment {
		// malformed_output fixture may still carry schema when invalid; require always.
		return newProviderError("parse", "raw_assessment schema required", false, 0)
	}
	if okStatus {
		if !ValidateSeverity(a.Severity) {
			return newProviderError("parse", "severity out of range", false, 0)
		}
		if !ValidateRawReasonCode(a.ReasonCode) {
			return newProviderError("parse", "unknown reason_code", false, 0)
		}
		// Host-owned provenance must match request exactly.
		if a.PromptHash != req.Prompt.PromptHash {
			return newProviderError("parse", "prompt_hash mismatch", false, 0)
		}
		if a.RulesetID != req.Input.RulesetID {
			return newProviderError("parse", "ruleset_id mismatch", false, 0)
		}
		if a.RulesetHash != req.Input.RulesetHash {
			return newProviderError("parse", "ruleset_hash mismatch", false, 0)
		}
		if res.Meta.ModelID != "" && a.ModelID != "" && a.ModelID != res.Meta.ModelID {
			return newProviderError("parse", "model_id mismatch", false, 0)
		}
		if res.Meta.ModelVersion != "" && a.ModelVersion != "" && a.ModelVersion != res.Meta.ModelVersion {
			return newProviderError("parse", "model_version mismatch", false, 0)
		}
		if err := validateEvidenceAgainstShown(req.Input, a.EvidenceEventIDs); err != nil {
			return newProviderError("parse", err.Error(), false, 0)
		}
	} else if invalidStatus {
		// Closed invalid representation — no score semantics.
		if len(a.EvidenceEventIDs) > MaxEvidenceIDs {
			return newProviderError("parse", "too many evidence ids", false, 0)
		}
	}
	return nil
}

func validateEvidenceAgainstShown(in ClassifierInput, ids []string) error {
	if len(ids) > MaxEvidenceIDs {
		return fmt.Errorf("too many evidence ids")
	}
	allow := evidenceAllowlistShownOnly(in)
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("empty evidence id")
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("duplicate evidence id")
		}
		seen[id] = struct{}{}
		if _, ok := allow[id]; !ok {
			return fmt.Errorf("evidence id not in shown digests")
		}
	}
	return nil
}

// evidenceAllowlistShownOnly prefers digests actually shown; for fixture-legacy
// mode falls back to ID lists when digests empty.
func evidenceAllowlistShownOnly(in ClassifierInput) map[string]struct{} {
	if len(in.RecentEvents) > 0 || len(in.RelatedEvents) > 0 {
		return EvidenceAllowlistFromDigests(in.RecentEvents, in.RelatedEvents)
	}
	if in.AllowLegacyFixtureIDs {
		return AllowedEvidenceSet(in)
	}
	// Production: no digests → empty allowlist (any evidence fails closed).
	return map[string]struct{}{}
}

// SortedEvidenceIDs returns allowlist IDs in deterministic order.
func SortedEvidenceIDs(allow map[string]struct{}) []string {
	out := make([]string, 0, len(allow))
	for id := range allow {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
