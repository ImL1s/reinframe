package classifier

import (
	"fmt"
	"time"
)

// Schema versions for provider runtime (#132).
const (
	SchemaProviderRequest   = "reinframe.classifier_provider_request.v1"
	SchemaProviderResult    = "reinframe.classifier_provider_result.v1"
	SchemaProviderCallAudit = "reinframe.classifier_provider_call_audit.v1"
	SchemaPromptPlan        = "reinframe.classifier_prompt_plan.v1"
)

// ParseStatus closed values.
const (
	ParseStatusOK      = "ok"
	ParseStatusInvalid = "invalid"
	ParseStatusError   = "error"
)

// Default bounds for provider I/O (#132).
const (
	DefaultMaxInputBytes  = 65536
	DefaultMaxOutputBytes = 8192
	DefaultTimeout        = 1500 * time.Millisecond
	MaxAllowedTimeout     = 60 * time.Second
	MaxAllowedInputBytes  = 1 << 20 // 1 MiB hard ceiling
	MaxAllowedOutputBytes = 256 << 10
	MaxAuditStringBytes   = 256
	MaxRetryCount         = 2
	MaxRetryAfter         = 5 * time.Second
)

// ProviderRequest is the canonical Stage-1 provider call envelope (#132).
type ProviderRequest struct {
	SchemaVersion  string
	Input          ClassifierInput
	Prompt         PromptPlan
	Timeout        time.Duration
	MaxInputBytes  int
	MaxOutputBytes int
}

// ProviderResult is the canonical Stage-1 provider response envelope (#132).
type ProviderResult struct {
	SchemaVersion string
	Assessment    RawAssessment
	Usage         ProviderUsage
	Meta          ProviderMeta
}

// ProviderUsage is transport-derived token/cache telemetry only.
// Model-generated content must never populate these fields.
type ProviderUsage struct {
	InputTokens         int64
	UncachedInputTokens int64
	CacheReadTokens     int64
	CacheWriteTokens    int64
	OutputTokens        int64
	ReasoningTokens     int64

	// UsagePresent is true when the transport envelope included a usage object.
	// Distinguishes unavailable telemetry (false) from a real zero (true, zeros).
	UsagePresent bool

	CacheHit     bool
	CacheBackend string
	CacheKeyHash string
}

// ProviderMeta is bounded call metadata (no raw bodies / secrets).
type ProviderMeta struct {
	Provider            string
	ModelID             string
	ModelVersion        string
	CapabilitiesProfile string
	ProviderRequestID   string
	HTTPStatus          int
	LatencyMS           int64
	RetryCount          int
	ParseStatus         string
	FallbackReason      string
	ErrorClass          string
}

// ValidateProviderRequest checks closed bounds and PromptPlan integrity before transport.
func ValidateProviderRequest(req ProviderRequest) error {
	if req.SchemaVersion != SchemaProviderRequest {
		return fmt.Errorf("classifier: provider request schema required")
	}
	if req.MaxInputBytes < 0 || req.MaxOutputBytes < 0 {
		return fmt.Errorf("classifier: negative byte limits")
	}
	if req.MaxInputBytes > MaxAllowedInputBytes {
		return fmt.Errorf("classifier: max_input_bytes exceeds ceiling")
	}
	if req.MaxOutputBytes > MaxAllowedOutputBytes {
		return fmt.Errorf("classifier: max_output_bytes exceeds ceiling")
	}
	if req.Timeout < 0 {
		return fmt.Errorf("classifier: negative timeout")
	}
	if req.Timeout > MaxAllowedTimeout {
		return fmt.Errorf("classifier: timeout exceeds ceiling")
	}
	if err := ValidateClassifierInput(req.Input); err != nil {
		return err
	}
	if err := ValidatePromptPlan(req.Prompt, req.Input); err != nil {
		return err
	}
	return nil
}

// ValidateProviderResult checks result schema and usage/meta bounds.
func ValidateProviderResult(res ProviderResult) error {
	if res.SchemaVersion != SchemaProviderResult {
		return fmt.Errorf("classifier: provider result schema required")
	}
	if err := ValidateProviderUsage(res.Usage); err != nil {
		return err
	}
	return ValidateProviderMeta(res.Meta)
}

// ValidateProviderUsage rejects negative/overflow token fields.
func ValidateProviderUsage(u ProviderUsage) error {
	if u.InputTokens < 0 || u.OutputTokens < 0 || u.ReasoningTokens < 0 ||
		u.CacheReadTokens < 0 || u.CacheWriteTokens < 0 || u.UncachedInputTokens < 0 {
		return fmt.Errorf("classifier: invalid negative usage tokens")
	}
	return nil
}

// ValidateProviderMeta bounds status and retry count.
func ValidateProviderMeta(m ProviderMeta) error {
	if m.HTTPStatus < 0 || m.HTTPStatus > 999 {
		return fmt.Errorf("classifier: invalid http_status")
	}
	if m.RetryCount < 0 || m.RetryCount > MaxRetryCount+1 {
		return fmt.Errorf("classifier: invalid retry_count")
	}
	if len(m.Provider) > MaxAuditStringBytes || len(m.ModelID) > MaxAuditStringBytes {
		return fmt.Errorf("classifier: meta string too long")
	}
	return nil
}

// ProviderBoundSources are optional upper bounds that may only tighten a request.
// Zero means "not constrained by this source".
type ProviderBoundSources struct {
	ConfigTimeout      time.Duration
	ConfigMaxInput     int
	ConfigMaxOutput    int
	CapabilityMaxInput int
}

// EffectiveProviderBounds computes limits as the minimum positive constraint.
// Request is the per-call authority; config/capabilities only cap, never widen.
func EffectiveProviderBounds(req ProviderRequest, src ProviderBoundSources) (timeout time.Duration, maxIn, maxOut int) {
	timeout = minPositiveDuration(req.Timeout, src.ConfigTimeout)
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxAllowedTimeout {
		timeout = MaxAllowedTimeout
	}

	maxIn = minPositiveInt(req.MaxInputBytes, src.ConfigMaxInput, src.CapabilityMaxInput)
	if maxIn <= 0 {
		maxIn = DefaultMaxInputBytes
	}
	if maxIn > MaxAllowedInputBytes {
		maxIn = MaxAllowedInputBytes
	}

	maxOut = minPositiveInt(req.MaxOutputBytes, src.ConfigMaxOutput)
	if maxOut <= 0 {
		maxOut = DefaultMaxOutputBytes
	}
	if maxOut > MaxAllowedOutputBytes {
		maxOut = MaxAllowedOutputBytes
	}
	return timeout, maxIn, maxOut
}

// EffectiveBounds is the request-only helper (no config/capability caps).
func EffectiveBounds(req ProviderRequest) (timeout time.Duration, maxIn, maxOut int) {
	return EffectiveProviderBounds(req, ProviderBoundSources{})
}

func minPositiveDuration(vals ...time.Duration) time.Duration {
	var min time.Duration
	for _, v := range vals {
		if v <= 0 {
			continue
		}
		if min == 0 || v < min {
			min = v
		}
	}
	return min
}

func minPositiveInt(vals ...int) int {
	min := 0
	for _, v := range vals {
		if v <= 0 {
			continue
		}
		if min == 0 || v < min {
			min = v
		}
	}
	return min
}

// Typed provider errors (bounded messages; never include secrets or raw bodies).
type ProviderError struct {
	Class      string // timeout|canceled|http|parse|config|transport|oversized|capability
	Message    string
	Retryable  bool
	HTTPStatus int
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "classifier: provider error"
	}
	return fmt.Sprintf("classifier: %s: %s", e.Class, boundErr(e.Message))
}

func boundErr(s string) string {
	const n = 200
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func newProviderError(class, msg string, retryable bool, status int) *ProviderError {
	return &ProviderError{Class: class, Message: msg, Retryable: retryable, HTTPStatus: status}
}
