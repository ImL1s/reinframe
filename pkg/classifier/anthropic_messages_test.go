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

func TestAnthropicMessages_StructuredToolAndExplicitCache(t *testing.T) {
	t.Parallel()
	var gotBody []byte
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path=%s", r.URL.Path)
		}
		gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		gotBody = append([]byte(nil), b...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"msg_1",
			"content":[{"type":"tool_use","id":"tu1","name":"reinframe_raw_assessment","input":{"schema_version":"reinframe.raw_assessment.v1","severity":12,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":[]}}],
			"usage":{"input_tokens":60,"output_tokens":10,"cache_creation_input_tokens":80,"cache_read_input_tokens":40}
		}`))
	}))
	defer srv.Close()

	p, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "claude-test", BaseURL: srv.URL, Path: "/v1/messages", AllowRemote: true,
		HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicExplicitPrefix5mV1,
		APIKeyRef: "${TEST_ANTHROPIC_KEY}", Platform: classifier.AnthropicPlatformClaudeAPI,
		LookupEnv: func(k string) (string, bool) {
			if k == "TEST_ANTHROPIC_KEY" {
				return "sk-ant-test", true
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
	if res.Assessment.Severity != 12 || res.Assessment.ReasonCode != "NORMAL_PROGRESS" {
		t.Fatalf("%+v", res.Assessment)
	}
	if !res.Usage.UsagePresent || res.Usage.CacheReadTokens != 40 || !res.Usage.CacheHit {
		t.Fatalf("usage=%+v", res.Usage)
	}
	if res.Usage.CacheWriteTokens != 80 || res.Usage.UncachedInputTokens != 60 {
		t.Fatalf("usage fields=%+v", res.Usage)
	}
	if res.Usage.InputTokens != 60+40+80 {
		t.Fatalf("logical input=%d", res.Usage.InputTokens)
	}
	if res.Meta.ProviderRequestID != "msg_1" {
		t.Fatal(res.Meta.ProviderRequestID)
	}
	if gotHeaders.Get("x-api-key") != "sk-ant-test" {
		t.Fatal("x-api-key missing")
	}
	if gotHeaders.Get("anthropic-version") != classifier.AnthropicAPIVersion {
		t.Fatal("anthropic-version missing")
	}

	var wire map[string]any
	if err := json.Unmarshal(gotBody, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["model"] != "claude-test" {
		t.Fatalf("model=%v", wire["model"])
	}
	// system: last stable block carries cache_control; no cache on messages (dynamic).
	system, _ := wire["system"].([]any)
	if len(system) == 0 {
		t.Fatal("expected system blocks from stable prefix")
	}
	last, _ := system[len(system)-1].(map[string]any)
	cc, _ := last["cache_control"].(map[string]any)
	if cc["type"] != "ephemeral" || cc["ttl"] != "5m" {
		t.Fatalf("cache_control=%v", cc)
	}
	for i := 0; i < len(system)-1; i++ {
		blk, _ := system[i].(map[string]any)
		if blk["cache_control"] != nil {
			t.Fatal("only last stable system block may have cache_control")
		}
	}
	msgs, _ := wire["messages"].([]any)
	for _, raw := range msgs {
		msg, _ := raw.(map[string]any)
		arr, _ := msg["content"].([]any)
		for _, p := range arr {
			part, _ := p.(map[string]any)
			if part["cache_control"] != nil {
				t.Fatal("dynamic messages must not carry cache_control")
			}
		}
	}
	tools, _ := wire["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools=%v", tools)
	}
	tc, _ := wire["tool_choice"].(map[string]any)
	if tc["type"] != "tool" || tc["name"] != "reinframe_raw_assessment" {
		t.Fatalf("tool_choice=%v", tc)
	}
}

func TestAnthropicMessages_AutomaticVsExplicitFixtures(t *testing.T) {
	t.Parallel()
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		TaskAnchor:    classifier.TaskAnchor{TaskID: "t", Objective: "o"},
	})
	if err != nil {
		t.Fatal(err)
	}
	auto, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/messages", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicAutomatic5mV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	off, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/messages", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	expl, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/messages", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicExplicitPrefix5mV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	bAuto, _, err := auto.BuildRequestJSONForTest(req)
	if err != nil {
		t.Fatal(err)
	}
	bOff, _, err := off.BuildRequestJSONForTest(req)
	if err != nil {
		t.Fatal(err)
	}
	bExpl, _, err := expl.BuildRequestJSONForTest(req)
	if err != nil {
		t.Fatal(err)
	}
	// Honesty: automatic does not enable Anthropic wire caching (same as off for cache_control).
	if strings.Contains(string(bAuto), "cache_control") {
		t.Fatal("automatic profile must omit cache_control on wire")
	}
	if strings.Contains(string(bOff), "cache_control") {
		t.Fatal("off profile must omit cache_control")
	}
	if strings.Contains(string(bAuto), "cache_control") != strings.Contains(string(bOff), "cache_control") {
		t.Fatal("automatic and off must both omit content-block cache_control")
	}
	if !strings.Contains(string(bExpl), `"cache_control"`) {
		t.Fatal("explicit profile must include cache_control")
	}
	if string(bAuto) == string(bExpl) {
		t.Fatal("automatic and explicit fixtures must differ")
	}
	if auto.CacheTTL() != "5m" || expl.CacheTTL() != "5m" {
		t.Fatalf("ttl auto=%s expl=%s", auto.CacheTTL(), expl.CacheTTL())
	}
	// Minimal tool schema (no const/min/max) on wire.
	var wire map[string]any
	if err := json.Unmarshal(bExpl, &wire); err != nil {
		t.Fatal(err)
	}
	tools, _ := wire["tools"].([]any)
	tool, _ := tools[0].(map[string]any)
	schema, _ := tool["input_schema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	sev, _ := props["severity"].(map[string]any)
	if _, ok := sev["minimum"]; ok {
		t.Fatal("anthropic tool schema must not send minimum/maximum constraints")
	}
	if _, ok := sev["const"]; ok {
		t.Fatal("anthropic tool schema must not send const")
	}
}

func TestAnthropicMessages_Explicit1hTTL(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/messages", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicExplicitPrefix1hV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
	})
	b, _, err := p.BuildRequestJSONForTest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"ttl":"1h"`) {
		t.Fatalf("1h ttl missing: %s", b)
	}
}

func TestAnthropicMessages_UnsupportedPlatformRejected(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: "https://api.anthropic.com", Path: "/v1/messages",
		Platform: "bedrock", CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
		LookupEnv: func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err == nil {
		t.Fatal("bedrock must fail closed")
	}
	// Config validation path.
	c := config.Default()
	c.ClassifierProvider = config.ClassifierProviderConfig{
		Kind: "anthropic_messages", Model: "m", BaseURL: "https://api.anthropic.com",
		APIKeyRef: "${ANTHROPIC_API_KEY}", Platform: "vertex",
		CapabilitiesProfile: "anthropic-off-v1",
	}
	if err := c.Validate(); err == nil {
		t.Fatal("vertex platform must fail config validation")
	}
}

func TestAnthropicMessages_OffOmitsCacheControl(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/messages", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
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
	if key != "" || strings.Contains(string(b), "cache_control") {
		t.Fatalf("off must omit cache: key=%q body=%s", key, b)
	}
}

func TestAnthropicMessages_CacheHitOnlyFromPositiveRead(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"msg_2",
			"content":[{"type":"tool_use","name":"reinframe_raw_assessment","input":{"schema_version":"reinframe.raw_assessment.v1","severity":1,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":[]}}],
			"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}
		}`))
	}))
	defer srv.Close()
	p, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/messages", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicAutomatic5mV1,
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
		t.Fatal("zero cache_read must not be a hit")
	}
}

func TestAnthropicMessages_MissingUsageNotFabricatedHit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"msg_3",
			"content":[{"type":"tool_use","name":"reinframe_raw_assessment","input":{"schema_version":"reinframe.raw_assessment.v1","severity":1,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":[]}}]
		}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/messages", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
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
		t.Fatalf("missing usage must stay honest: %+v", res.Usage)
	}
}

func TestAnthropicMessages_StablePrefixCacheIdentity(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "claude-test", BaseURL: "http://127.0.0.1:1", Path: "/v1/messages", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicExplicitPrefix5mV1,
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
	k1 := p.PromptCacheKeyHashForTest(r1)
	k2 := p.PromptCacheKeyHashForTest(r2)
	if k1 != k2 {
		t.Fatal("cache key must ignore dynamic suffix")
	}
	p2, _ := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "claude-other", BaseURL: "http://127.0.0.1:1", Path: "/v1/messages", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicExplicitPrefix5mV1,
		LookupEnv:           func(string) (string, bool) { return "k", true },
		APIKeyRef:           "${K}", EgressProfile: "e1",
	})
	if p2.PromptCacheKeyHashForTest(r1) == k1 {
		t.Fatal("model change must alter cache key")
	}
	p3, _ := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "claude-test", BaseURL: "http://127.0.0.1:1", Path: "/v1/messages", AllowRemote: true,
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicExplicitPrefix1hV1,
		LookupEnv:           func(string) (string, bool) { return "k", true },
		APIKeyRef:           "${K}", EgressProfile: "e1",
	})
	if p3.PromptCacheKeyHashForTest(r1) == k1 {
		t.Fatal("profile/ttl change must alter cache key")
	}
}

func TestAnthropicMessages_MalformedNotAssessment(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"id":"msg_bad",
			"content":[{"type":"text","text":"not json assessment"}],
			"usage":{"input_tokens":1,"output_tokens":1}
		}`))
	}))
	defer srv.Close()
	p, _ := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/messages", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
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

func TestAnthropicMessages_401NotRetried(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	p, _ := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/messages", AllowRemote: true, HTTPClient: srv.Client(),
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
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

func TestAnthropicMessages_FactoryAndConfig(t *testing.T) {
	t.Parallel()
	cfg := config.ClassifierProviderConfig{
		Kind: "anthropic_messages", Model: "claude-x", BaseURL: "https://api.anthropic.com",
		APIKeyRef: "${ANTHROPIC_API_KEY}", CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
		Platform: "claude_api", TimeoutMS: 1500,
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
	if _, ok := p.(*classifier.AnthropicMessagesProvider); !ok {
		t.Fatalf("type %T", p)
	}
	// Cross-kind profile rejection.
	bad := config.Default()
	bad.ClassifierProvider = config.ClassifierProviderConfig{
		Kind: "openai_compatible", Model: "m", BaseURL: "http://127.0.0.1:1",
		APIKeyRef: "${K}", CapabilitiesProfile: "anthropic-explicit-prefix-5m-v1",
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("anthropic profile must not validate on openai_compatible")
	}
}

func TestAnthropicMessages_RedactedConfigNoSecret(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: "https://api.anthropic.com", Path: "/v1/messages",
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
		APIKeyRef:           "${ANTHROPIC_API_KEY}",
		LookupEnv:           func(string) (string, bool) { return "sk-ant-live-secret-xyz", true },
	})
	if err != nil {
		t.Fatal(err)
	}
	m := p.RedactedConfig()
	b, _ := json.Marshal(m)
	if strings.Contains(string(b), "sk-ant-live-secret-xyz") {
		t.Fatal("secret leaked")
	}
	if m["source_url"] != classifier.AnthropicMessagesSourceURL {
		t.Fatal("source pin missing")
	}
	if m["platform"] != classifier.AnthropicPlatformClaudeAPI {
		t.Fatal("platform pin missing")
	}
}

func TestAnthropicMessages_WrongPathRejected(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
		Model: "m", BaseURL: "https://api.anthropic.com", Path: "/v1/complete",
		CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
		LookupEnv:           func(string) (string, bool) { return "k", true }, APIKeyRef: "${K}",
	})
	if err == nil {
		t.Fatal("non-messages path must fail")
	}
}
