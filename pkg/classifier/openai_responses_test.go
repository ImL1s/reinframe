package classifier_test

import (
	"context"
	"encoding/json"
	"errors"
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

func TestOpenAIResponses_StructuredOutputAndCacheKey(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path=%s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = append([]byte(nil), b...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"resp_1",
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":12,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}],
			"usage":{"input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":40},"output_tokens_details":{"reasoning_tokens":2}}
		}`))
	}))
	defer srv.Close()

	p, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "gpt-test", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
		HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIExplicitPrefixV1,
		APIKeyRef: "${TEST_OPENAI_KEY}",
		LookupEnv: func(k string) (string, bool) {
			if k == "TEST_OPENAI_KEY" {
				return "sk-test", true
			}
			return "", false
		},
		Timeout: time.Second, MaxRetries: 0, EgressProfile: "local-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs1",
		RulesetHash:   "rh1",
		TaskAnchor:    classifier.TaskAnchor{TaskID: "t1", Objective: "ship"},
		RecentEvents: []classifier.EventDigest{
			{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "edit"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.SchemaVersion != classifier.SchemaProviderResult {
		t.Fatal(res.SchemaVersion)
	}
	if res.Assessment.Severity != 12 || res.Assessment.ReasonCode != "NORMAL_PROGRESS" {
		t.Fatalf("%+v", res.Assessment)
	}
	if !res.Usage.UsagePresent || res.Usage.CacheReadTokens != 40 || !res.Usage.CacheHit {
		t.Fatalf("usage=%+v", res.Usage)
	}
	if res.Usage.UncachedInputTokens != 60 {
		t.Fatalf("uncached=%d", res.Usage.UncachedInputTokens)
	}
	if res.Meta.ProviderRequestID != "resp_1" {
		t.Fatal(res.Meta.ProviderRequestID)
	}
	// Wire request must include closed schema + explicit cache controls.
	var wire map[string]any
	if err := json.Unmarshal(gotBody, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["prompt_cache_key"] == nil || wire["prompt_cache_key"] == "" {
		t.Fatal("explicit profile must send prompt_cache_key")
	}
	opts, _ := wire["prompt_cache_options"].(map[string]any)
	if opts["mode"] != "explicit" {
		t.Fatalf("prompt_cache_options=%v", opts)
	}
	// Last stable input part must include prompt_cache_breakpoint; dynamic must not.
	input, _ := wire["input"].([]any)
	if len(input) < 2 {
		t.Fatalf("input len=%d", len(input))
	}
	foundBP := false
	for i, raw := range input {
		msg, _ := raw.(map[string]any)
		content := msg["content"]
		arr, ok := content.([]any)
		if !ok {
			continue
		}
		for _, p := range arr {
			part, _ := p.(map[string]any)
			if part["type"] == "prompt_cache_breakpoint" {
				if i >= len(req.Prompt.StablePrefix) {
					t.Fatal("breakpoint must not appear on dynamic messages")
				}
				foundBP = true
			}
		}
	}
	if !foundBP {
		t.Fatal("explicit profile must place prompt_cache_breakpoint after stable prefix")
	}
	text, _ := wire["text"].(map[string]any)
	format, _ := text["format"].(map[string]any)
	if format["type"] != "json_schema" || format["name"] != "reinframe_raw_assessment" {
		t.Fatalf("format=%v", format)
	}
}

func TestOpenAIResponses_StablePrefixCacheKeyIdentity(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "gpt-test", BaseURL: "http://127.0.0.1:1", Path: "/v1/responses", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIExplicitPrefixV1,
		LookupEnv:           func(string) (string, bool) { return "k", true },
		APIKeyRef:           "${K}", EgressProfile: "e1",
	})
	if err != nil {
		t.Fatal(err)
	}
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
		t.Fatal("stable prefix must be unchanged for dynamic-only changes")
	}
	if r1.Prompt.InputHash == r2.Prompt.InputHash {
		t.Fatal("dynamic change must alter InputHash")
	}
	k1 := p.PromptCacheKeyHashForTest(r1)
	k2 := p.PromptCacheKeyHashForTest(r2)
	if k1 != k2 {
		t.Fatal("cache key must ignore dynamic suffix (uses StablePrefixHash)")
	}
	// Model/profile change alters key.
	p2, _ := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "gpt-other", BaseURL: "http://127.0.0.1:1", Path: "/v1/responses", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIExplicitPrefixV1,
		LookupEnv:           func(string) (string, bool) { return "k", true },
		APIKeyRef:           "${K}", EgressProfile: "e1",
	})
	if p2.PromptCacheKeyHashForTest(r1) == k1 {
		t.Fatal("model change must alter cache key")
	}
}

func TestOpenAIResponses_UnsupportedProfileOmitsCacheKey(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "gpt-test", BaseURL: "http://127.0.0.1:1", Path: "/v1/responses", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true },
		APIKeyRef:           "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, key, err := p.BuildRequestJSONForTest(req)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" || strings.Contains(string(b), "prompt_cache_key") {
		t.Fatalf("off profile must omit cache key: key=%q body=%s", key, b)
	}
}

func TestOpenAIResponses_CacheHitOnlyFromPositiveCachedTokens(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"resp_2",
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}],
			"usage":{"input_tokens":10,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}
		}`))
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIImplicitV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 0, Timeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.CacheHit {
		t.Fatal("zero cached_tokens must not be a hit")
	}
}

func TestOpenAIResponses_MalformedOutputNotAssessment(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"resp_3",
			"output":[{"type":"message","content":[{"type":"output_text","text":"not json assessment"}]}],
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
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
		t.Fatalf("want parse error got %v", err)
	}
}

func TestOpenAIResponses_401NotRetried(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	p, _ := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 2, Timeout: time.Second,
		Sleep: func(ctx context.Context, d time.Duration) error { t.Fatal("must not sleep on 401"); return nil },
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

func TestOpenAIResponses_GenericCompatibleRemainsCacheNeutral(t *testing.T) {
	t.Parallel()
	// Existing generic profile still cannot enable cache modes.
	_, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/chat/completions", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIExplicitPrefixV1,
	})
	if err == nil {
		t.Fatal("generic adapter must reject non-none cache profiles")
	}
}

func TestOpenAIResponses_FactoryAndConfig(t *testing.T) {
	t.Parallel()
	cfg := config.ClassifierProviderConfig{
		Kind: "openai_responses", Model: "gpt-x", BaseURL: "https://api.openai.com",
		APIKeyRef: "${OPENAI_API_KEY}", CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		TimeoutMS: 1500,
	}
	c := config.Default()
	c.ClassifierProvider = cfg
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := classifier.NewClassifierProviderFromConfig(cfg, classifier.ProviderFactoryOptions{
		LookupEnv:   func(string) (string, bool) { return "sk", true },
		AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*classifier.OpenAIResponsesProvider); !ok {
		t.Fatalf("type %T", p)
	}
}

func TestOpenAIResponses_RedactedConfigNoSecret(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "m", BaseURL: "https://api.openai.com", Path: "/v1/responses",
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		APIKeyRef:           "${OPENAI_API_KEY}",
		LookupEnv:           func(string) (string, bool) { return "sk-live-secret-xyz", true },
	})
	if err != nil {
		t.Fatal(err)
	}
	m := p.RedactedConfig()
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "sk-live-secret-xyz") {
		t.Fatal("secret leaked")
	}
	if m["source_url"] != classifier.OpenAIResponsesSourceURL {
		t.Fatal("source pin missing")
	}
}
