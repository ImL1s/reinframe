package classifier

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// ModelGPT53CodexSpark is the official identifier for GPT-5.3-Codex-Spark (#188).
const ModelGPT53CodexSpark = "gpt-5.3-codex-spark"

// OpenAISparkSourcePin documents the official capability pin for Spark (#188).
const (
	OpenAISparkSourceURL       = "https://openai.com/index/introducing-gpt-5-3-codex-spark/"
	OpenAISparkSourceRetrieved = "2026-08-15"
)

// Reasoning effort levels for OpenAI reasoning / Spark models (#188).
const (
	ReasoningEffortLow    = "low"
	ReasoningEffortMedium = "medium"
	ReasoningEffortHigh   = "high"
)

// OpenAISparkConfig configures the dedicated capability-gated Spark API profile (#188).
// Gated for direct OpenAI API users targeting /v1/responses.
type OpenAISparkConfig struct {
	Model               string // default "gpt-5.3-codex-spark"
	BaseURL             string // origin only; default https://api.openai.com
	Path                string // default /v1/responses
	APIKeyRef           string
	Timeout             time.Duration
	MaxInputBytes       int
	MaxOutputBytes      int
	CapabilitiesProfile string // default "openai-spark-v1"
	EgressProfile       string
	HTTPClient          *http.Client
	Sleep               func(context.Context, time.Duration) error
	MaxRetries          int
	Now                 func() time.Time
	LookupEnv           func(string) (string, bool)
	AllowRemote         bool

	// Entitled must be explicitly true for projects entitled to use the Spark API profile (#188).
	Entitled bool
	// ReasoningEffort controls reasoning level: "low", "medium", "high" (optional).
	ReasoningEffort string
	// EntitlementVerifier is an optional custom verification callback (tests / enterprise).
	EntitlementVerifier func(model, profile string) bool
}

func NewOpenAISpark(cfg OpenAISparkConfig) (*OpenAIResponsesProvider, error) {
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = ModelGPT53CodexSpark
	}
	profile := strings.TrimSpace(cfg.CapabilitiesProfile)
	if profile == "" {
		profile = CapabilitiesProfileOpenAISparkV1
	}

	oaiCfg := OpenAIResponsesConfig{
		Kind:                KindOpenAIResponses,
		Model:               model,
		BaseURL:             cfg.BaseURL,
		Path:                cfg.Path,
		APIKeyRef:           cfg.APIKeyRef,
		Timeout:             cfg.Timeout,
		MaxInputBytes:       cfg.MaxInputBytes,
		MaxOutputBytes:      cfg.MaxOutputBytes,
		CapabilitiesProfile: profile,
		EgressProfile:       cfg.EgressProfile,
		HTTPClient:          cfg.HTTPClient,
		Sleep:               cfg.Sleep,
		MaxRetries:          cfg.MaxRetries,
		Now:                 cfg.Now,
		LookupEnv:           cfg.LookupEnv,
		AllowRemote:         cfg.AllowRemote,
		SparkEntitled:       cfg.Entitled,
		ReasoningEffort:     cfg.ReasoningEffort,
		EntitlementVerifier: cfg.EntitlementVerifier,
	}

	return NewOpenAIResponses(oaiCfg)
}

// IsSparkModel reports whether a model name refers to GPT-5.3-Codex-Spark.
func IsSparkModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return m == ModelGPT53CodexSpark || m == "gpt-5.3-codex-spark-preview"
}

// IsSparkProfile reports whether a capabilities profile is the Spark API profile.
func IsSparkProfile(profile string) bool {
	p := strings.ToLower(strings.TrimSpace(profile))
	return p == CapabilitiesProfileOpenAISparkV1
}

// ValidateReasoningEffort checks that reasoning effort is in the closed allowlist: "low", "medium", "high", or "".
func ValidateReasoningEffort(effort string) error {
	e := strings.ToLower(strings.TrimSpace(effort))
	switch e {
	case "", ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh:
		return nil
	default:
		return newProviderError("config", fmt.Sprintf("invalid reasoning_effort %q (must be low, medium, or high)", boundErr(effort)), false, 0)
	}
}

// ValidateSparkEntitlement verifies that project entitlement is explicitly granted.
func ValidateSparkEntitlement(model, profile string, isEntitled bool, verifier func(model, profile string) bool) error {
	if verifier != nil {
		if !verifier(model, profile) {
			return newProviderError("capability", "gpt-5.3-codex-spark api profile requires explicit project capability entitlement", false, 0)
		}
		return nil
	}
	if !isEntitled {
		return newProviderError("capability", "gpt-5.3-codex-spark api profile requires explicit project capability entitlement", false, 0)
	}
	return nil
}

// isProhibitedOAuthToken checks whether a token string appears to be a ChatGPT OAuth subscription credential
// rather than a direct OpenAI API key.
func isProhibitedOAuthToken(token string) bool {
	t := strings.ToLower(strings.TrimSpace(token))
	if strings.HasPrefix(t, "oauth-") ||
		strings.HasPrefix(t, "chatgpt-oauth-") ||
		strings.HasPrefix(t, "chatgpt_subscription") ||
		strings.HasPrefix(t, "session-") ||
		strings.Contains(t, "oauth_token") ||
		strings.Contains(t, "refresh_token") {
		return true
	}
	return false
}

// isProhibitedOAuthRef checks whether an APIKeyRef placeholder name indicates OAuth / subscription token.
func isProhibitedOAuthRef(ref string) bool {
	r := strings.ToLower(strings.TrimSpace(ref))
	return strings.Contains(r, "oauth") || strings.Contains(r, "session") || strings.Contains(r, "subscription")
}

// ValidateToolCallAlignment verifies that a proposed action conforms to tool call alignment requirements.
func ValidateToolCallAlignment(pa adapter.ProposedAction) error {
	if pa.SchemaVersion != "" && pa.SchemaVersion != adapter.ProposedActionSchemaVersion {
		return fmt.Errorf("classifier: invalid proposed action schema version %q", boundErr(pa.SchemaVersion))
	}
	if strings.TrimSpace(pa.ToolName) == "" {
		return fmt.Errorf("classifier: tool call alignment requires non-empty tool_name")
	}
	switch pa.ToolClass {
	case adapter.ToolClassShell, adapter.ToolClassEdit, adapter.ToolClassRead,
		adapter.ToolClassSearch, adapter.ToolClassUnknown, adapter.ToolClassOther:
		// ok
	case "":
		return fmt.Errorf("classifier: tool call alignment requires tool_class")
	default:
		return fmt.Errorf("classifier: unknown tool_class %q", boundErr(pa.ToolClass))
	}
	if pa.Truncated {
		return fmt.Errorf("classifier: truncated proposed action cannot be aligned")
	}
	return nil
}

// AlignSparkToolCall produces a deterministic structured representation of a proposed tool action
// for evaluation and alignment.
func AlignSparkToolCall(pa adapter.ProposedAction) (map[string]any, error) {
	if err := ValidateToolCallAlignment(pa); err != nil {
		return nil, err
	}
	aligned := map[string]any{
		"tool_name":   pa.ToolName,
		"tool_class":  pa.ToolClass,
		"command":     pa.Command,
		"arguments":   append([]string(nil), pa.Arguments...),
		"file_path":   pa.FilePath,
		"action_id":   pa.ActionID,
		"session_id":  pa.SessionID,
		"source":      pa.Source,
		"target_scope": append([]string(nil), pa.TargetScope...),
	}
	return aligned, nil
}
