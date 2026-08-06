package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// OpenAICompatibleConfig configures the generic OpenAI-compatible classifier adapter (#132).
// Separate from pkg/reviewer — does not reuse Reviewer advice path.
//
// Canonical URL contract:
//
//	base_url = origin only (scheme + host[:port], path empty or "/")
//	path     = absolute endpoint path (default /v1/chat/completions)
//
// Example:
//
//	base_url: http://127.0.0.1:11434
//	path: /v1/chat/completions
type OpenAICompatibleConfig struct {
	Kind                string
	Model               string
	BaseURL             string
	Path                string
	APIKeyRef           string
	Timeout             time.Duration
	MaxInputBytes       int
	MaxOutputBytes      int
	CapabilitiesProfile string
	HTTPClient          *http.Client
	Sleep               func(context.Context, time.Duration) error
	MaxRetries          int
	Now                 func() time.Time
	LookupEnv           func(string) (string, bool)
	// AllowRemote when true permits non-loopback URLs (tests only).
	AllowRemote bool
}

// OpenAICompatibleProvider is a production-shaped generic Chat Completions adapter.
type OpenAICompatibleProvider struct {
	cfg      OpenAICompatibleConfig
	caps     ProviderCapabilities
	apiKey   string
	client   *http.Client
	endpoint string // canonical absolute URL
	sleep    func(context.Context, time.Duration) error
	now      func() time.Time
	lookup   func(string) (string, bool)
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
	baseCanon, err := normalizeBaseURL(cfg.BaseURL, cfg.AllowRemote)
	if err != nil {
		return nil, err
	}
	pathCanon, err := normalizePath(cfg.Path)
	if err != nil {
		return nil, err
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

	// Store normalized values.
	cfg.Model = model
	cfg.BaseURL = baseCanon
	cfg.Path = pathCanon

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

	return &OpenAICompatibleProvider{
		cfg:      cfg,
		caps:     caps,
		apiKey:   apiKey,
		client:   client,
		endpoint: endpoint,
		sleep:    sleep,
		now:      now,
		lookup:   lookup,
	}, nil
}

func normalizeBaseURL(raw string, allowRemote bool) (string, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		return "", newProviderError("config", "base_url is required", false, 0)
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", newProviderError("config", "invalid base_url", false, 0)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", newProviderError("config", "base_url scheme must be http or https", false, 0)
	}
	if u.User != nil {
		return "", newProviderError("config", "base_url must not include userinfo", false, 0)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", newProviderError("config", "base_url must not include query or fragment", false, 0)
	}
	// Origin only: path must be empty or "/".
	p := u.EscapedPath()
	if p != "" && p != "/" {
		return "", newProviderError("config", "base_url must be origin only (no path)", false, 0)
	}
	host := u.Hostname()
	if strings.TrimSpace(host) == "" {
		return "", newProviderError("config", "base_url hostname is required", false, 0)
	}
	if port := u.Port(); port != "" {
		if err := validateTCPPort(port); err != nil {
			return "", newProviderError("config", "base_url port must be 1-65535", false, 0)
		}
	}
	// Reject hosts that are not a valid IP and contain no DNS label chars.
	if ip := net.ParseIP(host); ip == nil {
		// Hostname: at least one letter or digit (after stripping brackets for IPv6 already handled).
		okHost := false
		for _, r := range host {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
				okHost = true
				break
			}
		}
		if !okHost || strings.Contains(host, " ") {
			return "", newProviderError("config", "base_url hostname is invalid", false, 0)
		}
	}
	if !allowRemote && !isLoopbackHost(host) {
		return "", newProviderError("config", "base_url must be loopback unless AllowRemote", false, 0)
	}
	// Canonical form without trailing slash path noise.
	return strings.ToLower(u.Scheme) + "://" + u.Host, nil
}

func normalizePath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		p = "/v1/chat/completions"
	}
	if !strings.HasPrefix(p, "/") {
		return "", newProviderError("config", "path must begin with /", false, 0)
	}
	if strings.Contains(p, "://") {
		return "", newProviderError("config", "path must not contain scheme/host", false, 0)
	}
	if strings.ContainsAny(p, "?#") {
		return "", newProviderError("config", "path must not include query or fragment", false, 0)
	}
	// Reject ".." traversal components.
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", newProviderError("config", "path traversal rejected", false, 0)
		}
	}
	if len(p) > 512 {
		return "", newProviderError("config", "path too long", false, 0)
	}
	// Clean without removing leading slash (path.Clean("//a") ok).
	cleaned := path.Clean(p)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned, nil
}

func joinEndpoint(baseCanon, pathCanon string) (string, error) {
	u, err := url.Parse(baseCanon)
	if err != nil {
		return "", newProviderError("config", "invalid base_url", false, 0)
	}
	u.Path = pathCanon
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// wrapClientNoRedirect clones transport/timeout but always denies redirects
// and never copies CookieJar (session cookies must not leak across destinations).
func wrapClientNoRedirect(in *http.Client) *http.Client {
	out := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if in != nil {
		out.Transport = in.Transport
		out.Timeout = in.Timeout
		// Intentionally do NOT copy CheckRedirect or Jar.
	}
	return out
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

	timeout, maxIn, maxOut := EffectiveProviderBounds(req, ProviderBoundSources{
		ConfigTimeout:      p.cfg.Timeout,
		ConfigMaxInput:     p.cfg.MaxInputBytes,
		ConfigMaxOutput:    p.cfg.MaxOutputBytes,
		CapabilityMaxInput: p.caps.MaxInputBytes,
	})

	// Overall Assess budget covers HTTP attempts, retry sleeps, and parsing.
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if req.Prompt.PromptBytes() > maxIn {
		return ProviderResult{}, newProviderError("oversized", "request exceeds max_input_bytes", false, 0)
	}

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
	if p.caps.NativeStructuredOutput {
		body.ResponseFormat = &oaiResponseFormat{Type: "json_object"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ProviderResult{}, newProviderError("config", "marshal request", false, 0)
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
			// Respect remaining deadline of the overall Assess budget.
			if dl, ok := opCtx.Deadline(); ok {
				rem := time.Until(dl)
				if rem <= 0 {
					return ProviderResult{}, classifyOpError(ctx, opCtx, opCtx.Err())
				}
				if delay > rem {
					delay = rem
				}
			}
			if err := p.sleep(opCtx, delay); err != nil {
				// Sleep failure must never become (ProviderResult{}, nil).
				if ce := classifyOpError(ctx, opCtx, err); ce != nil {
					return ProviderResult{}, ce
				}
				return ProviderResult{}, newProviderError("transport", "retry backoff failed", false, 0)
			}
		}

		res, status, retryable, retryAfter, err := p.doOnce(opCtx, payload, maxOut, req)
		lastStatus = status
		nextDelay = retryAfter
		if err == nil {
			// Recheck parent after successful attempt (cancel may race with success).
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
		// Parent abort preserves raw identity; adapter-owned timeout → typed timeout.
		if ce := classifyOpError(ctx, opCtx, err); ce != nil {
			if isParentContextAbort(ce) || isAdapterTimeout(ce) {
				return ProviderResult{}, ce
			}
			// classifyOpError wraps ordinary errors; keep retryable path below.
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
	if lastErr != nil && (errors.Is(lastErr, context.Canceled) || errors.Is(lastErr, context.DeadlineExceeded)) {
		return ProviderResult{}, classifyOpError(ctx, opCtx, lastErr)
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

func (p *OpenAICompatibleProvider) doOnce(ctx context.Context, payload []byte, maxOut int, req ProviderRequest) (ProviderResult, int, bool, time.Duration, error) {
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
	body, err := readBounded(resp.Body, maxOut+1024)
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
		return ProviderResult{}, status, false, 0, newProviderError("http", fmt.Sprintf("HTTP %d", status), false, status)
	}

	// Content-Type: if present, must be JSON media type.
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		ctLower := strings.ToLower(ct)
		if !strings.Contains(ctLower, "application/json") && !strings.Contains(ctLower, "+json") {
			return ProviderResult{}, status, false, 0, newProviderError("parse", "non-JSON content-type", false, status)
		}
	}

	content, usage, reqID, err := parseOAIChatEnvelope(body)
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}

	assessment, err := ParseRawAssessmentStrict([]byte(content), maxOut, evidenceAllowlist(req.Input))
	if err != nil {
		return ProviderResult{}, status, false, 0, newProviderError("parse", err.Error(), false, status)
	}
	assessment.ModelID = p.cfg.Model
	assessment.ModelVersion = ""
	assessment.PromptHash = req.Prompt.PromptHash
	assessment.RulesetID = req.Input.RulesetID
	assessment.RulesetHash = req.Input.RulesetHash
	assessment.ParseStatus = ParseStatusOK
	assessment.LatencyMS = 0

	return ProviderResult{
		SchemaVersion: SchemaProviderResult,
		Assessment:    assessment,
		Usage:         usage,
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
	// Reject duplicate critical keys at top level via token walk for "choices"/"usage"/"id".
	if err := rejectDuplicateKeys(body); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope duplicate keys")
	}
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
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return "", ProviderUsage{}, "", fmt.Errorf("envelope json")
	}
	if len(env.Choices) == 0 {
		return "", ProviderUsage{}, "", fmt.Errorf("zero choices")
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
		// Negative tokens are invalid telemetry — reject; do not clamp to a genuine zero.
		if env.Usage.PromptTokens < 0 || env.Usage.CompletionTokens < 0 || env.Usage.TotalTokens < 0 {
			return "", ProviderUsage{}, "", fmt.Errorf("invalid negative token counts")
		}
		usage = ProviderUsage{
			InputTokens:  env.Usage.PromptTokens,
			OutputTokens: env.Usage.CompletionTokens,
			UsagePresent: true,
		}
	}
	return content, usage, reqID, nil
}

var errOversized = fmt.Errorf("oversized")

func readBounded(r io.Reader, max int) ([]byte, error) {
	if max <= 0 {
		max = DefaultMaxOutputBytes
	}
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

// validateTCPPort requires a decimal port in [1, 65535].
func validateTCPPort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil {
		return err
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("port out of range")
	}
	return nil
}

func isEnvPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "${") || !strings.HasSuffix(s, "}") || len(s) < 4 {
		return false
	}
	inner := s[2 : len(s)-1]
	if inner == "" {
		return false
	}
	// [A-Za-z_][A-Za-z0-9_]*
	for i, r := range inner {
		if i == 0 {
			if !envIdentStart(r) {
				return false
			}
			continue
		}
		if !envIdentCont(r) {
			return false
		}
	}
	return true
}

func envIdentStart(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_'
}

func envIdentCont(r rune) bool {
	return envIdentStart(r) || (r >= '0' && r <= '9')
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
	return map[string]any{
		"kind":                 "openai_compatible",
		"model":                p.cfg.Model,
		"base_url":             p.cfg.BaseURL,
		"path":                 p.cfg.Path,
		"api_key_ref":          p.cfg.APIKeyRef,
		"capabilities_profile": profileName(p.cfg.CapabilitiesProfile),
		"cache_mode":           p.caps.CacheMode,
		"timeout_ms":           p.cfg.Timeout.Milliseconds(),
		"max_input_bytes":      p.cfg.MaxInputBytes,
		"max_output_bytes":     p.cfg.MaxOutputBytes,
		"endpoint":             p.endpoint,
	}
}

// Endpoint returns the canonical request URL (tests).
func (p *OpenAICompatibleProvider) Endpoint() string {
	return p.endpoint
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
