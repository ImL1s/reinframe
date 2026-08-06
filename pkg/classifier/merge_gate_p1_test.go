package classifier_test

import (
	"context"
	"io"
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

const validJSON = `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NORMAL_PROGRESS"}`

func TestParseRawAssessmentStrictRejectsTrailingProse(t *testing.T) {
	t.Parallel()
	_, err := classifier.ParseRawAssessmentStrict([]byte(validJSON+` trailing`), 8192, nil)
	if err == nil {
		t.Fatal("expected trailing prose reject")
	}
}

func TestParseRawAssessmentStrictRejectsMalformedSuffix(t *testing.T) {
	t.Parallel()
	_, err := classifier.ParseRawAssessmentStrict([]byte(validJSON+` {bad`), 8192, nil)
	if err == nil {
		t.Fatal("expected malformed suffix reject")
	}
	_, err = classifier.ParseRawAssessmentStrict([]byte(validJSON+`{}`), 8192, nil)
	if err == nil {
		t.Fatal("expected second object reject")
	}
}

func TestParseRawAssessmentStrictRejectsUnicodeBoundaryWhitespace(t *testing.T) {
	t.Parallel()
	nbsp := "\u00a0"
	em := "\u2003"
	for _, body := range []string{nbsp + validJSON, validJSON + nbsp, em + validJSON, validJSON + em} {
		_, err := classifier.ParseRawAssessmentStrict([]byte(body), 8192, nil)
		if err == nil {
			t.Fatalf("expected unicode boundary reject for %q", body)
		}
	}
	// ASCII JSON whitespace still ok
	_, err := classifier.ParseRawAssessmentStrict([]byte("  \n"+validJSON+"\t\r\n"), 8192, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseRawAssessmentStrictEvidenceRequiresAllowlist(t *testing.T) {
	t.Parallel()
	withEv := `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":["e1"]}`
	_, err := classifier.ParseRawAssessmentStrict([]byte(withEv), 8192, nil)
	if err == nil {
		t.Fatal("nil allowlist must reject evidence")
	}
	_, err = classifier.ParseRawAssessmentStrict([]byte(withEv), 8192, map[string]struct{}{})
	if err == nil {
		t.Fatal("empty allowlist must reject evidence")
	}
	// empty evidence list ok with empty allowlist
	emptyEv := `{"schema_version":"reinframe.raw_assessment.v1","severity":10,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":[]}`
	if _, err := classifier.ParseRawAssessmentStrict([]byte(emptyEv), 8192, map[string]struct{}{}); err != nil {
		t.Fatal(err)
	}
	// present allowlist ok
	if _, err := classifier.ParseRawAssessmentStrict([]byte(withEv), 8192, map[string]struct{}{"e1": {}}); err != nil {
		t.Fatal(err)
	}
}

func TestEffectiveProviderBoundsNeverWiden(t *testing.T) {
	t.Parallel()
	req := classifier.ProviderRequest{
		Timeout: 100 * time.Millisecond, MaxInputBytes: 1024, MaxOutputBytes: 512,
	}
	to, in, out := classifier.EffectiveProviderBounds(req, classifier.ProviderBoundSources{
		ConfigTimeout: 1500 * time.Millisecond, ConfigMaxInput: 65536, ConfigMaxOutput: 8192,
	})
	if to != 100*time.Millisecond {
		t.Fatalf("timeout widened: %v", to)
	}
	if in != 1024 {
		t.Fatalf("input widened: %d", in)
	}
	if out != 512 {
		t.Fatalf("output widened: %d", out)
	}
	// capability stricter
	to2, in2, _ := classifier.EffectiveProviderBounds(req, classifier.ProviderBoundSources{
		ConfigMaxInput: 65536, CapabilityMaxInput: 512,
	})
	if in2 != 512 {
		t.Fatalf("capability should win: %d", in2)
	}
	_ = to2
	// zero config does not erase request
	to3, in3, out3 := classifier.EffectiveProviderBounds(req, classifier.ProviderBoundSources{})
	if to3 != 100*time.Millisecond || in3 != 1024 || out3 != 512 {
		t.Fatalf("zero config erased request: %v %d %d", to3, in3, out3)
	}
}

func TestOpenAICompatible_RequestTimeoutCapsOverallBudget(t *testing.T) {
	t.Parallel()
	// Server always 429 with large Retry-After; overall 100ms must expire.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "999")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), Timeout: 5 * time.Second, MaxRetries: 5,
		// real sleep respects context cancellation from overall budget
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{})
	if err != nil {
		t.Fatal(err)
	}
	req.Timeout = 100 * time.Millisecond
	start := time.Now()
	_, err = p.Assess(context.Background(), req)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("operation exceeded request budget: %v", elapsed)
	}
}

func TestOpenAICompatible_OversizedRequestUsesRequestCap(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + "`" + validJSON + "`" + `}}]}`))
	}))
	// Fix server response properly
	srv.Close()
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}"}}]}`)
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), MaxInputBytes: 65536,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SessionID: strings.Repeat("x", 2000),
	})
	if err != nil {
		t.Fatal(err)
	}
	req.MaxInputBytes = 1024
	_, err = p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("expected oversized reject before HTTP")
	}
	if hits.Load() != 0 {
		t.Fatal("HTTP must not be called")
	}
}

func TestValidatePromptPlanRejectsMutatedDynamicSuffix(t *testing.T) {
	t.Parallel()
	in := classifier.ClassifierInput{SessionID: "s", RulesetID: "r", RulesetHash: "rh"}
	plan, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in)
	if err != nil {
		t.Fatal(err)
	}
	plan.DynamicSuffix = append(plan.DynamicSuffix, classifier.PromptBlock{
		Role: classifier.PromptRoleUser, Type: classifier.PromptTypeMeta, Text: "tampered",
	})
	if err := classifier.ValidatePromptPlan(plan, in); err == nil {
		t.Fatal("mutated dynamic must fail")
	}
}

func TestValidatePromptPlanRejectsStaleHashes(t *testing.T) {
	t.Parallel()
	in := classifier.ClassifierInput{SessionID: "s", RulesetHash: "rh"}
	plan, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in)
	if err != nil {
		t.Fatal(err)
	}
	plan.PromptHash = "deadbeef"
	if err := classifier.ValidatePromptPlan(plan, in); err == nil {
		t.Fatal("stale PromptHash must fail")
	}
	plan, _ = classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in)
	plan.StablePrefixHash = "deadbeef"
	if err := classifier.ValidatePromptPlan(plan, in); err == nil {
		t.Fatal("stale StablePrefixHash must fail")
	}
	plan, _ = classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in)
	plan.InputHash = "deadbeef"
	if err := classifier.ValidatePromptPlan(plan, in); err == nil {
		t.Fatal("stale InputHash must fail")
	}
}

func TestValidatePromptPlanBindsProviderInput(t *testing.T) {
	t.Parallel()
	inA := classifier.ClassifierInput{
		SessionID:      "s",
		ProposedAction: &adapter.ProposedAction{ToolName: "Bash", Command: "echo a", ToolClass: adapter.ToolClassShell},
	}
	inB := classifier.ClassifierInput{
		SessionID:      "s",
		ProposedAction: &adapter.ProposedAction{ToolName: "Bash", Command: "echo b", ToolClass: adapter.ToolClassShell},
	}
	planA, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), inA)
	if err != nil {
		t.Fatal(err)
	}
	if err := classifier.ValidatePromptPlan(planA, inB); err == nil {
		t.Fatal("input B with plan A must fail")
	}
	req, err := classifier.NewProviderRequest(inA)
	if err != nil {
		t.Fatal(err)
	}
	req.Input = inB
	if err := classifier.ValidateProviderRequest(req); err == nil {
		t.Fatal("ValidateProviderRequest must bind input to plan")
	}
}

func TestCurrentRulesetChangeInvalidatesDynamicPromptIdentity(t *testing.T) {
	t.Parallel()
	mat := classifier.DefaultPromptPlanMaterial()
	in1 := classifier.ClassifierInput{SessionID: "s", RulesetID: "r", RulesetHash: "h1"}
	in2 := classifier.ClassifierInput{SessionID: "s", RulesetID: "r", RulesetHash: "h2"}
	p1, err := classifier.BuildPromptPlan(mat, in1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := classifier.BuildPromptPlan(mat, in2)
	if err != nil {
		t.Fatal(err)
	}
	if p1.StablePrefixHash != p2.StablePrefixHash {
		t.Fatal("stable must be preserved when only current ruleset hash changes")
	}
	if p1.InputHash == p2.InputHash || p1.PromptHash == p2.PromptHash {
		t.Fatal("dynamic identity must change")
	}
}

func TestPromptPlan_ClassifierPolicyChangeInvalidatesStable(t *testing.T) {
	t.Parallel()
	mat1 := classifier.DefaultPromptPlanMaterial()
	mat2 := mat1
	mat2.RulesetHash = "other-classifier-policy"
	in := classifier.ClassifierInput{SessionID: "s", RulesetHash: "task-h"}
	p1, _ := classifier.BuildPromptPlan(mat1, in)
	p2, _ := classifier.BuildPromptPlan(mat2, in)
	if p1.StablePrefixHash == p2.StablePrefixHash {
		t.Fatal("classifier policy ruleset must change stable hash")
	}
}

func TestOpenAICompatible_CanonicalEndpointPath(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}"}}]}`)
	}))
	defer srv.Close()
	// Origin-only base_url + default path.
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, AllowRemote: true, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.Endpoint(), "/v1/chat/completions") {
		t.Fatalf("endpoint=%s", p.Endpoint())
	}
	if strings.Contains(p.Endpoint(), "/v1/v1/") {
		t.Fatal("double v1 path")
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	if _, err := p.Assess(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("got path %q", gotPath)
	}
}

func TestOpenAICompatible_RejectsBaseURLPath(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "http://127.0.0.1:11434/v1", Path: "/v1/chat/completions", AllowRemote: true,
	})
	if err == nil {
		t.Fatal("base_url with path must fail")
	}
}

func TestOpenAICompatible_RedirectDeniedEvenWithPermissiveClient(t *testing.T) {
	t.Parallel()
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}"}}]}`)
	}))
	defer final.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL+"/v1/chat/completions", http.StatusFound)
	}))
	defer redir.Close()
	// Permissive client that would follow redirects if not wrapped.
	perm := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return nil }}
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: redir.URL, Path: "/v1/chat/completions", AllowRemote: true, HTTPClient: perm,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	_, err = p.Assess(context.Background(), req)
	// With ErrUseLastResponse, 302 body is not a valid assessment envelope → parse/http error, not success.
	if err == nil {
		t.Fatal("redirect must not silently succeed as assessment")
	}
}

func TestFactory_EmptyNoneIsFake(t *testing.T) {
	t.Parallel()
	p, err := classifier.NewClassifierProviderFromConfig(config.ClassifierProviderConfig{}, classifier.ProviderFactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(classifier.FakeClassifierProvider); !ok {
		t.Fatalf("want Fake got %T", p)
	}
	p2, err := classifier.NewClassifierProviderFromConfig(config.ClassifierProviderConfig{Kind: "none"}, classifier.ProviderFactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p2.(classifier.FakeClassifierProvider); !ok {
		t.Fatalf("want Fake got %T", p2)
	}
}

func TestFactory_OpenAICompatibleLocal(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":2,\"reason_code\":\"NORMAL_PROGRESS\"}"}}]}`)
	}))
	defer srv.Close()
	p, err := classifier.NewClassifierProviderFromConfig(config.ClassifierProviderConfig{
		Kind: "openai_compatible", Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions",
		APIKeyRef: "${TEST_KEY}", CapabilitiesProfile: "generic-none-v1",
	}, classifier.ProviderFactoryOptions{
		AllowRemote: true,
		HTTPClient:  srv.Client(),
		LookupEnv:   func(k string) (string, bool) { return "k", true },
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	res, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Assessment.Severity != 2 {
		t.Fatalf("%+v", res.Assessment)
	}
}

func TestAuditIncludesModelVersionAndReasoning(t *testing.T) {
	t.Parallel()
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	res := classifier.ProviderResult{
		Assessment: classifier.RawAssessment{Severity: 1, ReasonCode: "NORMAL_PROGRESS"},
		Usage: classifier.ProviderUsage{
			UsagePresent: true, InputTokens: 1, OutputTokens: 2, ReasoningTokens: 3, CacheKeyHash: "ck",
		},
		Meta: classifier.ProviderMeta{Provider: "fake", ModelID: "m", ModelVersion: "v9"},
	}
	a := classifier.BuildProviderCallAudit(req, res, "", "", time.Time{})
	b, err := a.AuditJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "reasoning_tokens") || !strings.Contains(s, "model_version") || !strings.Contains(s, "cache_key_hash") {
		t.Fatal(s)
	}
	if !strings.Contains(s, `"usage_present":true`) {
		t.Fatal(s)
	}
}
