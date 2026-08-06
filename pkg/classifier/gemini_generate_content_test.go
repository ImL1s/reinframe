package classifier_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/classifier"
	"github.com/ImL1s/reinframe/pkg/config"
)

func TestGeminiGenerateContent_StructuredAndCacheHit(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	var gotPath string
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-goog-api-key")
		b, _ := io.ReadAll(r.Body)
		gotBody = append([]byte(nil), b...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"responseId":"resp_g1",
			"candidates":[{"finishReason":"STOP","content":{"parts":[{"text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":12,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}}],
			"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"cachedContentTokenCount":40,"thoughtsTokenCount":2}
		}`))
	}))
	defer srv.Close()

	p, err := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "gemini-test", BaseURL: srv.URL, AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiImplicitV1,
		APIKeyRef:           "${TEST_GEMINI_KEY}",
		LookupEnv: func(k string) (string, bool) {
			if k == "TEST_GEMINI_KEY" {
				return "gk-test", true
			}
			return "", false
		},
		Timeout: time.Second, MaxRetries: 0, EgressProfile: "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Legitimate dynamic content within closed bounds (no pad tokens).
	events := make([]classifier.EventDigest, 0, 40)
	for i := 0; i < 40; i++ {
		events = append(events, classifier.EventDigest{
			EventID:  fmt.Sprintf("evt-%04d", i),
			Sequence: uint64(i + 1), EventType: "tool_call",
			Summary: strings.Repeat("dyn", 100),
		})
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs1", RulesetHash: "rh1",
		TaskAnchor:   classifier.TaskAnchor{TaskID: "t1", Objective: strings.Repeat("objective-", 40)},
		RecentEvents: events,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Assessment.Severity != 12 {
		t.Fatalf("%+v", res.Assessment)
	}
	if !res.Usage.UsagePresent || res.Usage.CacheReadTokens != 40 || !res.Usage.CacheHit {
		t.Fatalf("usage=%+v", res.Usage)
	}
	if res.Usage.UncachedInputTokens != 60 {
		t.Fatalf("uncached=%d", res.Usage.UncachedInputTokens)
	}
	if res.Usage.ReasoningTokens != 2 {
		t.Fatalf("thoughts=%d", res.Usage.ReasoningTokens)
	}
	if res.Meta.ProviderRequestID != "resp_g1" {
		t.Fatal(res.Meta.ProviderRequestID)
	}
	if gotKey != "gk-test" {
		t.Fatal("api key header missing")
	}
	if !strings.HasSuffix(gotPath, ":generateContent") {
		t.Fatalf("path=%s", gotPath)
	}

	var wire map[string]any
	if err := json.Unmarshal(gotBody, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["systemInstruction"] == nil {
		t.Fatal("expected systemInstruction from stable prefix")
	}
	gen, _ := wire["generationConfig"].(map[string]any)
	if gen["responseMimeType"] != "application/json" {
		t.Fatalf("gen=%v", gen)
	}
	if _, ok := gen["responseSchema"]; !ok {
		t.Fatal("responseSchema required")
	}
	// No unsupported cache-control / explicit cache fields.
	raw := string(gotBody)
	if strings.Contains(raw, "cache_control") || strings.Contains(raw, "cachedContent") {
		t.Fatal("must not send explicit cache object fields")
	}
}

func TestGeminiGenerateContent_BelowMinIneligibleNoPadding(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"responseId":"r",
			"candidates":[{"content":{"parts":[{"text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}}],
			"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":1,"cachedContentTokenCount":0}
		}`))
	}))
	defer srv.Close()
	p, err := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: srv.URL, AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiImplicitV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 0, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	mode, key, minTok, est := p.CacheAuditForTest(req)
	if mode != "ineligible" || key != "" || minTok != 2048 {
		t.Fatalf("mode=%s key=%s min=%d est=%d", mode, key, minTok, est)
	}
	// Body must not grow with padding filler.
	b, err := p.BuildRequestJSONForTest(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "pad") || strings.Contains(string(b), "PADDING") {
		t.Fatal("must not pad prompts")
	}
	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.CacheBackend != classifier.KindGeminiGenerateContent+":cache_ineligible" {
		t.Fatalf("backend=%s", res.Usage.CacheBackend)
	}
	if res.Usage.CacheHit {
		t.Fatal("zero cached must not hit")
	}
}

func TestGeminiGenerateContent_TwoMinProfiles(t *testing.T) {
	t.Parallel()
	p2048, err := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiImplicitV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	p1024, err := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiImplicitMin1024V1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p2048.MinEligibleTokens() != 2048 || p1024.MinEligibleTokens() != 1024 {
		t.Fatalf("%d %d", p2048.MinEligibleTokens(), p1024.MinEligibleTokens())
	}
}

func TestGeminiGenerateContent_OffNoCacheClaim(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	mode, _, _, _ := p.CacheAuditForTest(req)
	if mode != "none" {
		t.Fatalf("mode=%s", mode)
	}
	if p.MinEligibleTokens() != 0 {
		t.Fatal("off must not claim min tokens")
	}
}

func TestGeminiGenerateContent_StablePrefixIdentity(t *testing.T) {
	t.Parallel()
	base := classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs", RulesetHash: "rh",
		TaskAnchor: classifier.TaskAnchor{TaskID: "t", Objective: "o"},
	}
	in1 := base
	in1.RecentEvents = []classifier.EventDigest{{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "a"}}
	in2 := base
	in2.RecentEvents = []classifier.EventDigest{{EventID: "e2", Sequence: 2, EventType: "observation", Summary: "b"}}
	r1, _ := classifier.NewProviderRequest(in1)
	r2, _ := classifier.NewProviderRequest(in2)
	if r1.Prompt.StablePrefixHash != r2.Prompt.StablePrefixHash {
		t.Fatal("stable must be identical across action-only changes")
	}
	if r1.Prompt.InputHash == r2.Prompt.InputHash {
		t.Fatal("dynamic change must alter InputHash")
	}
	p, _ := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiImplicitV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		EgressProfile: "e",
	})
	// Force eligible via large material is hard; key hash for eligible uses StablePrefixHash only when eligible.
	// Verify build places systemInstruction before user contents.
	b, err := p.BuildRequestJSONForTest(r1)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	_ = json.Unmarshal(b, &wire)
	if wire["systemInstruction"] == nil || wire["contents"] == nil {
		t.Fatal("stable system + dynamic contents required")
	}
}

func TestGeminiGenerateContent_MissingUsage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"responseId":"r",
			"candidates":[{"content":{"parts":[{"text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}}]
		}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: srv.URL, AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 0, Timeout: time.Second,
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.UsagePresent || res.Usage.CacheHit {
		t.Fatalf("%+v", res.Usage)
	}
}

func TestGeminiGenerateContent_MultiCandidateRejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"candidates":[
				{"content":{"parts":[{"text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}},
				{"content":{"parts":[{"text":"extra"}]}}
			]
		}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: srv.URL, AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 0, Timeout: time.Second,
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	_, err := p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("multi-candidate must fail")
	}
}

func TestGeminiGenerateContent_401NotRetried(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	p, _ := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: srv.URL, AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 2, Timeout: time.Second,
		Sleep: func(ctx context.Context, d time.Duration) error { t.Fatal("no sleep on 401"); return nil },
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	_, err := p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("expected 401")
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d", hits.Load())
	}
}

func TestGeminiGenerateContent_RejectExplicitPathAndProfiles(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: "https://generativelanguage.googleapis.com",
		Path: "/v1/chat/completions", CapabilitiesProfile: classifier.CapabilitiesProfileGeminiOffV1,
		LookupEnv: func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err == nil {
		t.Fatal("openai path must fail")
	}
	// Config: openai explicit profile on gemini kind fails validation.
	c := config.Default()
	c.ClassifierProvider = config.ClassifierProviderConfig{
		Kind: "gemini_generate_content", Model: "m", BaseURL: "https://generativelanguage.googleapis.com",
		APIKeyRef: "${GEMINI_API_KEY}", CapabilitiesProfile: "openai-explicit-prefix-v1",
	}
	if err := c.Validate(); err == nil {
		t.Fatal("openai profile on gemini must fail")
	}
}

func TestGeminiGenerateContent_FactoryAndRedacted(t *testing.T) {
	t.Parallel()
	cfg := config.ClassifierProviderConfig{
		Kind: "gemini_generate_content", Model: "gemini-x", BaseURL: "https://generativelanguage.googleapis.com",
		APIKeyRef: "${GEMINI_API_KEY}", CapabilitiesProfile: classifier.CapabilitiesProfileGeminiOffV1,
		TimeoutMS: 1500,
	}
	c := config.Default()
	c.ClassifierProvider = cfg
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := classifier.NewClassifierProviderFromConfig(cfg, classifier.ProviderFactoryOptions{
		LookupEnv:   func(string) (string, bool) { return "secret-key-xyz", true },
		AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gp, ok := p.(*classifier.GeminiGenerateContentProvider)
	if !ok {
		t.Fatalf("%T", p)
	}
	m := gp.RedactedConfig()
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "secret-key-xyz") {
		t.Fatal("secret leaked")
	}
	if m["explicit_cache"] != false {
		t.Fatal("must document explicit deferred")
	}
	if m["source_url"] != classifier.GeminiGenerateContentSourceURL {
		t.Fatal("source pin missing")
	}
}

func TestGeminiGenerateContent_MalformedParse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"candidates":[{"content":{"parts":[{"text":"not assessment json"}]}}]
		}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
		Model: "m", BaseURL: srv.URL, AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileGeminiOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 0, Timeout: time.Second,
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	_, err := p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("malformed must fail")
	}
	var pe *classifier.ProviderError
	if !errors.As(err, &pe) || pe.Class != "parse" {
		t.Fatalf("%v", err)
	}
}
