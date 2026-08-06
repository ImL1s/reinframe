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

// KindXAIResponses is the closed config kind for the native xAI Responses API (#137).
const KindXAIResponses = "xai_responses"

// DefaultXAIResponsesPath is the pinned Responses endpoint path.
const DefaultXAIResponsesPath = "/v1/responses"

// XAIResponsesSourcePin documents the official capability pin for #137.
const (
	XAIResponsesSourceURL           = "https://docs.x.ai/developers/advanced-api-usage/prompt-caching"
	XAIResponsesMaximizingSourceURL = "https://docs.x.ai/developers/advanced-api-usage/prompt-caching/maximizing-cache-hits"
	XAIResponsesUsageSourceURL      = "https://docs.x.ai/developers/advanced-api-usage/prompt-caching/usage-and-pricing"
	XAIResponsesSourceRetrieved     = "2026-08-06"
)

// Capability profiles for xAI (#137).
const (
	CapabilitiesProfileXAIOffV1             = "xai-off-v1"
	CapabilitiesProfileXAIResponsesPrefixV1 = "xai-responses-prefix-v1"
)

// XAIResponsesConfig configures the native xAI Responses classifier adapter (#137).
// Separate from generic openai_compatible and openai_responses.
type XAIResponsesConfig struct {
	Kind                string
	Model               string
	BaseURL             string // origin only; default https://api.x.ai
	Path                string // default /v1/responses
	APIKeyRef           string
	Timeout             time.Duration
	MaxInputBytes       int
	MaxOutputBytes      int
	CapabilitiesProfile string
	EgressProfile       string
	HTTPClient          *http.Client
	Sleep               func(context.Context, time.Duration) error
	MaxRetries          int
	Now                 func() time.Time
	LookupEnv           func(string) (string, bool)
	AllowRemote         bool
}

// XAIResponsesProvider is the production-shaped native xAI Responses adapter.
type XAIResponsesProvider struct {
	cfg      XAIResponsesConfig
	caps     ProviderCapabilities
	apiKey   string
	client   *http.Client
	endpoint string
	sleep    func(context.Context, time.Duration) error
	now      func() time.Time
}

// NewXAIResponses builds a native xAI Responses adapter.
func NewXAIResponses(cfg XAIResponsesConfig) (*XAIResponsesProvider, error) {
	if cfg.Kind != "" && cfg.Kind != KindXAIResponses {
		return nil, newProviderError("config", "kind must be xai_responses", false, 0)
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, newProviderError("config", "model is required", false, 0)
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://api.x.ai"
	}
	allowRemote := cfg.AllowRemote || isXAIOfficialHost(base)
	baseCanon, err := normalizeBaseURL(base, allowRemote)
	if err != nil {
		return nil, err
	}
	pathCanon, err := normalizePath(cfg.Path)
	if err != nil {
		return nil, err
	}
	if cfg.Path == "" {
		pathCanon = DefaultXAIResponsesPath
	}
	if pathCanon != DefaultXAIResponsesPath {
		return nil, newProviderError("config", "xai_responses path must be /v1/responses", false, 0)
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
		prof = CapabilitiesProfileXAIOffV1
	}
	switch prof {
	case CapabilitiesProfileXAIOffV1, CapabilitiesProfileXAIResponsesPrefixV1:
	default:
		return nil, newProviderError("capability", "unknown xai profile", false, 0)
	}
	caps, err := LookupCapabilitiesProfile(prof)
	if err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	if err := ValidateCapabilities(caps); err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	if !caps.NativeStructuredOutput {
		return nil, newProviderError("capability", "xai_responses requires native structured output profile", false, 0)
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

	return &XAIResponsesProvider{
		cfg:      cfg,
		caps:     caps,
		apiKey:   apiKey,
		client:   client,
		endpoint: endpoint,
		sleep:    sleep,
		now:      now,
	}, nil
}

// Assess implements ClassifierProvider for the native xAI Responses API.
func (p *XAIResponsesProvider) Assess(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	if ctx == nil {
		return ProviderResult{}, newProviderError("config", "nil context", false, 0)
	}
	if err := ctx.Err(); err != nil {
		return ProviderResult{}, err
	}
	if req.Input.AllowLegacyFixtureIDs {
		return ProviderResult{}, newProviderError("config", "legacy fixture mode not allowed on xai_responses", false, 0)
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
			Provider:            KindXAIResponses,
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

func (p *XAIResponsesProvider) buildRequestJSON(req ProviderRequest, maxIn int) ([]byte, string, error) {
	// Stable prefix first, then dynamic. xAI exact-prefix caching + sticky prompt_cache_key.
	// Never send Chat Completions-only x-grok-conv-id on this Responses profile.
	msgs := make([]xaiRespInputItem, 0, len(req.Prompt.StablePrefix)+len(req.Prompt.DynamicSuffix))
	for _, b := range req.Prompt.StablePrefix {
		role := b.Role
		if role == "" {
			role = PromptRoleSystem
		}
		msgs = append(msgs, xaiRespInputItem{Role: role, Content: b.Text})
	}
	for _, b := range req.Prompt.DynamicSuffix {
		role := b.Role
		if role == "" {
			role = PromptRoleUser
		}
		msgs = append(msgs, xaiRespInputItem{Role: role, Content: b.Text})
	}
	body := xaiResponsesRequest{
		Model:       p.cfg.Model,
		Input:       msgs,
		Temperature: 0,
		Text: &xaiResponsesText{
			Format: &xaiResponsesFormat{
				Type:   "json_schema",
				Name:   "reinframe_raw_assessment",
				Strict: true,
				Schema: rawAssessmentJSONSchema(),
			},
		},
	}
	var cacheKeyHash string
	// Prefix profile: send secret-free prompt_cache_key for sticky routing (not a hit guarantee).
	if p.caps.CacheMode == CacheModeImplicitPrefix && p.caps.CacheKey {
		cacheKeyHash = p.promptCacheKeyHash(req)
		body.PromptCacheKey = cacheKeyHash
	}
	// Byte bound enforced by caller after marshal (Assess oversized check).
	_ = maxIn
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", newProviderError("config", "marshal request", false, 0)
	}
	// Chat Completions x-grok-conv-id is never a field on this wire type and never set as a header.
	return payload, cacheKeyHash, nil
}

func (p *XAIResponsesProvider) promptCacheKeyHash(req ProviderRequest) string {
	mat := fmt.Sprintf("xai_responses|%s|%s|%s|%s|%s",
		p.cfg.Model,
		p.cfg.CapabilitiesProfile,
		req.Prompt.StablePrefixHash,
		req.Prompt.RulesetHash,
		p.cfg.EgressProfile,
	)
	sum := sha256.Sum256([]byte(mat))
	return hex.EncodeToString(sum[:16])
}

func (p *XAIResponsesProvider) doOnce(ctx context.Context, payload []byte, maxOut int, req ProviderRequest, cacheKeyHash string) (ProviderResult, int, bool, time.Duration, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ProviderResult{}, 0, true, 0, newProviderError("transport", "build request", true, 0)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	// Never set x-grok-conv-id on Responses profile.

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

	// Reuse OpenAI-shaped Responses envelope parser (xAI documents OpenAI-shaped Responses usage).
	content, usage, reqID, err := parseResponsesEnvelope(body)
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}
	if cacheKeyHash != "" {
		usage.CacheKeyHash = cacheKeyHash
		usage.CacheBackend = KindXAIResponses
	}
	// Matching key is not a hit; only positive provider cached tokens.
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
			Provider:            KindXAIResponses,
			ModelID:             p.cfg.Model,
			CapabilitiesProfile: p.cfg.CapabilitiesProfile,
			ProviderRequestID:   reqID,
			HTTPStatus:          status,
			ParseStatus:         ParseStatusOK,
		},
	}, status, false, 0, nil
}

type xaiResponsesRequest struct {
	Model          string             `json:"model"`
	Input          []xaiRespInputItem `json:"input"`
	Temperature    float64            `json:"temperature"`
	Text           *xaiResponsesText  `json:"text,omitempty"`
	PromptCacheKey string             `json:"prompt_cache_key,omitempty"`
}

type xaiRespInputItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type xaiResponsesText struct {
	Format *xaiResponsesFormat `json:"format"`
}

type xaiResponsesFormat struct {
	Type   string         `json:"type"`
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

// Endpoint returns the canonical request URL (tests).
func (p *XAIResponsesProvider) Endpoint() string { return p.endpoint }

// Capabilities returns the resolved capability profile (tests).
func (p *XAIResponsesProvider) Capabilities() ProviderCapabilities { return p.caps }

// PromptCacheKeyHashForTest exports cache key hashing for identity tests.
func (p *XAIResponsesProvider) PromptCacheKeyHashForTest(req ProviderRequest) string {
	return p.promptCacheKeyHash(req)
}

// BuildRequestJSONForTest exports the wire request for fixture assertions.
func (p *XAIResponsesProvider) BuildRequestJSONForTest(req ProviderRequest) ([]byte, string, error) {
	_, maxIn, _ := EffectiveBounds(req)
	return p.buildRequestJSON(req, maxIn)
}

// RedactedConfig returns a logging-safe copy (no secrets).
func (p *XAIResponsesProvider) RedactedConfig() map[string]any {
	return map[string]any{
		"kind":                 KindXAIResponses,
		"model":                p.cfg.Model,
		"base_url":             p.cfg.BaseURL,
		"path":                 p.cfg.Path,
		"api_key_ref":          p.cfg.APIKeyRef,
		"capabilities_profile": p.cfg.CapabilitiesProfile,
		"cache_mode":           p.caps.CacheMode,
		"egress_profile":       p.cfg.EgressProfile,
		"source_url":           XAIResponsesSourceURL,
		"source_retrieved":     XAIResponsesSourceRetrieved,
		"endpoint":             p.endpoint,
		"x_grok_conv_id":       false,
	}
}

func isXAIOfficialHost(base string) bool {
	h, err := parseLooseURL(base)
	if err != nil {
		return false
	}
	h = strings.ToLower(h)
	return h == "api.x.ai" || strings.HasSuffix(h, ".api.x.ai")
}
