package classifier_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/challenge"
	"github.com/ImL1s/reinframe/pkg/classifier"
	"github.com/ImL1s/reinframe/pkg/config"
)

// --- P1-A: sleep failure never yields (ProviderResult{}, nil) ---

func TestOpenAICompatible_ParentCancelDuringBackoff(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	parent, cancel := context.WithCancel(context.Background())
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), Timeout: 2 * time.Second, MaxRetries: 2,
		Sleep: func(ctx context.Context, d time.Duration) error {
			cancel() // parent cancel during backoff
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
	})
	req.Timeout = 2 * time.Second
	_, e := p.Assess(parent, req)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("want parent Canceled during backoff, got %v", e)
	}
	var pe *classifier.ProviderError
	if errors.As(e, &pe) {
		t.Fatalf("must not wrap parent cancel: %+v", pe)
	}
}

func TestOpenAICompatible_NoEmptyNilAfterFailedAttempt(t *testing.T) {
	t.Parallel()
	// Multiple failure modes must never yield (ProviderResult{}, nil).
	cases := []struct {
		name  string
		sleep func(context.Context, time.Duration) error
		code  int
	}{
		{"sleep_err", func(ctx context.Context, d time.Duration) error { return errors.New("boom") }, 429},
		{"http_500", nil, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
			}))
			defer srv.Close()
			cfg := classifier.OpenAICompatibleConfig{
				Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
				HTTPClient: srv.Client(), Timeout: time.Second, MaxRetries: 1,
			}
			if tc.sleep != nil {
				cfg.Sleep = tc.sleep
			}
			p, err := classifier.NewOpenAICompatible(cfg)
			if err != nil {
				t.Fatal(err)
			}
			req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
				SchemaVersion: classifier.SchemaClassifierInput,
			})
			req.Timeout = time.Second
			res, e := p.Assess(context.Background(), req)
			if e == nil {
				t.Fatalf("nil error after failure: res=%+v", res)
			}
		})
	}
}

func TestOpenAICompatible_SleepErrorNeverNilSuccess(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusTooManyRequests)
		w.Header().Set("Retry-After", "1")
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), Timeout: 2 * time.Second, MaxRetries: 2,
		Sleep: func(ctx context.Context, d time.Duration) error {
			return errors.New("injected sleep failure")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Timeout = 2 * time.Second
	res, e := p.Assess(context.Background(), req)
	if e == nil {
		t.Fatalf("sleep failure must not yield nil error; res=%+v", res)
	}
	var pe *classifier.ProviderError
	if !errors.As(e, &pe) {
		t.Fatalf("want ProviderError, got %T %v", e, e)
	}
	if pe.Class != "transport" {
		t.Fatalf("class=%s", pe.Class)
	}
}

func TestOpenAICompatible_AdapterTimeoutTypedNotParentDeadline(t *testing.T) {
	t.Parallel()
	// Hang until client opCtx times out; parent stays live.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), Timeout: 25 * time.Millisecond, MaxRetries: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
	})
	req.Timeout = 25 * time.Millisecond
	_, e := p.Assess(context.Background(), req)
	if e == nil {
		t.Fatal("expected timeout")
	}
	// Must be typed adapter timeout — not raw parent DeadlineExceeded identity.
	if errors.Is(e, context.DeadlineExceeded) {
		// ProviderError must not unwrap to DeadlineExceeded for policy matrix.
		var pe *classifier.ProviderError
		if errors.As(e, &pe) && pe.Class == "timeout" {
			return // preferred: typed timeout
		}
		// If raw deadline leaks, fail — shadow would treat as cancel.
		t.Fatalf("adapter timeout must be typed ProviderError timeout, got %v (%T)", e, e)
	}
	var pe *classifier.ProviderError
	if !errors.As(e, &pe) || pe.Class != "timeout" {
		t.Fatalf("want ProviderError class=timeout, got %v (%T)", e, e)
	}
}

func TestOpenAICompatible_ParentCancelNotTypedTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), Timeout: 2 * time.Second, MaxRetries: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
	})
	req.Timeout = 2 * time.Second
	_, e := p.Assess(ctx, req)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("want raw Canceled, got %v", e)
	}
	var pe *classifier.ProviderError
	if errors.As(e, &pe) {
		t.Fatalf("parent cancel must not be ProviderError: %+v", pe)
	}
}

// --- P1-B: trajectory packet binds into prompt dynamic side ---

func TestPromptPlan_TrajectoryDigestBindsIdentity(t *testing.T) {
	t.Parallel()
	base := classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		SessionID:     "s",
		TaskAnchor: classifier.TaskAnchor{
			TaskID: "t1", Objective: "ship", Acceptance: []string{"tests pass"},
		},
		ContractRevision: 1,
		EvidenceRevision: 2,
		RecentEvents: []classifier.EventDigest{
			{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "ran go test", ContentHash: "h1"},
		},
		Window: classifier.WindowMeta{EventCount: 1, ByteCount: 40, Truncated: false},
	}
	in2 := base
	in2.RecentEvents = []classifier.EventDigest{
		{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "ran go test -race", ContentHash: "h2"},
	}
	// Window must stay valid for ValidateClassifierInput via BuildPromptPlan? BuildPromptPlan doesn't validate.
	p1, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), base)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in2)
	if err != nil {
		t.Fatal(err)
	}
	if p1.InputHash == p2.InputHash {
		t.Fatal("event summary change must rebind InputHash")
	}
	// Task anchor objective binds.
	in3 := base
	in3.TaskAnchor.Objective = "other"
	p3, _ := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in3)
	if p1.InputHash == p3.InputHash {
		t.Fatal("task objective must bind")
	}
	// Contract revision binds.
	in4 := base
	in4.ContractRevision = 9
	p4, _ := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in4)
	if p1.InputHash == p4.InputHash {
		t.Fatal("contract revision must bind")
	}
}

func TestNewProviderRequest_BoundsTrajectoryAndSyncsIDs(t *testing.T) {
	t.Parallel()
	events := make([]classifier.EventDigest, 0, 50)
	for i := 0; i < 50; i++ {
		events = append(events, classifier.EventDigest{
			EventID: fmt.Sprintf("ev-%d", i), Sequence: uint64(i), EventType: "observation",
			Summary: strings.Repeat("x", 20),
		})
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		RecentEvents:  events,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Input.RecentEvents) > classifier.MaxRecentEvents {
		t.Fatalf("recent not bounded: %d", len(req.Input.RecentEvents))
	}
	if !req.Input.Window.Truncated {
		t.Fatal("expected truncated window")
	}
	if len(req.Input.RecentEventIDs) != len(req.Input.RecentEvents) {
		t.Fatal("legacy IDs must sync to digests shown")
	}
}

// --- ChallengeContext closed fields bind ---

func TestPromptPlan_ChallengeFullFieldsBind(t *testing.T) {
	t.Parallel()
	ch := classifier.ChallengeContext{
		ChallengeID: "c1", State: "JUSTIFIED", BlockClass: "OVER_SOP",
		ReasonCode: "above_threshold", Appealability: "APPEALABLE",
		RequiredClaims: []string{"concrete_value"}, RetryBudget: 2,
		ExpiresAtSequence: 10, OriginalActionID: "a1", ActionFingerprint: "fp",
		ConcreteValue: "v", VerificationPlan: "test", Claims: []string{"concrete_value"},
	}
	in1 := classifier.ClassifierInput{SchemaVersion: classifier.SchemaClassifierInput, Challenge: &ch}
	in2 := in1
	ch2 := ch
	ch2.RequiredClaims = []string{"verification_plan"}
	in2.Challenge = &ch2
	p1, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := classifier.BuildPromptPlan(classifier.DefaultPromptPlanMaterial(), in2)
	if err != nil {
		t.Fatal(err)
	}
	if p1.InputHash == p2.InputHash {
		t.Fatal("RequiredClaims must bind InputHash")
	}
}

// --- P2-C/D: shadow matrix + audit observer ---

type memObserver struct {
	mu     sync.Mutex
	audits []classifier.ProviderCallAudit
}

func (m *memObserver) RecordProviderCall(ctx context.Context, a classifier.ProviderCallAudit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audits = append(m.audits, a)
	return nil
}

type timeoutProvider struct{}

func (timeoutProvider) Assess(ctx context.Context, req classifier.ProviderRequest) (classifier.ProviderResult, error) {
	return classifier.ProviderResult{
		SchemaVersion: classifier.SchemaProviderResult,
		Meta:          classifier.ProviderMeta{Provider: "fake", ErrorClass: "timeout", ParseStatus: classifier.ParseStatusError},
	}, &classifier.ProviderError{Class: "timeout", Message: "provider request timeout", Retryable: false}
}

func TestShadow_AdapterTimeoutFailOpenProductivity(t *testing.T) {
	t.Parallel()
	obs := &memObserver{}
	s := &classifier.ShadowClassifier{Provider: timeoutProvider{}, Observer: obs}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		PolicyClass:    classifier.PolicyClassProductivity,
		HookGateAction: adapter.HookActionAllow,
		Threshold:      50,
	})
	if err != nil {
		t.Fatalf("adapter timeout must not return parent-cancel err: %v", err)
	}
	if res.Resolved.Decision != classifier.DecisionAllow || res.Resolved.ResolverReason != "fail_open_productivity" {
		t.Fatalf("got decision=%s reason=%s", res.Resolved.Decision, res.Resolved.ResolverReason)
	}
	if res.ProviderCall == nil || res.ProviderCall.ErrorClass != "timeout" {
		t.Fatalf("audit must retain timeout class: %+v", res.ProviderCall)
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.audits) != 1 {
		t.Fatalf("observer audits=%d", len(obs.audits))
	}
}

func TestShadow_AdapterTimeoutFailClosedSecurity(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{Provider: timeoutProvider{}}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		PolicyClass: classifier.PolicyClassSecurity,
		Threshold:   50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Resolved.Decision != classifier.DecisionBlock || res.Resolved.ResolverReason != "fail_closed_security" {
		t.Fatalf("got decision=%s reason=%s", res.Resolved.Decision, res.Resolved.ResolverReason)
	}
}

func TestShadow_FakeSuccessRetainsUsageMetaAudit(t *testing.T) {
	t.Parallel()
	obs := &memObserver{}
	s := &classifier.ShadowClassifier{
		Provider: classifier.FakeClassifierProvider{ProviderKind: "fake"},
		Observer: obs,
	}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		FixtureName:    "clear_allow",
		PolicyClass:    classifier.PolicyClassProductivity,
		HookGateAction: adapter.HookActionAllow,
		Threshold:      50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.LastProviderResult == nil || res.LastProviderResult.SchemaVersion != classifier.SchemaProviderResult {
		t.Fatalf("must retain versioned ProviderResult: %+v", res.LastProviderResult)
	}
	if res.ProviderCall == nil || res.ProviderCall.SchemaVersion != classifier.SchemaProviderCallAudit {
		t.Fatalf("must surface ProviderCallAudit: %+v", res.ProviderCall)
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	if len(obs.audits) != 1 {
		t.Fatalf("observer not called")
	}
}

func TestReEval_AdapterTimeoutPolicyMatrix(t *testing.T) {
	t.Parallel()
	re := challenge.DefaultReEvaluator{}
	rec := challenge.ChallengeRecord{
		ChallengeID: "c", SessionID: "s", State: challenge.StateJustified,
		BlockClass: challenge.BlockClassOverSOP, Appealability: challenge.AppealAppealable,
		SideEffectClass: challenge.SideEffectShellGeneric,
		RequiredClaims:  []string{"concrete_value"},
	}
	just := &challenge.Justification{ConcreteValue: "x", VerificationPlan: "t",
		PreventedFailureOrThreat: "b", EstimatedCost: "l", AlternativesConsidered: "n",
		ScopeLimit: "s", RollbackPlan: "r"}
	// Productivity fail-open on typed timeout
	out, err := re.ReEvaluate(context.Background(), rec, adapter.ProposedAction{
		ToolName: "Bash", Command: "echo", ToolClass: adapter.ToolClassShell,
	}, just, &challenge.ReEvalContext{
		Provider: timeoutProvider{}, PolicyClass: classifier.PolicyClassProductivity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Stage2Decision != challenge.DecisionAllow || out.Reason != "provider_fail_open" {
		t.Fatalf("productivity timeout: %+v", out)
	}
	// Security fail-closed
	out2, err := re.ReEvaluate(context.Background(), rec, adapter.ProposedAction{
		ToolName: "Bash", Command: "echo", ToolClass: adapter.ToolClassShell,
	}, just, &challenge.ReEvalContext{
		Provider: timeoutProvider{}, PolicyClass: classifier.PolicyClassSecurity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out2.Stage2Decision != challenge.DecisionBlock || out2.Reason != "provider_fail_closed" {
		t.Fatalf("security timeout: %+v", out2)
	}
}

func TestReEval_ParentCancelNeverFailOpen(t *testing.T) {
	t.Parallel()
	re := challenge.DefaultReEvaluator{}
	rec := challenge.ChallengeRecord{
		ChallengeID: "c", SessionID: "s", State: challenge.StateJustified,
		BlockClass: challenge.BlockClassOverSOP, Appealability: challenge.AppealAppealable,
		SideEffectClass: challenge.SideEffectShellGeneric,
	}
	_, err := re.ReEvaluate(context.Background(), rec, adapter.ProposedAction{
		ToolName: "Bash", Command: "echo", ToolClass: adapter.ToolClassShell,
	}, &challenge.Justification{ConcreteValue: "x", VerificationPlan: "t",
		PreventedFailureOrThreat: "b", EstimatedCost: "l", AlternativesConsidered: "n",
		ScopeLimit: "s", RollbackPlan: "r"}, &challenge.ReEvalContext{
		Provider: cancelProvider{}, PolicyClass: classifier.PolicyClassProductivity,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}
}

// --- P2-E: versioned contracts ---

func TestValidateProviderResult_RequiresSchema(t *testing.T) {
	t.Parallel()
	if err := classifier.ValidateProviderResult(classifier.ProviderResult{}); err == nil {
		t.Fatal("empty schema must fail")
	}
	if err := classifier.ValidateProviderResult(classifier.ProviderResult{
		SchemaVersion: classifier.SchemaProviderResult,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFake_ReturnsVersionedProviderResult(t *testing.T) {
	t.Parallel()
	f := classifier.FakeClassifierProvider{}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		FixtureName:   "clear_allow",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.SchemaVersion != classifier.SchemaProviderResult {
		t.Fatalf("schema=%s", res.SchemaVersion)
	}
	if err := classifier.ValidateProviderResult(res); err != nil {
		t.Fatal(err)
	}
}

// --- P2-F: secret-safe fmt/YAML ---

func TestClassifierProviderConfig_FmtSecretSafe(t *testing.T) {
	t.Parallel()
	cp := config.ClassifierProviderConfig{
		Kind: "openai_compatible", Model: "m",
		APIKeyRef: "sk-live-super-secret-value",
	}
	for _, s := range []string{cp.String(), fmt.Sprintf("%v", cp), fmt.Sprintf("%#v", cp), fmt.Sprintf("%+v", cp)} {
		if strings.Contains(s, "sk-live-super-secret-value") {
			t.Fatalf("secret leaked in diagnostics: %s", s)
		}
		if !strings.Contains(s, "[REDACTED]") {
			t.Fatalf("expected redaction marker: %s", s)
		}
	}
	// Env placeholder preserved.
	cp2 := config.ClassifierProviderConfig{APIKeyRef: "${OPENAI_API_KEY}"}
	if !strings.Contains(cp2.String(), "${OPENAI_API_KEY}") {
		t.Fatalf("placeholder must remain: %s", cp2.String())
	}
}

func TestValidateChallengeContext_ClosedEnums(t *testing.T) {
	t.Parallel()
	err := classifier.ValidateChallengeContext(classifier.ChallengeContext{State: "NOT_A_STATE"})
	if err == nil {
		t.Fatal("unknown state must fail")
	}
}

func TestValidateProposedAction_RejectsTruncated(t *testing.T) {
	t.Parallel()
	err := classifier.ValidateProposedActionForModel(adapter.ProposedAction{Truncated: true})
	if err == nil {
		t.Fatal("truncated must reject")
	}
}

func TestBoundTrajectory_ByteOverflowMarker(t *testing.T) {
	t.Parallel()
	// Large maxN, tiny maxB → pure byte overflow must mark "bytes", not "events".
	events := []classifier.EventDigest{
		{EventID: "a", Sequence: 1, EventType: "observation", Summary: strings.Repeat("Z", 200)},
		{EventID: "b", Sequence: 2, EventType: "observation", Summary: strings.Repeat("Y", 200)},
		{EventID: "c", Sequence: 3, EventType: "observation", Summary: strings.Repeat("X", 200)},
	}
	recent, related, win := classifier.BoundTrajectory(events, nil, 40, 250)
	if len(related) != 0 {
		t.Fatalf("related=%d", len(related))
	}
	if !win.Truncated {
		t.Fatal("expected truncated")
	}
	if win.OverflowMarker != classifier.OverflowBytes && win.OverflowMarker != classifier.OverflowEventsAndBytes {
		// Pure byte budget trip should be bytes (or events_and_bytes if count also hits).
		if win.OverflowMarker == classifier.OverflowEvents {
			t.Fatalf("byte overflow mislabeled as events: recent=%d win=%+v", len(recent), win)
		}
	}
	if win.OverflowMarker != classifier.OverflowBytes {
		// Prefer exact bytes when only size budget fires.
		if len(recent) < 3 && win.ByteCount <= 250 && win.OverflowMarker == classifier.OverflowEvents {
			t.Fatalf("want OverflowBytes, got %q win=%+v", win.OverflowMarker, win)
		}
	}
	if win.OverflowMarker != classifier.OverflowBytes {
		t.Fatalf("want OverflowBytes, got %q (recent=%d byte=%d)", win.OverflowMarker, len(recent), win.ByteCount)
	}
}

func TestShadow_TrajectoryPlumbedToProvider(t *testing.T) {
	t.Parallel()
	var saw classifier.ProviderRequest
	prov := &captureProvider{fn: func(req classifier.ProviderRequest) {
		saw = req
	}}
	s := &classifier.ShadowClassifier{Provider: prov}
	_, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		SessionID:        "s1",
		TaskAnchor:       classifier.TaskAnchor{TaskID: "t1", Objective: "ship feature"},
		ContractRevision: 3,
		EvidenceRevision: 4,
		RecentEvents: []classifier.EventDigest{
			{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "edit file"},
		},
		FixtureName: "clear_allow",
		Threshold:   50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if saw.Input.TaskAnchor.Objective != "ship feature" {
		t.Fatalf("task anchor not plumbed: %+v", saw.Input.TaskAnchor)
	}
	if saw.Input.ContractRevision != 3 || saw.Input.EvidenceRevision != 4 {
		t.Fatalf("revisions not plumbed: %d %d", saw.Input.ContractRevision, saw.Input.EvidenceRevision)
	}
	if len(saw.Input.RecentEvents) != 1 || saw.Input.RecentEvents[0].EventID != "e1" {
		t.Fatalf("digests not plumbed: %+v", saw.Input.RecentEvents)
	}
	planText := ""
	for _, b := range saw.Prompt.DynamicSuffix {
		planText += b.Text
	}
	if !strings.Contains(planText, "edit file") {
		t.Fatalf("prompt missing digest summary: %s", planText)
	}
	if !strings.Contains(planText, "ship feature") {
		t.Fatalf("prompt missing task objective: %s", planText)
	}
}

func TestReEval_TrajectoryPlumbed(t *testing.T) {
	t.Parallel()
	var sawObj string
	var digests int
	var auditSeen bool
	prov := &captureProvider{fn: func(req classifier.ProviderRequest) {
		sawObj = req.Input.TaskAnchor.Objective
		digests = len(req.Input.RecentEvents)
	}}
	re := challenge.DefaultReEvaluator{}
	rec := challenge.ChallengeRecord{
		ChallengeID: "c", SessionID: "s", State: challenge.StateJustified,
		BlockClass: challenge.BlockClassOverSOP, Appealability: challenge.AppealAppealable,
		SideEffectClass: challenge.SideEffectShellGeneric,
	}
	out, err := re.ReEvaluate(context.Background(), rec, adapter.ProposedAction{
		ToolName: "Bash", Command: "echo", ToolClass: adapter.ToolClassShell,
	}, &challenge.Justification{ConcreteValue: "x", VerificationPlan: "t",
		PreventedFailureOrThreat: "b", EstimatedCost: "l", AlternativesConsidered: "n",
		ScopeLimit: "s", RollbackPlan: "r"}, &challenge.ReEvalContext{
		Provider: prov, PolicyClass: classifier.PolicyClassProductivity,
		TaskAnchor: classifier.TaskAnchor{TaskID: "t", Objective: "fix bug"},
		RecentEvents: []classifier.EventDigest{
			{EventID: "e9", Sequence: 9, EventType: "error", Summary: "panic"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawObj != "fix bug" || digests != 1 {
		t.Fatalf("trajectory not plumbed: obj=%q digests=%d", sawObj, digests)
	}
	if out.ProviderCall == nil {
		t.Fatal("ReEvalResult must retain ProviderCall audit")
	}
	_ = auditSeen
}
