package classifier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// KindAnthropicMessages is the closed config kind for the native Anthropic Messages API (#135).
const KindAnthropicMessages = "anthropic_messages"

// DefaultAnthropicMessagesPath is the pinned Messages endpoint path.
const DefaultAnthropicMessagesPath = "/v1/messages"

// Anthropic API version header (stable Messages contract pin).
const AnthropicAPIVersion = "2023-06-01"

// Anthropic platform pins (#135). Only direct Claude API is supported in this revision.
const (
	AnthropicPlatformClaudeAPI = "claude_api"
)

// AnthropicMessagesSourcePin documents the official capability pin for #135.
const (
	AnthropicMessagesSourceURL       = "https://platform.claude.com/docs/en/build-with-claude/prompt-caching"
	AnthropicMessagesSourceRetrieved = "2026-08-06"
)

// Capability profile names for Anthropic (#135).
const (
	CapabilitiesProfileAnthropicOffV1              = "anthropic-off-v1"
	CapabilitiesProfileAnthropicAutomatic5mV1      = "anthropic-automatic-5m-v1"
	CapabilitiesProfileAnthropicAutomatic1hV1      = "anthropic-automatic-1h-v1"
	CapabilitiesProfileAnthropicExplicitPrefix5mV1 = "anthropic-explicit-prefix-5m-v1"
	CapabilitiesProfileAnthropicExplicitPrefix1hV1 = "anthropic-explicit-prefix-1h-v1"
)

// Anthropic cache TTL wire values (provider-documented).
const (
	AnthropicCacheTTL5m = "5m"
	AnthropicCacheTTL1h = "1h"
)

// AnthropicMessagesConfig configures the native Anthropic Messages classifier adapter (#135).
type AnthropicMessagesConfig struct {
	Kind      string
	Model     string
	BaseURL   string // origin only; default https://api.anthropic.com
	Path      string // default /v1/messages
	APIKeyRef string
	// Platform pins direct Claude API vs hosted variants. Empty defaults to claude_api.
	// Only claude_api is supported; bedrock/vertex/azure fail closed.
	Platform            string
	Timeout             time.Duration
	MaxInputBytes       int
	MaxOutputBytes      int
	CapabilitiesProfile string
	// EgressProfile is a bounded secret-free partition for cache key identity (optional).
	EgressProfile string
	HTTPClient    *http.Client
	Sleep         func(context.Context, time.Duration) error
	MaxRetries    int
	Now           func() time.Time
	LookupEnv     func(string) (string, bool)
	// AllowRemote when true permits non-loopback URLs (tests / production Anthropic).
	AllowRemote bool
}

// AnthropicMessagesProvider is the production-shaped native Messages adapter.
type AnthropicMessagesProvider struct {
	cfg      AnthropicMessagesConfig
	caps     ProviderCapabilities
	cacheTTL string // "" | "5m" | "1h" derived from profile
	apiKey   string
	client   *http.Client
	endpoint string
	sleep    func(context.Context, time.Duration) error
	now      func() time.Time
}

// NewAnthropicMessages builds a native Messages adapter.
func NewAnthropicMessages(cfg AnthropicMessagesConfig) (*AnthropicMessagesProvider, error) {
	if cfg.Kind != "" && cfg.Kind != KindAnthropicMessages {
		return nil, newProviderError("config", "kind must be anthropic_messages", false, 0)
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, newProviderError("config", "model is required", false, 0)
	}
	platform := strings.TrimSpace(strings.ToLower(cfg.Platform))
	if platform == "" {
		platform = AnthropicPlatformClaudeAPI
	}
	if platform != AnthropicPlatformClaudeAPI {
		return nil, newProviderError("config", "unsupported anthropic platform (only claude_api)", false, 0)
	}

	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://api.anthropic.com"
	}
	allowRemote := cfg.AllowRemote || isAnthropicOfficialHost(base)
	baseCanon, err := normalizeBaseURL(base, allowRemote)
	if err != nil {
		return nil, err
	}
	pathCanon, err := normalizePath(cfg.Path)
	if err != nil {
		return nil, err
	}
	if cfg.Path == "" {
		pathCanon = DefaultAnthropicMessagesPath
	}
	if pathCanon != DefaultAnthropicMessagesPath {
		return nil, newProviderError("config", "anthropic_messages path must be /v1/messages", false, 0)
	}
	if cfg.APIKeyRef != "" && !isEnvPlaceholder(cfg.APIKeyRef) {
		return nil, newProviderError("config", "api_key_ref must be ${ENV} placeholder", false, 0)
	}
	if cfg.Timeout < 0 || cfg.Timeout > MaxAllowedTimeout {
		return nil, newProviderError("config", "invalid timeout", false, 0)
	}
	if cfg.MaxInputBytes < 0 || cfg.MaxInputBytes > MaxAllowedInputBytes {
		return nil, newProviderError("config", "invalid max_input_bytes", false, 0)
	}
	if cfg.MaxOutputBytes < 0 || cfg.MaxOutputBytes > MaxAllowedOutputBytes {
		return nil, newProviderError("config", "invalid max_output_bytes", false, 0)
	}
	prof := strings.TrimSpace(cfg.CapabilitiesProfile)
	if prof == "" {
		prof = CapabilitiesProfileAnthropicOffV1
	}
	caps, err := LookupCapabilitiesProfile(prof)
	if err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	if err := ValidateCapabilities(caps); err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	if !caps.NativeStructuredOutput {
		return nil, newProviderError("capability", "anthropic_messages requires native structured output profile", false, 0)
	}
	ttl, err := anthropicTTLForProfile(prof)
	if err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}

	lookup := cfg.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	var apiKey string
	if cfg.APIKeyRef != "" {
		name := envNameFromPlaceholder(cfg.APIKeyRef)
		v, ok := lookup(name)
		if !ok || v == "" {
			return nil, newProviderError("config", "api key env not set", false, 0)
		}
		apiKey = v
	}

	cfg.Model = model
	cfg.BaseURL = baseCanon
	cfg.Path = pathCanon
	cfg.Platform = platform
	cfg.CapabilitiesProfile = prof
	if len(cfg.EgressProfile) > MaxAuditStringBytes {
		return nil, newProviderError("config", "egress_profile too long", false, 0)
	}

	endpoint, err := joinEndpoint(baseCanon, pathCanon)
	if err != nil {
		return nil, err
	}
	client := wrapClientNoRedirect(cfg.HTTPClient)
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.MaxRetries < 0 {
		return nil, newProviderError("config", "invalid max retries", false, 0)
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = MaxRetryCount
	}
	if cfg.MaxRetries > MaxRetryCount {
		cfg.MaxRetries = MaxRetryCount
	}

	return &AnthropicMessagesProvider{
		cfg:      cfg,
		caps:     caps,
		cacheTTL: ttl,
		apiKey:   apiKey,
		client:   client,
		endpoint: endpoint,
		sleep:    sleep,
		now:      now,
	}, nil
}

func anthropicTTLForProfile(prof string) (string, error) {
	switch prof {
	case CapabilitiesProfileAnthropicOffV1:
		return "", nil
	case CapabilitiesProfileAnthropicAutomatic5mV1, CapabilitiesProfileAnthropicExplicitPrefix5mV1:
		return AnthropicCacheTTL5m, nil
	case CapabilitiesProfileAnthropicAutomatic1hV1, CapabilitiesProfileAnthropicExplicitPrefix1hV1:
		return AnthropicCacheTTL1h, nil
	default:
		return "", fmt.Errorf("unknown anthropic profile %q", boundErr(prof))
	}
}

// Assess implements ClassifierProvider for the native Messages API.
func (p *AnthropicMessagesProvider) Assess(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	if ctx == nil {
		return ProviderResult{}, newProviderError("config", "nil context", false, 0)
	}
	if err := ctx.Err(); err != nil {
		return ProviderResult{}, err
	}
	if req.Input.AllowLegacyFixtureIDs {
		return ProviderResult{}, newProviderError("config", "legacy fixture mode not allowed on anthropic_messages", false, 0)
	}
	if (len(req.Input.RecentEventIDs) > 0 || len(req.Input.RelatedEventIDs) > 0) &&
		len(req.Input.RecentEvents) == 0 && len(req.Input.RelatedEvents) == 0 {
		return ProviderResult{}, newProviderError("config", "legacy event IDs without digests rejected before HTTP", false, 0)
	}
	if err := ValidateProviderRequest(req); err != nil {
		return ProviderResult{}, newProviderError("config", err.Error(), false, 0)
	}

	timeout, maxIn, maxOut := EffectiveProviderBounds(req, ProviderBoundSources{
		ConfigTimeout:      p.cfg.Timeout,
		ConfigMaxInput:     p.cfg.MaxInputBytes,
		ConfigMaxOutput:    p.cfg.MaxOutputBytes,
		CapabilityMaxInput: p.caps.MaxInputBytes,
	})
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	payload, cacheKeyHash, err := p.buildRequestJSON(req, maxIn)
	if err != nil {
		return ProviderResult{}, err
	}
	if len(payload) > maxIn {
		return ProviderResult{}, newProviderError("oversized", "serialized request exceeds max_input_bytes", false, 0)
	}

	start := p.now()
	var lastStatus int
	var retryCount int
	var lastErr error
	var nextDelay time.Duration
	maxAttempts := p.cfg.MaxRetries + 1

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := classifyOpError(ctx, opCtx, nil); err != nil {
			return ProviderResult{}, err
		}
		if attempt > 0 {
			retryCount++
			delay := nextDelay
			if delay <= 0 {
				delay = retryBackoff(attempt)
			}
			if delay > MaxRetryAfter {
				delay = MaxRetryAfter
			}
			if dl, ok := opCtx.Deadline(); ok {
				rem := dl.Sub(p.now())
				if rem <= 0 {
					return ProviderResult{}, errFromExhaustedBudget(ctx, opCtx, opCtx.Err())
				}
				if delay > rem {
					delay = rem
				}
			}
			if err := p.sleep(opCtx, delay); err != nil {
				if ce := classifyOpError(ctx, opCtx, err); ce != nil {
					return ProviderResult{}, ce
				}
				return ProviderResult{}, newProviderError("transport", "retry backoff failed", false, 0)
			}
		}

		res, status, retryable, retryAfter, err := p.doOnce(opCtx, payload, maxOut, req, cacheKeyHash)
		lastStatus = status
		nextDelay = retryAfter
		if err == nil {
			if ce := classifyOpError(ctx, opCtx, nil); ce != nil {
				return ProviderResult{}, ce
			}
			res.SchemaVersion = SchemaProviderResult
			res.Meta.RetryCount = retryCount
			res.Meta.LatencyMS = p.now().Sub(start).Milliseconds()
			res.Meta.HTTPStatus = status
			res.Assessment.LatencyMS = res.Meta.LatencyMS
			res.Assessment.PromptHash = req.Prompt.PromptHash
			res.Assessment.RulesetHash = req.Input.RulesetHash
			res.Assessment.RulesetID = req.Input.RulesetID
			return res, nil
		}
		if ce := classifyOpError(ctx, opCtx, err); ce != nil {
			if isParentContextAbort(ce) || isAdapterTimeout(ce) {
				return ProviderResult{}, ce
			}
			if !retryable || attempt+1 >= maxAttempts {
				lastErr = ce
				break
			}
			lastErr = ce
			continue
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ProviderResult{}, classifyOpError(ctx, opCtx, err)
		}
		lastErr = err
		if !retryable || attempt+1 >= maxAttempts {
			break
		}
	}

	if ce := classifyOpError(ctx, opCtx, lastErr); ce != nil && (isParentContextAbort(ce) || isAdapterTimeout(ce)) {
		return ProviderResult{}, ce
	}
	pe, ok := lastErr.(*ProviderError)
	if !ok {
		var typed *ProviderError
		if errors.As(lastErr, &typed) {
			pe = typed
		} else {
			pe = newProviderError("transport", "provider call failed", false, lastStatus)
		}
	}
	pe.Message = redactSecret(pe.Message, p.apiKey)
	return ProviderResult{
		SchemaVersion: SchemaProviderResult,
		Meta: ProviderMeta{
			Provider:            KindAnthropicMessages,
			ModelID:             p.cfg.Model,
			CapabilitiesProfile: p.cfg.CapabilitiesProfile,
			HTTPStatus:          lastStatus,
			LatencyMS:           p.now().Sub(start).Milliseconds(),
			RetryCount:          retryCount,
			ParseStatus:         ParseStatusError,
			ErrorClass:          pe.Class,
			FallbackReason:      pe.Class,
		},
	}, pe
}

func (p *AnthropicMessagesProvider) buildRequestJSON(req ProviderRequest, maxIn int) ([]byte, string, error) {
	// Stable system blocks first; dynamic user/assistant after. Explicit profile:
	// cache_control on the last stable system text block only (one breakpoint).
	explicit := p.caps.CacheMode == CacheModeExplicitBreakpoint
	// Automatic profiles: no wire cache_control (provider may still cache); document TTL via profile.
	// Off: no cache_control.

	var system []anthContentBlock
	var messages []anthMessage

	nStable := len(req.Prompt.StablePrefix)
	for i, b := range req.Prompt.StablePrefix {
		role := b.Role
		if role == "" {
			role = PromptRoleSystem
		}
		block := anthContentBlock{Type: "text", Text: b.Text}
		if explicit && i == nStable-1 && p.cacheTTL != "" {
			block.CacheControl = &anthCacheControl{Type: "ephemeral", TTL: p.cacheTTL}
		}
		if role == PromptRoleSystem {
			system = append(system, block)
			continue
		}
		// Non-system stable blocks become messages (rare; still before dynamic).
		messages = append(messages, anthMessage{Role: role, Content: []anthContentBlock{block}})
	}
	for _, b := range req.Prompt.DynamicSuffix {
		role := b.Role
		if role == "" {
			role = PromptRoleUser
		}
		if role == PromptRoleSystem {
			// Dynamic system is not used by plan; fold into user to keep order honest.
			role = PromptRoleUser
		}
		// Dynamic never receives cache_control (explicit-only breakpoint on stable).
		messages = append(messages, anthMessage{
			Role:    role,
			Content: []anthContentBlock{{Type: "text", Text: b.Text}},
		})
	}
	if len(messages) == 0 {
		// Messages API requires at least one user message.
		messages = append(messages, anthMessage{
			Role:    PromptRoleUser,
			Content: []anthContentBlock{{Type: "text", Text: "Assess the supplied task against the closed schema."}},
		})
	}

	body := anthMessagesRequest{
		Model:       p.cfg.Model,
		MaxTokens:   1024,
		Temperature: 0,
		System:      system,
		Messages:    messages,
		Tools: []anthTool{{
			Name:        "reinframe_raw_assessment",
			Description: "Emit exactly one closed RawAssessment object for Reinframe Stage-1.",
			// Minimal tool schema for Anthropic wire; Reinframe enforces closed fields in ParseRawAssessmentStrict.
			InputSchema: rawAssessmentJSONSchemaAnthropicTool(),
		}},
		ToolChoice: &anthToolChoice{Type: "tool", Name: "reinframe_raw_assessment"},
	}

	var cacheKeyHash string
	if explicit || p.caps.CacheMode == CacheModeImplicitPrefix {
		// Secret-free identity for audit — not sent as Anthropic request field.
		cacheKeyHash = p.promptCacheKeyHash(req)
	}
	_ = maxIn
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", newProviderError("config", "marshal request", false, 0)
	}
	return payload, cacheKeyHash, nil
}

func (p *AnthropicMessagesProvider) promptCacheKeyHash(req ProviderRequest) string {
	mat := fmt.Sprintf("anthropic_messages|%s|%s|%s|%s|%s|%s|%s",
		p.cfg.Model,
		p.cfg.Platform,
		p.cfg.CapabilitiesProfile,
		p.cacheTTL,
		req.Prompt.StablePrefixHash,
		req.Prompt.RulesetHash,
		p.cfg.EgressProfile,
	)
	sum := sha256.Sum256([]byte(mat))
	return hex.EncodeToString(sum[:16])
}

func (p *AnthropicMessagesProvider) doOnce(ctx context.Context, payload []byte, maxOut int, req ProviderRequest, cacheKeyHash string) (ProviderResult, int, bool, time.Duration, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ProviderResult{}, 0, true, 0, newProviderError("transport", "build request", true, 0)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", AnthropicAPIVersion)
	if p.apiKey != "" {
		httpReq.Header.Set("x-api-key", p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ProviderResult{}, 0, false, 0, err
		}
		if ctx.Err() != nil {
			return ProviderResult{}, 0, false, 0, ctx.Err()
		}
		return ProviderResult{}, 0, true, 0, newProviderError("transport", "http do failed", true, 0)
	}
	defer func() { _ = resp.Body.Close() }()

	status := resp.StatusCode
	retryAfter := ParseRetryAfterMs(resp.Header)
	body, err := readBounded(resp.Body, maxOut+4096)
	if err != nil {
		if err == errOversized {
			return ProviderResult{}, status, false, 0, newProviderError("oversized", "response exceeds max_output_bytes", false, status)
		}
		return ProviderResult{}, status, true, retryAfter, newProviderError("transport", "read body", true, status)
	}

	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ProviderResult{}, status, false, 0, newProviderError("http", fmt.Sprintf("HTTP %d", status), false, status)
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		return ProviderResult{}, status, true, retryAfter, newProviderError("http", fmt.Sprintf("HTTP %d", status), true, status)
	}
	if status < 200 || status >= 300 {
		return ProviderResult{}, status, false, 0, newProviderError("http", fmt.Sprintf("HTTP %d", status), false, status)
	}

	content, usage, reqID, err := parseAnthropicMessagesEnvelope(body)
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}
	if cacheKeyHash != "" {
		usage.CacheKeyHash = cacheKeyHash
		usage.CacheBackend = KindAnthropicMessages
	}
	// CacheHit only from positive provider-reported cache-read tokens.
	usage.CacheHit = usage.UsagePresent && usage.CacheReadTokens > 0

	assessment, err := ParseRawAssessmentStrict([]byte(content), maxOut, evidenceAllowlist(req.Input))
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}
	assessment.ModelID = p.cfg.Model
	assessment.PromptHash = req.Prompt.PromptHash
	assessment.RulesetID = req.Input.RulesetID
	assessment.RulesetHash = req.Input.RulesetHash
	assessment.ParseStatus = ParseStatusOK

	return ProviderResult{
		SchemaVersion: SchemaProviderResult,
		Assessment:    assessment,
		Usage:         usage,
		Meta: ProviderMeta{
			Provider:            KindAnthropicMessages,
			ModelID:             p.cfg.Model,
			CapabilitiesProfile: p.cfg.CapabilitiesProfile,
			ProviderRequestID:   reqID,
			HTTPStatus:          status,
			ParseStatus:         ParseStatusOK,
		},
	}, status, false, 0, nil
}

type anthMessagesRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
	System      []anthContentBlock `json:"system,omitempty"`
	Messages    []anthMessage      `json:"messages"`
	Tools       []anthTool         `json:"tools,omitempty"`
	ToolChoice  *anthToolChoice    `json:"tool_choice,omitempty"`
}

type anthMessage struct {
	Role    string             `json:"role"`
	Content []anthContentBlock `json:"content"`
}

type anthContentBlock struct {
	Type         string            `json:"type"`
	Text         string            `json:"text,omitempty"`
	CacheControl *anthCacheControl `json:"cache_control,omitempty"`
}

type anthCacheControl struct {
	Type string `json:"type"` // "ephemeral"
	TTL  string `json:"ttl,omitempty"`
}

type anthTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// rawAssessmentJSONSchemaAnthropicTool is a minimal tool input_schema for Messages API.
// Avoids const/min/max/maxItems constraints that some Anthropic tool paths reject;
// ParseRawAssessmentStrict still enforces the closed Reinframe contract.
func rawAssessmentJSONSchemaAnthropicTool() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"schema_version", "severity", "reason_code", "evidence_event_ids"},
		"properties": map[string]any{
			"schema_version":     map[string]any{"type": "string"},
			"severity":           map[string]any{"type": "integer"},
			"reason_code":        map[string]any{"type": "string"},
			"evidence_event_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
}

func parseAnthropicMessagesEnvelope(body []byte) (content string, usage ProviderUsage, reqID string, err error) {
	if err := rejectDuplicateKeys(body); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope duplicate keys")
	}
	var env struct {
		ID      string `json:"id"`
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
			Text  string          `json:"text"`
		} `json:"content"`
		Usage *struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope json")
	}
	// Fail-closed: forced tool_choice requires exactly one matching tool_use.
	// Do not accept free-form text as an assessment payload.
	var toolInputs []json.RawMessage
	for _, c := range env.Content {
		if c.Type == "tool_use" && c.Name == "reinframe_raw_assessment" && len(c.Input) > 0 {
			toolInputs = append(toolInputs, c.Input)
		}
	}
	if len(toolInputs) != 1 {
		return "", ProviderUsage{}, "", fmt.Errorf("expected exactly one reinframe_raw_assessment tool_use")
	}
	content = string(toolInputs[0])
	reqID = env.ID
	if env.Usage != nil {
		if env.Usage.InputTokens < 0 || env.Usage.OutputTokens < 0 ||
			env.Usage.CacheCreationInputTokens < 0 || env.Usage.CacheReadInputTokens < 0 {
			return "", ProviderUsage{}, "", fmt.Errorf("invalid negative token counts")
		}
		// Anthropic documents: input_tokens is after the last breakpoint;
		// total logical input = input + cache_read + cache_creation.
		uncached := env.Usage.InputTokens
		read := env.Usage.CacheReadInputTokens
		write := env.Usage.CacheCreationInputTokens
		usage = ProviderUsage{
			InputTokens:         uncached + read + write,
			UncachedInputTokens: uncached,
			CacheReadTokens:     read,
			CacheWriteTokens:    write,
			OutputTokens:        env.Usage.OutputTokens,
			UsagePresent:        true,
		}
	}
	return content, usage, reqID, nil
}

// Endpoint returns the canonical request URL (tests).
func (p *AnthropicMessagesProvider) Endpoint() string { return p.endpoint }

// Capabilities returns the resolved capability profile (tests).
func (p *AnthropicMessagesProvider) Capabilities() ProviderCapabilities { return p.caps }

// CacheTTL returns the profile TTL wire value (tests).
func (p *AnthropicMessagesProvider) CacheTTL() string { return p.cacheTTL }

// PromptCacheKeyHashForTest exports cache key hashing for identity tests.
func (p *AnthropicMessagesProvider) PromptCacheKeyHashForTest(req ProviderRequest) string {
	return p.promptCacheKeyHash(req)
}

// BuildRequestJSONForTest exports the wire request for fixture assertions.
func (p *AnthropicMessagesProvider) BuildRequestJSONForTest(req ProviderRequest) ([]byte, string, error) {
	_, maxIn, _ := EffectiveBounds(req)
	return p.buildRequestJSON(req, maxIn)
}

// RedactedConfig returns a logging-safe copy (no secrets).
func (p *AnthropicMessagesProvider) RedactedConfig() map[string]any {
	return map[string]any{
		"kind":                 KindAnthropicMessages,
		"model":                p.cfg.Model,
		"base_url":             p.cfg.BaseURL,
		"path":                 p.cfg.Path,
		"platform":             p.cfg.Platform,
		"api_key_ref":          p.cfg.APIKeyRef,
		"capabilities_profile": p.cfg.CapabilitiesProfile,
		"cache_mode":           p.caps.CacheMode,
		"cache_ttl":            p.cacheTTL,
		"egress_profile":       p.cfg.EgressProfile,
		"source_url":           AnthropicMessagesSourceURL,
		"source_retrieved":     AnthropicMessagesSourceRetrieved,
		"endpoint":             p.endpoint,
	}
}

func isAnthropicOfficialHost(base string) bool {
	h, err := parseLooseURL(base)
	if err != nil {
		return false
	}
	h = strings.ToLower(h)
	return h == "api.anthropic.com" || strings.HasSuffix(h, ".api.anthropic.com")
}
