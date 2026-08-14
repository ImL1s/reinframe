package classifier_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
	"github.com/ImL1s/reinframe/pkg/config"
)

func TestOpenAISpark_CapabilityGatingAndEntitlement(t *testing.T) {
	t.Parallel()

	// 1. Unentitled project fails closed on NewOpenAISpark
	_, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:            false,
		BaseURL:             "http://127.0.0.1:1",
		AllowRemote:         true,
		APIKeyRef:           "${TEST_OPENAI_KEY}",
		LookupEnv:           func(string) (string, bool) { return "sk-test-key", true },
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAISparkV1,
	})
	if err == nil {
		t.Fatal("unentitled spark config must fail closed")
	}
	var pe *classifier.ProviderError
	if !errors.As(err, &pe) || pe.Class != "capability" {
		t.Fatalf("expected capability error, got %v (%T)", err, err)
	}
	if !strings.Contains(pe.Message, "requires explicit project capability entitlement") {
		t.Fatalf("unexpected message: %s", pe.Message)
	}

	// 2. Unentitled project fails closed on NewOpenAIResponses with Spark model
	_, err = classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model:               classifier.ModelGPT53CodexSpark,
		BaseURL:             "http://127.0.0.1:1",
		AllowRemote:         true,
		SparkEntitled:       false,
		APIKeyRef:           "${TEST_OPENAI_KEY}",
		LookupEnv:           func(string) (string, bool) { return "sk-test-key", true },
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAISparkV1,
	})
	if err == nil {
		t.Fatal("unentitled spark model must fail closed on NewOpenAIResponses")
	}

	// 3. Custom entitlement verifier callback (entitlement rejected)
	_, err = classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:    true,
		BaseURL:     "http://127.0.0.1:1",
		AllowRemote: true,
		APIKeyRef:   "${TEST_OPENAI_KEY}",
		LookupEnv:   func(string) (string, bool) { return "sk-test-key", true },
		EntitlementVerifier: func(model, profile string) bool {
			return false // Deny project entitlement
		},
	})
	if err == nil {
		t.Fatal("verifier rejection must fail closed")
	}

	// 4. Entitled project succeeds
	p, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:    true,
		BaseURL:     "http://127.0.0.1:1",
		AllowRemote: true,
		APIKeyRef:   "${TEST_OPENAI_KEY}",
		LookupEnv:   func(string) (string, bool) { return "sk-test-key", true },
	})
	if err != nil {
		t.Fatalf("entitled spark config must succeed: %v", err)
	}
	if p == nil {
		t.Fatal("provider must not be nil")
	}
}

func TestOpenAISpark_OAuthAndSubscriptionTokenSeparation(t *testing.T) {
	t.Parallel()

	// 1. Prohibited OAuth token value in environment
	for _, badToken := range []string{
		"oauth-user-token-xyz",
		"chatgpt-oauth-session-token",
		"chatgpt_subscription_pro_token",
		"session-token-12345",
	} {
		_, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
			Entitled:    true,
			BaseURL:     "http://127.0.0.1:1",
			AllowRemote: true,
			APIKeyRef:   "${OAUTH_TOKEN}",
			LookupEnv:   func(string) (string, bool) { return badToken, true },
		})
		if err == nil {
			t.Fatalf("oauth token %q must be rejected for direct spark API profile", badToken)
		}
		var pe *classifier.ProviderError
		if !errors.As(err, &pe) || pe.Class != "config" {
			t.Fatalf("expected config error for oauth token, got: %v", err)
		}
		if !strings.Contains(pe.Message, "requires direct API key, not ChatGPT Pro OAuth subscription runtime") {
			t.Fatalf("unexpected message: %s", pe.Message)
		}
	}

	// 2. Prohibited OAuth placeholder ref
	for _, badRef := range []string{
		"${CHATGPT_OAUTH_TOKEN}",
		"${CODEX_SUBSCRIPTION_KEY}",
		"${SESSION_TOKEN}",
	} {
		_, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
			Entitled:    true,
			BaseURL:     "http://127.0.0.1:1",
			AllowRemote: true,
			APIKeyRef:   badRef,
			LookupEnv:   func(string) (string, bool) { return "sk-valid-key", true },
		})
		if err == nil {
			t.Fatalf("oauth ref %q must be rejected", badRef)
		}
	}
}

func TestOpenAISpark_ZeroSilentSubstitution(t *testing.T) {
	t.Parallel()

	// Verify that if gpt-5.3-codex-spark is requested, Reinframe does not silently substitute another model.
	p, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:    true,
		BaseURL:     "http://127.0.0.1:1",
		AllowRemote: true,
		APIKeyRef:   "${TEST_KEY}",
		LookupEnv:   func(string) (string, bool) { return "sk-valid-key", true },
	})
	if err != nil {
		t.Fatal(err)
	}

	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs1",
		RulesetHash:   "rh1",
		TaskAnchor:    classifier.TaskAnchor{TaskID: "t1", Objective: "maintain invariant"},
	})
	if err != nil {
		t.Fatal(err)
	}

	wireBytes, _, err := p.BuildRequestJSONForTest(req)
	if err != nil {
		t.Fatal(err)
	}

	var wire map[string]any
	if err := json.Unmarshal(wireBytes, &wire); err != nil {
		t.Fatal(err)
	}

	// Must be exactly gpt-5.3-codex-spark, never substituted
	if wire["model"] != classifier.ModelGPT53CodexSpark {
		t.Fatalf("model was modified or substituted: %v (want %s)", wire["model"], classifier.ModelGPT53CodexSpark)
	}
}

func TestOpenAISpark_RequestFormattingAndReasoningEffort(t *testing.T) {
	t.Parallel()

	// 1. Test reasoning effort controls (low, medium, high)
	for _, effort := range []string{classifier.ReasoningEffortLow, classifier.ReasoningEffortMedium, classifier.ReasoningEffortHigh} {
		p, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
			Entitled:        true,
			ReasoningEffort: effort,
			BaseURL:         "http://127.0.0.1:1",
			AllowRemote:     true,
			APIKeyRef:       "${TEST_KEY}",
			LookupEnv:       func(string) (string, bool) { return "sk-valid-key", true },
		})
		if err != nil {
			t.Fatalf("reasoning_effort %s failed: %v", effort, err)
		}

		req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
			SchemaVersion: classifier.SchemaClassifierInput,
			PolicyClass:   classifier.PolicyClassProductivity,
			RulesetID:     "rs1",
			RulesetHash:   "rh1",
			TaskAnchor:    classifier.TaskAnchor{TaskID: "t1", Objective: "audit"},
		})
		if err != nil {
			t.Fatal(err)
		}

		wireBytes, cacheKey, err := p.BuildRequestJSONForTest(req)
		if err != nil {
			t.Fatal(err)
		}

		var wire map[string]any
		if err := json.Unmarshal(wireBytes, &wire); err != nil {
			t.Fatal(err)
		}

		if wire["model"] != classifier.ModelGPT53CodexSpark {
			t.Fatalf("model = %v", wire["model"])
		}
		if wire["reasoning_effort"] != effort {
			t.Fatalf("wire reasoning_effort = %v, want %s", wire["reasoning_effort"], effort)
		}
		reasoningObj, ok := wire["reasoning"].(map[string]any)
		if !ok || reasoningObj["effort"] != effort {
			t.Fatalf("wire reasoning obj = %v", reasoningObj)
		}
		if cacheKey == "" || wire["prompt_cache_key"] == nil {
			t.Fatal("prompt_cache_key must be present for Spark profile")
		}

		// Structured JSON schema verification
		textObj, ok := wire["text"].(map[string]any)
		if !ok {
			t.Fatalf("text object missing: %v", wire)
		}
		formatObj, ok := textObj["format"].(map[string]any)
		if !ok || formatObj["type"] != "json_schema" || formatObj["name"] != "reinframe_raw_assessment" {
			t.Fatalf("format invalid: %v", formatObj)
		}
		if formatObj["strict"] != true {
			t.Fatalf("strict mode must be true: %v", formatObj)
		}
	}

	// 2. Test invalid reasoning effort rejected
	_, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:        true,
		ReasoningEffort: "extreme_invalid",
		BaseURL:         "http://127.0.0.1:1",
		AllowRemote:     true,
		APIKeyRef:       "${TEST_KEY}",
		LookupEnv:       func(string) (string, bool) { return "sk-valid-key", true },
	})
	if err == nil {
		t.Fatal("invalid reasoning effort must be rejected")
	}
}

func TestOpenAISpark_ToolCallAlignment(t *testing.T) {
	t.Parallel()

	// 1. Valid tool call alignment
	pa := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion,
		SessionID:     "sess-spark-1",
		ActionID:      "action-spark-1",
		ToolName:      "Bash",
		ToolClass:     adapter.ToolClassShell,
		Command:       "git status",
		Arguments:     []string{"-s"},
		FilePath:      "",
		TargetScope:   []string{"pkg/classifier"},
		Source:        "codex_pretool",
		ParseStatus:   "ok",
	}

	if err := classifier.ValidateToolCallAlignment(pa); err != nil {
		t.Fatalf("valid tool call alignment failed: %v", err)
	}

	aligned, err := classifier.AlignSparkToolCall(pa)
	if err != nil {
		t.Fatalf("align tool call failed: %v", err)
	}
	if aligned["tool_name"] != "Bash" || aligned["tool_class"] != adapter.ToolClassShell || aligned["command"] != "git status" {
		t.Fatalf("unexpected aligned output: %+v", aligned)
	}

	// 2. Invalid tool calls
	// Missing tool_name
	invalidPA := pa
	invalidPA.ToolName = ""
	if err := classifier.ValidateToolCallAlignment(invalidPA); err == nil {
		t.Fatal("missing tool_name must fail alignment validation")
	}

	// Unknown tool_class
	invalidPA2 := pa
	invalidPA2.ToolClass = "invalid_nonexistent_class"
	if err := classifier.ValidateToolCallAlignment(invalidPA2); err == nil {
		t.Fatal("unknown tool_class must fail alignment validation")
	}

	// Truncated tool call rejected for alignment
	invalidPA3 := pa
	invalidPA3.Truncated = true
	if err := classifier.ValidateToolCallAlignment(invalidPA3); err == nil {
		t.Fatal("truncated tool call must fail alignment validation")
	}
}

func TestOpenAISpark_OutputParsingAndTelemetry(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-spark-secret" {
			t.Errorf("authorization header missing or incorrect: %s", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_spark_123",
			"output": [{
				"type": "message",
				"content": [{
					"type": "output_text",
					"text": "{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":42,\"reason_code\":\"SCOPE_DRIFT\",\"evidence_event_ids\":[\"ev1\"]}"
				}]
			}],
			"usage": {
				"input_tokens": 150,
				"output_tokens": 25,
				"input_tokens_details": {
					"cached_tokens": 50,
					"cache_write_tokens": 100
				},
				"output_tokens_details": {
					"reasoning_tokens": 15
				}
			}
		}`))
	}))
	defer srv.Close()

	p, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:        true,
		BaseURL:         srv.URL,
		Path:            "/v1/responses",
		AllowRemote:     true,
		HTTPClient:      srv.Client(),
		APIKeyRef:       "${SPARK_KEY}",
		LookupEnv:       func(string) (string, bool) { return "sk-spark-secret", true },
		ReasoningEffort: classifier.ReasoningEffortMedium,
		Timeout:         time.Second,
		MaxRetries:      0,
	})
	if err != nil {
		t.Fatal(err)
	}

	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs1",
		RulesetHash:   "rh1",
		TaskAnchor:    classifier.TaskAnchor{TaskID: "t1", Objective: "refactor"},
		RecentEvents: []classifier.EventDigest{
			{EventID: "ev1", Sequence: 1, EventType: "tool_call", Summary: "touch"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatalf("Assess failed: %v", err)
	}

	// Verify assessment fields
	if res.Assessment.Severity != 42 {
		t.Fatalf("severity = %d, want 42", res.Assessment.Severity)
	}
	if res.Assessment.ReasonCode != "SCOPE_DRIFT" {
		t.Fatalf("reason_code = %s, want SCOPE_DRIFT", res.Assessment.ReasonCode)
	}
	if len(res.Assessment.EvidenceEventIDs) != 1 || res.Assessment.EvidenceEventIDs[0] != "ev1" {
		t.Fatalf("evidence = %v, want [ev1]", res.Assessment.EvidenceEventIDs)
	}
	if res.Assessment.ParseStatus != classifier.ParseStatusOK {
		t.Fatalf("parse status = %s", res.Assessment.ParseStatus)
	}

	// Verify telemetry and usage
	if !res.Usage.UsagePresent {
		t.Fatal("usage must be present")
	}
	if res.Usage.InputTokens != 150 || res.Usage.OutputTokens != 25 {
		t.Fatalf("tokens in=%d out=%d", res.Usage.InputTokens, res.Usage.OutputTokens)
	}
	if res.Usage.CacheReadTokens != 50 || !res.Usage.CacheHit {
		t.Fatalf("cache read=%d hit=%t", res.Usage.CacheReadTokens, res.Usage.CacheHit)
	}
	if res.Usage.CacheWriteTokens != 100 {
		t.Fatalf("cache write=%d", res.Usage.CacheWriteTokens)
	}
	if res.Usage.ReasoningTokens != 15 {
		t.Fatalf("reasoning tokens = %d, want 15", res.Usage.ReasoningTokens)
	}
	if res.Usage.UncachedInputTokens != 100 {
		t.Fatalf("uncached tokens = %d, want 100", res.Usage.UncachedInputTokens)
	}

	// Meta check
	if res.Meta.ProviderRequestID != "resp_spark_123" {
		t.Fatalf("request ID = %s", res.Meta.ProviderRequestID)
	}
	if res.Meta.ModelID != classifier.ModelGPT53CodexSpark {
		t.Fatalf("model ID = %s", res.Meta.ModelID)
	}
}

func TestOpenAISpark_RateLimitAndRetryHandling(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := attempts.Add(1)
		if att < 2 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded for spark preview","type":"requests","code":"rate_limit_exceeded"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_spark_retry",
			"output": [{
				"type": "message",
				"content": [{
					"type": "output_text",
					"text": "{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":0,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"
				}]
			}],
			"usage": {"input_tokens": 10, "output_tokens": 2}
		}`))
	}))
	defer srv.Close()

	p, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:    true,
		BaseURL:     srv.URL,
		Path:        "/v1/responses",
		AllowRemote: true,
		HTTPClient:  srv.Client(),
		APIKeyRef:   "${SPARK_KEY}",
		LookupEnv:   func(string) (string, bool) { return "sk-spark-secret", true },
		MaxRetries:  2,
		Timeout:     5 * time.Second,
		Sleep:       func(ctx context.Context, d time.Duration) error { return nil }, // fast sleep
	})
	if err != nil {
		t.Fatal(err)
	}

	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
	})

	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatalf("Assess after retry failed: %v", err)
	}
	if res.Assessment.Severity != 0 || res.Assessment.ReasonCode != "NORMAL_PROGRESS" {
		t.Fatalf("unexpected assessment: %+v", res.Assessment)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
	if res.Meta.RetryCount != 1 {
		t.Fatalf("retry count = %d, want 1", res.Meta.RetryCount)
	}
}

func TestOpenAISpark_RedactionAndZeroSecretLeakage(t *testing.T) {
	t.Parallel()

	secretKey := "sk-spark-ultra-confidential-token-12345"
	p, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:        true,
		BaseURL:         "https://api.openai.com",
		Path:            "/v1/responses",
		APIKeyRef:       "${SPARK_SECRET_KEY}",
		LookupEnv:       func(string) (string, bool) { return secretKey, true },
		ReasoningEffort: classifier.ReasoningEffortLow,
		EgressProfile:   "sec-partition",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. RedactedConfig must contain zero secrets
	redacted := p.RedactedConfig()
	jsonBytes, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	jsonStr := string(jsonBytes)
	if strings.Contains(jsonStr, secretKey) {
		t.Fatalf("secret key leaked in RedactedConfig(): %s", jsonStr)
	}
	if redacted["spark_entitled"] != true {
		t.Fatalf("spark_entitled missing or false in RedactedConfig: %v", redacted)
	}
	if redacted["reasoning_effort"] != classifier.ReasoningEffortLow {
		t.Fatalf("reasoning_effort missing in RedactedConfig: %v", redacted)
	}
	if redacted["spark_source_url"] != classifier.OpenAISparkSourceURL {
		t.Fatalf("spark_source_url missing in RedactedConfig: %v", redacted)
	}

	// 2. Error message redaction
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		// Server echoes authorization header in error body
		_, _ = w.Write([]byte("internal error with key " + secretKey))
	}))
	defer errServer.Close()

	pErr, err := classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
		Entitled:    true,
		BaseURL:     errServer.URL,
		Path:        "/v1/responses",
		AllowRemote: true,
		HTTPClient:  errServer.Client(),
		APIKeyRef:   "${SPARK_KEY}",
		LookupEnv:   func(string) (string, bool) { return secretKey, true },
		MaxRetries:  0,
		Timeout:     time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}

	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
	})

	_, assessErr := pErr.Assess(context.Background(), req)
	if assessErr == nil {
		t.Fatal("expected error from 500 server")
	}

	errStr := assessErr.Error()
	if strings.Contains(errStr, secretKey) {
		t.Fatalf("secret key leaked in error string: %s", errStr)
	}
}

func TestOpenAISpark_FactoryIntegration(t *testing.T) {
	t.Parallel()

	// 1. Factory with Spark config and entitlement
	cfg := config.ClassifierProviderConfig{
		Kind:                "openai_responses",
		Model:               "gpt-5.3-codex-spark",
		BaseURL:             "https://api.openai.com",
		Path:                "/v1/responses",
		APIKeyRef:           "${OPENAI_API_KEY}",
		CapabilitiesProfile: "openai-spark-v1",
		SparkEntitled:       true,
		ReasoningEffort:     "low",
		TimeoutMS:           1500,
	}

	p, err := classifier.NewClassifierProviderFromConfig(cfg, classifier.ProviderFactoryOptions{
		LookupEnv:   func(string) (string, bool) { return "sk-spark-key", true },
		AllowRemote: true,
	})
	if err != nil {
		t.Fatalf("factory instantiation failed: %v", err)
	}
	if p == nil {
		t.Fatal("provider is nil")
	}
	sparkProvider, ok := p.(*classifier.OpenAIResponsesProvider)
	if !ok {
		t.Fatalf("expected *classifier.OpenAIResponsesProvider, got %T", p)
	}

	caps := sparkProvider.Capabilities()
	if !caps.NativeStructuredOutput {
		t.Fatal("spark profile must have native structured output")
	}
	if !caps.CacheKey || !caps.CacheUsageTelemetry {
		t.Fatalf("spark profile must support cache key & telemetry: %+v", caps)
	}
}
