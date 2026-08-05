package classifier_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/classifier"
)

func TestOpenAICompatible_ValidSuccess(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "" && strings.Contains(auth, "super-secret-key") {
			// ok bearer present
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"id":"req_abc",
			"choices":[{"message":{"content":%q}}],
			"usage":{"prompt_tokens":11,"completion_tokens":7}
		}`, `{"schema_version":"reinframe.raw_assessment.v1","severity":12,"reason_code":"NORMAL_PROGRESS"}`)
	}))
	defer srv.Close()

	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model:       "test-model",
		BaseURL:     srv.URL,
		Path:        "/",
		APIKeyRef:   "${TEST_CLASSIFIER_KEY}",
		AllowRemote: true,
		HTTPClient:  srv.Client(),
		LookupEnv: func(k string) (string, bool) {
			if k == "TEST_CLASSIFIER_KEY" {
				return "super-secret-key", true
			}
			return "", false
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{SessionID: "s", RulesetHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Assessment.Severity != 12 || res.Assessment.ReasonCode != "NORMAL_PROGRESS" {
		t.Fatalf("%+v", res.Assessment)
	}
	if !res.Usage.UsagePresent || res.Usage.InputTokens != 11 || res.Usage.OutputTokens != 7 {
		t.Fatalf("usage=%+v", res.Usage)
	}
	if res.Usage.CacheHit {
		t.Fatal("cache hit must be false for generic-none")
	}
	if res.Meta.ProviderRequestID != "req_abc" {
		t.Fatal(res.Meta.ProviderRequestID)
	}
	// Secret must not appear in redacted config or audit.
	rc := p.RedactedConfig()
	b, _ := json.Marshal(rc)
	if strings.Contains(string(b), "super-secret-key") {
		t.Fatal("secret leaked in redacted config")
	}
	audit := classifier.BuildProviderCallAudit(req, res, "c1", "c2", time.Now())
	ab, _ := audit.AuditJSON()
	if strings.Contains(string(ab), "super-secret-key") {
		t.Fatal("secret in audit")
	}
}

func TestOpenAICompatible_ModelContentCannotInjectUsage(t *testing.T) {
	t.Parallel()
	// Content tries to inject usage; parser rejects forbidden fields → Assess error.
	// Separate path: valid content but we verify transport usage wins when content is clean.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"x",
			"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":5,\"reason_code\":\"NORMAL_PROGRESS\",\"input_tokens\":999,\"cached_tokens\":1,\"cache_hit\":true,\"provider_request_id\":\"evil\"}"}}],
			"usage":{"prompt_tokens":3,"completion_tokens":1}
		}`))
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/", AllowRemote: true, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	_, err = p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("expected parse reject for injected usage fields")
	}
}

func TestOpenAICompatible_NoCacheFieldsInRequest(t *testing.T) {
	t.Parallel()
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}"}}]}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/", AllowRemote: true, HTTPClient: srv.Client(),
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	_, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"cache_control", "prompt_cache_key", "cachedContent", "x-grok-conv-id", "response_format"} {
		// response_format must NOT be sent for generic-none (NativeStructuredOutput=false)
		if strings.Contains(raw, bad) {
			t.Fatalf("must not send %s: %s", bad, raw)
		}
	}
}

func TestOpenAICompatible_Retry429Bounded(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}"}}]}`))
	}))
	defer srv.Close()
	var sleeps atomic.Int32
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/", AllowRemote: true, HTTPClient: srv.Client(),
		MaxRetries: 2,
		Sleep: func(ctx context.Context, d time.Duration) error {
			sleeps.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits=%d", hits.Load())
	}
	if res.Meta.RetryCount != 2 {
		t.Fatalf("retry_count=%d", res.Meta.RetryCount)
	}
	if sleeps.Load() != 2 {
		t.Fatalf("sleeps=%d", sleeps.Load())
	}
}

func TestOpenAICompatible_NonRetryable4xx(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	p, _ := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/", AllowRemote: true, HTTPClient: srv.Client(),
		MaxRetries: 2,
		Sleep:      func(context.Context, time.Duration) error { t.Fatal("should not sleep"); return nil },
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	_, err := p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d want 1", hits.Load())
	}
}

func TestOpenAICompatible_CancelStopsRetries(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	p, _ := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/", AllowRemote: true, HTTPClient: srv.Client(),
		MaxRetries: 5,
		Sleep: func(ctx context.Context, d time.Duration) error {
			cancel()
			return ctx.Err()
		},
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	_, err := p.Assess(ctx, req)
	if err == nil {
		t.Fatal("expected cancel error")
	}
}

func TestOpenAICompatible_OversizedRequestRejected(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "http://127.0.0.1:9", Path: "/", AllowRemote: true,
		MaxInputBytes: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SessionID: strings.Repeat("x", 200),
	})
	req.MaxInputBytes = 32
	_, err = p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("expected oversized request reject")
	}
}

func TestOpenAICompatible_RawAPIKeyRejected(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "http://127.0.0.1:9", APIKeyRef: "sk-raw-secret", AllowRemote: true,
	})
	if err == nil {
		t.Fatal("raw api key must be rejected")
	}
}

func TestOpenAICompatible_UnknownProfileFailsClosed(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "http://127.0.0.1:9", CapabilitiesProfile: "openai-native-v1", AllowRemote: true,
	})
	if err == nil {
		t.Fatal("unknown profile must fail")
	}
}

func TestOpenAICompatible_LoopbackRequired(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "https://api.openai.com/v1", AllowRemote: false,
	})
	if err == nil {
		t.Fatal("non-loopback without AllowRemote must fail")
	}
}

func TestCapabilities_GenericNone(t *testing.T) {
	t.Parallel()
	c, err := classifier.LookupCapabilitiesProfile(classifier.CapabilitiesProfileGenericNoneV1)
	if err != nil {
		t.Fatal(err)
	}
	if c.CacheMode != classifier.CacheModeNone || c.NativeStructuredOutput {
		t.Fatalf("%+v", c)
	}
}

func TestParseRetryAfterBounded(t *testing.T) {
	t.Parallel()
	h := http.Header{}
	h.Set("Retry-After", "999")
	d := classifier.ParseRetryAfterMs(h)
	if d > classifier.MaxRetryAfter {
		t.Fatal(d)
	}
}
