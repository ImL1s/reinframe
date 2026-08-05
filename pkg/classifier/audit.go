package classifier

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProviderCallAudit is a closed, size-bounded provider-call audit record (#132).
// It must never contain raw prompts, API keys, or unrestricted model bodies.
type ProviderCallAudit struct {
	SchemaVersion       string   `json:"schema_version"`
	Provider            string   `json:"provider"`
	ModelID             string   `json:"model_id,omitempty"`
	CapabilitiesProfile string   `json:"capabilities_profile,omitempty"`
	PromptHash          string   `json:"prompt_hash,omitempty"`
	StablePrefixHash    string   `json:"stable_prefix_hash,omitempty"`
	RulesetHash         string   `json:"ruleset_hash,omitempty"`
	InputHash           string   `json:"input_hash,omitempty"`
	EvidenceEventIDs    []string `json:"evidence_event_ids,omitempty"`
	HTTPStatus          int      `json:"http_status,omitempty"`
	LatencyMS           int64    `json:"latency_ms,omitempty"`
	RetryCount          int      `json:"retry_count,omitempty"`
	ProviderRequestID   string   `json:"provider_request_id,omitempty"`
	ParseStatus         string   `json:"parse_status,omitempty"`
	FallbackReason      string   `json:"fallback_reason,omitempty"`
	ErrorClass          string   `json:"error_class,omitempty"`
	// Usage telemetry (transport only).
	UsagePresent        bool   `json:"usage_present"`
	InputTokens         int64  `json:"input_tokens,omitempty"`
	OutputTokens        int64  `json:"output_tokens,omitempty"`
	CacheReadTokens     int64  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens    int64  `json:"cache_write_tokens,omitempty"`
	UncachedInputTokens int64  `json:"uncached_input_tokens,omitempty"`
	CacheHit            bool   `json:"cache_hit,omitempty"`
	CacheBackend        string `json:"cache_backend,omitempty"`
	// Correlation
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
	// Assessment summary only (no free-form explanation).
	Severity   int    `json:"severity,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
}

// BuildProviderCallAudit constructs a closed audit record from request/result.
func BuildProviderCallAudit(req ProviderRequest, res ProviderResult, corr, cause string, now time.Time) ProviderCallAudit {
	ev := append([]string(nil), res.Assessment.EvidenceEventIDs...)
	if len(ev) > MaxEvidenceIDs {
		ev = ev[:MaxEvidenceIDs]
	}
	a := ProviderCallAudit{
		SchemaVersion:       SchemaProviderCallAudit,
		Provider:            truncateAudit(res.Meta.Provider),
		ModelID:             truncateAudit(res.Meta.ModelID),
		CapabilitiesProfile: truncateAudit(res.Meta.CapabilitiesProfile),
		PromptHash:          truncateAudit(req.Prompt.PromptHash),
		StablePrefixHash:    truncateAudit(req.Prompt.StablePrefixHash),
		RulesetHash:         truncateAudit(req.Prompt.RulesetHash),
		InputHash:           truncateAudit(req.Prompt.InputHash),
		EvidenceEventIDs:    ev,
		HTTPStatus:          res.Meta.HTTPStatus,
		LatencyMS:           res.Meta.LatencyMS,
		RetryCount:          res.Meta.RetryCount,
		ProviderRequestID:   truncateAudit(res.Meta.ProviderRequestID),
		ParseStatus:         truncateAudit(res.Meta.ParseStatus),
		FallbackReason:      truncateAudit(res.Meta.FallbackReason),
		ErrorClass:          truncateAudit(res.Meta.ErrorClass),
		UsagePresent:        res.Usage.UsagePresent,
		InputTokens:         res.Usage.InputTokens,
		OutputTokens:        res.Usage.OutputTokens,
		CacheReadTokens:     res.Usage.CacheReadTokens,
		CacheWriteTokens:    res.Usage.CacheWriteTokens,
		UncachedInputTokens: res.Usage.UncachedInputTokens,
		CacheHit:            res.Usage.CacheHit,
		CacheBackend:        truncateAudit(res.Usage.CacheBackend),
		CorrelationID:       truncateAudit(corr),
		CausationID:         truncateAudit(cause),
		Severity:            res.Assessment.Severity,
		ReasonCode:          truncateAudit(res.Assessment.ReasonCode),
	}
	if !now.IsZero() {
		a.CreatedAt = now.UTC().Format(time.RFC3339Nano)
	}
	return a
}

// MarshalJSON deterministic closed serialization.
func (a ProviderCallAudit) MarshalJSON() ([]byte, error) {
	type alias ProviderCallAudit
	return json.Marshal(alias(a))
}

// AuditJSON returns bounded JSON bytes.
func (a ProviderCallAudit) AuditJSON() ([]byte, error) {
	b, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	if len(b) > 16<<10 {
		return nil, fmt.Errorf("classifier: audit record exceeds size limit")
	}
	return b, nil
}

func truncateAudit(s string) string {
	if len(s) <= MaxAuditStringBytes {
		return s
	}
	return s[:MaxAuditStringBytes]
}
