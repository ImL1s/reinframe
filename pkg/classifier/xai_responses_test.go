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

func TestXAIResponses_StructuredAndPromptCacheKey(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path=%s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("x-grok-conv-id") != "" {
			t.Error("x-grok-conv-id must not be sent on Responses profile")
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = append([]byte(nil), b...)
		_, _ = w.Write([]byte(`{
			"id":"xai_1",
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":12,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}],
			"usage":{"input_tokens":100,"output_tokens":10,"input_tokens_details":{"cached_tokens":40}}
		}`))
	}))
	defer srv.Close()

	p, err := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "grok-test", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
		HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileXAIResponsesPrefixV1,
		APIKeyRef: "${TEST_XAI_KEY}", LookupEnv: func(k string) (string, bool) {
			if k == "TEST_XAI_KEY" {
				return "xai-secret", true
			}
			return "", false
		},
		Timeout: time.Second, MaxRetries: 0, EgressProfile: "eg1",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs", RulesetHash: "rh",
		TaskAnchor: classifier.TaskAnchor{TaskID: "t1", Objective: "ship"},
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
	if res.Assessment.Severity != 12 || !res.Usage.CacheHit || res.Usage.CacheReadTokens != 40 {
		t.Fatalf("%+v %+v", res.Assessment, res.Usage)
	}
	if gotAuth != "Bearer xai-secret" {
		t.Fatal("auth header")
	}
	var wire map[string]any
	if err := json.Unmarshal(gotBody, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["prompt_cache_key"] == nil || wire["prompt_cache_key"] == "" {
		t.Fatal("prefix profile must send prompt_cache_key")
	}
	raw := string(gotBody)
	if strings.Contains(raw, "x-grok-conv-id") || strings.Contains(raw, "x_grok_conv_id") {
		t.Fatal("must not send chat completions routing fields")
	}
	if strings.Contains(raw, "xai-secret") || strings.Contains(raw, "sk-") {
		t.Fatal("secret in body")
	}
}

func TestXAIResponses_CacheKeyIdentity(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/responses", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileXAIResponsesPrefixV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		EgressProfile: "e1",
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
		t.Fatal("stable prefix must match")
	}
	if p.PromptCacheKeyHashForTest(r1) != p.PromptCacheKeyHashForTest(r2) {
		t.Fatal("key must ignore dynamic suffix")
	}
	p2, _ := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "other", BaseURL: "http://127.0.0.1:1", Path: "/v1/responses", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileXAIResponsesPrefixV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		EgressProfile: "e1",
	})
	if p2.PromptCacheKeyHashForTest(r1) == p.PromptCacheKeyHashForTest(r1) {
		t.Fatal("model change must alter key")
	}
	p3, _ := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/responses", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileXAIResponsesPrefixV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		EgressProfile: "e2",
	})
	if p3.PromptCacheKeyHashForTest(r1) == p.PromptCacheKeyHashForTest(r1) {
		t.Fatal("egress change must alter key")
	}
}

func TestXAIResponses_OffOmitsKey(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/responses", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileXAIOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	b, key, err := p.BuildRequestJSONForTest(req)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" || strings.Contains(string(b), "prompt_cache_key") {
		t.Fatalf("off must omit key: %s", b)
	}
}

func TestXAIResponses_KeyMatchZeroCachedIsMiss(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"xai_2",
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}],
			"usage":{"input_tokens":10,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}
		}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileXAIResponsesPrefixV1,
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
	if res.Usage.CacheHit {
		t.Fatal("zero cached tokens is miss even with key")
	}
	if res.Usage.CacheKeyHash == "" {
		t.Fatal("key hash still audited")
	}
}

func TestXAIResponses_401NotRetried(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	p, _ := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileXAIOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 2, Timeout: time.Second,
		Sleep: func(ctx context.Context, d time.Duration) error { t.Fatal("no sleep"); return nil },
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	_, err := p.Assess(context.Background(), req)
	if err == nil || hits.Load() != 1 {
		t.Fatalf("err=%v hits=%d", err, hits.Load())
	}
}

func TestXAIResponses_MalformedAndGenericNeutral(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"x","output":[{"type":"message","content":[{"type":"output_text","text":"not json"}]}]}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileXAIOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
		MaxRetries: 0, Timeout: time.Second,
	})
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	_, err := p.Assess(context.Background(), req)
	var pe *classifier.ProviderError
	if !errors.As(err, &pe) || pe.Class != "parse" {
		t.Fatalf("%v", err)
	}
	// Generic openai_compatible remains cache-neutral for xAI profiles.
	_, err = classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/chat/completions", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileXAIResponsesPrefixV1,
	})
	if err == nil {
		t.Fatal("generic must reject xai cache profile")
	}
}

func TestXAIResponses_FactoryAndRedacted(t *testing.T) {
	t.Parallel()
	cfg := config.ClassifierProviderConfig{
		Kind: "xai_responses", Model: "grok-x", BaseURL: "https://api.x.ai",
		APIKeyRef: "${XAI_API_KEY}", CapabilitiesProfile: classifier.CapabilitiesProfileXAIOffV1,
		TimeoutMS: 1500,
	}
	c := config.Default()
	c.ClassifierProvider = cfg
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := classifier.NewClassifierProviderFromConfig(cfg, classifier.ProviderFactoryOptions{
		LookupEnv:   func(string) (string, bool) { return "xai-live-secret", true },
		AllowRemote: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	xp, ok := p.(*classifier.XAIResponsesProvider)
	if !ok {
		t.Fatalf("%T", p)
	}
	m := xp.RedactedConfig()
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "xai-live-secret") {
		t.Fatal("secret leaked")
	}
	if m["x_grok_conv_id"] != false {
		t.Fatal("must document no chat routing")
	}
	// Foreign native profiles fail closed on constructor.
	_, err = classifier.NewXAIResponses(classifier.XAIResponsesConfig{
		Model: "m", BaseURL: "https://api.x.ai", Path: "/v1/responses",
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err == nil {
		t.Fatal("openai profile must not construct xai provider")
	}
	// Config: xai profile on wrong kind.
	bad := config.Default()
	bad.ClassifierProvider = config.ClassifierProviderConfig{
		Kind: "openai_compatible", Model: "m", BaseURL: "http://127.0.0.1:1",
		APIKeyRef: "${K}", CapabilitiesProfile: "xai-responses-prefix-v1",
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("xai profile requires xai_responses kind")
	}
}
