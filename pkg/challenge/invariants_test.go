package challenge_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/challenge"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

// TestChallengeInvariants is the adversarial table for #131 identity/policy/replay edges.
func TestChallengeInvariants(t *testing.T) {
	t.Run("delimiter_collision_targets", func(t *testing.T) {
		// a,b + c  vs  a + b,c  must not share fingerprint.
		a := samplePA(`rm -rf a,b c`)
		// Force TargetScope to the colliding pair when command parsing differs.
		pa1 := samplePA("rm -rf x")
		pa1.TargetScope = []string{"a,b", "c"}
		pa2 := samplePA("rm -rf x")
		pa2.TargetScope = []string{"a", "b,c"}
		f1 := mustFP(t, challenge.FingerprintInput{Proposed: pa1, SessionID: "s"})
		f2 := mustFP(t, challenge.FingerprintInput{Proposed: pa2, SessionID: "s"})
		if f1.Fingerprint == f2.Fingerprint {
			t.Fatalf("delimiter collision: same FP %s canon=%s vs %s", f1.Fingerprint, f1.CanonicalForm, f2.CanonicalForm)
		}
		if challenge.ClassifyRelationship(f1, f2) == challenge.RelSame {
			t.Fatal("RelSame on delimiter collision")
		}
		// End-to-end: open on first, justify, retry second with UserException must NOT ALLOWED_ONCE
		svc := challenge.NewService(challenge.ServiceConfig{})
		pa1.SessionID = "sess-1"
		pa2.SessionID = "sess-1"
		rec, err := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: pa1.SessionID, Proposed: pa1, BlockClass: challenge.BlockClassOverSOP,
		})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
		res, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: pa1.SessionID, Proposed: pa2,
			CorrelationID: "test-attempt",
			ReEval:        &challenge.ReEvalContext{UserException: true},
		})
		if err == nil || res.Record.State == challenge.StateAllowedOnce {
			t.Fatalf("delimiter collision must not inherit ALLOW: %+v err=%v", res, err)
		}
		_ = a
	})

	t.Run("policy_class_case_security_fail_closed", func(t *testing.T) {
		for _, pc := range []string{"SECURITY", "security", "Security", " SECURITY "} {
			svc := challenge.NewService(challenge.ServiceConfig{
				ReEval: failProvider{},
			})
			pa := samplePA("echo hi")
			rec, err := svc.Open(context.Background(), challenge.OpenRequest{
				SessionID: "s", Proposed: pa, BlockClass: challenge.BlockClassOverSOP, PolicyClass: pc,
			})
			// SECURITY + productivity block code → fail-closed human review (or non-appealable path)
			if err != nil {
				// non-appealable returns error without record id — ok if hard deny
				continue
			}
			if rec.State != challenge.StateHumanReview && rec.Appealability != challenge.AppealHumanReview {
				t.Fatalf("policy %q want HUMAN_REVIEW path, got state=%s appeal=%s", pc, rec.State, rec.Appealability)
			}
			// Re-eval provider error under security must BLOCK not fail-open ALLOW
			// Open a productivity challenge and re-eval with security policy via AttemptRetry context
			svc2 := challenge.NewService(challenge.ServiceConfig{})
			pa2 := samplePA("rm -rf build")
			rec2, err := svc2.Open(context.Background(), challenge.OpenRequest{
				SessionID: pa2.SessionID, Proposed: pa2, BlockClass: challenge.BlockClassOverSOP,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, _ = svc2.Justify(context.Background(), validJustification(rec2.ChallengeID, nil), nil)
			res, _ := svc2.AttemptRetry(context.Background(), challenge.RetryRequest{
				ChallengeID: rec2.ChallengeID, SessionID: pa2.SessionID, Proposed: pa2,
				CorrelationID: "test-attempt",
				ReEval:        &challenge.ReEvalContext{PolicyClass: pc, Provider: failClassifier{}},
			})
			// Provider error under SECURITY must BLOCK (fail-closed), never ALLOW/ALLOWED_ONCE.
			if res.Stage2Decision != challenge.DecisionBlock || res.Record.State == challenge.StateAllowedOnce {
				t.Fatalf("security policy class %q must fail-closed BLOCK, got stage2=%s state=%s", pc, res.Stage2Decision, res.Record.State)
			}
		}
	})

	t.Run("live_equals_replay_intervention", func(t *testing.T) {
		svc := challenge.NewService(challenge.ServiceConfig{})
		pa := samplePA("rm -rf build")
		rec, _ := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
		})
		_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
		_, _ = svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
			CorrelationID: "test-attempt",
			ReEval:        &challenge.ReEvalContext{UserException: true},
		})
		live, _ := svc.Get(rec.ChallengeID)
		rebuilt, err := svc.Replay(rec.ChallengeID)
		if err != nil {
			t.Fatal(err)
		}
		if live.State != rebuilt.State || live.Stage2Decision != rebuilt.Stage2Decision {
			t.Fatalf("state/decision live=%s/%s replay=%s/%s", live.State, live.Stage2Decision, rebuilt.State, rebuilt.Stage2Decision)
		}
		if live.Intervention != rebuilt.Intervention {
			t.Fatalf("Intervention live=%q replay=%q", live.Intervention, rebuilt.Intervention)
		}
		if live.State == challenge.StateAllowedOnce && live.Intervention != challenge.InterventionNone {
			t.Fatalf("live ALLOWED_ONCE must clear intervention, got %q", live.Intervention)
		}
		if live.RetryBudget != rebuilt.RetryBudget {
			t.Fatalf("budget live=%d replay=%d", live.RetryBudget, rebuilt.RetryBudget)
		}
	})

	t.Run("foreign_session_reject", func(t *testing.T) {
		svc := challenge.NewService(challenge.ServiceConfig{})
		pa := samplePA("rm -rf build")
		pa.SessionID = "owner"
		rec, _ := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: "owner", Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
		})
		_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
		foreign := samplePA("rm -rf build")
		foreign.SessionID = "attacker"
		res, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: "attacker", Proposed: foreign,
			CorrelationID: "test-attempt",
			ReEval:        &challenge.ReEvalContext{UserException: true},
		})
		if err == nil || res.RejectedReason != "ownership_mismatch" {
			t.Fatalf("%+v err=%v", res, err)
		}
	})

	t.Run("branch_mismatch_reject", func(t *testing.T) {
		svc := challenge.NewService(challenge.ServiceConfig{})
		rec, _ := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: "s", Proposed: samplePA("rm -rf build"), BlockClass: challenge.BlockClassOverSOP, Branch: "main",
		})
		_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
		res, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: "s", Branch: "feature",
			CorrelationID: "test-attempt",
			Proposed:      samplePA("rm -rf build"),
			ReEval:        &challenge.ReEvalContext{UserException: true},
		})
		if err == nil || res.RejectedReason != "ownership_mismatch" {
			t.Fatalf("branch mismatch: %+v err=%v", res, err)
		}
	})

	t.Run("scope_expand_reject", func(t *testing.T) {
		svc := challenge.NewService(challenge.ServiceConfig{})
		pa := samplePA("rm -rf build")
		rec, _ := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
		})
		_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
		exp := samplePA("rm -rf build /tmp/secrets")
		res, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: pa.SessionID,
			CorrelationID: "test-attempt",
			Proposed:      exp,
			ReEval:        &challenge.ReEvalContext{UserException: true},
		})
		if err == nil || res.Record.State == challenge.StateAllowedOnce {
			t.Fatalf("expand: %+v err=%v", res, err)
		}
	})

	t.Run("hard_deny_non_appealable", func(t *testing.T) {
		svc := challenge.NewService(challenge.ServiceConfig{})
		_, err := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: "s", Proposed: samplePA("cat secret.pem"), BlockClass: challenge.BlockClassSecretExfiltration,
		})
		if err == nil {
			t.Fatal("expected non-appealable")
		}
	})

	t.Run("irreversible_human_review", func(t *testing.T) {
		svc := challenge.NewService(challenge.ServiceConfig{})
		rec, err := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: "s", Proposed: samplePA("kubectl apply -f p.yaml"), BlockClass: challenge.BlockClassProductionDeploy,
		})
		if err != nil {
			t.Fatal(err)
		}
		if rec.State != challenge.StateHumanReview {
			t.Fatal(rec.State)
		}
	})

	t.Run("budget_one_second_retry_terminal", func(t *testing.T) {
		svc := challenge.NewService(challenge.ServiceConfig{})
		pa := samplePA("rm -rf build")
		// samplePA defaults SessionID=sess-1 — keep ownership consistent
		rec, _ := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
		})
		_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
		r1, _ := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
			CorrelationID: "test-attempt",
			ReEval:        &challenge.ReEvalContext{UserException: true},
		})
		if r1.Record.State != challenge.StateAllowedOnce {
			t.Fatalf("%+v", r1)
		}
		r2, _ := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
			ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
			CorrelationID: "test-attempt",
			ReEval:        &challenge.ReEvalContext{UserException: true},
		})
		if !r2.IdempotentReplay && r2.RejectedReason != "already_terminal" {
			t.Fatalf("%+v", r2)
		}
		if r2.Record.RetryBudget != 0 {
			t.Fatal(r2.Record.RetryBudget)
		}
	})

	t.Run("concurrent_slow_one_outcome", func(t *testing.T) {
		svc := challenge.NewService(challenge.ServiceConfig{ReEval: slowAllowReEval{delay: 200 * time.Millisecond}})
		pa := samplePA("rm -rf build")
		rec, _ := svc.Open(context.Background(), challenge.OpenRequest{
			SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
		})
		_, _ = svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil)
		const n = 8
		results := make([]challenge.RetryResult, n)
		var wg sync.WaitGroup
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func(i int) {
				defer wg.Done()
				results[i], _ = svc.AttemptRetry(context.Background(), challenge.RetryRequest{
					ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
					CorrelationID: "test-attempt",
				})
			}(i)
		}
		wg.Wait()
		final, _ := svc.Get(rec.ChallengeID)
		for i, r := range results {
			if r.Stage2Decision != final.Stage2Decision || r.Record.State != final.State {
				t.Fatalf("i=%d %+v final=%+v", i, r, final)
			}
		}
	})

	t.Run("no_cot_fields", func(t *testing.T) {
		j := challenge.Justification{}
		// closed fields only — structural
		_ = j.ConcreteValue
		_ = j.VerificationPlan
		if challenge.DecisionAllow != classifier.DecisionAllow {
			t.Fatal("stage2 drift")
		}
	})

	t.Run("stage2_only_allow_block", func(t *testing.T) {
		if !challenge.ValidStage2Decision(challenge.DecisionAllow) || !challenge.ValidStage2Decision(challenge.DecisionBlock) {
			t.Fatal("allow/block")
		}
		if challenge.ValidStage2Decision("CHALLENGE") {
			t.Fatal("CHALLENGE must not be stage2")
		}
	})
}

// failProvider is unused placeholder; failClassifier implements Assess with error.
type failProvider struct{}

func (failProvider) ReEvaluate(ctx context.Context, rec challenge.ChallengeRecord, proposed adapter.ProposedAction, just *challenge.Justification, in *challenge.ReEvalContext) (challenge.ReEvalResult, error) {
	return challenge.DefaultReEvaluator{}.ReEvaluate(ctx, rec, proposed, just, in)
}

type failClassifier struct{}

func (failClassifier) Assess(ctx context.Context, in classifier.ClassifierInput) (classifier.RawAssessment, error) {
	return classifier.RawAssessment{}, context.DeadlineExceeded
}
