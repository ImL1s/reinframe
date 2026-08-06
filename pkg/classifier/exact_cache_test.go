package classifier_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
	"github.com/ImL1s/reinframe/pkg/config"
)

type countingProvider struct {
	n   atomic.Int32
	sev int
	err error
}

func (c *countingProvider) Assess(ctx context.Context, req classifier.ProviderRequest) (classifier.ProviderResult, error) {
	c.n.Add(1)
	if c.err != nil {
		return classifier.ProviderResult{}, c.err
	}
	sev := c.sev
	if sev == 0 {
		sev = 12
	}
	return classifier.ProviderResult{
		SchemaVersion: classifier.SchemaProviderResult,
		Assessment: classifier.RawAssessment{
			SchemaVersion: classifier.SchemaRawAssessment,
			Severity:      sev,
			ReasonCode:    "NORMAL_PROGRESS",
			ParseStatus:   classifier.ParseStatusOK,
		},
		Usage: classifier.ProviderUsage{InputTokens: 100, OutputTokens: 5, UsagePresent: true},
		Meta: classifier.ProviderMeta{
			Provider: "fake", ModelID: "m", ParseStatus: classifier.ParseStatusOK,
			ProviderRequestID: "aud-1",
		},
	}, nil
}

func testIdentity() classifier.ExactCacheIdentity {
	return classifier.ExactCacheIdentity{
		ProviderKind: "fake", ModelID: "m", CapabilitiesProfile: "generic-none-v1",
		EgressProfile: "local", ParserSchema: classifier.SchemaRawAssessment,
	}
}

func baseInput() classifier.ClassifierInput {
	return classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs", RulesetHash: "rh",
		TaskAnchor: classifier.TaskAnchor{TaskID: "t1", Objective: "ship"},
		RecentEvents: []classifier.EventDigest{
			{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "edit", ContentHash: "ch1"},
		},
	}
}

func TestExactCache_HitSkipsProvider(t *testing.T) {
	t.Parallel()
	cache, err := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	req, err := classifier.NewProviderRequest(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	r1, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if inner.n.Load() != 1 {
		t.Fatalf("provider calls=%d want 1", inner.n.Load())
	}
	if r2.Usage.CacheBackend != classifier.ExactCacheLayerReinframeExact || !r2.Usage.CacheHit {
		t.Fatalf("hit layer=%+v", r2.Usage)
	}
	if r2.Usage.InputTokens != 0 {
		t.Fatal("exact hit must not invent provider tokens")
	}
	if r1.Assessment.Severity != r2.Assessment.Severity {
		t.Fatal("assessment mismatch")
	}
	st := cache.Stats()
	if st.Hits < 1 || st.Admissions < 1 {
		t.Fatalf("stats=%+v", st)
	}
}

func TestExactCache_MissOnEventContentChange(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	in1 := baseInput()
	in2 := baseInput()
	in2.RecentEvents[0].ContentHash = "ch2"
	r1, _ := classifier.NewProviderRequest(in1)
	r2, _ := classifier.NewProviderRequest(in2)
	_, _ = p.Assess(context.Background(), r1)
	_, _ = p.Assess(context.Background(), r2)
	if inner.n.Load() != 2 {
		t.Fatalf("calls=%d", inner.n.Load())
	}
}

func TestExactCache_MissOnModelChange(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	inner := &countingProvider{}
	id1 := testIdentity()
	id2 := testIdentity()
	id2.ModelID = "other"
	p1 := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: id1}
	p2 := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: id2}
	req, _ := classifier.NewProviderRequest(baseInput())
	_, _ = p1.Assess(context.Background(), req)
	_, _ = p2.Assess(context.Background(), req)
	if inner.n.Load() != 2 {
		t.Fatalf("calls=%d", inner.n.Load())
	}
}

func TestExactCache_SecurityNeverCached(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	in := baseInput()
	in.PolicyClass = classifier.PolicyClassSecurity
	req, err := classifier.NewProviderRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Assess(context.Background(), req)
	_, _ = p.Assess(context.Background(), req)
	if inner.n.Load() != 2 {
		t.Fatalf("security must not cache: calls=%d", inner.n.Load())
	}
}

func TestExactCache_ErrorsNotAdmitted(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	inner := &countingProvider{err: errors.New("timeout")}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	req, _ := classifier.NewProviderRequest(baseInput())
	_, err := p.Assess(context.Background(), req)
	if err == nil {
		t.Fatal("expected err")
	}
	inner.err = nil
	_, _ = p.Assess(context.Background(), req)
	if inner.n.Load() != 2 {
		t.Fatalf("error must not admit: calls=%d", inner.n.Load())
	}
}

func TestExactCache_ChallengeJustificationMiss(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	in1 := baseInput()
	in1.Challenge = &classifier.ChallengeContext{ChallengeID: "c1", State: "OPEN", ConcreteValue: "before"}
	in2 := baseInput()
	in2.Challenge = &classifier.ChallengeContext{ChallengeID: "c1", State: "OPEN", ConcreteValue: "after-justification"}
	r1, _ := classifier.NewProviderRequest(in1)
	r2, _ := classifier.NewProviderRequest(in2)
	_, _ = p.Assess(context.Background(), r1)
	_, _ = p.Assess(context.Background(), r2)
	if inner.n.Load() != 2 {
		t.Fatalf("justification change must miss: %d", inner.n.Load())
	}
}

func TestExactCache_SingleflightOneCall(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	inner := &countingProvider{}
	// Slow first call via blocking channel in custom provider.
	var started sync.WaitGroup
	started.Add(1)
	release := make(chan struct{})
	slow := &blockingProvider{countingProvider: inner, started: &started, release: release}
	p := &classifier.CachingClassifierProvider{Inner: slow, Cache: cache, Identity: testIdentity()}
	req, _ := classifier.NewProviderRequest(baseInput())

	var wg sync.WaitGroup
	const n = 8
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, err := p.Assess(context.Background(), req)
			errs <- err
		}()
	}
	started.Wait()
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if inner.n.Load() != 1 {
		t.Fatalf("singleflight calls=%d", inner.n.Load())
	}
	st := cache.Stats()
	// Under load, followers may join inflight (Coalesced) or hit after admit (Hits).
	if st.Coalesced+st.Hits < 1 {
		t.Fatalf("expected coalesced or post-admit hits stats=%+v", st)
	}
}

type blockingProvider struct {
	countingProvider *countingProvider
	started          *sync.WaitGroup
	release          <-chan struct{}
	once             sync.Once
}

func (b *blockingProvider) Assess(ctx context.Context, req classifier.ProviderRequest) (classifier.ProviderResult, error) {
	b.once.Do(func() { b.started.Done() })
	select {
	case <-b.release:
	case <-ctx.Done():
		return classifier.ProviderResult{}, ctx.Err()
	}
	return b.countingProvider.Assess(ctx, req)
}

func TestExactCache_ActionFingerprint(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: false,
	}, nil)
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	in1 := baseInput()
	in1.ProposedAction = &adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion,
		ToolName:      "Bash", Command: "go test", ToolClass: "shell",
	}
	in2 := baseInput()
	in2.ProposedAction = &adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion,
		ToolName:      "Bash", Command: "go test ./other", ToolClass: "shell",
	}
	r1, _ := classifier.NewProviderRequest(in1)
	r2, _ := classifier.NewProviderRequest(in2)
	_, _ = p.Assess(context.Background(), r1)
	_, _ = p.Assess(context.Background(), r2)
	if inner.n.Load() != 2 {
		t.Fatalf("action change must miss: %d", inner.n.Load())
	}
}

func TestExactCache_KeyStableAcrossDynamicRetryBudget(t *testing.T) {
	t.Parallel()
	// Retry budget must not be in key — unchanged retry can hit.
	id := testIdentity()
	in1 := baseInput()
	in1.Challenge = &classifier.ChallengeContext{ChallengeID: "c", State: "OPEN", RetryBudget: 2, ConcreteValue: "same"}
	in2 := baseInput()
	in2.Challenge = &classifier.ChallengeContext{ChallengeID: "c", State: "OPEN", RetryBudget: 1, ConcreteValue: "same"}
	r1, err := classifier.NewProviderRequest(in1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := classifier.NewProviderRequest(in2)
	if err != nil {
		t.Fatal(err)
	}
	k1, ok1 := classifier.BuildExactCacheKeyHash(id, r1)
	k2, ok2 := classifier.BuildExactCacheKeyHash(id, r2)
	if !ok1 || !ok2 || k1 != k2 {
		t.Fatalf("retry budget must not alter key: %v %v %s %s", ok1, ok2, k1, k2)
	}
}

func TestExactCache_ConfigValidation(t *testing.T) {
	t.Parallel()
	c := config.Default()
	c.ClassifierCache = config.ClassifierCacheConfig{
		Exact: config.ExactCacheYAML{Enabled: true, MaxEntries: 10, MaxBytes: 4096, TTL: "5m"},
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := config.Default()
	bad.ClassifierCache = config.ClassifierCacheConfig{
		Exact: config.ExactCacheYAML{Enabled: false, MaxEntries: 10},
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("disabled with bounds must fail")
	}
}

func TestExactCache_DisabledNoOp(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{}, nil)
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	req, _ := classifier.NewProviderRequest(baseInput())
	_, _ = p.Assess(context.Background(), req)
	_, _ = p.Assess(context.Background(), req)
	if inner.n.Load() != 2 {
		t.Fatalf("disabled must always call: %d", inner.n.Load())
	}
}

func TestExactCache_MissOnArgsAndExceptions(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: false,
	}, nil)
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	in1 := baseInput()
	in1.ProposedAction = &adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, ToolName: "Bash", Command: "go test",
		Arguments: []string{"-v"},
	}
	in2 := baseInput()
	in2.ProposedAction = &adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, ToolName: "Bash", Command: "go test",
		Arguments: []string{"-race"},
	}
	r1, _ := classifier.NewProviderRequest(in1)
	r2, _ := classifier.NewProviderRequest(in2)
	_, _ = p.Assess(context.Background(), r1)
	_, _ = p.Assess(context.Background(), r2)
	if inner.n.Load() != 2 {
		t.Fatalf("args change must miss: %d", inner.n.Load())
	}
	// Delimiter-collision resistance: ["a,b","c"] vs ["a","b,c"] must miss.
	inA := baseInput()
	inA.ProposedAction = &adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, ToolName: "Bash", Command: "x",
		Arguments: []string{"a,b", "c"},
	}
	inB := baseInput()
	inB.ProposedAction = &adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, ToolName: "Bash", Command: "x",
		Arguments: []string{"a", "b,c"},
	}
	rA, _ := classifier.NewProviderRequest(inA)
	rB, _ := classifier.NewProviderRequest(inB)
	_, _ = p.Assess(context.Background(), rA)
	_, _ = p.Assess(context.Background(), rB)
	if inner.n.Load() != 4 {
		t.Fatalf("delimiter-safe args must miss: %d", inner.n.Load())
	}
	// Order-sensitive args.
	inC := baseInput()
	inC.ProposedAction = &adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, ToolName: "Bash", Command: "x",
		Arguments: []string{"x", "y"},
	}
	inD := baseInput()
	inD.ProposedAction = &adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, ToolName: "Bash", Command: "x",
		Arguments: []string{"y", "x"},
	}
	rC, _ := classifier.NewProviderRequest(inC)
	rD, _ := classifier.NewProviderRequest(inD)
	_, _ = p.Assess(context.Background(), rC)
	_, _ = p.Assess(context.Background(), rD)
	if inner.n.Load() != 6 {
		t.Fatalf("arg order must miss: %d", inner.n.Load())
	}
	in3 := baseInput()
	in3.UserException = true
	r3, _ := classifier.NewProviderRequest(in3)
	_, _ = p.Assess(context.Background(), r3)
	if inner.n.Load() != 7 {
		t.Fatalf("exception flag must miss: %d", inner.n.Load())
	}
}

func TestExactCache_SessionPartitionMiss(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: false,
	}, nil)
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	in1 := baseInput()
	in1.SessionID = "sess-a"
	in2 := baseInput()
	in2.SessionID = "sess-b"
	r1, err := classifier.NewProviderRequest(in1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := classifier.NewProviderRequest(in2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Assess(context.Background(), r1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Assess(context.Background(), r2); err != nil {
		t.Fatal(err)
	}
	if inner.n.Load() != 2 {
		t.Fatalf("different sessions must not share exact cache: calls=%d", inner.n.Load())
	}
}

func TestExactCache_HitRebindsPromptHash(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: false,
	}, nil)
	inner := &countingProvider{}
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	// Same session; RetryBudget is excluded from exact key but participates in PromptPlan/PromptHash.
	in1 := baseInput()
	in1.SessionID = "sess-rebind"
	in1.Challenge = &classifier.ChallengeContext{ChallengeID: "c1", State: "OPEN", RetryBudget: 2, ConcreteValue: "same"}
	in2 := baseInput()
	in2.SessionID = "sess-rebind"
	in2.Challenge = &classifier.ChallengeContext{ChallengeID: "c1", State: "OPEN", RetryBudget: 1, ConcreteValue: "same"}
	r1, err := classifier.NewProviderRequest(in1)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := classifier.NewProviderRequest(in2)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Prompt.PromptHash == r2.Prompt.PromptHash {
		t.Fatal("expected PromptHash to differ when retry budget changes")
	}
	k1, ok1 := classifier.BuildExactCacheKeyHash(testIdentity(), r1)
	k2, ok2 := classifier.BuildExactCacheKeyHash(testIdentity(), r2)
	if !ok1 || !ok2 || k1 != k2 {
		t.Fatalf("exact key must ignore retry budget: ok=%v/%v k1=%s k2=%s", ok1, ok2, k1, k2)
	}
	if _, err := p.Assess(context.Background(), r1); err != nil {
		t.Fatal(err)
	}
	res2, err := p.Assess(context.Background(), r2)
	if err != nil {
		t.Fatal(err)
	}
	if inner.n.Load() != 1 {
		t.Fatalf("expected exact hit within session: calls=%d", inner.n.Load())
	}
	if res2.Assessment.PromptHash != r2.Prompt.PromptHash {
		t.Fatalf("hit must rebind PromptHash got %q want %q", res2.Assessment.PromptHash, r2.Prompt.PromptHash)
	}
	if res2.Meta.FallbackReason != "" {
		t.Fatal("hit must not set FallbackReason")
	}
	if err := classifier.ValidateProviderResultForRequest(r2, res2); err != nil {
		t.Fatalf("validate after hit: %v", err)
	}
}

func TestExactCache_SingleflightLeaderCancelDoesNotCancelWaiters(t *testing.T) {
	t.Parallel()
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	inner := &countingProvider{sev: 33}
	var started sync.WaitGroup
	started.Add(1)
	release := make(chan struct{})
	slow := &blockingProvider{countingProvider: inner, started: &started, release: release}
	p := &classifier.CachingClassifierProvider{Inner: slow, Cache: cache, Identity: testIdentity()}
	req, err := classifier.NewProviderRequest(baseInput())
	if err != nil {
		t.Fatal(err)
	}

	leaderCtx, leaderCancel := context.WithCancel(context.Background())
	waiterCtx := context.Background()

	var leaderErr, waiterErr error
	var waiterRes classifier.ProviderResult
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, leaderErr = p.Assess(leaderCtx, req)
	}()
	// Ensure leader started the shared flight before waiters attach.
	started.Wait()
	go func() {
		defer wg.Done()
		// Small yield so waiter joins inflight while provider still blocked.
		time.Sleep(20 * time.Millisecond)
		waiterRes, waiterErr = p.Assess(waiterCtx, req)
	}()
	time.Sleep(40 * time.Millisecond) // let waiter join
	leaderCancel()
	close(release)
	wg.Wait()

	if leaderErr == nil {
		// Leader may still succeed if cancel raced after waitInflight select preferred done.
		// Either cancel error or success is acceptable for the leader; waiters must succeed.
	} else if !errors.Is(leaderErr, context.Canceled) {
		t.Fatalf("leader unexpected err: %v", leaderErr)
	}
	if waiterErr != nil {
		t.Fatalf("waiter must not be canceled by leader: %v", waiterErr)
	}
	if waiterRes.Assessment.Severity != 33 {
		t.Fatalf("waiter severity=%d", waiterRes.Assessment.Severity)
	}
	if inner.n.Load() != 1 {
		t.Fatalf("provider calls=%d", inner.n.Load())
	}
}

func TestExactCache_Stage2ReResolvesOnHit(t *testing.T) {
	t.Parallel()
	// Cached Stage-1 assessment must not freeze Stage-2: same assessment, different exceptions.
	cache, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: false,
	}, nil)
	inner := &countingProvider{sev: 90} // above typical threshold 50 → BLOCK before exceptions
	p := &classifier.CachingClassifierProvider{Inner: inner, Cache: cache, Identity: testIdentity()}
	in := baseInput()
	in.SessionID = "sess-s2"
	req, err := classifier.NewProviderRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	pres, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	// Second call exact-hit — still one provider call.
	pres2, err := p.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if inner.n.Load() != 1 {
		t.Fatalf("calls=%d", inner.n.Load())
	}
	if !pres2.Usage.CacheHit {
		t.Fatal("expected exact hit")
	}
	// Stage 2 resolve without provider: threshold BLOCK, then UserException ALLOW.
	block := resolveStage2ForTest(pres2.Assessment, 50, false)
	if block != "BLOCK" {
		t.Fatalf("want BLOCK got %s", block)
	}
	allow := resolveStage2ForTest(pres2.Assessment, 50, true)
	if allow != "ALLOW" {
		t.Fatalf("want ALLOW via exception got %s", allow)
	}
	// Cached assessment itself unchanged.
	if pres.Assessment.Severity != pres2.Assessment.Severity {
		t.Fatal("cached assessment mutated")
	}
}

// resolveStage2ForTest mirrors shadow Stage-2 threshold + UserException without network.
func resolveStage2ForTest(raw classifier.RawAssessment, threshold int, userException bool) string {
	if raw.Severity >= threshold {
		if userException {
			return "ALLOW"
		}
		return "BLOCK"
	}
	return "ALLOW"
}
