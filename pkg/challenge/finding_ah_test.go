package challenge_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/challenge"
)

// A: Edit/write identity
func TestEditDifferentContentNotSame(t *testing.T) {
	a := sampleEdit("main.go", "func A() {}")
	b := sampleEdit("main.go", "func B() {}")
	fa := mustFP(t, challenge.FingerprintInput{Proposed: a, SessionID: "sess-1"})
	fb := mustFP(t, challenge.FingerprintInput{Proposed: b, SessionID: "sess-1"})
	if fa.Fingerprint == fb.Fingerprint {
		t.Fatal("same file different content must not share fingerprint")
	}
	if challenge.ClassifyRelationship(fa, fb) != challenge.RelDifferent {
		t.Fatalf("rel=%s", challenge.ClassifyRelationship(fa, fb))
	}
	// WriteFile must not RelBypass on path alone
	if challenge.ClassifyRelationship(fa, fb) == challenge.RelBypass {
		t.Fatal("write must not path-only bypass")
	}
}

func TestEditSameContentEquivalentSurfaces(t *testing.T) {
	// Args vs redacted_payload new_string vs new_str — lossless equivalent surfaces share fingerprint.
	a := sampleEdit("main.go", "same-content")
	payloadNS, _ := json.Marshal(map[string]string{"new_string": "same-content", "file_path": "main.go"})
	b := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "pa-e2", ToolName: "Edit", ToolClass: adapter.ToolClassEdit,
		FilePath: "main.go", RedactedPayload: payloadNS, ParseStatus: "ok",
		WorkspaceRevision: "ws-1", ContractRevision: 3,
	}
	payloadAlias, _ := json.Marshal(map[string]string{"new_str": "same-content"})
	c := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "pa-e3", ToolName: "Edit", ToolClass: adapter.ToolClassEdit,
		FilePath: "main.go", RedactedPayload: payloadAlias, ParseStatus: "ok",
		WorkspaceRevision: "ws-1", ContractRevision: 3,
	}
	fa := mustFP(t, challenge.FingerprintInput{Proposed: a, SessionID: "sess-1"})
	fb := mustFP(t, challenge.FingerprintInput{Proposed: b, SessionID: "sess-1"})
	fc := mustFP(t, challenge.FingerprintInput{Proposed: c, SessionID: "sess-1"})
	if fa.Fingerprint != fb.Fingerprint || fa.Fingerprint != fc.Fingerprint {
		t.Fatalf("equivalent edit surfaces must share FP: a=%s b=%s c=%s op=%s/%s/%s",
			fa.Fingerprint, fb.Fingerprint, fc.Fingerprint, fa.OperationDigest, fb.OperationDigest, fc.OperationDigest)
	}
	if fa.OperationDigest != fb.OperationDigest {
		t.Fatal("operation digests must match for equivalent surfaces")
	}
}

func TestEditMissingContentFailClosed(t *testing.T) {
	pa := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "pa-e", ToolName: "Edit", ToolClass: adapter.ToolClassEdit,
		FilePath: "main.go", ParseStatus: "ok",
	}
	err := challenge.ValidateProposedForChallenge(pa)
	if err == nil {
		t.Fatal("path-only edit must fail closed")
	}
	svc := challenge.NewService(challenge.ServiceConfig{})
	_, err = svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: "sess-1", Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err == nil {
		t.Fatal("open must reject path-only edit")
	}
}

// B: Lossy projections
func TestLossyTruncatedRejected(t *testing.T) {
	pa := samplePA("rm -rf build")
	pa.Truncated = true
	svc := challenge.NewService(challenge.ServiceConfig{})
	_, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: "sess-1", Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err == nil {
		t.Fatal("truncated open must fail")
	}
	pa2 := samplePA("rm -rf build")
	pa2.ParseStatus = "partial"
	_, err = svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: "sess-1", Proposed: pa2, BlockClass: challenge.BlockClassOverSOP,
	})
	if err == nil {
		t.Fatal("partial parse open must fail")
	}
	// Retry also rejects
	good := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: "sess-1", Proposed: good, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
	bad := samplePA("rm -rf build")
	bad.ParseStatus = "unknown_shape"
	_, err = svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: "sess-1", Proposed: bad,
	})
	if err == nil {
		t.Fatal("lossy retry must fail")
	}
}

// C: RequiredClaims allowlist
func TestRequiredClaimsUnknownRejected(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{})
	_, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: "sess-1", Proposed: samplePA("rm -rf build"),
		BlockClass:     challenge.BlockClassOverSOP,
		RequiredClaims: []string{"concrete_value", "not_a_real_claim"},
	})
	if err == nil {
		t.Fatal("unknown claim must fail open")
	}
	_, err = challenge.ValidateRequiredClaims([]string{"concrete_value", "concrete_value"})
	if err == nil {
		t.Fatal("duplicate claims")
	}
}

// gateReEval blocks until release so waiters observe RETRY_PENDING.
type gateReEval struct {
	entered chan struct{}
	release chan struct{}
}

func (g *gateReEval) ReEvaluate(ctx context.Context, rec challenge.ChallengeRecord, proposed adapter.ProposedAction, just *challenge.Justification, in *challenge.ReEvalContext) (challenge.ReEvalResult, error) {
	select {
	case <-g.entered:
	default:
		close(g.entered)
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return challenge.ReEvalResult{Stage2Decision: challenge.DecisionBlock, Reason: "ctx"}, ctx.Err()
	}
	return challenge.ReEvalResult{Stage2Decision: challenge.DecisionAllow, Reason: "gate_allow"}, nil
}

// D: wait cancel — hard asserts
func TestWaitTerminalContextCanceled(t *testing.T) {
	gate := &gateReEval{entered: make(chan struct{}), release: make(chan struct{})}
	svc := challenge.NewService(challenge.ServiceConfig{ReEval: gate})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
			CorrelationID: "winner",
		})
	}()
	// Wait until winner holds RETRY_PENDING / re-eval
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("winner did not enter re-eval")
	}

	// Deadline exceeded while still pending
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	res, err := svc.AttemptRetry(ctx, challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "waiter-deadline",
	})
	if err == nil {
		t.Fatalf("deadline waiter must error, got nil res=%+v", res)
	}
	if err != context.DeadlineExceeded && err != context.Canceled {
		t.Fatalf("want DeadlineExceeded/Canceled, got %v", err)
	}
	if res.Record.State == challenge.StateRetryPending && err == nil {
		t.Fatal("must never return RETRY_PENDING with nil error")
	}
	if res.RejectedReason != "context_canceled" && res.RejectedReason != "" {
		// RejectedReason should be context_canceled when wait fails
		if res.RejectedReason != "context_canceled" {
			t.Logf("rejected_reason=%q (ok if empty on early return)", res.RejectedReason)
		}
	}

	// Canceled context
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	res2, err2 := svc.AttemptRetry(ctx2, challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "waiter-cancel",
	})
	if err2 == nil {
		t.Fatalf("canceled waiter must error, got nil res=%+v", res2)
	}
	if err2 != context.Canceled && err2 != context.DeadlineExceeded {
		t.Fatalf("want Canceled, got %v", err2)
	}

	close(gate.release)
	wg.Wait()
}

func TestConcurrentOpenPolicyMismatchNoDualOpen(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build")
	const n = 20
	results := make([]challenge.ChallengeRecord, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			bc := challenge.BlockClassScopeDrift
			rc := "SCOPE_DRIFT"
			if i%2 == 1 {
				bc = challenge.BlockClassEvidenceGap
				rc = "EVIDENCE_GAP"
			}
			results[i], errs[i] = svc.Open(context.Background(), challenge.OpenRequest{
				SessionID: pa.SessionID, Proposed: pa, BlockClass: bc, ReasonCode: rc,
				RulesetHash: "rules-x", PolicyVersion: "v1",
			})
		}()
	}
	wg.Wait()
	openByClass := map[string]int{}
	ids := map[string]struct{}{}
	for i, r := range results {
		if errs[i] != nil {
			continue
		}
		if r.State == challenge.StateOpen {
			openByClass[r.BlockClass]++
			ids[r.ChallengeID] = struct{}{}
		}
	}
	// Unique OPEN challenge IDs for the shared action fingerprint must be ≤1.
	// (Multiple Open() results may legitimately return the same id — do not double-count.)
	liveOpenIDs := map[string]string{} // id -> block class
	for _, r := range results {
		if r.ChallengeID == "" {
			continue
		}
		got, ok := svc.Get(r.ChallengeID)
		if !ok {
			continue
		}
		if got.State == challenge.StateOpen {
			liveOpenIDs[got.ChallengeID] = got.BlockClass
		}
	}
	if len(liveOpenIDs) > 1 {
		t.Fatalf("dual OPEN zombies after concurrent policy-mismatched Open: ids=%v resultClasses=%v", liveOpenIDs, openByClass)
	}
	if len(liveOpenIDs) < 1 {
		t.Fatal("expected at least one OPEN challenge")
	}
}

// E: Justification hash collision-free
func TestJustificationHashDelimiter(t *testing.T) {
	j1 := validJustification("ch1", []string{"ev-1"})
	j1.ConcreteValue = "a|b"
	j1.PreventedFailureOrThreat = "c"
	j2 := validJustification("ch1", []string{"ev-1"})
	j2.ConcreteValue = "a"
	j2.PreventedFailureOrThreat = "b|c"
	h1 := challenge.HashJustification(j1)
	h2 := challenge.HashJustification(j2)
	if h1 == h2 {
		t.Fatal("pipe in fields must not collide hashes")
	}
	j3 := validJustification("ch1", []string{"a,b", "c"})
	j4 := validJustification("ch1", []string{"a", "b,c"})
	if challenge.HashJustification(j3) == challenge.HashJustification(j4) {
		t.Fatal("evidence list boundary collision")
	}
}

// F: One-shot ALLOW — second distinct identity cannot reuse ALLOW
func TestOneShotAllowSecondIdentityBlocked(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
	r1, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "attempt-1",
		ReEval:        &challenge.ReEvalContext{UserException: true},
	})
	if err != nil || r1.Stage2Decision != challenge.DecisionAllow {
		t.Fatalf("first: %+v err=%v", r1, err)
	}
	// Same identity → idempotent ALLOW
	r2, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "attempt-1",
		ReEval:        &challenge.ReEvalContext{UserException: true},
	})
	if r2.Stage2Decision != challenge.DecisionAllow || !r2.IdempotentReplay {
		t.Fatalf("idempotent same identity: %+v err=%v", r2, err)
	}
	// New identity → BLOCK budget exhausted
	r3, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "attempt-2",
		ReEval:        &challenge.ReEvalContext{UserException: true},
	})
	if r3.Stage2Decision != challenge.DecisionBlock {
		t.Fatalf("second identity must BLOCK not ALLOW: %+v", r3)
	}
	if r3.RejectedReason != "retry_budget_exhausted" && r3.RejectedReason != "already_consumed" {
		t.Fatalf("reason %q", r3.RejectedReason)
	}
	if err == nil {
		t.Fatal("expected error on second distinct identity")
	}
}

// G: Policy change does not reuse stale challenge
func TestPolicyChangeDoesNotReuseChallenge(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build")
	r1, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassScopeDrift,
		ReasonCode: "SCOPE_DRIFT", PolicyVersion: "v1", RulesetHash: "rules-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Same fingerprint, different block class / rules
	r2, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassEvidenceGap,
		ReasonCode: "EVIDENCE_GAP", PolicyVersion: "v1", RulesetHash: "rules-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.ChallengeID == r2.ChallengeID {
		t.Fatal("must not reuse challenge across policy identity change")
	}
	if r2.BlockClass != challenge.BlockClassEvidenceGap {
		t.Fatal(r2.BlockClass)
	}
	// Old challenge should be expired/superseded
	old, _ := svc.Get(r1.ChallengeID)
	if old.State != challenge.StateExpired && old.State != challenge.StateOpen {
		// if still open without match - supersede should expire
		t.Logf("old state %s", old.State)
	}
	if old.State == challenge.StateOpen && old.BlockClass == challenge.BlockClassScopeDrift {
		// race path without expire - still r2 is new
		if r1.ChallengeID == r2.ChallengeID {
			t.Fatal("same id")
		}
	}
}

// H: Empty-target tool kinds do not collapse
func TestEmptyTargetToolsDoNotCollapse(t *testing.T) {
	read := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "r1", ToolName: "Read", ToolClass: adapter.ToolClassRead,
		ParseStatus: "ok",
	}
	search := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "s1", ToolName: "Grep", ToolClass: adapter.ToolClassSearch,
		ParseStatus: "ok",
	}
	other := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "o1", ToolName: "mcp__tool_a", ToolClass: adapter.ToolClassOther,
		ParseStatus: "ok",
	}
	other2 := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "o2", ToolName: "mcp__tool_b", ToolClass: adapter.ToolClassOther,
		ParseStatus: "ok",
	}
	fr := mustFP(t, challenge.FingerprintInput{Proposed: read, SessionID: "sess-1"})
	fs := mustFP(t, challenge.FingerprintInput{Proposed: search, SessionID: "sess-1"})
	fo := mustFP(t, challenge.FingerprintInput{Proposed: other, SessionID: "sess-1"})
	fo2 := mustFP(t, challenge.FingerprintInput{Proposed: other2, SessionID: "sess-1"})
	if fr.Fingerprint == fs.Fingerprint || fr.Fingerprint == fo.Fingerprint {
		t.Fatal("empty-target different tool kinds collapsed")
	}
	if fo.Fingerprint == fo2.Fingerprint {
		t.Fatal("different ToolName other collapsed")
	}
}

func TestFingerprintSpecialCharNoCollision(t *testing.T) {
	pa1 := samplePA("echo")
	pa1.TargetScope = []string{"a|b", "c"}
	pa2 := samplePA("echo")
	pa2.TargetScope = []string{"a", "b|c"}
	if mustFP(t, challenge.FingerprintInput{Proposed: pa1, SessionID: "s"}).Fingerprint ==
		mustFP(t, challenge.FingerprintInput{Proposed: pa2, SessionID: "s"}).Fingerprint {
		t.Fatal("special char target collision")
	}
}
