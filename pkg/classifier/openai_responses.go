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

// KindOpenAIResponses is the closed config kind for the native OpenAI Responses API (#134).
const KindOpenAIResponses = "openai_responses"

// DefaultOpenAIResponsesPath is the pinned Responses endpoint path.
const DefaultOpenAIResponsesPath = "/v1/responses"

// OpenAIResponsesSourcePin documents the official capability pin for #134.
// Retrieval date is UTC calendar day of the pinned docs revision used for this adapter.
const (
	OpenAIResponsesSourceURL       = "https://developers.openai.com/api/docs/guides/prompt-caching"
	OpenAIResponsesSourceRetrieved = "2026-08-06"
)

// OpenAIResponsesConfig configures the native OpenAI Responses classifier adapter (#134).
// Separate from the generic openai_compatible Chat Completions path.
type OpenAIResponsesConfig struct {
	Kind                string
	Model               string
	BaseURL             string // origin only; default https://api.openai.com
	Path                string // default /v1/responses
	APIKeyRef           string
	Timeout             time.Duration
	MaxInputBytes       int
	MaxOutputBytes      int
	CapabilitiesProfile string // openai-off-v1 | openai-implicit-v1 | openai-explicit-prefix-v1
	// EgressProfile is a bounded secret-free partition for cache keying (optional).
	EgressProfile string
	HTTPClient    *http.Client
	Sleep         func(context.Context, time.Duration) error
	MaxRetries    int
	Now           func() time.Time
	LookupEnv     func(string) (string, bool)
	// AllowRemote when true permits non-loopback URLs (tests / production OpenAI).
	AllowRemote bool
}

// OpenAIResponsesProvider is the production-shaped native Responses adapter.
type OpenAIResponsesProvider struct {
	cfg      OpenAIResponsesConfig
	caps     ProviderCapabilities
	apiKey   string
	client   *http.Client
	endpoint string
	sleep    func(context.Context, time.Duration) error
	now      func() time.Time
}

// NewOpenAIResponses builds a native Responses adapter.
func NewOpenAIResponses(cfg OpenAIResponsesConfig) (*OpenAIResponsesProvider, error) {
	if cfg.Kind != "" && cfg.Kind != KindOpenAIResponses {
		return nil, newProviderError("config", "kind must be openai_responses", false, 0)
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, newProviderError("config", "model is required", false, 0)
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://api.openai.com"
	}
	// Production OpenAI is remote; tests set AllowRemote for httptest hosts.
	allowRemote := cfg.AllowRemote || isOpenAIOfficialHost(base)
	baseCanon, err := normalizeBaseURL(base, allowRemote)
	if err != nil {
		return nil, err
	}
	pathCanon, err := normalizePath(cfg.Path)
	if err != nil {
		return nil, err
	}
	if cfg.Path == "" {
		pathCanon = DefaultOpenAIResponsesPath
	}
	if pathCanon == "/v1/chat/completions" {
		return nil, newProviderError("config", "openai_responses path must not be chat completions", false, 0)
	}
	if pathCanon != DefaultOpenAIResponsesPath {
		// Allow only the pinned Responses path for this kind.
		return nil, newProviderError("config", "openai_responses path must be /v1/responses", false, 0)
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
		prof = CapabilitiesProfileOpenAIOffV1
	}
	caps, err := LookupCapabilitiesProfile(prof)
	if err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	if err := ValidateCapabilities(caps); err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	// Native structured output is required for Responses closed assessment.
	if !caps.NativeStructuredOutput {
		return nil, newProviderError("capability", "openai_responses requires native structured output profile", false, 0)
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

	return &OpenAIResponsesProvider{
		cfg:      cfg,
		caps:     caps,
		apiKey:   apiKey,
		client:   client,
		endpoint: endpoint,
		sleep:    sleep,
		now:      now,
	}, nil
}

// Assess implements ClassifierProvider for the native Responses API.
func (p *OpenAIResponsesProvider) Assess(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	if ctx == nil {
		return ProviderResult{}, newProviderError("config", "nil context", false, 0)
	}
	if err := ctx.Err(); err != nil {
		return ProviderResult{}, err
	}
	// Real native path never accepts fixture-only legacy packets.
	if req.Input.AllowLegacyFixtureIDs {
		return ProviderResult{}, newProviderError("config", "legacy fixture mode not allowed on openai_responses", false, 0)
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
			Provider:            KindOpenAIResponses,
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

func (p *OpenAIResponsesProvider) buildRequestJSON(req ProviderRequest, maxIn int) ([]byte, string, error) {
	// Stable prefix first, then dynamic — order is cache identity (#134).
	// Explicit profile: structured content parts + breakpoint after last stable block;
	// dynamic messages never carry breakpoints (explicit-only mode).
	explicit := p.caps.CacheMode == CacheModeExplicitBreakpoint && p.caps.CacheKey
	nStable := len(req.Prompt.StablePrefix)
	msgs := make([]oaiRespInputItem, 0, len(req.Prompt.StablePrefix)+len(req.Prompt.DynamicSuffix))
	for i, b := range req.Prompt.StablePrefix {
		role := b.Role
		if role == "" {
			role = PromptRoleSystem
		}
		if explicit {
			parts := []oaiRespContentPart{{Type: "input_text", Text: b.Text}}
			if i == nStable-1 {
				// Documented explicit breakpoint at end of stable prefix.
				parts = append(parts, oaiRespContentPart{Type: "prompt_cache_breakpoint"})
			}
			msgs = append(msgs, oaiRespInputItem{Role: role, Content: parts})
		} else {
			msgs = append(msgs, oaiRespInputItem{Role: role, Content: b.Text})
		}
	}
	for _, b := range req.Prompt.DynamicSuffix {
		role := b.Role
		if role == "" {
			role = PromptRoleUser
		}
		// Dynamic suffix: plain text content, never a cache breakpoint.
		if explicit {
			msgs = append(msgs, oaiRespInputItem{Role: role, Content: []oaiRespContentPart{{Type: "input_text", Text: b.Text}}})
		} else {
			msgs = append(msgs, oaiRespInputItem{Role: role, Content: b.Text})
		}
	}
	body := oaiResponsesRequest{
		Model:       p.cfg.Model,
		Input:       msgs,
		Temperature: 0,
		// Closed structured output for RawAssessment only.
		Text: &oaiResponsesText{
			Format: &oaiResponsesFormat{
				Type:   "json_schema",
				Name:   "reinframe_raw_assessment",
				Strict: true,
				Schema: rawAssessmentJSONSchema(),
			},
		},
	}
	var cacheKeyHash string
	if explicit {
		// Secret-free bounded key: provider+profile+stable+egress (not full prompt).
		cacheKeyHash = p.promptCacheKeyHash(req)
		body.PromptCacheKey = cacheKeyHash
		// Explicit-only: do not create an implicit changing-suffix breakpoint.
		body.PromptCacheOptions = &oaiPromptCacheOptions{Mode: "explicit"}
	}
	_ = maxIn
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", newProviderError("config", "marshal request", false, 0)
	}
	return payload, cacheKeyHash, nil
}

func (p *OpenAIResponsesProvider) promptCacheKeyHash(req ProviderRequest) string {
	// Audit-safe hash of identities — never raw prompt text or secrets.
	mat := fmt.Sprintf("openai_responses|%s|%s|%s|%s|%s",
		p.cfg.Model,
		p.cfg.CapabilitiesProfile,
		req.Prompt.StablePrefixHash,
		req.Prompt.RulesetHash,
		p.cfg.EgressProfile,
	)
	sum := sha256.Sum256([]byte(mat))
	return hex.EncodeToString(sum[:16])
}

func (p *OpenAIResponsesProvider) doOnce(ctx context.Context, payload []byte, maxOut int, req ProviderRequest, cacheKeyHash string) (ProviderResult, int, bool, time.Duration, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ProviderResult{}, 0, true, 0, newProviderError("transport", "build request", true, 0)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
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

	content, usage, reqID, err := parseResponsesEnvelope(body)
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}
	if cacheKeyHash != "" {
		usage.CacheKeyHash = cacheKeyHash
		usage.CacheBackend = KindOpenAIResponses
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
			Provider:            KindOpenAIResponses,
			ModelID:             p.cfg.Model,
			CapabilitiesProfile: p.cfg.CapabilitiesProfile,
			ProviderRequestID:   reqID,
			HTTPStatus:          status,
			ParseStatus:         ParseStatusOK,
		},
	}, status, false, 0, nil
}

type oaiResponsesRequest struct {
	Model              string                 `json:"model"`
	Input              []oaiRespInputItem     `json:"input"`
	Temperature        float64                `json:"temperature"`
	Text               *oaiResponsesText      `json:"text,omitempty"`
	PromptCacheKey     string                 `json:"prompt_cache_key,omitempty"`
	PromptCacheOptions *oaiPromptCacheOptions `json:"prompt_cache_options,omitempty"`
}

// oaiRespInputItem Content is string (off/implicit) or []oaiRespContentPart (explicit).
type oaiRespInputItem struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type oaiRespContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type oaiPromptCacheOptions struct {
	Mode string `json:"mode"`
}

type oaiResponsesText struct {
	Format *oaiResponsesFormat `json:"format"`
}

type oaiResponsesFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

func rawAssessmentJSONSchema() map[string]any {
	// Closed model payload only — host-owned fields are not model-provided.
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"schema_version", "severity", "reason_code", "evidence_event_ids"},
		"properties": map[string]any{
			"schema_version":     map[string]any{"type": "string", "const": SchemaRawAssessment},
			"severity":           map[string]any{"type": "integer", "minimum": 0, "maximum": 100},
			"reason_code":        map[string]any{"type": "string"},
			"evidence_event_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": MaxEvidenceIDs},
		},
	}
}

func parseResponsesEnvelope(body []byte) (content string, usage ProviderUsage, reqID string, err error) {
	if err := rejectDuplicateKeys(body); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope duplicate keys")
	}
	var env struct {
		ID     string `json:"id"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage *struct {
			InputTokens        int64 `json:"input_tokens"`
			OutputTokens       int64 `json:"output_tokens"`
			InputTokensDetails *struct {
				CachedTokens     int64 `json:"cached_tokens"`
				CacheWriteTokens int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
			OutputTokensDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope json")
	}
	// Exactly one structured message content block with assessment text.
	var texts []string
	for _, out := range env.Output {
		if out.Type != "" && out.Type != "message" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" || c.Type == "text" || c.Type == "" {
				if strings.TrimSpace(c.Text) != "" {
					texts = append(texts, c.Text)
				}
			}
		}
	}
	if len(texts) != 1 {
		return "", ProviderUsage{}, "", fmt.Errorf("expected exactly one structured assessment text")
	}
	content = texts[0]
	reqID = env.ID
	if env.Usage != nil {
		if env.Usage.InputTokens < 0 || env.Usage.OutputTokens < 0 {
			return "", ProviderUsage{}, "", fmt.Errorf("invalid negative token counts")
		}
		usage = ProviderUsage{
			InputTokens:  env.Usage.InputTokens,
			OutputTokens: env.Usage.OutputTokens,
			UsagePresent: true,
		}
		if env.Usage.InputTokensDetails != nil {
			if env.Usage.InputTokensDetails.CachedTokens < 0 {
				return "", ProviderUsage{}, "", fmt.Errorf("invalid negative cached tokens")
			}
			usage.CacheReadTokens = env.Usage.InputTokensDetails.CachedTokens
			// Uncached = total input - cached when both present and consistent.
			if usage.InputTokens >= usage.CacheReadTokens {
				usage.UncachedInputTokens = usage.InputTokens - usage.CacheReadTokens
			}
			if env.Usage.InputTokensDetails.CacheWriteTokens < 0 {
				return "", ProviderUsage{}, "", fmt.Errorf("invalid negative cache write tokens")
			}
			usage.CacheWriteTokens = env.Usage.InputTokensDetails.CacheWriteTokens
		}
		if env.Usage.OutputTokensDetails != nil {
			if env.Usage.OutputTokensDetails.ReasoningTokens < 0 {
				return "", ProviderUsage{}, "", fmt.Errorf("invalid negative reasoning tokens")
			}
			usage.ReasoningTokens = env.Usage.OutputTokensDetails.ReasoningTokens
		}
	}
	return content, usage, reqID, nil
}

// Endpoint returns the canonical request URL (tests).
func (p *OpenAIResponsesProvider) Endpoint() string { return p.endpoint }

// Capabilities returns the resolved capability profile (tests).
func (p *OpenAIResponsesProvider) Capabilities() ProviderCapabilities { return p.caps }

// PromptCacheKeyHashForTest exports cache key hashing for identity tests.
func (p *OpenAIResponsesProvider) PromptCacheKeyHashForTest(req ProviderRequest) string {
	return p.promptCacheKeyHash(req)
}

// BuildRequestJSONForTest exports the wire request for fixture assertions.
func (p *OpenAIResponsesProvider) BuildRequestJSONForTest(req ProviderRequest) ([]byte, string, error) {
	_, maxIn, _ := EffectiveBounds(req)
	return p.buildRequestJSON(req, maxIn)
}

// RedactedConfig returns a logging-safe copy (no secrets).
func (p *OpenAIResponsesProvider) RedactedConfig() map[string]any {
	return map[string]any{
		"kind":                 KindOpenAIResponses,
		"model":                p.cfg.Model,
		"base_url":             p.cfg.BaseURL,
		"path":                 p.cfg.Path,
		"api_key_ref":          p.cfg.APIKeyRef,
		"capabilities_profile": p.cfg.CapabilitiesProfile,
		"cache_mode":           p.caps.CacheMode,
		"egress_profile":       p.cfg.EgressProfile,
		"source_url":           OpenAIResponsesSourceURL,
		"source_retrieved":     OpenAIResponsesSourceRetrieved,
		"endpoint":             p.endpoint,
	}
}

func isOpenAIOfficialHost(base string) bool {
	u, err := parseLooseURL(base)
	if err != nil {
		return false
	}
	h := strings.ToLower(u)
	return h == "api.openai.com" || strings.HasSuffix(h, ".api.openai.com")
}

func parseLooseURL(base string) (host string, err error) {
	// Reuse normalize path by extracting host only for allowlist check.
	base = strings.TrimSpace(base)
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	// Minimal host extract without importing net/url again if unused.
	// Use existing normalizeBaseURL's package dependency on net/url via other file.
	from := strings.Index(base, "://")
	if from < 0 {
		return "", fmt.Errorf("bad url")
	}
	rest := base[from+3:]
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.Index(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		// strip port (not IPv6 for this allowlist)
		if !strings.Contains(rest, "]") {
			rest = rest[:i]
		}
	}
	return rest, nil
}
