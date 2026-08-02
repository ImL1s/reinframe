package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/state"
)

// ============================================================================
// Tier 4: Scenario persistence workloads (hand-built protocol events → store/query).
// These are NOT full Anti-Tunnel E2E: no live Detector, Reviewer, Policy Engine,
// Intervention Executor, Git rollback, or Supervisor Orchestrator is executed.
// ============================================================================

// Scenario 1: Hand-built L3 lifecycle events persisted and queried in order.
func TestTier4_Scenario1_UnattendedHighControlAgentLifecycle(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	sessionID := "sess-realworld-l3-lifecycle"
	agentID := "agent-autonomous-claude"

	// 1. Handshake Request requesting Level 3 with Level 3 capabilities
	manifest := protocol.FromBitmask(protocol.Level3RequiredMask)
	manifest.AgentID = agentID
	manifest.Version = "2.5.0"

	req := &protocol.HandshakeRequest{
		SessionID:      sessionID,
		RequestedLevel: 3,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("Handshake negotiation failed: %v", err)
	}
	if resp.NegotiatedLevel != 3 || resp.IsDegraded {
		t.Fatalf("Expected non-degraded Level 3 negotiation, got level %d (degraded=%v)", resp.NegotiatedLevel, resp.IsDegraded)
	}

	// 2. Persist Session Initialization (Seq #1)
	sess := &protocol.AgentSession{
		SessionID:        sessionID,
		AgentID:          agentID,
		AdapterType:      "claude_code",
		IntegrationLevel: resp.NegotiatedLevel,
		WorkspacePath:    "/workspace/reinframe",
		Status:           "active",
		StartedAt:        time.Now().UTC(),
	}
	sessBytes, _ := json.Marshal(sess)

	err = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-seq-1-session",
		SessionID:   sessionID,
		SequenceNum: 1,
		EventType:   "agent_session",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(sessBytes),
	})
	if err != nil {
		t.Fatalf("Failed to append session init event: %v", err)
	}

	// 3. Stream 20 ToolCall and FileChange events (Seq #2 - #21)
	var currentSeq int64 = 2
	for i := 1; i <= 10; i++ {
		toolCall := &protocol.ToolCallEvent{
			ToolCallID: fmt.Sprintf("tc-exec-%d", i),
			ToolName:   "write_file",
			Arguments:  map[string]any{"path": fmt.Sprintf("pkg/feature_%d.go", i), "content": "package main"},
			ExitCode:   ptr(0),
			DurationMs: int64(100 * i),
		}
		tcBytes, _ := json.Marshal(toolCall)

		err = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-seq-%d-tool", currentSeq),
			SessionID:   sessionID,
			SequenceNum: currentSeq,
			EventType:   "tool_call_event",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(tcBytes),
		})
		if err != nil {
			t.Fatalf("Failed to append tool event at seq %d: %v", currentSeq, err)
		}
		currentSeq++

		fileChange := &protocol.FileChangeEvent{
			FilePath:         fmt.Sprintf("pkg/feature_%d.go", i),
			ChangeType:       "created",
			LinesAdded:       25,
			LinesRemoved:     0,
			IsScopeViolation: false,
		}
		fcBytes, _ := json.Marshal(fileChange)

		err = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-seq-%d-file", currentSeq),
			SessionID:   sessionID,
			SequenceNum: currentSeq,
			EventType:   "file_change_event",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(fcBytes),
		})
		if err != nil {
			t.Fatalf("Failed to append file event at seq %d: %v", currentSeq, err)
		}
		currentSeq++
	}

	// 4. Harness logs ProviderUsage and BudgetState (Seq #22 - #23)
	usage := &protocol.ProviderUsage{
		UsageID:          "usage-l3-total",
		SessionID:        sessionID,
		ProviderName:     "anthropic",
		Model:            "claude-3-5-sonnet",
		PromptTokens:     15000,
		CompletionTokens: 3500,
		TotalTokens:      18500,
		EstimatedCostUSD: 0.0975,
		Timestamp:        time.Now().UTC(),
	}
	uBytes, _ := json.Marshal(usage)
	err = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-seq-22-usage",
		SessionID:   sessionID,
		SequenceNum: 22,
		EventType:   "provider_usage",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(uBytes),
	})
	if err != nil {
		t.Fatalf("Failed to append usage event: %v", err)
	}

	budget := &protocol.BudgetState{
		SessionID:         sessionID,
		MaxTokens:         100000,
		UsedTokens:        18500,
		MaxCostUSD:        5.00,
		CurrentCostUSD:    0.0975,
		MaxInterventions:  10,
		InterventionCount: 0,
		IsExhausted:       false,
	}
	bBytes, _ := json.Marshal(budget)
	err = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-seq-23-budget",
		SessionID:   sessionID,
		SequenceNum: 23,
		EventType:   "budget_state",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(bBytes),
	})
	if err != nil {
		t.Fatalf("Failed to append budget event: %v", err)
	}

	// 5. Harness triggers Checkpoint event (Seq #24)
	ckpt := &protocol.Checkpoint{
		CheckpointID:     "ckpt-final-l3",
		SessionID:        sessionID,
		GitCommitHash:    "f8e7d6c5b4a39281",
		BranchName:       "main",
		Description:      "Completed 10 feature files cleanly",
		PassingTestCount: 10,
		CreatedAt:        time.Now().UTC(),
	}
	ckptBytes, _ := json.Marshal(ckpt)
	err = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-seq-24-checkpoint",
		SessionID:   sessionID,
		SequenceNum: 24,
		EventType:   "checkpoint",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(ckptBytes),
	})
	if err != nil {
		t.Fatalf("Failed to append checkpoint event: %v", err)
	}

	// 6. Verify Latest Sequence Number is 24
	latestSeq, err := store.GetLatestSequenceNum(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	if latestSeq != 24 {
		t.Fatalf("Expected latest sequence 24, got %d", latestSeq)
	}

	// 7. Audit Trail Replay Verification
	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: sessionID,
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents audit replay failed: %v", err)
	}
	if len(events) != 24 {
		t.Fatalf("Expected 24 total audit trail events, got %d", len(events))
	}

	// Assert strict sequence contiguity across all 24 events
	for idx, evt := range events {
		expectedSeq := int64(idx + 1)
		if evt.SequenceNum != expectedSeq {
			t.Errorf("Sequence gap at audit index %d: got %d, expected %d", idx, evt.SequenceNum, expectedSeq)
		}
	}
}

// Scenario 2: Restricted Legacy Agent Graceful Degradation (L3 -> L1)
func TestTier4_Scenario2_RestrictedLegacyAgentDegradation(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	sessionID := "sess-realworld-legacy-degradation"
	agentID := "legacy-agent-v1"

	// 1. Handshake Request: Legacy agent requests L3, but only has L1 manifest capabilities
	manifest := protocol.FromBitmask(protocol.Level1RequiredMask)
	manifest.AgentID = agentID
	manifest.Version = "1.0.0"

	req := &protocol.HandshakeRequest{
		SessionID:      sessionID,
		RequestedLevel: 3,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("NegotiateLevel failed: %v", err)
	}
	if !resp.IsDegraded || resp.NegotiatedLevel != 1 || resp.DegradedFrom != 3 {
		t.Fatalf("Expected degradation L3->L1: negotiated=%d degraded=%v from=%d",
			resp.NegotiatedLevel, resp.IsDegraded, resp.DegradedFrom)
	}
	if len(resp.MissingFlags) == 0 {
		t.Fatalf("Expected MissingFlags list for L3 requirements")
	}

	// 2. Log Handshake Degradation Event (Seq #1)
	respBytes, _ := json.Marshal(resp)
	err = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-legacy-seq-1",
		SessionID:   sessionID,
		SequenceNum: 1,
		EventType:   "handshake_degraded",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(respBytes),
	})
	if err != nil {
		t.Fatalf("Failed to log degradation event: %v", err)
	}

	// 3. Agent operates under Level 1 (Advisory mode): 5 tool calls + advisory decisions (Seq #2 - #6)
	for i := 2; i <= 6; i++ {
		decision := &protocol.ReviewDecision{
			DecisionID:       fmt.Sprintf("dec-%d", i),
			RequestID:        fmt.Sprintf("req-%d", i),
			ReviewerRole:     "adviser",
			TunnelConfidence: 0.1,
			Classification:   "normal",
			Rationale:        "Tool call within advisory threshold",
			SuggestedAdvice:  "Proceed with care",
			DecidedAt:        time.Now().UTC(),
		}
		dBytes, _ := json.Marshal(decision)

		err = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-legacy-seq-%d", i),
			SessionID:   sessionID,
			SequenceNum: int64(i),
			EventType:   "review_decision",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(dBytes),
		})
		if err != nil {
			t.Fatalf("Failed to append advisory decision at seq %d: %v", i, err)
		}
	}

	// 4. Harness attempts forbidden Level 2 intervention (Pause/Rollback); intervention is BLOCKED due to Level 1
	blockedIntervention := &protocol.Intervention{
		InterventionID: "inv-blocked-101",
		SessionID:      sessionID,
		Level:          2,
		ActionType:     "pause_and_rollback",
		Status:         "blocked_by_level_constraint",
		ExecutedAt:     time.Now().UTC(),
	}
	biBytes, _ := json.Marshal(blockedIntervention)
	err = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-legacy-seq-7-blocked",
		SessionID:   sessionID,
		SequenceNum: 7,
		EventType:   "intervention_blocked",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(biBytes),
	})
	if err != nil {
		t.Fatalf("Failed to log blocked intervention event: %v", err)
	}

	// 5. Query Audit Trail
	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: sessionID,
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 7 {
		t.Fatalf("Expected 7 total events in legacy session, got %d", len(events))
	}
	if events[0].EventType != "handshake_degraded" {
		t.Errorf("First event should be handshake_degraded, got %s", events[0].EventType)
	}
	if events[6].EventType != "intervention_blocked" {
		t.Errorf("Final event should be intervention_blocked, got %s", events[6].EventType)
	}
}

// Scenario 3: Anomaly Detection, Tunnel Intervention & State Rollback (L2 Guarded Workflow)
// Scenario 3: Hand-built tunnel_signal / assessment / intervention / rollback_result
// records are written and re-read. Does not run detectors, policy, or real Git rollback.
func TestTier4_Scenario3_AnomalyDetectionInterventionRollback(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	sessionID := "sess-realworld-l2-anomaly-rollback"

	// 1. Negotiate Level 2 Guarded mode
	req := &protocol.HandshakeRequest{
		SessionID:      sessionID,
		RequestedLevel: 2,
		Manifest:       protocol.FromBitmask(protocol.Level2RequiredMask),
	}
	resp, err := protocol.NegotiateLevel(req)
	if err != nil || resp.NegotiatedLevel != 2 {
		t.Fatalf("NegotiateLevel L2 failed: negotiated=%d err=%v", resp.NegotiatedLevel, err)
	}

	// Seq #1: Session Init
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e1-sess-init",
		SessionID:   sessionID,
		SequenceNum: 1,
		EventType:   "agent_session",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{"status":"started"}`),
	})

	// Seq #2 - #6: Repeating error loop (FileChange & TestResult failures)
	for i := 2; i <= 6; i++ {
		testFail := &protocol.TestResultEvent{
			TestRunID:     fmt.Sprintf("trun-fail-%d", i),
			Command:       "go test ./...",
			PassedCount:   10,
			FailedCount:   3,
			FailureOutput: "FAIL: TestDatabaseLock (deadlock detected)",
			DurationMs:    800,
		}
		tfBytes, _ := json.Marshal(testFail)

		_ = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("e%d-test-fail", i),
			SessionID:   sessionID,
			SequenceNum: int64(i),
			EventType:   "test_result",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(tfBytes),
		})
	}

	// Seq #7: Anomaly detector emits TunnelSignal
	signal := &protocol.TunnelSignal{
		SignalID:     "sig-loop-01",
		SessionID:    sessionID,
		DetectorName: "repeating_test_failure_detector",
		FailureMode:  "infinite_error_loop",
		Weight:       0.9,
		Score:        0.95,
		TriggeredAt:  time.Now().UTC(),
	}
	sigBytes, _ := json.Marshal(signal)
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e7-tunnel-signal",
		SessionID:   sessionID,
		SequenceNum: 7,
		EventType:   "tunnel_signal",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(sigBytes),
	})

	// Seq #8: Aggregate TunnelAssessment
	assess := &protocol.TunnelAssessment{
		AssessmentID:       "assess-01",
		SessionID:          sessionID,
		AggregateScore:     0.95,
		PrimaryFailureMode: "infinite_error_loop",
		IsTunnelDetected:   true,
		RecommendedAction:  "pause_and_rollback",
		EvaluatedAt:        time.Now().UTC(),
	}
	assBytes, _ := json.Marshal(assess)
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e8-tunnel-assessment",
		SessionID:   sessionID,
		SequenceNum: 8,
		EventType:   "tunnel_assessment",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(assBytes),
	})

	// Seq #9: Harness executes Intervention (Level 2 Pause)
	interv := &protocol.Intervention{
		InterventionID:     "inv-pause-01",
		SessionID:          sessionID,
		Level:              2,
		ActionType:         "pause",
		AdvicePrompt:       "Agent paused due to infinite error loop. Rolling back git state.",
		TargetCheckpointID: "ckpt-stable-001",
		Status:             "executed",
		ExecutedAt:         time.Now().UTC(),
	}
	invBytes, _ := json.Marshal(interv)
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e9-intervention-pause",
		SessionID:   sessionID,
		SequenceNum: 9,
		EventType:   "intervention",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(invBytes),
	})

	// Seq #10: Git Rollback execution result
	rollback := &protocol.RollbackResult{
		RollbackID:         "rb-01",
		SessionID:          sessionID,
		TargetCheckpointID: "ckpt-stable-001",
		PreviousCommitHash: "b2c3d4e5f6789012",
		RestoredCommitHash: "a1b2c3d4e5f67890",
		Success:            true,
		CompletedAt:        time.Now().UTC(),
	}
	rbBytes, _ := json.Marshal(rollback)
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e10-rollback-result",
		SessionID:   sessionID,
		SequenceNum: 10,
		EventType:   "rollback_result",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(rbBytes),
	})

	// Seq #11: Agent resumed via advice prompt
	resumedInterv := &protocol.Intervention{
		InterventionID: "inv-resume-01",
		SessionID:      sessionID,
		Level:          2,
		ActionType:     "resume",
		AdvicePrompt:   "Git workspace restored to checkpoint ckpt-stable-001. Please try alternative approach.",
		Status:         "executed",
		ExecutedAt:     time.Now().UTC(),
	}
	riBytes, _ := json.Marshal(resumedInterv)
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e11-intervention-resume",
		SessionID:   sessionID,
		SequenceNum: 11,
		EventType:   "intervention",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(riBytes),
	})

	// Seq #12: Session completed status
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e12-session-completed",
		SessionID:   sessionID,
		SequenceNum: 12,
		EventType:   "agent_session",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{"status":"completed"}`),
	})

	// Verify all 12 events queryable in exact sequence
	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: sessionID,
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 12 {
		t.Fatalf("Expected 12 events in anomaly rollback lifecycle, got %d", len(events))
	}

	for idx, evt := range events {
		expectedSeq := int64(idx + 1)
		if evt.SequenceNum != expectedSeq {
			t.Errorf("Discontinuity at index %d: got seq %d, expected %d", idx, evt.SequenceNum, expectedSeq)
		}
	}
}

// Scenario 4: Graceful Close + reopen of the same SQLite file, then replay query.
// This is NOT process-crash / SIGKILL / uncommitted-tx recovery — only orderly Close/NewStore.
func TestTier4_Scenario4_StoreGracefulReopenAndReplay(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	opts := state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5000 * time.Millisecond,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	}

	sessionID := "sess-graceful-reopen-test"

	// 1. Initial Store instance streams 50 events
	s1, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore s1 failed: %v", err)
	}

	ctx := context.Background()
	const totalEvents = 50
	var batch []*protocol.AgentEvent

	for i := 1; i <= totalEvents; i++ {
		batch = append(batch, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-reopen-%d", i),
			SessionID:   sessionID,
			SequenceNum: int64(i),
			EventType:   "audit_record",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(fmt.Sprintf(`{"audit_index":%d,"data":"persisted_before_graceful_close"}`, i)),
		})
	}

	if err := s1.AppendEvents(ctx, batch); err != nil {
		t.Fatalf("AppendEvents 50 items failed: %v", err)
	}

	// 2. Graceful Close (not process crash / SIGKILL)
	if err := s1.Close(); err != nil {
		t.Fatalf("s1 Close failed: %v", err)
	}

	// 3. Re-open the same DB file (graceful reopen; SQLite may apply clean WAL if any)
	s2, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore s2 graceful reopen failed: %v", err)
	}
	defer s2.Close()

	// 4. Verify Latest Sequence Number is 50
	latestSeq, err := s2.GetLatestSequenceNum(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetLatestSequenceNum post-reopen failed: %v", err)
	}
	if latestSeq != totalEvents {
		t.Fatalf("Expected latest sequence %d after reopen, got %d", totalEvents, latestSeq)
	}

	// 5. Query all 50 events and verify zero data loss or corruption
	events, err := s2.QueryEvents(ctx, state.EventFilter{
		SessionID: sessionID,
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents post-reopen failed: %v", err)
	}
	if len(events) != totalEvents {
		t.Fatalf("Expected %d events retrieved after graceful reopen, got %d", totalEvents, len(events))
	}

	for idx, evt := range events {
		expectedSeq := int64(idx + 1)
		if evt.SequenceNum != expectedSeq {
			t.Errorf("Sequence gap post-reopen at index %d: got %d, expected %d", idx, evt.SequenceNum, expectedSeq)
		}
		if evt.EventType != "audit_record" {
			t.Errorf("Corrupted event type at index %d: got %s", idx, evt.EventType)
		}
	}
}

func ptr[T any](v T) *T {
	return &v
}
