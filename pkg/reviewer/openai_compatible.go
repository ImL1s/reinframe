package reviewer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// OpenAICompatibleProvider calls an OpenAI-compatible chat/completions endpoint
// and maps the model JSON reply into protocol.ReviewDecision (#18-class).
//
// This is the situational LLM advice backend for the optional uncertain slow path.
// It is not used on high-confidence deterministic ZOOM_OUT.
type OpenAICompatibleProvider struct {
	baseURL    string // e.g. http://127.0.0.1:11434/v1 (no trailing slash required)
	model      string
	apiKey     string
	httpClient *http.Client
	// path defaults to /chat/completions (joined under baseURL).
	path string
	// systemPrompt overrides the default classifier instruction.
	systemPrompt string
	// now for DecidedAt (tests).
	now func() time.Time
}

// OpenAICompatibleConfig configures OpenAICompatibleProvider.
type OpenAICompatibleConfig struct {
	// BaseURL is required (e.g. "http://127.0.0.1:11434/v1" or mock server URL).
	BaseURL string
	// Model is sent as the chat completions model field.
	Model string
	// APIKey optional Bearer token (empty for local stubs that ignore auth).
	APIKey string
	// HTTPClient optional; default has a short timeout.
	HTTPClient *http.Client
	// Path overrides "/chat/completions".
	Path string
	// SystemPrompt optional classifier instruction.
	SystemPrompt string
	// Now overrides clock (tests).
	Now func() time.Time
}

// NewOpenAICompatible builds a non-fake ReviewerProvider.
func NewOpenAICompatible(cfg OpenAICompatibleConfig) (*OpenAICompatibleProvider, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("reviewer: BaseURL is required")
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	path := cfg.Path
	if path == "" {
		path = "/chat/completions"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	sys := cfg.SystemPrompt
	if sys == "" {
		sys = defaultTunnelClassifierSystem
	}
	return &OpenAICompatibleProvider{
		baseURL:      base,
		model:        model,
		apiKey:       cfg.APIKey,
		httpClient:   client,
		path:         path,
		systemPrompt: sys,
		now:          now,
	}, nil
}

const defaultTunnelClassifierSystem = `You are Reinframe TunnelClassifier. Reply with a single JSON object only (no markdown) using keys:
classification (TUNNEL_VISION or NORMAL_PROGRESS),
tunnel_confidence (0..1 number),
rationale (short string),
suggested_advice (string; zoom-out guidance when tunnel, empty when normal).
Do not include secrets or raw credentials.`

// Generate implements ReviewerProvider.
func (p *OpenAICompatibleProvider) Generate(ctx context.Context, req protocol.ReviewRequest) (protocol.ReviewDecision, error) {
	if ctx == nil {
		return protocol.ReviewDecision{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return protocol.ReviewDecision{}, err
	}
	if req.RequestID == "" {
		return protocol.ReviewDecision{}, ErrEmptyRequestID
	}

	model := req.Model
	if model == "" || model == "policy-optional" {
		model = p.model
	}

	userPrompt := req.Prompt
	if userPrompt == "" {
		userPrompt = "No signal details provided."
	}

	body := chatCompletionRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: p.systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0,
	}
	// Prefer JSON object mode when servers support it; fixtures may ignore.
	body.ResponseFormat = &chatResponseFormat{Type: "json_object"}

	payload, err := json.Marshal(body)
	if err != nil {
		return protocol.ReviewDecision{}, err
	}

	url := p.baseURL + p.path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return protocol.ReviewDecision{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return protocol.ReviewDecision{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return protocol.ReviewDecision{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return protocol.ReviewDecision{}, fmt.Errorf("reviewer: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}

	content, tokens, err := parseChatCompletionContent(raw)
	if err != nil {
		return protocol.ReviewDecision{}, err
	}
	dec, err := parseReviewDecisionJSON(content)
	if err != nil {
		return protocol.ReviewDecision{}, err
	}
	if dec.DecisionID == "" {
		dec.DecisionID = fmt.Sprintf("oai-%s", req.RequestID)
	}
	dec.RequestID = req.RequestID
	if dec.ReviewerRole == "" {
		dec.ReviewerRole = req.ReviewerRole
	}
	if dec.ReviewerRole == "" {
		dec.ReviewerRole = "TunnelClassifier"
	}
	if dec.TokensUsed == 0 {
		dec.TokensUsed = tokens
	}
	if dec.DecidedAt.IsZero() {
		dec.DecidedAt = p.now()
	}
	return dec, nil
}

type chatCompletionRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	Temperature    float64             `json:"temperature"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func parseChatCompletionContent(raw []byte) (content string, tokens int, err error) {
	var resp chatCompletionResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", 0, fmt.Errorf("reviewer: decode chat completion: %w", err)
	}
	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return "", 0, fmt.Errorf("reviewer: empty choices/content")
	}
	return resp.Choices[0].Message.Content, resp.Usage.TotalTokens, nil
}

// decisionPayload is the JSON object we ask the model to return.
type decisionPayload struct {
	Classification   string  `json:"classification"`
	TunnelConfidence float64 `json:"tunnel_confidence"`
	Rationale        string  `json:"rationale"`
	SuggestedAdvice  string  `json:"suggested_advice"`
}

func parseReviewDecisionJSON(content string) (protocol.ReviewDecision, error) {
	content = strings.TrimSpace(content)
	// Strip optional markdown fences.
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```JSON")
		content = strings.TrimPrefix(content, "```")
		if i := strings.LastIndex(content, "```"); i >= 0 {
			content = content[:i]
		}
		content = strings.TrimSpace(content)
	}
	var p decisionPayload
	if err := json.Unmarshal([]byte(content), &p); err != nil {
		return protocol.ReviewDecision{}, fmt.Errorf("reviewer: decision JSON: %w", err)
	}
	class := strings.TrimSpace(p.Classification)
	if class == "" {
		return protocol.ReviewDecision{}, fmt.Errorf("reviewer: classification required")
	}
	return protocol.ReviewDecision{
		Classification:   class,
		TunnelConfidence: p.TunnelConfidence,
		Rationale:        p.Rationale,
		SuggestedAdvice:  p.SuggestedAdvice,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ ReviewerProvider = (*OpenAICompatibleProvider)(nil)
