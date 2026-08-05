package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// OpenAICompatibleConfig configures the generic OpenAI-compatible classifier adapter (#132).
// Separate from pkg/reviewer — does not reuse Reviewer advice path.
type OpenAICompatibleConfig struct {
	// Kind must be "openai_compatible" when set.
	Kind string
	// Model is required.
	Model string
	// BaseURL required (e.g. http://127.0.0.1:11434/v1).
	BaseURL string
	// Path defaults to /v1/chat/completions when empty (joined carefully with BaseURL).
	Path string
	// APIKeyRef is ${ENV} only; raw secrets rejected.
	APIKeyRef string
	// Timeout defaults to DefaultTimeout.
	Timeout time.Duration
	// MaxInputBytes / MaxOutputBytes defaults applied when zero.
	MaxInputBytes  int
	MaxOutputBytes int
	// CapabilitiesProfile defaults to generic-none-v1.
	CapabilitiesProfile string
	// HTTPClient injectable for tests.
	HTTPClient *http.Client
	// Sleep injectable for retry backoff tests (default time.Sleep).
	Sleep func(context.Context, time.Duration) error
	// MaxRetries caps transport retries (default MaxRetryCount).
	MaxRetries int
	// Now for latency (tests).
	Now func() time.Time
	// LookupEnv resolves APIKeyRef (default os.LookupEnv).
	LookupEnv func(string) (string, bool)
	// AllowRemote when true permits non-loopback URLs (tests/CI only).
	// Production config should leave this false and honor ADR 003 local-only.
	AllowRemote bool
}

// OpenAICompatibleProvider is a production-shaped generic Chat Completions adapter.
// CacheMode is always none for generic-none-v1; no vendor cache fields are sent.
type OpenAICompatibleProvider struct {
	cfg    OpenAICompatibleConfig
	caps   ProviderCapabilities
	apiKey string // resolved once at construction; never logged
	client *http.Client
	sleep  func(context.Context, time.Duration) error
	now    func() time.Time
	lookup func(string) (string, bool)
}

// NewOpenAICompatible builds a generic classifier adapter.
func NewOpenAICompatible(cfg OpenAICompatibleConfig) (*OpenAICompatibleProvider, error) {
	if cfg.Kind != "" && cfg.Kind != "openai_compatible" {
		return nil, newProviderError("config", "kind must be openai_compatible", false, 0)
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		return nil, newProviderError("config", "model is required", false, 0)
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, newProviderError("config", "base_url is required", false, 0)
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, newProviderError("config", "invalid base_url", false, 0)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		// ok
	default:
		return nil, newProviderError("config", "base_url scheme must be http or https", false, 0)
	}
	// Reject URL userinfo (credentials in URL must never be accepted or logged).
	if u.User != nil {
		return nil, newProviderError("config", "base_url must not include userinfo", false, 0)
	}
	if !cfg.AllowRemote && !isLoopbackHost(u.Hostname()) {
		return nil, newProviderError("config", "base_url must be loopback unless AllowRemote", false, 0)
	}
	if cfg.APIKeyRef != "" {
		if !isEnvPlaceholder(cfg.APIKeyRef) {
			return nil, newProviderError("config", "api_key_ref must be ${ENV} placeholder", false, 0)
		}
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
	caps, err := LookupCapabilitiesProfile(cfg.CapabilitiesProfile)
	if err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	if err := ValidateCapabilities(caps); err != nil {
		return nil, newProviderError("capability", err.Error(), false, 0)
	}
	// generic-none must remain cache-neutral.
	if caps.CacheMode != CacheModeNone {
		return nil, newProviderError("capability", "generic adapter requires cache_mode=none", false, 0)
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

	path := cfg.Path
	if path == "" {
		path = "/v1/chat/completions"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	cfg.Path = path

	client := cfg.HTTPClient
	if client == nil {
		// No client-level Timeout: per-call budget is enforced via context.WithTimeout
		// so configured/request timeouts (up to MaxAllowedTimeout) are honored.
		// No redirects by default — prevent silent egress destination change.
		client = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
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

	return &OpenAICompatibleProvider{
		cfg:    cfg,
		caps:   caps,
		apiKey: apiKey,
		client: client,
		sleep:  sleep,
		now:    now,
		lookup: lookup,
	}, nil
}

// Assess implements ClassifierProvider.
func (p *OpenAICompatibleProvider) Assess(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	if ctx == nil {
		return ProviderResult{}, newProviderError("config", "nil context", false, 0)
	}
	if err := ctx.Err(); err != nil {
		return ProviderResult{}, err
	}
	if err := ValidateProviderRequest(req); err != nil {
		return ProviderResult{}, newProviderError("config", err.Error(), false, 0)
	}
	timeout, maxIn, maxOut := EffectiveBounds(req)
	if p.cfg.Timeout > 0 {
		timeout = p.cfg.Timeout
	}
	if p.cfg.MaxInputBytes > 0 {
		maxIn = p.cfg.MaxInputBytes
	}
	if p.cfg.MaxOutputBytes > 0 {
		maxOut = p.cfg.MaxOutputBytes
	}
	if req.Prompt.PromptBytes() > maxIn {
		return ProviderResult{}, newProviderError("oversized", "request exceeds max_input_bytes", false, 0)
	}

	// Build chat messages: stable prefix then dynamic suffix — no reorder.
	msgs := make([]oaiMessage, 0, len(req.Prompt.Messages()))
	for _, b := range req.Prompt.Messages() {
		role := b.Role
		if role == "" {
			role = PromptRoleUser
		}
		msgs = append(msgs, oaiMessage{Role: role, Content: b.Text})
	}
	body := oaiChatRequest{
		Model:       p.cfg.Model,
		Messages:    msgs,
		Temperature: 0,
		MaxTokens:   estimateMaxTokens(maxOut),
	}
	// JSON response_format only when capability allows native structured output.
	if p.caps.NativeStructuredOutput {
		body.ResponseFormat = &oaiResponseFormat{Type: "json_object"}
	}
	// Never set cache_control, prompt_cache_key, cachedContent, x-grok-conv-id, etc.

	payload, err := json.Marshal(body)
	if err != nil {
		return ProviderResult{}, newProviderError("config", "marshal request", false, 0)
	}
	if len(payload) > maxIn {
		return ProviderResult{}, newProviderError("oversized", "serialized request exceeds max_input_bytes", false, 0)
	}

	endpoint := strings.TrimRight(p.cfg.BaseURL, "/") + p.cfg.Path
	start := p.now()
	var lastStatus int
	var retryCount int
	var lastErr error
	var nextDelay time.Duration
	maxAttempts := p.cfg.MaxRetries + 1

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
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
			if err := p.sleep(ctx, delay); err != nil {
				return ProviderResult{}, err
			}
		}

		callCtx := ctx
		var cancel context.CancelFunc
		if timeout > 0 {
			callCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		res, status, retryable, retryAfter, err := p.doOnce(callCtx, endpoint, payload, maxOut, req)
		if cancel != nil {
			cancel()
		}
		lastStatus = status
		nextDelay = retryAfter
		if err == nil {
			res.Meta.RetryCount = retryCount
			res.Meta.LatencyMS = p.now().Sub(start).Milliseconds()
			res.Meta.HTTPStatus = status
			res.Assessment.LatencyMS = res.Meta.LatencyMS
			if res.Assessment.PromptHash == "" {
				res.Assessment.PromptHash = req.Prompt.PromptHash
			}
			if res.Assessment.RulesetHash == "" {
				res.Assessment.RulesetHash = req.Input.RulesetHash
			}
			if res.Assessment.RulesetID == "" {
				res.Assessment.RulesetID = req.Input.RulesetID
			}
			return res, nil
		}
		lastErr = err
		if !retryable || attempt+1 >= maxAttempts {
			break
		}
		// Only retry selected 429/5xx / transport.
	}

	// Terminal failure — no assessment decision; typed error for resolver.
	pe, ok := lastErr.(*ProviderError)
	if !ok {
		pe = newProviderError("transport", "provider call failed", false, lastStatus)
	}
	// Ensure error message never contains API key.
	pe.Message = redactSecret(pe.Message, p.apiKey)
	_ = retryCount
	return ProviderResult{
		Meta: ProviderMeta{
			Provider:            "openai_compatible",
			ModelID:             p.cfg.Model,
			CapabilitiesProfile: profileName(p.cfg.CapabilitiesProfile),
			HTTPStatus:          lastStatus,
			LatencyMS:           p.now().Sub(start).Milliseconds(),
			RetryCount:          retryCount,
			ParseStatus:         ParseStatusError,
			ErrorClass:          pe.Class,
			FallbackReason:      pe.Class,
		},
	}, pe
}

// doOnce performs one HTTP attempt. retryAfter is the capped delay from Retry-After
// (zero when absent); callers must still clamp to MaxRetryAfter before sleep.
func (p *OpenAICompatibleProvider) doOnce(ctx context.Context, endpoint string, payload []byte, maxOut int, req ProviderRequest) (ProviderResult, int, bool, time.Duration, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return ProviderResult{}, 0, true, 0, newProviderError("transport", "build request", true, 0)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ProviderResult{}, 0, false, 0, ctx.Err()
		}
		return ProviderResult{}, 0, true, 0, newProviderError("transport", "http do failed", true, 0)
	}
	defer func() { _ = resp.Body.Close() }()

	status := resp.StatusCode
	retryAfter := ParseRetryAfterMs(resp.Header)
	body, err := readBounded(resp.Body, maxOut+1024) // small slack for envelope
	if err != nil {
		if err == errOversized {
			return ProviderResult{}, status, false, 0, newProviderError("oversized", "response exceeds max_output_bytes", false, status)
		}
		return ProviderResult{}, status, true, retryAfter, newProviderError("transport", "read body", true, status)
	}

	if status == http.StatusTooManyRequests || status >= 500 {
		return ProviderResult{}, status, true, retryAfter, newProviderError("http", fmt.Sprintf("HTTP %d", status), true, status)
	}
	if status < 200 || status >= 300 {
		// Non-retryable 4xx
		return ProviderResult{}, status, false, 0, newProviderError("http", fmt.Sprintf("HTTP %d", status), false, status)
	}

	content, usage, reqID, err := parseOAIChatEnvelope(body)
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}

	// Usage only from transport metadata.
	// Ignore any usage-like fields inside content (parser also rejects them).
	assessment, err := ParseRawAssessmentStrict([]byte(content), maxOut, AllowedEvidenceSet(req.Input))
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}
	// Host-owned identity fields — never trust model content for these.
	assessment.ModelID = p.cfg.Model
	assessment.ModelVersion = ""
	assessment.PromptHash = req.Prompt.PromptHash
	assessment.RulesetID = req.Input.RulesetID
	assessment.RulesetHash = req.Input.RulesetHash
	assessment.ParseStatus = ParseStatusOK
	assessment.LatencyMS = 0 // filled by caller from wall clock

	return ProviderResult{
		Assessment: assessment,
		Usage:      usage,
		Meta: ProviderMeta{
			Provider:            "openai_compatible",
			ModelID:             p.cfg.Model,
			CapabilitiesProfile: profileName(p.cfg.CapabilitiesProfile),
			ProviderRequestID:   reqID,
			HTTPStatus:          status,
			ParseStatus:         ParseStatusOK,
		},
	}, status, false, 0, nil
}

type oaiChatRequest struct {
	Model          string             `json:"model"`
	Messages       []oaiMessage       `json:"messages"`
	Temperature    float64            `json:"temperature"`
	MaxTokens      int                `json:"max_tokens,omitempty"`
	ResponseFormat *oaiResponseFormat `json:"response_format,omitempty"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiResponseFormat struct {
	Type string `json:"type"`
}

func parseOAIChatEnvelope(body []byte) (content string, usage ProviderUsage, reqID string, err error) {
	// Closed envelope: choices[0].message.content + optional usage + optional id.
	var env struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
			TotalTokens      int64 `json:"total_tokens"`
			// Generic endpoints may omit cache splits — do not fabricate.
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope json")
	}
	if len(env.Choices) != 1 {
		return "", ProviderUsage{}, "", fmt.Errorf("expected exactly one choice")
	}
	content = env.Choices[0].Message.Content
	if content == "" {
		return "", ProviderUsage{}, "", fmt.Errorf("empty assistant content")
	}
	reqID = env.ID
	if env.Usage != nil {
		if env.Usage.PromptTokens < 0 || env.Usage.CompletionTokens < 0 {
			return "", ProviderUsage{}, "", fmt.Errorf("negative token counts")
		}
		usage = ProviderUsage{
			InputTokens:  env.Usage.PromptTokens,
			OutputTokens: env.Usage.CompletionTokens,
			UsagePresent: true,
			// CacheHit stays false — generic-none has no cache telemetry.
			CacheBackend: "",
		}
	}
	return content, usage, reqID, nil
}

var errOversized = fmt.Errorf("oversized")

func readBounded(r io.Reader, max int) ([]byte, error) {
	if max <= 0 {
		max = DefaultMaxOutputBytes
	}
	// Read max+1 to detect overflow without unbounded allocation.
	lr := io.LimitReader(r, int64(max)+1)
	b, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, errOversized
	}
	return b, nil
}

func estimateMaxTokens(maxOutBytes int) int {
	// Rough lower bound; keep small to enforce bounded output.
	if maxOutBytes <= 0 {
		return 1024
	}
	t := maxOutBytes / 4
	if t < 64 {
		t = 64
	}
	if t > 4096 {
		t = 4096
	}
	return t
}

func retryBackoff(attempt int) time.Duration {
	// attempt is 1-based retry index here when called with attempt>0 from loop.
	d := time.Duration(attempt) * 50 * time.Millisecond
	if d > MaxRetryAfter {
		d = MaxRetryAfter
	}
	return d
}

func isLoopbackHost(host string) bool {
	h := strings.ToLower(host)
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "[::1]"
}

func isEnvPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") && len(s) > 3
}

func envNameFromPlaceholder(s string) string {
	s = strings.TrimSpace(s)
	return s[2 : len(s)-1]
}

func profileName(s string) string {
	if s == "" {
		return CapabilitiesProfileGenericNoneV1
	}
	return s
}

func redactSecret(msg, secret string) string {
	if secret == "" || msg == "" {
		return msg
	}
	return strings.ReplaceAll(msg, secret, "[REDACTED]")
}

// RedactedConfig returns a copy safe for logging/serialization (no resolved secrets).
func (p *OpenAICompatibleProvider) RedactedConfig() map[string]any {
	base := p.cfg.BaseURL
	if u, err := url.Parse(base); err == nil {
		u.User = nil
		base = u.String()
	}
	return map[string]any{
		"kind":                 "openai_compatible",
		"model":                p.cfg.Model,
		"base_url":             base,
		"path":                 p.cfg.Path,
		"api_key_ref":          p.cfg.APIKeyRef, // placeholder only
		"capabilities_profile": profileName(p.cfg.CapabilitiesProfile),
		"cache_mode":           p.caps.CacheMode,
		"timeout_ms":           p.cfg.Timeout.Milliseconds(),
		"max_input_bytes":      p.cfg.MaxInputBytes,
		"max_output_bytes":     p.cfg.MaxOutputBytes,
	}
}

// ParseRetryAfterMs bounds Retry-After header to MaxRetryAfter.
func ParseRetryAfterMs(h http.Header) time.Duration {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0
	}
	if sec, err := strconv.Atoi(v); err == nil {
		d := time.Duration(sec) * time.Second
		if d > MaxRetryAfter {
			return MaxRetryAfter
		}
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
