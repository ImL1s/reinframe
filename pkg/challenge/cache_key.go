package challenge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
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
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|%s|%s|%s|%s|%d|%s|%s|%s|%s|%s",
		in.SchemaVersion, in.SessionID, in.ChallengeID, in.ChallengeState,
		in.ActionFingerprint, in.JustificationHash, in.EvidenceIDsHash,
		in.ContractRevision, in.WorkspaceRevision, in.RulesetHash, in.PolicyHash,
		in.ModelID, in.PromptHash)
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func hashStrings(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	h := sha256.Sum256([]byte(strings.Join(ids, ",")))
	return hex.EncodeToString(h[:8])
}
