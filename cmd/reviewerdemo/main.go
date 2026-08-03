// Command reviewerdemo shows when Reinframe uses fixed ZOOM_OUT vs optional
// LLM/reviewer suggested advice (adaptive_task_supervisor two-tier rule).
//
//	go run ./cmd/reviewerdemo
//
// Uses an in-process OpenAI-compatible fixture (no live cloud). High-confidence
// signals never call the provider; uncertain signals use SuggestedAdvice.
//
// Not a multi-role product hard-gate. ADR 003: remote modes require opt-out of
// local_only_reviewer (demo stays loopback fixture).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/ImL1s/reinframe/pkg/config"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/reviewer"
)

func main() {
	fmt.Println("=== Reinframe optional LLM reviewer demo ===")
	fmt.Println("Rule: high-confidence → fixed template (no LLM); uncertain → Reviewer SuggestedAdvice")
	fmt.Println()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content, _ := json.Marshal(map[string]any{
			"classification":    "TUNNEL_VISION",
			"tunnel_confidence": 0.92,
			"rationale":         "fixture: ambiguous vs healthy deep work",
			"suggested_advice":  "SITUATIONAL: restate the hypothesis, list evidence IDs you already have, then pick one new probe.",
		})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": string(content)}},
			},
			"usage": map[string]any{"total_tokens": 17},
		})
	}))
	defer srv.Close()

	// Config shape operators would use for a local OpenAI-compatible endpoint.
	cfg := config.Default()
	cfg.Session.LocalOnlyReviewer = true
	cfg.Reviewer.Mode = "local"
	cfg.Reviewer.BaseURL = srv.URL
	cfg.Reviewer.Model = "fixture-tunnel-classifier"
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("config: mode=%s model=%s local_only=%v base_url=%s\n",
		cfg.Reviewer.Mode, cfg.Reviewer.Model, cfg.Session.LocalOnlyReviewer, cfg.Reviewer.BaseURL)

	prov, err := reviewer.NewProviderFromConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider: %v\n", err)
		os.Exit(1)
	}
	eng := policy.NewEngine(policy.EngineConfig{
		Reviewer:      prov,
		ReviewerModel: cfg.Reviewer.Model,
	})
	ctx := context.Background()

	// A) High-confidence deterministic — must not hit fixture for advice text.
	fmt.Println("--- A) High-confidence repeated_error_loop (deterministic template) ---")
	resA, err := eng.EvaluateSlow(ctx, policy.SlowInput{
		Signal: &protocol.TunnelSignal{
			SignalID:     "sig-det",
			SessionID:    "demo",
			DetectorName: detector.DetectorNameRepeatedFailure,
			FailureMode:  detector.FailureModeRepeatedErrorLoop,
			Score:        1.0,
			TriggeredAt:  time.Now().UTC(),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "A: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  used_reviewer=%v reason=%s\n", resA.UsedReviewer, resA.Reason)
	if resA.Intervention != nil {
		fmt.Printf("  advice=%q\n", resA.Intervention.AdvicePrompt)
	}
	if resA.UsedReviewer {
		fmt.Fprintln(os.Stderr, "FAIL: high-confidence must not use reviewer")
		os.Exit(1)
	}

	// B) Uncertain — LLM/fixture suggested advice.
	fmt.Println("--- B) Uncertain signal (reviewer SuggestedAdvice) ---")
	resB, err := eng.EvaluateSlow(ctx, policy.SlowInput{
		Uncertain: true,
		Signal: &protocol.TunnelSignal{
			SignalID:     "sig-unc",
			SessionID:    "demo",
			DetectorName: "AmbiguousDetector",
			FailureMode:  "ambiguous_progress",
			Score:        0.4,
			TriggeredAt:  time.Now().UTC(),
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "B: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  used_reviewer=%v reason=%s\n", resB.UsedReviewer, resB.Reason)
	if resB.Intervention != nil {
		fmt.Printf("  advice=%q\n", resB.Intervention.AdvicePrompt)
	}
	if !resB.UsedReviewer || resB.Intervention == nil {
		fmt.Fprintln(os.Stderr, "FAIL: expected reviewer advice")
		os.Exit(1)
	}
	if resB.Intervention.AdvicePrompt == policy.DefaultZoomOutAdvice {
		fmt.Fprintln(os.Stderr, "FAIL: expected situational advice, got default template")
		os.Exit(1)
	}

	// C) Remote blocked by local_only (honesty).
	fmt.Println("--- C) ADR 003: remote mode blocked while local_only=true ---")
	cfgRemote := config.Default()
	cfgRemote.Session.LocalOnlyReviewer = true
	cfgRemote.Reviewer.Mode = "openai_compatible"
	cfgRemote.Reviewer.BaseURL = "https://api.example.com/v1"
	cfgRemote.Reviewer.Model = "gpt-x"
	_, err = reviewer.NewProviderFromConfig(cfgRemote)
	if err == nil {
		fmt.Fprintln(os.Stderr, "FAIL: expected remote blocked")
		os.Exit(1)
	}
	fmt.Printf("  err=%v\n", err)

	fmt.Println()
	fmt.Println("=== reviewerdemo OK ===")
	fmt.Println("Honesty: fixture HTTP only; not multi-role product gate; not always-on LLM.")
}
