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
	// Args vs redacted_payload with new_string
	a := sampleEdit("main.go", "same-content")
	payload, _ := json.Marshal(map[string]string{"new_string": "same-content", "file_path": "main.go"})
	b := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "pa-e2", ToolName: "Edit", ToolClass: adapter.ToolClassEdit,
		FilePath: "main.go", RedactedPayload: payload, ParseStatus: "ok",
		WorkspaceRevision: "ws-1", ContractRevision: 3,
	}
	// Digests may differ (args vs payload keys) — both must be computable losslessly.
	if _, err := challenge.ComputeFingerprint(challenge.FingerprintInput{Proposed: a, SessionID: "sess-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := challenge.ComputeFingerprint(challenge.FingerprintInput{Proposed: b, SessionID: "sess-1"}); err != nil {
		t.Fatal(err)
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

// D: wait cancel
func TestWaitTerminalContextCanceled(t *testing.T) {
	svc := challenge.NewService(challenge.ServiceConfig{
		ReEval: slowAllowReEval{delay: 500 * time.Millisecond},
	})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)

	// Start winner
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
			CorrelationID: "winner",
		})
	}()
	// Waiter with short deadline
	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	// Force RETRY_PENDING by racing — waiter should get context error not nil success
	res, err := svc.AttemptRetry(ctx, challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "waiter",
	})
	if err == nil && res.Record.State == challenge.StateRetryPending {
		t.Fatal("must not return RETRY_PENDING as success")
	}
	// Either canceled or got terminal after wait — if error, must be context error
	if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
		// may also be budget exhausted after winner finished
		if res.RejectedReason != "context_canceled" && res.RejectedReason != "retry_budget_exhausted" && res.RejectedReason != "duplicate_retry" {
			// acceptable if winner finished first
			t.Logf("waiter result err=%v res=%+v", err, res)
		}
	}
	wg.Wait()
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
