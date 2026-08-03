package policy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/reviewer"
)

// High-confidence signal must not call OpenAI-compatible provider either.
func TestEvaluateSlow_HighConfidence_ZeroReviewerHTTPCalls(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "should not be called", 500)
	}))
	t.Cleanup(srv.Close)
	p, err := reviewer.NewOpenAICompatible(reviewer.OpenAICompatibleConfig{
		BaseURL: srv.URL, Model: "m", Path: "/chat/completions",
	})
	if err != nil {
		t.Fatal(err)
	}
	eng := policy.NewEngine(policy.EngineConfig{Reviewer: p, ReviewerModel: "m"})
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{
		Signal: &protocol.TunnelSignal{
			SignalID: "sig-hc", SessionID: "s",
			DetectorName: detector.DetectorNameRepeatedFailure,
			FailureMode:  detector.FailureModeRepeatedErrorLoop,
			Score:        1, TriggeredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.UsedReviewer || calls != 0 {
		t.Fatalf("used=%v http_calls=%d res=%+v", res.UsedReviewer, calls, res)
	}
	if !strings.Contains(res.Intervention.AdvicePrompt, "ZOOM_OUT") {
		t.Fatalf("want default template, got %q", res.Intervention.AdvicePrompt)
	}
}

func TestEvaluateSlow_Uncertain_UsesHTTPSuggestedAdvice(t *testing.T) {
	t.Parallel()
	const wantAdvice = "Situational: drop the patch loop; inspect root cause in package X first."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, _ := json.Marshal(map[string]any{
			"classification":    "TUNNEL_VISION",
			"tunnel_confidence": 0.93,
			"rationale":         "ambiguous vs healthy deep work",
			"suggested_advice":  wantAdvice,
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": string(content)}}},
		})
	}))
	t.Cleanup(srv.Close)
	p, err := reviewer.NewOpenAICompatible(reviewer.OpenAICompatibleConfig{
		BaseURL: srv.URL, Model: "fixture-model", Path: "/chat/completions",
	})
	if err != nil {
		t.Fatal(err)
	}
	eng := policy.NewEngine(policy.EngineConfig{Reviewer: p, ReviewerModel: "fixture-model"})
	// Weak/unknown mode so deterministic short-circuit does not apply without Uncertain.
	res, err := eng.EvaluateSlow(context.Background(), policy.SlowInput{
		Uncertain: true,
		Signal: &protocol.TunnelSignal{
			SignalID: "sig-u", SessionID: "s",
			DetectorName: "CustomAmbiguous",
			FailureMode:  "ambiguous_progress",
			Score:        0.5, TriggeredAt: time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.UsedReviewer {
		t.Fatal("expected reviewer")
	}
	if res.Reason != "reviewer_tunnel_suggested_advice" {
		t.Fatalf("reason=%s", res.Reason)
	}
	if res.Intervention == nil || res.Intervention.AdvicePrompt != wantAdvice {
		t.Fatalf("advice=%q want %q", res.Intervention.AdvicePrompt, wantAdvice)
	}
	if res.ReviewDecision == nil || res.ReviewDecision.SuggestedAdvice != wantAdvice {
		t.Fatalf("decision=%+v", res.ReviewDecision)
	}
}
