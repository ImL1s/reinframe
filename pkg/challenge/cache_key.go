package challenge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// BuildCacheKeyInputs constructs assessment identity inputs for #131 / future #138.
// Does not implement a cache. Includes challenge state and justification_hash so
// new justification/evidence invalidates prior exact-assessment entries.
func BuildCacheKeyInputs(rec ChallengeRecord, evidenceIDs []string, modelID, promptHash string) CacheKeyInputs {
	ids := append([]string(nil), evidenceIDs...)
	sort.Strings(ids)
	eh := hashStrings(ids)
	in := CacheKeyInputs{
		SchemaVersion:     SchemaCacheKeyInputs,
		SessionID:         rec.SessionID,
		ChallengeID:       rec.ChallengeID,
		ChallengeState:    string(rec.State),
		ActionFingerprint: rec.ActionFingerprint,
		JustificationHash: rec.JustificationHash,
		EvidenceIDsHash:   eh,
		ContractRevision:  rec.ContractRevision,
		WorkspaceRevision: rec.WorkspaceRevision,
		RulesetHash:       rec.RulesetHash,
		PolicyHash:        rec.PolicyHash,
		ModelID:           modelID,
		PromptHash:        promptHash,
	}
	in.CanonicalKey = hashCacheKey(in)
	return in
}

// CacheKeyChanges reports whether two key inputs differ (invalidation).
func CacheKeyChanges(a, b CacheKeyInputs) bool {
	return a.CanonicalKey != b.CanonicalKey
}

func hashCacheKey(in CacheKeyInputs) string {
	// Length-prefixed fields — never join raw caller strings with '|' (collision risk).
	h := sha256.New()
	fields := []struct{ k, v string }{
		{"schema", in.SchemaVersion},
		{"session", in.SessionID},
		{"challenge", in.ChallengeID},
		{"state", in.ChallengeState},
		{"fp", in.ActionFingerprint},
		{"just", in.JustificationHash},
		{"evidence", in.EvidenceIDsHash},
		{"contract", fmt.Sprintf("%d", in.ContractRevision)},
		{"ws", in.WorkspaceRevision},
		{"ruleset", in.RulesetHash},
		{"policy", in.PolicyHash},
		{"model", in.ModelID},
		{"prompt", in.PromptHash},
	}
	_, _ = fmt.Fprintf(h, "n=%d", len(fields))
	for _, f := range fields {
		_, _ = fmt.Fprintf(h, ";k=%d:%s;v=%d:%s", len(f.k), f.k, len(f.v), f.v)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func hashStrings(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	// Length-prefixed encoding avoids comma-collision between ID sets.
	h := sha256.New()
	for _, id := range ids {
		_, _ = fmt.Fprintf(h, "%d:%s\x00", len(id), id)
	}
	return hex.EncodeToString(h.Sum(nil)[:8])
}
