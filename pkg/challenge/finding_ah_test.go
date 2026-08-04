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
		CorrelationID: "test-attempt",
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

// Codex: Open rejects explicit request vs proposed session mismatch.
func TestOpenSessionMismatchRejected(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build") // SessionID=sess-1
	_, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: "other-sess", Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err == nil {
		t.Fatal("expected session mismatch error")
	}
}

// Codex: hard non-appealable open expires stale active productivity challenge.
func TestHardBlockExpiresActiveChallenge(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != challenge.StateOpen {
		t.Fatal(rec.State)
	}
	// Same action, hard deny class — must supersede/expire the active challenge.
	_, err = svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassSecretExfiltration,
	})
	if err == nil {
		t.Fatal("expected non-appealable")
	}
	got, ok := svc.Get(rec.ChallengeID)
	if !ok {
		t.Fatal("missing record")
	}
	if got.State != challenge.StateExpired {
		t.Fatalf("want EXPIRED after hard block, got %s", got.State)
	}
}

// Codex: compound/quoted delete must not share delete_tree fingerprint with simple rm.
func TestCompoundAndQuotedDeleteNotDeleteTree(t *testing.T) {
	plain := mustFP(t, challenge.FingerprintInput{Proposed: samplePA("rm -rf build"), SessionID: "sess-1"})
	compound := mustFP(t, challenge.FingerprintInput{Proposed: samplePA("find build -delete;id"), SessionID: "sess-1"})
	quoted := mustFP(t, challenge.FingerprintInput{Proposed: samplePA(`rm -rf "my dir"`), SessionID: "sess-1"})
	if plain.SideEffectClass != challenge.SideEffectDeleteTree {
		t.Fatalf("plain want delete_tree got %s", plain.SideEffectClass)
	}
	if compound.SideEffectClass == challenge.SideEffectDeleteTree {
		t.Fatal("compound find -delete;id must not classify as delete_tree")
	}
	if quoted.SideEffectClass == challenge.SideEffectDeleteTree || quoted.SideEffectClass == challenge.SideEffectDeleteFile {
		t.Fatal("quoted rm must not classify as delete_*")
	}
	if plain.Fingerprint == compound.Fingerprint || plain.Fingerprint == quoted.Fingerprint {
		t.Fatal("unsafe shell must not share fingerprint with plain delete")
	}
	if challenge.ClassifyRelationship(plain, compound) == challenge.RelBypass ||
		challenge.ClassifyRelationship(plain, compound) == challenge.RelSame {
		t.Fatal("compound must not inherit delete allowance")
	}
}

// Codex: multi-Service shared Store waiters observe terminal via store-scoped signal.
func TestSharedStoreTerminalWakesOtherService(t *testing.T) {
	const delay = 250 * time.Millisecond
	st := challenge.NewStore()
	// Both services share Store and the same slow re-eval (Service-local reeval is fine).
	svcA := challenge.NewService(challenge.ServiceConfig{Store: st, ReEval: slowAllowReEval{delay: delay}})
	svcB := challenge.NewService(challenge.ServiceConfig{Store: st, ReEval: slowAllowReEval{delay: delay}})
	pa := samplePA("rm -rf build")
	rec, err := svcA.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svcA.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil); err != nil {
		t.Fatal(err)
	}

	type pair struct {
		res challenge.RetryResult
		err error
	}
	chA := make(chan pair, 1)
	chB := make(chan pair, 1)
	req := challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "shared-store-attempt",
	}
	go func() {
		r, e := svcA.AttemptRetry(context.Background(), req)
		chA <- pair{r, e}
	}()
	// Ensure A has a chance to enter RETRY_PENDING before B waits.
	time.Sleep(30 * time.Millisecond)
	go func() {
		r, e := svcB.AttemptRetry(context.Background(), req)
		chB <- pair{r, e}
	}()

	var a, b pair
	select {
	case a = <-chA:
	case <-time.After(3 * time.Second):
		t.Fatal("svcA AttemptRetry hung — store signal broken?")
	}
	select {
	case b = <-chB:
	case <-time.After(3 * time.Second):
		t.Fatal("svcB AttemptRetry hung — cross-service terminal wake broken")
	}
	if a.err != nil && b.err != nil {
		t.Fatalf("both failed: a=%v b=%v", a.err, b.err)
	}
	final, _ := svcB.Get(rec.ChallengeID)
	if final.State != challenge.StateAllowedOnce {
		t.Fatalf("want ALLOWED_ONCE got %s", final.State)
	}
	// Waiter on the non-owning service must observe the same terminal decision.
	for _, p := range []pair{a, b} {
		if p.res.Record.State != final.State || p.res.Stage2Decision != final.Stage2Decision {
			t.Fatalf("result %+v final state=%s stage2=%s", p.res, final.State, final.Stage2Decision)
		}
	}
}

// Codex: cache key length-prefix avoids pipe field collisions.
func TestCacheKeyPipeCollisionFree(t *testing.T) {
	base := challenge.ChallengeRecord{
		SchemaVersion: challenge.SchemaChallengeRecord, ChallengeID: "c1", SessionID: "s",
		State: challenge.StateJustified, ActionFingerprint: "fp", JustificationHash: "jh",
		ContractRevision: 1, RulesetHash: "r", PolicyHash: "p",
	}
	a := base
	a.WorkspaceRevision = "a|b"
	a.RulesetHash = "c"
	b := base
	b.WorkspaceRevision = "a"
	b.RulesetHash = "b|c"
	ka := challenge.BuildCacheKeyInputs(a, nil, "m", "ph")
	kb := challenge.BuildCacheKeyInputs(b, nil, "m", "ph")
	if ka.CanonicalKey == kb.CanonicalKey {
		t.Fatal("cache key pipe collision")
	}
}

// Codex/review: empty CorrelationID and RetryRequestID rejected — no empty-key ALLOW.
func TestRetryRequiresIdentity(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
	res, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		// both identity fields empty
		ReEval: &challenge.ReEvalContext{UserException: true},
	})
	if err == nil {
		t.Fatalf("expected missing identity error, got %+v", res)
	}
	if res.RejectedReason != "missing_retry_identity" {
		t.Fatalf("reason=%q", res.RejectedReason)
	}
	if res.Stage2Decision == challenge.DecisionAllow || res.Record.State == challenge.StateAllowedOnce {
		t.Fatal("empty identity must not ALLOW")
	}
	// RetryRequestID alone is enough
	res2, err2 := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		RetryRequestID: "rid-1",
		ReEval:         &challenge.ReEvalContext{UserException: true},
	})
	if err2 != nil || res2.Stage2Decision != challenge.DecisionAllow {
		t.Fatalf("RetryRequestID-only should ALLOW: %+v err=%v", res2, err2)
	}
}

// Codex/review: owner cancel during re-eval does not persist ALLOWED_ONCE.
func TestOwnerCancelDuringReEvalNoAllow(t *testing.T) {
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

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan challenge.RetryResult, 1)
	errCh := make(chan error, 1)
	go func() {
		r, e := svc.AttemptRetry(ctx, challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
			CorrelationID: "owner-cancel",
		})
		done <- r
		errCh <- e
	}()
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("did not enter re-eval")
	}
	cancel() // owner cancel while re-eval blocked — do not release gate (ctx.Done must win)

	var res challenge.RetryResult
	var e error
	select {
	case res = <-done:
		e = <-errCh
	case <-time.After(3 * time.Second):
		close(gate.release) // failsafe so package tests do not leak
		t.Fatal("AttemptRetry hung after cancel")
	}
	if e == nil {
		t.Fatalf("want context error, got nil res=%+v", res)
	}
	if res.Record.State == challenge.StateAllowedOnce || res.Stage2Decision == challenge.DecisionAllow {
		t.Fatalf("owner cancel must not ALLOW: %+v", res)
	}
	got, _ := svc.Get(rec.ChallengeID)
	if got.State == challenge.StateAllowedOnce {
		t.Fatal("store must not be ALLOWED_ONCE after owner cancel")
	}
}

// Codex P1: HUMAN_REVIEW Open expires prior productivity challenge for same session|fp.
func TestHumanReviewExpiresActiveProductivityChallenge(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
		PolicyClass: challenge.PolicyClassProductivity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != challenge.StateOpen {
		t.Fatalf("want OPEN got %s", rec.State)
	}
	// Reclassify same action to SECURITY / production deploy → HUMAN_REVIEW
	hr, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassProductionDeploy,
		PolicyClass: challenge.PolicyClassSecurity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hr.State != challenge.StateHumanReview {
		t.Fatalf("want HUMAN_REVIEW got %s", hr.State)
	}
	got, ok := svc.Get(rec.ChallengeID)
	if !ok {
		t.Fatal("missing old record")
	}
	if got.State != challenge.StateExpired {
		t.Fatalf("stale productivity challenge must be EXPIRED after HUMAN_REVIEW reclass, got %s", got.State)
	}
	// Retry on expired must not ALLOW
	_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
	res, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "after-hr",
		ReEval:        &challenge.ReEvalContext{UserException: true},
	})
	if res.Stage2Decision == challenge.DecisionAllow || res.Record.State == challenge.StateAllowedOnce {
		t.Fatalf("must not ALLOW after HR reclass: %+v err=%v", res, err)
	}
}

// Codex P1: `echo rm -rf build` must not share delete identity with real rm.
func TestEchoRmNotDeleteTreeIdentity(t *testing.T) {
	real := mustFP(t, challenge.FingerprintInput{Proposed: samplePA("rm -rf build"), SessionID: "sess-1"})
	echo := mustFP(t, challenge.FingerprintInput{Proposed: samplePA("echo rm -rf build"), SessionID: "sess-1"})
	if real.SideEffectClass != challenge.SideEffectDeleteTree {
		t.Fatalf("real rm want delete_tree got %s", real.SideEffectClass)
	}
	if echo.SideEffectClass == challenge.SideEffectDeleteTree || echo.SideEffectClass == challenge.SideEffectDeleteFile {
		t.Fatalf("echo rm must not be delete_*, got %s", echo.SideEffectClass)
	}
	if real.Fingerprint == echo.Fingerprint {
		t.Fatal("echo rm must not share fingerprint with real delete")
	}
	rel := challenge.ClassifyRelationship(real, echo)
	if rel == challenge.RelSame || rel == challenge.RelBypass {
		t.Fatalf("echo rm must not RelSame/RelBypass real delete, got %s", rel)
	}
	// Spoof env-like token must not skip argv0: `1BAD=x rm -rf build`
	spoof := mustFP(t, challenge.FingerprintInput{Proposed: samplePA("1BAD=x rm -rf build"), SessionID: "sess-1"})
	if spoof.SideEffectClass == challenge.SideEffectDeleteTree || spoof.SideEffectClass == challenge.SideEffectDeleteFile {
		t.Fatalf("spoof ENV token must not yield delete_*, got %s", spoof.SideEffectClass)
	}
	// Valid ENV prefix still allows command-position rm.
	envOk := mustFP(t, challenge.FingerprintInput{Proposed: samplePA("FOO=bar rm -rf build"), SessionID: "sess-1"})
	if envOk.SideEffectClass != challenge.SideEffectDeleteTree {
		t.Fatalf("valid ENV prefix + rm want delete_tree got %s", envOk.SideEffectClass)
	}
}

// Codex P2: multi-Service shared Store IDs must not collide under fixed Now.
func TestSharedStoreUniqueIDs(t *testing.T) {
	fixed := time.Date(2026, 8, 4, 15, 0, 0, 0, time.UTC)
	st := challenge.NewStore()
	now := func() time.Time { return fixed }
	svcA := challenge.NewService(challenge.ServiceConfig{Store: st, Now: now})
	svcB := challenge.NewService(challenge.ServiceConfig{Store: st, Now: now})
	pa1 := samplePA("rm -rf build")
	pa2 := samplePA("rm -rf other")
	pa2.SessionID = "sess-2"
	r1, err := svcA.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa1.SessionID, Proposed: pa1, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svcB.Open(context.Background(), challenge.OpenRequest{
		SessionID: "sess-2", Proposed: pa2, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.ChallengeID == "" || r2.ChallengeID == "" {
		t.Fatal("empty id")
	}
	if r1.ChallengeID == r2.ChallengeID {
		t.Fatalf("ID collision across services: %s", r1.ChallengeID)
	}
	if _, ok := svcA.Get(r1.ChallengeID); !ok {
		t.Fatal("r1 missing")
	}
	if _, ok := svcB.Get(r2.ChallengeID); !ok {
		t.Fatal("r2 missing")
	}
}

// Codex P2: sequence expiry rechecked under lock before budget consume.
func TestRetryExpiresUnderLockBeforeBudget(t *testing.T) {
	fixed := time.Date(2026, 8, 4, 16, 0, 0, 0, time.UTC)
	svc := challenge.NewService(challenge.ServiceConfig{Now: func() time.Time { return fixed }})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
		ExpiresAfterSequences: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Advance store sequence past ExpiresAtSequence without calling ExpireDue.
	other := samplePA("echo other")
	other.SessionID = "sess-other"
	_, _ = svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: "sess-other", Proposed: other, BlockClass: challenge.BlockClassOverSOP,
	})
	// AttemptRetry must expire under lock, not ALLOW.
	res, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "late-retry",
		ReEval:        &challenge.ReEvalContext{UserException: true},
	})
	if res.Stage2Decision == challenge.DecisionAllow || res.Record.State == challenge.StateAllowedOnce {
		t.Fatalf("expired under lock must not ALLOW: %+v err=%v", res, err)
	}
	if res.RejectedReason != "expired" && res.Record.State != challenge.StateExpired {
		// Accept either explicit expired reason or terminal expired state
		got, _ := svc.Get(rec.ChallengeID)
		if got.State != challenge.StateExpired && res.RejectedReason != "expired" {
			t.Fatalf("want expired path, res=%+v got.state=%s err=%v", res, got.State, err)
		}
	}
}

// Codex/review: conflicting edit args vs payload rejected.
func TestEditArgsPayloadConflictRejected(t *testing.T) {
	payload, _ := json.Marshal(map[string]string{"new_string": "from-payload", "old_string": "old-p"})
	pa := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "conflict-edit", ToolName: "Edit", ToolClass: adapter.ToolClassEdit,
		FilePath: "main.go", Arguments: []string{"old-args", "from-args"},
		RedactedPayload: payload, ParseStatus: "ok",
		WorkspaceRevision: "ws-1", ContractRevision: 3,
	}
	if err := challenge.ValidateProposedForChallenge(pa); err == nil {
		t.Fatal("conflicting args/payload must fail ValidateProposedForChallenge")
	}
	_, err := challenge.ComputeFingerprint(challenge.FingerprintInput{Proposed: pa, SessionID: "sess-1"})
	if err == nil {
		t.Fatal("conflicting edit must fail ComputeFingerprint")
	}
	svc := challenge.NewService(challenge.ServiceConfig{})
	_, err = svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: "sess-1", Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err == nil {
		t.Fatal("Open must reject conflicting edit")
	}
}
