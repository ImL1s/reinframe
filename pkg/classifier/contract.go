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
	Assessment RawAssessment
	Usage      ProviderUsage
	Meta       ProviderMeta
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

// ValidateProviderRequest checks closed bounds before transport.
func ValidateProviderRequest(req ProviderRequest) error {
	if req.SchemaVersion != "" && req.SchemaVersion != SchemaProviderRequest {
		return fmt.Errorf("classifier: unsupported provider request schema %q", boundErr(req.SchemaVersion))
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
	return nil
}

// EffectiveBounds fills zero defaults for timeout and byte limits.
func EffectiveBounds(req ProviderRequest) (timeout time.Duration, maxIn, maxOut int) {
	timeout = req.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxIn = req.MaxInputBytes
	if maxIn <= 0 {
		maxIn = DefaultMaxInputBytes
	}
	maxOut = req.MaxOutputBytes
	if maxOut <= 0 {
		maxOut = DefaultMaxOutputBytes
	}
	return timeout, maxIn, maxOut
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
