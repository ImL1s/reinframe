package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/state"
)

// ============================================================================
// Tier 3: Cross-Feature Pairwise Interaction Suite
// Combining Handshake Level Negotiation (pkg/protocol) with SQLite WAL Event Store (pkg/state)
// ============================================================================

// 1. Handshake to Store Session Initialization
func TestTier3_Pairwise_HandshakeToStore_SessionInit(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	manifest := protocol.FromBitmask(protocol.Level3RequiredMask)
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-pairwise-init",
		RequestedLevel: 3,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("NegotiateLevel failed: %v", err)
	}
	if resp.NegotiatedLevel != 3 {
		t.Fatalf("Expected negotiated level 3, got %d", resp.NegotiatedLevel)
	}

	sess := &protocol.AgentSession{
		SessionID:        resp.SessionID,
		AgentID:          "agent-pair-1",
		AdapterType:      "claude_code",
		IntegrationLevel: resp.NegotiatedLevel,
		WorkspacePath:    "/work/repo",
		Status:           "active",
		StartedAt:        time.Now().UTC(),
	}
	sessBytes, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("Failed to marshal AgentSession: %v", err)
	}

	evt := &protocol.AgentEvent{
		EventID:     "evt-init-001",
		SessionID:   resp.SessionID,
		SequenceNum: 1,
		EventType:   "agent_session",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(sessBytes),
	}

	if err := store.AppendEvent(ctx, evt); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: resp.SessionID})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 persisted event, got %d", len(events))
	}
	if events[0].SequenceNum != 1 || events[0].EventType != "agent_session" {
		t.Errorf("Event mismatch: seq=%d type=%s", events[0].SequenceNum, events[0].EventType)
	}
}

// 2. Degraded Handshake Audit Logging
func TestTier3_Pairwise_DegradedHandshakeLogging(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	// Manifest missing L3 capabilities -> degrades L3 to L2
	manifest := protocol.FromBitmask(protocol.Level2RequiredMask)
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-pairwise-degraded",
		RequestedLevel: 3,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("NegotiateLevel failed: %v", err)
	}
	if !resp.IsDegraded || resp.NegotiatedLevel != 2 {
		t.Fatalf("Expected degradation to level 2, got level %d (degraded=%v)", resp.NegotiatedLevel, resp.IsDegraded)
	}

	respBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal HandshakeResponse: %v", err)
	}

	degradeEvt := &protocol.AgentEvent{
		EventID:     "evt-degrade-001",
		SessionID:   resp.SessionID,
		SequenceNum: 1,
		EventType:   "handshake_degraded",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(respBytes),
	}

	if err := store.AppendEvent(ctx, degradeEvt); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: resp.SessionID})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}

	var restoredResp protocol.HandshakeResponse
	if err := json.Unmarshal(events[0].Payload, &restoredResp); err != nil {
		t.Fatalf("Failed to unmarshal restored response: %v", err)
	}
	if !restoredResp.IsDegraded || restoredResp.DegradedFrom != 3 || restoredResp.NegotiatedLevel != 2 {
		t.Errorf("Restored HandshakeResponse mismatch: degraded=%v from=%d level=%d",
			restoredResp.IsDegraded, restoredResp.DegradedFrom, restoredResp.NegotiatedLevel)
	}
}

// 3. Level 0 Observe-Only Event Stream Persistence
func TestTier3_Pairwise_L0_ObserveOnlyPersistence(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	manifest := protocol.FromBitmask(protocol.Level0RequiredMask)
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-l0-stream",
		RequestedLevel: 0,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil || resp.NegotiatedLevel != 0 {
		t.Fatalf("Expected NegotiatedLevel 0, got %d (err: %v)", resp.NegotiatedLevel, err)
	}

	const streamCount = 50
	var batch []*protocol.AgentEvent
	for i := 1; i <= streamCount; i++ {
		batch = append(batch, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-stream-%d", i),
			SessionID:   resp.SessionID,
			SequenceNum: int64(i),
			EventType:   "agent_event",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(fmt.Sprintf(`{"stream_index":%d}`, i)),
		})
	}

	if err := store.AppendEvents(ctx, batch); err != nil {
		t.Fatalf("AppendEvents stream batch failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: resp.SessionID,
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != streamCount {
		t.Fatalf("Expected %d streamed events, got %d", streamCount, len(events))
	}

	for i, evt := range events {
		expectedSeq := int64(i + 1)
		if evt.SequenceNum != expectedSeq {
			t.Errorf("Sequence discontinuity at index %d: got %d, expected %d", i, evt.SequenceNum, expectedSeq)
		}
	}
}

// 4. Level 2 Guarded Checkpoint & Test Result Persistence
func TestTier3_Pairwise_L2_GuardedCheckpointPersistence(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	manifest := protocol.FromBitmask(protocol.Level2RequiredMask)
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-l2-checkpoint",
		RequestedLevel: 2,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil || resp.NegotiatedLevel != 2 {
		t.Fatalf("Expected Level 2 negotiation, got level %d (err: %v)", resp.NegotiatedLevel, err)
	}

	ckpt := &protocol.Checkpoint{
		CheckpointID:     "ckpt-101",
		SessionID:        resp.SessionID,
		GitCommitHash:    "a1b2c3d4e5f67890",
		BranchName:       "feature/issue-7",
		Description:      "Pre-execution checkpoint",
		PassingTestCount: 42,
		CreatedAt:        time.Now().UTC(),
	}
	ckptBytes, _ := json.Marshal(ckpt)

	testRes := &protocol.TestResultEvent{
		TestRunID:   "trun-201",
		Command:     "go test ./pkg/...",
		PassedCount: 42,
		FailedCount: 0,
		DurationMs:  1250,
	}
	testResBytes, _ := json.Marshal(testRes)

	evt1 := &protocol.AgentEvent{
		EventID:     "evt-ckpt-1",
		SessionID:   resp.SessionID,
		SequenceNum: 1,
		EventType:   "checkpoint",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(ckptBytes),
	}
	evt2 := &protocol.AgentEvent{
		EventID:     "evt-test-2",
		SessionID:   resp.SessionID,
		SequenceNum: 2,
		EventType:   "test_result",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(testResBytes),
	}

	if err := store.AppendEvents(ctx, []*protocol.AgentEvent{evt1, evt2}); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: resp.SessionID,
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}
	if events[0].EventType != "checkpoint" || events[1].EventType != "test_result" {
		t.Errorf("Event types sequence mismatch: got %s then %s", events[0].EventType, events[1].EventType)
	}
}

// 5. Level 3 Full-Control Usage Tracking & Filtered Querying
func TestTier3_Pairwise_L3_FullControlUsageTracking(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	manifest := protocol.FromBitmask(protocol.Level3RequiredMask)
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-l3-usage",
		RequestedLevel: 3,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil || resp.NegotiatedLevel != 3 {
		t.Fatalf("Expected Level 3 negotiation, got %d (err: %v)", resp.NegotiatedLevel, err)
	}

	for i := 1; i <= 5; i++ {
		usage := &protocol.ProviderUsage{
			UsageID:          fmt.Sprintf("use-%d", i),
			SessionID:        resp.SessionID,
			ProviderName:     "anthropic",
			Model:            "claude-3-5-sonnet",
			PromptTokens:     1000 * i,
			CompletionTokens: 200 * i,
			TotalTokens:      1200 * i,
			EstimatedCostUSD: 0.005 * float64(i),
			Timestamp:        time.Now().UTC(),
		}
		uBytes, _ := json.Marshal(usage)

		_ = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-usage-%d", i),
			SessionID:   resp.SessionID,
			SequenceNum: int64(i * 2),
			EventType:   "provider_usage",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(uBytes),
		})

		_ = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-tool-%d", i),
			SessionID:   resp.SessionID,
			SequenceNum: int64(i*2 - 1),
			EventType:   "tool_call",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(`{"tool":"view_file"}`),
		})
	}

	usageEvents, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID:  resp.SessionID,
		EventTypes: []string{"provider_usage"},
	})
	if err != nil {
		t.Fatalf("QueryEvents for provider_usage failed: %v", err)
	}
	if len(usageEvents) != 5 {
		t.Fatalf("Expected 5 provider_usage events, got %d", len(usageEvents))
	}
	for _, e := range usageEvents {
		if e.EventType != "provider_usage" {
			t.Errorf("Unexpected event type in filtered results: %s", e.EventType)
		}
	}
}

// 6. Concurrent Handshakes and Initial Event Appends
func TestTier3_Pairwise_ConcurrentNegotiationAndAppends(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	const numSessions = 20
	var wg sync.WaitGroup
	wg.Add(numSessions)

	for i := 0; i < numSessions; i++ {
		go func(id int) {
			defer wg.Done()

			sid := fmt.Sprintf("sess-concurrent-%d", id)
			requestedLvl := id % 4
			manifest := protocol.FromBitmask(protocol.Level3RequiredMask)

			req := &protocol.HandshakeRequest{
				SessionID:      sid,
				RequestedLevel: requestedLvl,
				Manifest:       manifest,
			}

			resp, err := protocol.NegotiateLevel(req)
			if err != nil {
				t.Errorf("Routine %d NegotiateLevel failed: %v", id, err)
				return
			}

			sess := &protocol.AgentSession{
				SessionID:        resp.SessionID,
				AgentID:          fmt.Sprintf("agent-%d", id),
				IntegrationLevel: resp.NegotiatedLevel,
				Status:           "active",
				StartedAt:        time.Now().UTC(),
			}
			sBytes, _ := json.Marshal(sess)

			evt := &protocol.AgentEvent{
				EventID:     fmt.Sprintf("evt-conc-init-%d", id),
				SessionID:   resp.SessionID,
				SequenceNum: 1,
				EventType:   "agent_session",
				Timestamp:   time.Now().UTC(),
				Payload:     json.RawMessage(sBytes),
			}

			if err := store.AppendEvent(ctx, evt); err != nil {
				t.Errorf("Routine %d AppendEvent failed: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	for i := 0; i < numSessions; i++ {
		sid := fmt.Sprintf("sess-concurrent-%d", i)
		latest, err := store.GetLatestSequenceNum(ctx, sid)
		if err != nil {
			t.Errorf("GetLatestSequenceNum for %s failed: %v", sid, err)
		}
		if latest != 1 {
			t.Errorf("Session %s sequence mismatch: expected 1, got %d", sid, latest)
		}
	}
}

// 7. Store Replay Repopulates Capability Manifest & Re-evaluates Level
func TestTier3_Pairwise_StoreReplayRepopulatesManifest(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	opts := state.StoreOptions{DatabasePath: dbPath, BusyTimeout: 5000 * time.Millisecond}

	s1, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore s1 failed: %v", err)
	}

	ctx := context.Background()
	manifestInput := protocol.FromBitmask(protocol.Level2RequiredMask)
	reqInput := &protocol.HandshakeRequest{
		SessionID:      "sess-replay-manifest",
		RequestedLevel: 2,
		Manifest:       manifestInput,
	}

	respInput, err := protocol.NegotiateLevel(reqInput)
	if err != nil {
		t.Fatalf("NegotiateLevel failed: %v", err)
	}

	reqBytes, _ := json.Marshal(reqInput)
	respBytes, _ := json.Marshal(respInput)

	_ = s1.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-req-1",
		SessionID:   reqInput.SessionID,
		SequenceNum: 1,
		EventType:   "handshake_request",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(reqBytes),
	})
	_ = s1.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-resp-2",
		SessionID:   reqInput.SessionID,
		SequenceNum: 2,
		EventType:   "handshake_response",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(respBytes),
	})

	s1.Close()

	// Re-open store from database file
	s2, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore s2 failed: %v", err)
	}
	defer s2.Close()

	events, err := s2.QueryEvents(ctx, state.EventFilter{
		SessionID:  "sess-replay-manifest",
		EventTypes: []string{"handshake_request"},
	})
	if err != nil || len(events) != 1 {
		t.Fatalf("QueryEvents for handshake_request failed: len=%d err=%v", len(events), err)
	}

	var replayedReq protocol.HandshakeRequest
	if err := json.Unmarshal(events[0].Payload, &replayedReq); err != nil {
		t.Fatalf("Failed to unmarshal replayed handshake request: %v", err)
	}

	reevaluatedLvl := protocol.EvaluateAchievableLevel(&replayedReq.Manifest)
	if reevaluatedLvl != respInput.NegotiatedLevel {
		t.Errorf("Re-evaluated level mismatch: got %d, expected %d", reevaluatedLvl, respInput.NegotiatedLevel)
	}
}

// 8. Filtered Query Replay by Negotiated Level Session Boundaries
func TestTier3_Pairwise_FilteredReplayByNegotiatedLevel(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	sessions := map[string]int{
		"sess-l0": 0,
		"sess-l2": 2,
		"sess-l3": 3,
	}

	for sid, lvl := range sessions {
		var mask uint64
		switch lvl {
		case 0:
			mask = protocol.Level0RequiredMask
		case 2:
			mask = protocol.Level2RequiredMask
		case 3:
			mask = protocol.Level3RequiredMask
		}
		req := &protocol.HandshakeRequest{
			SessionID:      sid,
			RequestedLevel: lvl,
			Manifest:       protocol.FromBitmask(mask),
		}
		resp, _ := protocol.NegotiateLevel(req)

		respBytes, _ := json.Marshal(resp)
		_ = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-%s-1", sid),
			SessionID:   sid,
			SequenceNum: 1,
			EventType:   "handshake_response",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(respBytes),
		})
	}

	// Filter strictly by target L2 session
	l2Events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-l2"})
	if err != nil {
		t.Fatalf("QueryEvents for sess-l2 failed: %v", err)
	}
	if len(l2Events) != 1 {
		t.Fatalf("Expected 1 event for sess-l2, got %d", len(l2Events))
	}
	if l2Events[0].SessionID != "sess-l2" {
		t.Errorf("Cross-session leakage detected: retrieved session %s", l2Events[0].SessionID)
	}
}

// 9. Degradation Event Sequence Contiguity
func TestTier3_Pairwise_DegradationEventSequenceContiguity(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	manifest := protocol.FromBitmask(protocol.Level1RequiredMask)
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-contig-degrade",
		RequestedLevel: 3,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil || !resp.IsDegraded {
		t.Fatalf("Expected degraded negotiation response, got level %d", resp.NegotiatedLevel)
	}

	// Append sequential events: handshake_degraded (seq 1), tunnel_signal (seq 2), intervention (seq 3)
	respBytes, _ := json.Marshal(resp)
	e1 := &protocol.AgentEvent{
		EventID:     "e1",
		SessionID:   resp.SessionID,
		SequenceNum: 1,
		EventType:   "handshake_degraded",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(respBytes),
	}

	sig := &protocol.TunnelSignal{
		SignalID:     "sig-1",
		SessionID:    resp.SessionID,
		DetectorName: "loop_detector",
		Score:        0.85,
		TriggeredAt:  time.Now().UTC(),
	}
	sigBytes, _ := json.Marshal(sig)
	e2 := &protocol.AgentEvent{
		EventID:     "e2",
		SessionID:   resp.SessionID,
		SequenceNum: 2,
		EventType:   "tunnel_signal",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(sigBytes),
	}

	interv := &protocol.Intervention{
		InterventionID: "inv-1",
		SessionID:      resp.SessionID,
		Level:          resp.NegotiatedLevel,
		ActionType:     "prompt_advice",
		AdvicePrompt:   "Stop repeating errors and review stack trace.",
		ExecutedAt:     time.Now().UTC(),
	}
	intervBytes, _ := json.Marshal(interv)
	e3 := &protocol.AgentEvent{
		EventID:     "e3",
		SessionID:   resp.SessionID,
		SequenceNum: 3,
		EventType:   "intervention",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(intervBytes),
	}

	if err := store.AppendEvents(ctx, []*protocol.AgentEvent{e1, e2, e3}); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: resp.SessionID,
		Ascending: true,
	})
	if err != nil || len(events) != 3 {
		t.Fatalf("QueryEvents failed: len=%d err=%v", len(events), err)
	}

	for idx, evt := range events {
		expectedSeq := int64(idx + 1)
		if evt.SequenceNum != expectedSeq {
			t.Errorf("Discontinuous sequence number at index %d: got %d, expected %d", idx, evt.SequenceNum, expectedSeq)
		}
	}
}

// 10. Store persistence after graceful reopen following a degraded handshake session
// (Close + NewStore on the same file — not process-crash recovery).
func TestTier3_Pairwise_StoreRecoveryAfterDegradedHandshake(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	opts := state.StoreOptions{DatabasePath: dbPath, BusyTimeout: 5000 * time.Millisecond}

	s1, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore s1 failed: %v", err)
	}

	ctx := context.Background()
	sid := "sess-wal-degraded-reopen"
	req := &protocol.HandshakeRequest{
		SessionID:      sid,
		RequestedLevel: 3,
		Manifest:       protocol.FromBitmask(protocol.Level2RequiredMask),
	}

	resp, _ := protocol.NegotiateLevel(req)
	respBytes, _ := json.Marshal(resp)

	_ = s1.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "evt-rec-1",
		SessionID:   sid,
		SequenceNum: 1,
		EventType:   "handshake_degraded",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(respBytes),
	})

	for i := 2; i <= 6; i++ {
		toolCall := &protocol.ToolCallEvent{
			ToolCallID: fmt.Sprintf("tc-%d", i),
			ToolName:   "execute_command",
			Arguments:  map[string]any{"cmd": "go test"},
			DurationMs: 50,
		}
		tcBytes, _ := json.Marshal(toolCall)

		_ = s1.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-rec-%d", i),
			SessionID:   sid,
			SequenceNum: int64(i),
			EventType:   "tool_call_event",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(tcBytes),
		})
	}

	// Flush and close store
	if err := s1.Close(); err != nil {
		t.Fatalf("s1 Close failed: %v", err)
	}

	// Re-open store after graceful close (scenario persistence, not process-crash recovery)
	s2, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("s2 NewStore graceful reopen failed: %v", err)
	}
	defer s2.Close()

	latestSeq, err := s2.GetLatestSequenceNum(ctx, sid)
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	if latestSeq != 6 {
		t.Errorf("Expected sequence 6 after graceful reopen, got %d", latestSeq)
	}

	events, err := s2.QueryEvents(ctx, state.EventFilter{SessionID: sid, Ascending: true})
	if err != nil || len(events) != 6 {
		t.Fatalf("QueryEvents after reopen failed: len=%d err=%v", len(events), err)
	}
	if events[0].EventType != "handshake_degraded" {
		t.Errorf("First reopened-session event mismatch: got %s, expected handshake_degraded", events[0].EventType)
	}
}
