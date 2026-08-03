package reviewer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/config"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/reviewer"
)

func TestOpenAICompatible_ParsesSuggestedAdvice(t *testing.T) {
	t.Parallel()
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		decision := map[string]any{
			"classification":    "TUNNEL_VISION",
			"tunnel_confidence": 0.91,
			"rationale":         "same error thrice",
			"suggested_advice":  "Stop patching; re-read the failing test and replan.",
		}
		content, _ := json.Marshal(decision)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": string(content)}},
			},
			"usage": map[string]any{"total_tokens": 42},
		})
	}))
	t.Cleanup(srv.Close)

	p, err := reviewer.NewOpenAICompatible(reviewer.OpenAICompatibleConfig{
		BaseURL: srv.URL,
		Model:   "test-model",
		APIKey:  "sk-test",
		Path:    "/chat/completions",
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := p.Generate(context.Background(), protocol.ReviewRequest{
		RequestID:    "req-1",
		ReviewerRole: "TunnelClassifier",
		Model:        "policy-optional",
		Prompt:       "session=s failure_mode=repeated_error_loop",
		RequestedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotBody["model"] != "test-model" {
		t.Fatalf("model field=%v (request should use provider model when policy-optional)", gotBody["model"])
	}
	if dec.Classification != "TUNNEL_VISION" {
		t.Fatalf("class=%s", dec.Classification)
	}
	if dec.TunnelConfidence < 0.9 {
		t.Fatalf("conf=%v", dec.TunnelConfidence)
	}
	if !strings.Contains(dec.SuggestedAdvice, "replan") {
		t.Fatalf("advice=%q", dec.SuggestedAdvice)
	}
	if dec.TokensUsed != 42 {
		t.Fatalf("tokens=%d", dec.TokensUsed)
	}
	if dec.RequestID != "req-1" {
		t.Fatalf("req=%s", dec.RequestID)
	}
}

func TestOpenAICompatible_HTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	p, err := reviewer.NewOpenAICompatible(reviewer.OpenAICompatibleConfig{
		BaseURL: srv.URL, Model: "m", Path: "/chat/completions",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Generate(context.Background(), protocol.ReviewRequest{RequestID: "r"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAICompatible_ContextCancel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	p, err := reviewer.NewOpenAICompatible(reviewer.OpenAICompatibleConfig{
		BaseURL: srv.URL, Model: "m", Path: "/chat/completions",
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Generate(ctx, protocol.ReviewRequest{RequestID: "r"})
	if err == nil {
		t.Fatal("expected cancel error")
	}
}

func TestOpenAICompatible_RequiresBaseURL(t *testing.T) {
	t.Parallel()
	_, err := reviewer.NewOpenAICompatible(reviewer.OpenAICompatibleConfig{Model: "m"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewProviderFromConfig_LocalOnlyBlocksRemote(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Session.LocalOnlyReviewer = true
	cfg.Reviewer.Mode = "openai_compatible"
	cfg.Reviewer.BaseURL = "https://api.openai.com/v1"
	cfg.Reviewer.Model = "gpt-x"
	_, err := reviewer.NewProviderFromConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), "local_only") {
		t.Fatalf("err=%v", err)
	}
}

func TestNewProviderFromConfig_LocalRequiresLoopback(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Session.LocalOnlyReviewer = true
	cfg.Reviewer.Mode = "local"
	cfg.Reviewer.BaseURL = "https://example.com/v1"
	cfg.Reviewer.Model = "local-model"
	_, err := reviewer.NewProviderFromConfig(cfg)
	if err == nil {
		t.Fatal("expected loopback error")
	}
}

func TestNewProviderFromConfig_LocalLoopbackOK(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, _ := json.Marshal(map[string]any{
			"classification": "NORMAL_PROGRESS", "tunnel_confidence": 0.1,
			"rationale": "ok", "suggested_advice": "",
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": string(content)}}},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Default()
	cfg.Session.LocalOnlyReviewer = true
	cfg.Reviewer.Mode = "local"
	cfg.Reviewer.BaseURL = srv.URL // httptest is 127.0.0.1
	cfg.Reviewer.Model = "local-stub"
	p, err := reviewer.NewProviderFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := p.Generate(context.Background(), protocol.ReviewRequest{RequestID: "x", Prompt: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Classification != "NORMAL_PROGRESS" {
		t.Fatalf("%+v", dec)
	}
}

func TestNewProviderFromConfig_RemoteOptIn(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, _ := json.Marshal(map[string]any{
			"classification": "TUNNEL_VISION", "tunnel_confidence": 0.95,
			"rationale": "fix", "suggested_advice": "situational zoom from remote fixture",
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": string(content)}}},
		})
	}))
	t.Cleanup(srv.Close)

	cfg := config.Default()
	cfg.Session.LocalOnlyReviewer = false // opt-in remote
	cfg.Reviewer.Mode = "openai_compatible"
	cfg.Reviewer.BaseURL = srv.URL
	cfg.Reviewer.Model = "remote-fixture"
	p, err := reviewer.NewProviderFromConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := p.Generate(context.Background(), protocol.ReviewRequest{RequestID: "y", Prompt: "sig"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dec.SuggestedAdvice, "situational") {
		t.Fatalf("%q", dec.SuggestedAdvice)
	}
}
