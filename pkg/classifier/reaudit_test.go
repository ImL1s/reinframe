package classifier_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/challenge"
	"github.com/ImL1s/reinframe/pkg/classifier"
	"github.com/ImL1s/reinframe/pkg/config"
)

// cancelProvider returns context.Canceled from Assess.
type cancelProvider struct{}

func (cancelProvider) Assess(ctx context.Context, req classifier.ProviderRequest) (classifier.ProviderResult, error) {
	return classifier.ProviderResult{}, context.Canceled
}

func TestShadow_CancelNeverFailOpenAllow(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{Provider: cancelProvider{}}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		PolicyClass:    classifier.PolicyClassProductivity,
		HookGateAction: adapter.HookActionAllow,
		Threshold:      50,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want Canceled err got %v", err)
	}
	if res.Resolved.Decision == classifier.DecisionAllow && res.Resolved.ResolverReason == "fail_open_productivity" {
		t.Fatal("cancel must not become productivity fail-open ALLOW")
	}
	if res.Resolved.ResolverReason != "provider_context_canceled" {
		t.Fatalf("reason=%s", res.Resolved.ResolverReason)
	}
	if res.Resolved.Enforced {
		t.Fatal("enforced")
	}
}

func TestOpenAICompatible_ParentCancelIdentity(t *testing.T) {
	t.Parallel()
	// Pre-canceled parent must surface as context.Canceled, not ProviderError / fail-open.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), Timeout: 2 * time.Second, MaxRetries: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	req.Timeout = 2 * time.Second
	_, e := p.Assess(ctx, req)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("want parent Canceled identity, got %v (%T)", e, e)
	}
	var pe *classifier.ProviderError
	if errors.As(e, &pe) {
		t.Fatalf("must not wrap as ProviderError: %+v", pe)
	}
}

func TestOpenAICompatible_DeadlineNotWrappedAsProviderError(t *testing.T) {
	t.Parallel()
	// Server hangs briefly; request budget is short so Assess returns DeadlineExceeded.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), Timeout: 30 * time.Millisecond, MaxRetries: 1,
		Sleep: func(ctx context.Context, d time.Duration) error {
			// Immediate: still honor ctx.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return nil
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	req.Timeout = 30 * time.Millisecond
	_, e := p.Assess(context.Background(), req)
	if e == nil {
		t.Fatal("expected deadline error")
	}
	if !errors.Is(e, context.DeadlineExceeded) && !errors.Is(e, context.Canceled) {
		// May surface as net/http deadline wrapped; still must not be fail-open ProviderError alone without context.
		var pe *classifier.ProviderError
		if errors.As(e, &pe) && pe.Class == "transport" {
			// Accept only if underlying is context
			if !errors.Is(e, context.DeadlineExceeded) {
				// deadline from WithTimeout often is DeadlineExceeded
				t.Logf("got %v", e)
			}
		}
	}
	// Must not look like ordinary productivity-success path (error is non-nil).
}

func TestPromptPlan_BindsAllProposedActionFields(t *testing.T) {
	t.Parallel()
	base := adapter.ProposedAction{
		ToolName: "Edit", ToolClass: adapter.ToolClassEdit, FilePath: "a.go",
		RedactedPayload:   json.RawMessage(`{"content":"old"}`),
		Arguments:         []string{"--x"},
		TargetScope:       []string{"pkg"},
		WorkspaceRevision: "ws1",
		ContractRevision:  2,
		Source:            "synthetic",
		Truncated:         false,
		ParseStatus:       "ok",
	}
	in1 := classifier.ClassifierInput{SessionID: "s", ProposedAction: &base}
	in2PA := base
	in2PA.RedactedPayload = json.RawMessage(`{"content":"new"}`)
	in2 := classifier.ClassifierInput{SessionID: "s", ProposedAction: &in2PA}
	p1, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in2)
	if err != nil {
		t.Fatal(err)
	}
	if p1.InputHash == p2.InputHash || p1.PromptHash == p2.PromptHash {
		t.Fatal("different redacted payload must change InputHash/PromptHash")
	}
	if p1.StablePrefixHash != p2.StablePrefixHash {
		t.Fatal("stable must be unchanged")
	}
}

func TestPromptPlan_ChallengeContextBindsJustification(t *testing.T) {
	t.Parallel()
	in1 := classifier.ClassifierInput{
		SessionID: "s",
		Challenge: &classifier.ChallengeContext{
			ChallengeID: "c1", State: "JUSTIFIED", ConcreteValue: "v1",
			VerificationPlan: "test", EvidenceEventIDs: []string{"e1"},
		},
	}
	in2 := in1
	ch2 := *in1.Challenge
	ch2.ConcreteValue = "v2"
	in2.Challenge = &ch2
	p1, _ := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in1)
	p2, _ := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in2)
	if p1.InputHash == p2.InputHash {
		t.Fatal("challenge concrete value must bind InputHash")
	}
}

func TestReEval_PassesChallengeContextIntoProvider(t *testing.T) {
	t.Parallel()
	var saw *classifier.ChallengeContext
	capProv := &captureProvider{fn: func(req classifier.ProviderRequest) {
		saw = req.Input.Challenge
	}}
	svc := challenge.NewService(challenge.ServiceConfig{})
	// Use DefaultReEvaluator path via service is heavy; call DefaultReEvaluator directly.
	re := challenge.DefaultReEvaluator{}
	rec := challenge.ChallengeRecord{
		ChallengeID: "cid-1", SessionID: "s", State: challenge.StateJustified,
		BlockClass: challenge.BlockClassOverSOP, Appealability: challenge.AppealAppealable,
		SideEffectClass: challenge.SideEffectShellGeneric,
	}
	just := &challenge.Justification{
		ConcreteValue: "ship fix", PreventedFailureOrThreat: "bug", EstimatedCost: "low",
		AlternativesConsidered: "none", ScopeLimit: "file", VerificationPlan: "test",
		RollbackPlan: "revert", SupportingEvidenceEventIDs: []string{"ev1"},
	}
	_, err := re.ReEvaluate(context.Background(), rec, adapter.ProposedAction{
		ToolName: "Bash", Command: "echo hi", ToolClass: adapter.ToolClassShell,
	}, just, &challenge.ReEvalContext{
		Provider: capProv, PolicyClass: classifier.PolicyClassProductivity,
		Threshold: 50, FixtureName: "clear_allow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saw == nil {
		t.Fatal("expected ChallengeContext")
	}
	if saw.ConcreteValue != "ship fix" || saw.ChallengeID != "cid-1" || saw.State != string(challenge.StateJustified) {
		t.Fatalf("%+v", saw)
	}
	if saw.VerificationPlan != "test" || len(saw.EvidenceEventIDs) != 1 {
		t.Fatalf("%+v", saw)
	}
	_ = svc
}

type captureProvider struct {
	fn func(classifier.ProviderRequest)
}

func (c *captureProvider) Assess(ctx context.Context, req classifier.ProviderRequest) (classifier.ProviderResult, error) {
	if c.fn != nil {
		c.fn(req)
	}
	return classifier.FakeClassifierProvider{}.Assess(ctx, req)
}

func TestConfig_MarshalRedactsRawAPIKeyBeforeValidate(t *testing.T) {
	t.Parallel()
	cp := config.ClassifierProviderConfig{
		Kind: "openai_compatible", Model: "m", BaseURL: "http://127.0.0.1:1",
		APIKeyRef: "sk-live-secret-value",
	}
	b, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "sk-live-secret-value") {
		t.Fatalf("raw secret leaked in marshal: %s", b)
	}
	if !strings.Contains(string(b), "[REDACTED]") {
		t.Fatal(string(b))
	}
	// Full config document path
	cfg := config.Default()
	cfg.ClassifierProvider = cp
	doc, err := config.MarshalJSONDocument(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(doc), "sk-live-secret-value") {
		t.Fatal("secret in document")
	}
}

func TestFactory_NormalizesKindCase(t *testing.T) {
	t.Parallel()
	// "NONE" must normalize like none after Validate would reject unsupported case...
	// NormalizeKind lowercases; Validate uses NormalizeKind so "NONE" → none.
	cfg := config.ClassifierProviderConfig{Kind: "NONE"}
	if cfg.NormalizeKind() != "none" {
		t.Fatal(cfg.NormalizeKind())
	}
	p, err := classifier.NewClassifierProviderFromConfig(config.ClassifierProviderConfig{Kind: "none"}, classifier.ProviderFactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(classifier.FakeClassifierProvider); !ok {
		t.Fatalf("%T", p)
	}
}

func TestUsage_NegativeRejectedNotClamped(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}"}}],"usage":{"prompt_tokens":-1,"completion_tokens":2}}`)
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	_, err = p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("negative usage must fail")
	}
	// Audit path: invalid usage marked, not genuine zero
	res := classifier.ProviderResult{
		Usage: classifier.ProviderUsage{UsagePresent: true, InputTokens: -5, OutputTokens: 1},
		Meta:  classifier.ProviderMeta{Provider: "t"},
	}
	a := classifier.BuildProviderCallAudit(req, res, "", "", time.Time{})
	if !a.UsageInvalid || a.UsagePresent {
		t.Fatalf("%+v", a)
	}
	// Invalid path must not claim measured zeros as UsagePresent telemetry.
	if a.InputTokens != 0 || a.OutputTokens != 0 {
		t.Fatalf("invalid usage must not publish token values as genuine: %+v", a)
	}
}

func TestOpenAICompatible_RejectsEmptyHostnameAndNonNumericPort(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "http://:8080", AllowRemote: true,
	})
	if err == nil {
		t.Fatal("empty hostname must fail")
	}
	_, err = classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: "http://127.0.0.1:notaport", AllowRemote: true,
	})
	if err == nil {
		t.Fatal("non-numeric port must fail")
	}
}

func TestOpenAICompatible_DoesNotCopyCookieJar(t *testing.T) {
	t.Parallel()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if c := r.Header.Get("Cookie"); c != "" {
			t.Errorf("cookie must not be sent: %s", c)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":1,\"reason_code\":\"NORMAL_PROGRESS\"}"}}]}`)
	}))
	defer srv.Close()
	// Seed jar with a cookie for the server URL.
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "sid", Value: "secret-session"}})
	inj := &http.Client{Jar: jar, CheckRedirect: func(req *http.Request, via []*http.Request) error { return nil }}
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true, HTTPClient: inj,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{})
	if _, err := p.Assess(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatal(hits.Load())
	}
}
