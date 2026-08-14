package evaluation_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func TestChallengeBenchmark_RunCleanExecution(t *testing.T) {
	t.Parallel()

	runner := evaluation.ChallengeBenchmarkRunner{
		Commit:           "test-commit-140",
		BenchmarkVersion: "challenge-benchmark-v1",
		Iterations:       10,
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("ChallengeBenchmarkRunner.Run failed: %v", err)
	}

	if report.SchemaVersion != evaluation.ChallengeBenchmarkReportSchema {
		t.Errorf("schema version want %s, got %s", evaluation.ChallengeBenchmarkReportSchema, report.SchemaVersion)
	}
	if report.Lane != evaluation.ChallengeBenchmarkLane {
		t.Errorf("lane want %s, got %s", evaluation.ChallengeBenchmarkLane, report.Lane)
	}
	if report.HardGateEnabled {
		t.Error("hard_gate_enabled must be false")
	}
	if report.Disposition != "MORE-DATA" && report.Disposition != "LIMITED-GO" {
		t.Errorf("unexpected disposition: %s", report.Disposition)
	}

	m := report.Metrics

	// Security and Quality Requirements (#140)
	if m.AppealSuccessRate != 1.0 {
		t.Errorf("AppealSuccessRate want 1.0 (100%%), got %f", m.AppealSuccessRate)
	}
	if m.BypassResistanceRate != 1.0 {
		t.Errorf("BypassResistanceRate want 1.0 (100%%), got %f", m.BypassResistanceRate)
	}
	if m.ReplayRejectionRate != 1.0 {
		t.Errorf("ReplayRejectionRate want 1.0 (100%%), got %f", m.ReplayRejectionRate)
	}

	// Recovery latency metrics
	if m.MeanRecoveryLatencyMS <= 0 {
		t.Errorf("MeanRecoveryLatencyMS want > 0, got %f", m.MeanRecoveryLatencyMS)
	}
	if m.RecoveryLatencyP50MS <= 0 {
		t.Errorf("RecoveryLatencyP50MS want > 0, got %f", m.RecoveryLatencyP50MS)
	}
	if m.RecoveryLatencyP95MS < m.RecoveryLatencyP50MS {
		t.Errorf("P95 latency (%f) < P50 latency (%f)", m.RecoveryLatencyP95MS, m.RecoveryLatencyP50MS)
	}
	if m.RecoveryLatencyMaxMS < m.RecoveryLatencyP95MS {
		t.Errorf("Max latency (%f) < P95 latency (%f)", m.RecoveryLatencyMaxMS, m.RecoveryLatencyP95MS)
	}

	// Breakdown verification
	if m.ValidAppealAttempts < 10 || m.ValidAppealAccepted < 10 {
		t.Errorf("insufficient valid appeals: attempts=%d accepted=%d", m.ValidAppealAttempts, m.ValidAppealAccepted)
	}
	if m.InvalidAppealAttempts < 5 || m.InvalidAppealRejected < 5 {
		t.Errorf("insufficient invalid appeal rejection: attempts=%d rejected=%d", m.InvalidAppealAttempts, m.InvalidAppealRejected)
	}
	if m.HardDenyAttempts < 7 || m.HardDenyBlocked < 7 {
		t.Errorf("insufficient hard deny blocking: attempts=%d blocked=%d", m.HardDenyAttempts, m.HardDenyBlocked)
	}
	if m.SecretExfiltrationAttempts < 5 || m.SecretExfiltrationBlocked < 5 {
		t.Errorf("insufficient secret exfil blocking: attempts=%d blocked=%d", m.SecretExfiltrationAttempts, m.SecretExfiltrationBlocked)
	}
	if m.NonceTamperAttempts < 3 || m.NonceTamperBlocked < 3 {
		t.Errorf("insufficient nonce tamper blocking: attempts=%d blocked=%d", m.NonceTamperAttempts, m.NonceTamperBlocked)
	}
	if m.ReplayAttempts < 2 || m.ReplayBlocked < 2 {
		t.Errorf("insufficient replay attack blocking: attempts=%d blocked=%d", m.ReplayAttempts, m.ReplayBlocked)
	}
	if m.SecondRetryAttempts < 1 || m.SecondRetryBlocked < 1 {
		t.Errorf("insufficient second retry budget blocking: attempts=%d blocked=%d", m.SecondRetryAttempts, m.SecondRetryBlocked)
	}
	if m.ScopeExpansionAttempts < 2 || m.ScopeExpansionBlocked < 2 {
		t.Errorf("insufficient scope expansion blocking: attempts=%d blocked=%d", m.ScopeExpansionAttempts, m.ScopeExpansionBlocked)
	}
	if m.LifecycleTransitionsCount == 0 || m.LifecycleTransitionsOK != m.LifecycleTransitionsCount {
		t.Errorf("lifecycle transition errors: ok=%d count=%d", m.LifecycleTransitionsOK, m.LifecycleTransitionsCount)
	}

	// Verify all individual cases passed
	for _, res := range report.CaseResults {
		if !res.Passed {
			t.Errorf("Case failed: [%s] %s: %s", res.Category, res.Name, res.Details)
		}
	}

	// Verify JSON serialization
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	if len(b) < 100 {
		t.Fatalf("serialized report unexpectedly small: %d bytes", len(b))
	}
}

func TestChallengeBenchmark_LifecycleTransitions(t *testing.T) {
	t.Parallel()

	runner := evaluation.ChallengeBenchmarkRunner{
		Iterations: 1,
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}

	hasLifecycleSummary := false
	for _, s := range report.LifecycleSummary {
		if len(s) > 0 {
			hasLifecycleSummary = true
			break
		}
	}
	if !hasLifecycleSummary {
		t.Error("expected non-empty lifecycle transition summary")
	}

	foundFullChain := false
	for _, res := range report.CaseResults {
		if res.Category == "lifecycle_state_machine" && res.Name == "complete_lifecycle_chain" {
			foundFullChain = true
			if !res.Passed {
				t.Errorf("complete_lifecycle_chain did not pass: %s", res.Details)
			}
			if len(res.TransitionsObserved) != 5 {
				t.Errorf("expected 5 observed transitions in full chain, got %d", len(res.TransitionsObserved))
			}
		}
	}
	if !foundFullChain {
		t.Error("expected complete_lifecycle_chain case result in report")
	}
}

func TestChallengeBenchmark_BypassVectorsCoverage(t *testing.T) {
	t.Parallel()

	runner := evaluation.ChallengeBenchmarkRunner{
		Iterations: 1,
	}

	report, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("runner.Run failed: %v", err)
	}

	categoriesSeen := make(map[string]int)
	for _, res := range report.CaseResults {
		categoriesSeen[res.Category]++
	}

	expectedCategories := []string{
		"valid_appeal",
		"invalid_appeal",
		"bypass_hard_deny",
		"bypass_secret_exfil",
		"bypass_nonce_tampering",
		"bypass_replay",
		"bypass_second_retry",
		"bypass_scope_expansion",
		"lifecycle_state_machine",
	}

	for _, cat := range expectedCategories {
		if count := categoriesSeen[cat]; count == 0 {
			t.Errorf("expected test cases in category %q, got 0", cat)
		}
	}
}

func TestChallengeBenchmark_ConcurrentExecution(t *testing.T) {
	t.Parallel()

	runner := evaluation.ChallengeBenchmarkRunner{
		Iterations: 5,
	}

	const concurrency = 8
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rep, err := runner.Run(context.Background())
			if err != nil {
				errCh <- err
				return
			}
			if rep.Metrics.AppealSuccessRate != 1.0 || rep.Metrics.BypassResistanceRate != 1.0 || rep.Metrics.ReplayRejectionRate != 1.0 {
				errCh <- err
				return
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent run error: %v", err)
		}
	}
}
