package challenge_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/challenge"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

// JUSTIFIED → provider 429 → injected Sleep ordinary error → PRODUCTIVITY
// must never mint ALLOWED_ONCE. Sleep failure must not masquerade as successful assessment.
func TestAttemptRetry_SleepErrorAfter429NeverAllowedOnce(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
		Model: "m", BaseURL: srv.URL, Path: "/v1/chat/completions", AllowRemote: true,
		HTTPClient: srv.Client(), Timeout: 2 * time.Second, MaxRetries: 2,
		Sleep: func(ctx context.Context, d time.Duration) error {
			// Ordinary runtime failure while both contexts remain live.
			return errors.New("injected sleep failure")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Direct Assess path: sleep error must be non-nil typed failure, not empty success.
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
	})
	if err != nil {
		t.Fatal(err)
	}
	req.Timeout = 2 * time.Second
	res, aerr := p.Assess(context.Background(), req)
	if aerr == nil {
		t.Fatalf("Assess after sleep fail must not succeed: %+v", res)
	}
	var pe *classifier.ProviderError
	if !errors.As(aerr, &pe) || pe.Class != "transport" {
		t.Fatalf("want transport ProviderError, got %v (%T)", aerr, aerr)
	}

	// End-to-end challenge: JUSTIFIED + PRODUCTIVITY + same provider must not ALLOWED_ONCE.
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
		PolicyClass: classifier.PolicyClassProductivity,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil); err != nil {
		t.Fatal(err)
	}
	retry, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "sleep-fail-e2e",
		ReEval: &challenge.ReEvalContext{
			Provider:    p,
			PolicyClass: classifier.PolicyClassProductivity,
		},
	})
	if retry.Record.State == challenge.StateAllowedOnce {
		t.Fatalf("sleep error must not mint ALLOWED_ONCE: %+v err=%v", retry, err)
	}
	if retry.Stage2Decision == challenge.DecisionAllow {
		t.Fatalf("sleep error must not Stage2 ALLOW for one-shot: %+v", retry)
	}
	got, _ := svc.Get(rec.ChallengeID)
	if got.State == challenge.StateAllowedOnce {
		t.Fatal("store must not be ALLOWED_ONCE after sleep-fail provider path")
	}
	if hits < 1 {
		t.Fatal("provider never hit 429 path")
	}
}

func TestAttemptRetry_ProviderFailOpenNeverAllowedOnce(t *testing.T) {
	t.Parallel()
	// Explicit transport-class provider error on PRODUCTIVITY: fail-open policy may
	// surface as ALLOW at Stage2 for shadow, but AttemptRetry must reject one-shot.
	fail := failClassifier{}
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("rm -rf build")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil); err != nil {
		t.Fatal(err)
	}
	retry, _ := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "fail-open-no-once",
		ReEval: &challenge.ReEvalContext{
			Provider:    fail,
			PolicyClass: classifier.PolicyClassProductivity,
		},
	})
	if retry.Record.State == challenge.StateAllowedOnce || retry.Stage2Decision == challenge.DecisionAllow {
		t.Fatalf("provider fail-open must not ALLOWED_ONCE: %+v", retry)
	}
}

func TestAttemptRetry_GenuineBelowThresholdStillAllowedOnce(t *testing.T) {
	t.Parallel()
	// Control: successful Fake clear_allow assessment may still ALLOWED_ONCE.
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := samplePA("echo hi")
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Justify(context.Background(), validJustification(rec.ChallengeID, nil), nil); err != nil {
		t.Fatal(err)
	}
	retry, err := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "genuine-allow",
		ReEval: &challenge.ReEvalContext{
			Provider:    classifier.FakeClassifierProvider{},
			FixtureName: "clear_allow",
			PolicyClass: classifier.PolicyClassProductivity,
			Threshold:   50,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Record.State != challenge.StateAllowedOnce || retry.Stage2Decision != challenge.DecisionAllow {
		t.Fatalf("genuine below-threshold allow must ALLOWED_ONCE: %+v", retry)
	}
}
