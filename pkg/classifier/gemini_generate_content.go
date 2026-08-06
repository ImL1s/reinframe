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

// KindGeminiGenerateContent is the closed config kind for native Gemini generateContent (#136).
const KindGeminiGenerateContent = "gemini_generate_content"

// DefaultGeminiAPIVersion pins the REST surface used by this adapter.
const DefaultGeminiAPIVersion = "v1beta"

// GeminiGenerateContentSourcePin documents the official capability pin for #136.
const (
	GeminiGenerateContentSourceURL       = "https://ai.google.dev/gemini-api/docs/caching"
	GeminiGenerateContentAPISourceURL    = "https://ai.google.dev/api"
	GeminiGenerateContentSourceRetrieved = "2026-08-06"
)

// Capability profile names for Gemini (#136).
const (
	CapabilitiesProfileGeminiOffV1             = "gemini-off-v1"
	CapabilitiesProfileGeminiImplicitV1        = "gemini-implicit-v1"         // min tokens 2048
	CapabilitiesProfileGeminiImplicitMin1024V1 = "gemini-implicit-min1024-v1" // alternate min for fixtures
)

// GeminiGenerateContentConfig configures the native Gemini generateContent adapter (#136).
type GeminiGenerateContentConfig struct {
	Kind    string
	Model   string
	BaseURL string // origin only; default https://generativelanguage.googleapis.com
	// Path optional; empty uses /{apiVersion}/models/{model}:generateContent
	Path                string
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

// GeminiGenerateContentProvider is the production-shaped native generateContent adapter.
type GeminiGenerateContentProvider struct {
	cfg               GeminiGenerateContentConfig
	caps              ProviderCapabilities
	minEligibleTokens int // 0 = no eligibility claim
	apiKey            string
	client            *http.Client
	endpoint          string
	sleep             func(context.Context, time.Duration) error
	now               func() time.Time
}

// NewGeminiGenerateContent builds a native generateContent adapter.
func NewGeminiGenerateContent(cfg GeminiGenerateContentConfig) (*GeminiGenerateContentProvider, error) {
	if cfg.Kind != "" && cfg.Kind != KindGeminiGenerateContent {
		return nil, newProviderError("config", "kind must be gemini_generate_content", false, 0)
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, newProviderError("config", "model is required", false, 0)
	}
	// Model path segment must not inject path traversal or alternate methods.
	if strings.ContainsAny(model, "/?#") || strings.Contains(model, "..") {
		return nil, newProviderError("config", "model is invalid", false, 0)
	}

	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	allowRemote := cfg.AllowRemote || isGeminiOfficialHost(base)
	baseCanon, err := normalizeBaseURL(base, allowRemote)
	if err != nil {
		return nil, err
	}

	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = fmt.Sprintf("/%s/models/%s:generateContent", DefaultGeminiAPIVersion, model)
	}
	pathCanon, err := normalizePath(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(pathCanon, ":generateContent") {
		return nil, newProviderError("config", "gemini path must end with :generateContent", false, 0)
	}
	if strings.Contains(pathCanon, "chat/completions") || strings.Contains(pathCanon, "/openai") {
		return nil, newProviderError("config", "gemini must not use openai-compatible path", false, 0)
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
		prof = CapabilitiesProfileGeminiOffV1
	}
	caps, err := LookupCapabilitiesProfile(prof)
	if err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	if err := ValidateCapabilities(caps); err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	if !caps.NativeStructuredOutput {
		return nil, newProviderError("capability", "gemini_generate_content requires native structured output profile", false, 0)
	}
	// Explicit cache objects are deferred (#136 non-goal).
	if caps.CacheMode == CacheModeExplicitObject || caps.CacheMode == CacheModeExplicitBreakpoint {
		return nil, newProviderError("capability", "gemini explicit cache modes not supported in this adapter", false, 0)
	}
	minTok, err := geminiMinEligibleTokens(prof)
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

	return &GeminiGenerateContentProvider{
		cfg:               cfg,
		caps:              caps,
		minEligibleTokens: minTok,
		apiKey:            apiKey,
		client:            client,
		endpoint:          endpoint,
		sleep:             sleep,
		now:               now,
	}, nil
}

func geminiMinEligibleTokens(prof string) (int, error) {
	switch prof {
	case CapabilitiesProfileGeminiOffV1:
		return 0, nil // no eligibility claim
	case CapabilitiesProfileGeminiImplicitV1:
		return 2048, nil
	case CapabilitiesProfileGeminiImplicitMin1024V1:
		return 1024, nil
	default:
		return 0, fmt.Errorf("unknown gemini profile %q", boundErr(prof))
	}
}

// Assess implements ClassifierProvider for native generateContent.
func (p *GeminiGenerateContentProvider) Assess(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	if ctx == nil {
		return ProviderResult{}, newProviderError("config", "nil context", false, 0)
	}
	if err := ctx.Err(); err != nil {
		return ProviderResult{}, err
	}
	if req.Input.AllowLegacyFixtureIDs {
		return ProviderResult{}, newProviderError("config", "legacy fixture mode not allowed on gemini_generate_content", false, 0)
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

	payload, cacheAudit, err := p.buildRequestJSON(req, maxIn)
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

		res, status, retryable, retryAfter, err := p.doOnce(opCtx, payload, maxOut, req, cacheAudit)
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
			Provider:            KindGeminiGenerateContent,
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

// geminiCacheAudit is secret-free request-side cache eligibility metadata.
type geminiCacheAudit struct {
	// Mode: none | ineligible | eligible | unknown
	Mode     string
	KeyHash  string
	MinTok   int
	EstInput int // approximate input size for eligibility (UTF-8 runes of stable+dynamic / 4)
}

func (p *GeminiGenerateContentProvider) buildRequestJSON(req ProviderRequest, maxIn int) ([]byte, geminiCacheAudit, error) {
	// Stable system first, then dynamic user contents — no timestamps/request IDs in prefix.
	var systemParts []gemPart
	var userParts []gemPart
	for _, b := range req.Prompt.StablePrefix {
		systemParts = append(systemParts, gemPart{Text: b.Text})
	}
	for _, b := range req.Prompt.DynamicSuffix {
		userParts = append(userParts, gemPart{Text: b.Text})
	}
	if len(userParts) == 0 {
		userParts = append(userParts, gemPart{Text: "Assess the supplied task against the closed schema."})
	}

	body := gemGenerateContentRequest{
		SystemInstruction: &gemContent{Parts: systemParts},
		Contents: []gemContent{{
			Role:  "user",
			Parts: userParts,
		}},
		GenerationConfig: &gemGenerationConfig{
			Temperature:      0,
			ResponseMIMEType: "application/json",
			ResponseSchema:   rawAssessmentJSONSchemaGemini(),
			CandidateCount:   1,
			MaxOutputTokens:  1024,
		},
	}

	audit := p.cacheAudit(req)
	_ = maxIn
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, geminiCacheAudit{}, newProviderError("config", "marshal request", false, 0)
	}
	return payload, audit, nil
}

func (p *GeminiGenerateContentProvider) cacheAudit(req ProviderRequest) geminiCacheAudit {
	if p.caps.CacheMode == CacheModeNone || p.minEligibleTokens <= 0 {
		return geminiCacheAudit{Mode: "none"}
	}
	// Approximate input tokens from plan text (no padding). Below min → ineligible.
	est := estimateGeminiTokens(req)
	if est < p.minEligibleTokens {
		return geminiCacheAudit{Mode: "ineligible", MinTok: p.minEligibleTokens, EstInput: est}
	}
	// Secret-free identity for eligible implicit profile (audit only; not a Gemini request field).
	mat := fmt.Sprintf("gemini_generate_content|%s|%s|%s|%s|%s|%d",
		p.cfg.Model, p.cfg.CapabilitiesProfile, req.Prompt.StablePrefixHash,
		req.Prompt.RulesetHash, p.cfg.EgressProfile, p.minEligibleTokens)
	sum := sha256.Sum256([]byte(mat))
	return geminiCacheAudit{
		Mode:     "eligible",
		KeyHash:  hex.EncodeToString(sum[:16]),
		MinTok:   p.minEligibleTokens,
		EstInput: est,
	}
}

func estimateGeminiTokens(req ProviderRequest) int {
	// Conservative byte/4 estimate of stable+dynamic text; never pads.
	n := 0
	for _, b := range req.Prompt.StablePrefix {
		n += len(b.Text)
	}
	for _, b := range req.Prompt.DynamicSuffix {
		n += len(b.Text)
	}
	if n == 0 {
		return 0
	}
	return (n + 3) / 4
}

func (p *GeminiGenerateContentProvider) doOnce(ctx context.Context, payload []byte, maxOut int, req ProviderRequest, audit geminiCacheAudit) (ProviderResult, int, bool, time.Duration, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return ProviderResult{}, 0, true, 0, newProviderError("transport", "build request", true, 0)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		// Documented Gemini API key header (not query string, avoids logs of URL secrets).
		httpReq.Header.Set("x-goog-api-key", p.apiKey)
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

	content, usage, reqID, err := parseGeminiGenerateContentEnvelope(body)
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}
	usage = p.applyCacheAudit(usage, audit)

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
			Provider:            KindGeminiGenerateContent,
			ModelID:             p.cfg.Model,
			CapabilitiesProfile: p.cfg.CapabilitiesProfile,
			ProviderRequestID:   reqID,
			HTTPStatus:          status,
			ParseStatus:         ParseStatusOK,
		},
	}, status, false, 0, nil
}

func (p *GeminiGenerateContentProvider) applyCacheAudit(usage ProviderUsage, audit geminiCacheAudit) ProviderUsage {
	// CacheHit only from positive provider-reported cached tokens.
	usage.CacheHit = usage.UsagePresent && usage.CacheReadTokens > 0
	switch audit.Mode {
	case "none":
		// No cache support claim for off profile.
		usage.CacheBackend = ""
		usage.CacheKeyHash = ""
		// Even if provider reports cached tokens, off profile does not claim cache product semantics;
		// still surface transport numbers honestly but CacheHit stays transport-true when positive.
	case "ineligible":
		usage.CacheBackend = KindGeminiGenerateContent + ":cache_ineligible"
		usage.CacheKeyHash = ""
		// Do not claim a product hit when profile says below minimum (transport field still visible).
		// CacheHit remains based on provider tokens only; eligibility is in CacheBackend.
	case "eligible":
		usage.CacheBackend = KindGeminiGenerateContent
		usage.CacheKeyHash = audit.KeyHash
	default:
		usage.CacheBackend = KindGeminiGenerateContent + ":unknown"
	}
	return usage
}

type gemGenerateContentRequest struct {
	SystemInstruction *gemContent          `json:"systemInstruction,omitempty"`
	Contents          []gemContent         `json:"contents"`
	GenerationConfig  *gemGenerationConfig `json:"generationConfig,omitempty"`
}

type gemContent struct {
	Role  string    `json:"role,omitempty"`
	Parts []gemPart `json:"parts"`
}

type gemPart struct {
	Text string `json:"text"`
}

type gemGenerationConfig struct {
	Temperature      float64        `json:"temperature"`
	ResponseMIMEType string         `json:"responseMimeType,omitempty"`
	ResponseSchema   map[string]any `json:"responseSchema,omitempty"`
	CandidateCount   int            `json:"candidateCount,omitempty"`
	MaxOutputTokens  int            `json:"maxOutputTokens,omitempty"`
}

func rawAssessmentJSONSchemaGemini() map[string]any {
	// Minimal schema for Gemini responseSchema; ParseRawAssessmentStrict enforces closed contract.
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

func parseGeminiGenerateContentEnvelope(body []byte) (content string, usage ProviderUsage, reqID string, err error) {
	if err := rejectDuplicateKeys(body); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope duplicate keys")
	}
	var env struct {
		ResponseID string `json:"responseId"`
		Candidates []struct {
			FinishReason string `json:"finishReason"`
			Content      *struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount        int64 `json:"promptTokenCount"`
			CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
			TotalTokenCount         int64 `json:"totalTokenCount"`
			CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
			ThoughtsTokenCount      int64 `json:"thoughtsTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope json")
	}
	if len(env.Candidates) != 1 {
		return "", ProviderUsage{}, "", fmt.Errorf("expected exactly one candidate")
	}
	cand := env.Candidates[0]
	if cand.Content == nil || len(cand.Content.Parts) == 0 {
		return "", ProviderUsage{}, "", fmt.Errorf("candidate has no content")
	}
	var texts []string
	for _, part := range cand.Content.Parts {
		if strings.TrimSpace(part.Text) != "" {
			texts = append(texts, part.Text)
		}
	}
	if len(texts) != 1 {
		return "", ProviderUsage{}, "", fmt.Errorf("expected exactly one assessment text part")
	}
	content = texts[0]
	reqID = env.ResponseID
	if env.UsageMetadata != nil {
		u := env.UsageMetadata
		if u.PromptTokenCount < 0 || u.CandidatesTokenCount < 0 || u.CachedContentTokenCount < 0 || u.ThoughtsTokenCount < 0 {
			return "", ProviderUsage{}, "", fmt.Errorf("invalid negative token counts")
		}
		usage = ProviderUsage{
			InputTokens:     u.PromptTokenCount,
			OutputTokens:    u.CandidatesTokenCount,
			CacheReadTokens: u.CachedContentTokenCount,
			ReasoningTokens: u.ThoughtsTokenCount,
			UsagePresent:    true,
		}
		if usage.InputTokens >= usage.CacheReadTokens {
			usage.UncachedInputTokens = usage.InputTokens - usage.CacheReadTokens
		}
	}
	return content, usage, reqID, nil
}

// Endpoint returns the canonical request URL (tests).
func (p *GeminiGenerateContentProvider) Endpoint() string { return p.endpoint }

// Capabilities returns the resolved capability profile (tests).
func (p *GeminiGenerateContentProvider) Capabilities() ProviderCapabilities { return p.caps }

// MinEligibleTokens returns the profile minimum for cache eligibility (tests).
func (p *GeminiGenerateContentProvider) MinEligibleTokens() int { return p.minEligibleTokens }

// CacheAuditForTest exports request-side eligibility audit.
func (p *GeminiGenerateContentProvider) CacheAuditForTest(req ProviderRequest) (mode, keyHash string, minTok, est int) {
	a := p.cacheAudit(req)
	return a.Mode, a.KeyHash, a.MinTok, a.EstInput
}

// BuildRequestJSONForTest exports the wire request for fixture assertions.
func (p *GeminiGenerateContentProvider) BuildRequestJSONForTest(req ProviderRequest) ([]byte, error) {
	_, maxIn, _ := EffectiveBounds(req)
	b, _, err := p.buildRequestJSON(req, maxIn)
	return b, err
}

// RedactedConfig returns a logging-safe copy (no secrets).
func (p *GeminiGenerateContentProvider) RedactedConfig() map[string]any {
	return map[string]any{
		"kind":                 KindGeminiGenerateContent,
		"model":                p.cfg.Model,
		"base_url":             p.cfg.BaseURL,
		"path":                 p.cfg.Path,
		"api_key_ref":          p.cfg.APIKeyRef,
		"capabilities_profile": p.cfg.CapabilitiesProfile,
		"cache_mode":           p.caps.CacheMode,
		"min_eligible_tokens":  p.minEligibleTokens,
		"egress_profile":       p.cfg.EgressProfile,
		"source_url":           GeminiGenerateContentSourceURL,
		"source_retrieved":     GeminiGenerateContentSourceRetrieved,
		"endpoint":             p.endpoint,
		"explicit_cache":       false,
	}
}

func isGeminiOfficialHost(base string) bool {
	h, err := parseLooseURL(base)
	if err != nil {
		return false
	}
	h = strings.ToLower(h)
	return h == "generativelanguage.googleapis.com" || strings.HasSuffix(h, ".googleapis.com")
}
