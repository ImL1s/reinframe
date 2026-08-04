package state_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/state"
)

// TestChallenger_EmptyFilters tests edge cases around empty or partially empty filters.
func TestChallenger_EmptyFilters(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "empty_filters.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// 1. Query empty store with completely empty filter
	events, err := store.QueryEvents(ctx, state.EventFilter{})
	if err != nil {
		t.Fatalf("QueryEvents empty store failed: %v", err)
	}
	if events == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}

	// Insert test data
	evt1 := &protocol.AgentEvent{EventID: "e1", SessionID: "s1", SequenceNum: 1, EventType: "typeA", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	evt2 := &protocol.AgentEvent{EventID: "e2", SessionID: "s2", SequenceNum: 1, EventType: "typeB", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	if err := store.AppendEvents(ctx, []*protocol.AgentEvent{evt1, evt2}); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	// 2. Empty filter on populated store should return all events
	allEvents, err := store.QueryEvents(ctx, state.EventFilter{})
	if err != nil {
		t.Fatalf("QueryEvents populated store failed: %v", err)
	}
	if len(allEvents) != 2 {
		t.Fatalf("expected 2 events, got %d", len(allEvents))
	}

	// 3. EventTypes slice provided but empty ([]string{})
	typeFiltered, err := store.QueryEvents(ctx, state.EventFilter{EventTypes: []string{}})
	if err != nil {
		t.Fatalf("QueryEvents with empty EventTypes slice failed: %v", err)
	}
	if len(typeFiltered) != 2 {
		t.Fatalf("expected 2 events when EventTypes is empty slice, got %d", len(typeFiltered))
	}

	// 4. Non-matching SessionID
	noMatch, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "non-existent-session"})
	if err != nil {
		t.Fatalf("QueryEvents non-matching session failed: %v", err)
	}
	if len(noMatch) != 0 {
		t.Fatalf("expected 0 events for non-existent session, got %d", len(noMatch))
	}
}

// TestChallenger_PaginationLimits tests edge cases in pagination (Limit and Offset).
func TestChallenger_PaginationLimits(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pagination.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Insert 10 events
	for i := 1; i <= 10; i++ {
		err := store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-pag-%d", i),
			SessionID:   "sess-pag",
			SequenceNum: int64(i),
			EventType:   "log",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("AppendEvent failed: %v", err)
		}
	}

	// 1. Limit: 0, Offset: 0 -> All 10 events
	res1, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-pag", Ascending: true, Limit: 0, Offset: 0})
	if err != nil || len(res1) != 10 {
		t.Fatalf("Limit 0 Offset 0 failed: len=%d, err=%v", len(res1), err)
	}

	// 2. Limit: 3, Offset: 0 -> First 3 events
	res2, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-pag", Ascending: true, Limit: 3, Offset: 0})
	if err != nil || len(res2) != 3 {
		t.Fatalf("Limit 3 Offset 0 failed: len=%d, err=%v", len(res2), err)
	}
	if res2[0].SequenceNum != 1 || res2[2].SequenceNum != 3 {
		t.Errorf("Limit 3 Offset 0 wrong sequences: %d..%d", res2[0].SequenceNum, res2[2].SequenceNum)
	}

	// 3. Limit: 3, Offset: 5 -> Events 6, 7, 8
	res3, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-pag", Ascending: true, Limit: 3, Offset: 5})
	if err != nil || len(res3) != 3 {
		t.Fatalf("Limit 3 Offset 5 failed: len=%d, err=%v", len(res3), err)
	}
	if res3[0].SequenceNum != 6 || res3[2].SequenceNum != 8 {
		t.Errorf("Limit 3 Offset 5 wrong sequences: %d..%d", res3[0].SequenceNum, res3[2].SequenceNum)
	}

	// 4. Limit: 0, Offset: 7 -> Events 8, 9, 10 (via LIMIT -1 OFFSET 7)
	res4, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-pag", Ascending: true, Limit: 0, Offset: 7})
	if err != nil || len(res4) != 3 {
		t.Fatalf("Limit 0 Offset 7 failed: len=%d, err=%v", len(res4), err)
	}
	if res4[0].SequenceNum != 8 || res4[2].SequenceNum != 10 {
		t.Errorf("Limit 0 Offset 7 wrong sequences: %d..%d", res4[0].SequenceNum, res4[2].SequenceNum)
	}

	// 5. Offset beyond total count (Offset: 100) -> Empty slice
	res5, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-pag", Ascending: true, Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("Offset out of bounds error: %v", err)
	}
	if res5 == nil || len(res5) != 0 {
		t.Fatalf("Offset out of bounds expected len 0 non-nil, got len=%d", len(res5))
	}
}

// TestChallenger_TimeRanges tests precision, ordering, boundary filtering, and non-UTC timestamps.
func TestChallenger_TimeRanges(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "timeranges.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	t0 := time.Date(2026, 8, 2, 12, 0, 0, 123456789, time.UTC)
	t1 := t0.Add(100 * time.Millisecond)
	t2 := t0.Add(200 * time.Millisecond)
	t3 := t0.Add(300 * time.Millisecond)

	// Insert events with micro/nano precision timestamps
	events := []*protocol.AgentEvent{
		{EventID: "t-1", SessionID: "sess-time", SequenceNum: 1, EventType: "step", Timestamp: t0, Payload: json.RawMessage(`{}`)},
		{EventID: "t-2", SessionID: "sess-time", SequenceNum: 2, EventType: "step", Timestamp: t1, Payload: json.RawMessage(`{}`)},
		{EventID: "t-3", SessionID: "sess-time", SequenceNum: 3, EventType: "step", Timestamp: t2, Payload: json.RawMessage(`{}`)},
		{EventID: "t-4", SessionID: "sess-time", SequenceNum: 4, EventType: "step", Timestamp: t3, Payload: json.RawMessage(`{}`)},
	}
	if err := store.AppendEvents(ctx, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	// 1. Exact start and end bounds
	resExact, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-time",
		StartTime: &t1,
		EndTime:   &t2,
		Ascending: true,
	})
	if err != nil || len(resExact) != 2 {
		t.Fatalf("Exact time range query failed: len=%d, err=%v", len(resExact), err)
	}
	if resExact[0].SequenceNum != 2 || resExact[1].SequenceNum != 3 {
		t.Errorf("Exact time range returned incorrect events")
	}

	// 2. Inverted time range (StartTime > EndTime) -> should return 0 events, no crash
	resInverted, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-time",
		StartTime: &t3,
		EndTime:   &t0,
	})
	if err != nil {
		t.Fatalf("Inverted time range query returned error: %v", err)
	}
	if len(resInverted) != 0 {
		t.Fatalf("Inverted time range expected 0 events, got %d", len(resInverted))
	}

	// 3. Non-UTC location timestamp filter input
	loc := time.FixedZone("UTC+8", 8*3600)
	t1Local := t1.In(loc)
	t2Local := t2.In(loc)
	resLocal, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-time",
		StartTime: &t1Local,
		EndTime:   &t2Local,
		Ascending: true,
	})
	if err != nil || len(resLocal) != 2 {
		t.Fatalf("Non-UTC time range query failed: len=%d, err=%v", len(resLocal), err)
	}
}

// TestChallenger_SequenceBounds tests edge cases in sequence range bounds and sequence values.
func TestChallenger_SequenceBounds(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "seqbounds.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Append event with large sequence number
	largeSeq := int64(math.MaxInt64 - 100)
	evtLarge := &protocol.AgentEvent{
		EventID:     "e-large",
		SessionID:   "sess-seq",
		SequenceNum: largeSeq,
		EventType:   "checkpoint",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{}`),
	}
	if err := store.AppendEvent(ctx, evtLarge); err != nil {
		t.Fatalf("AppendEvent with large sequence failed: %v", err)
	}

	latest, err := store.GetLatestSequenceNum(ctx, "sess-seq")
	if err != nil || latest != largeSeq {
		t.Fatalf("GetLatestSequenceNum for large sequence failed: got %d, want %d, err %v", latest, largeSeq, err)
	}

	// 1. Inverted sequence filter (StartSequence > EndSequence)
	start := int64(100)
	end := int64(50)
	resInverted, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID:     "sess-seq",
		StartSequence: &start,
		EndSequence:   &end,
	})
	if err != nil {
		t.Fatalf("Inverted sequence filter returned error: %v", err)
	}
	if len(resInverted) != 0 {
		t.Fatalf("Inverted sequence filter expected 0 events, got %d", len(resInverted))
	}

	// 2. Invalid SequenceNum <= 0 on AppendEvent
	invalidSeqs := []int64{0, -1, -9999}
	for _, seq := range invalidSeqs {
		err := store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("e-invalid-%d", seq),
			SessionID:   "sess-seq",
			SequenceNum: seq,
			EventType:   "test",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(`{}`),
		})
		if !errors.Is(err, state.ErrInvalidEvent) {
			t.Errorf("SequenceNum %d expected ErrInvalidEvent, got %v", seq, err)
		}
	}
}

// TestChallenger_SequenceCollisions verifies sequence collisions return ErrDuplicateSequence.
func TestChallenger_SequenceCollisions(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "collisions.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	evt1 := &protocol.AgentEvent{EventID: "evt-a", SessionID: "sess-col", SequenceNum: 42, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	if err := store.AppendEvent(ctx, evt1); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	// Collide on same session and sequence_num, different EventID
	evt2 := &protocol.AgentEvent{EventID: "evt-b", SessionID: "sess-col", SequenceNum: 42, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	err = store.AppendEvent(ctx, evt2)
	if !errors.Is(err, state.ErrDuplicateSequence) {
		t.Fatalf("Expected ErrDuplicateSequence, got %v", err)
	}

	// Different session with same sequence_num should SUCCEED
	evt3 := &protocol.AgentEvent{EventID: "evt-c", SessionID: "sess-other", SequenceNum: 42, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	if err := store.AppendEvent(ctx, evt3); err != nil {
		t.Fatalf("Different session append failed: %v", err)
	}
}

// TestChallenger_StoreClosed verifies all operations fail with ErrStoreClosed after Close().
func TestChallenger_StoreClosed(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "store_closed.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Close store once
	if err := store.Close(); err != nil {
		t.Fatalf("First Close failed: %v", err)
	}

	// Close store twice (should be idempotent and return nil)
	if err := store.Close(); err != nil {
		t.Fatalf("Second Close failed: %v", err)
	}

	evt := &protocol.AgentEvent{EventID: "e", SessionID: "s", SequenceNum: 1, EventType: "t", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)}

	if err := store.AppendEvent(ctx, evt); !errors.Is(err, state.ErrStoreClosed) {
		t.Errorf("AppendEvent expected ErrStoreClosed, got %v", err)
	}

	if err := store.AppendEvents(ctx, []*protocol.AgentEvent{evt}); !errors.Is(err, state.ErrStoreClosed) {
		t.Errorf("AppendEvents expected ErrStoreClosed, got %v", err)
	}

	if _, err := store.QueryEvents(ctx, state.EventFilter{}); !errors.Is(err, state.ErrStoreClosed) {
		t.Errorf("QueryEvents expected ErrStoreClosed, got %v", err)
	}

	if _, err := store.GetLatestSequenceNum(ctx, "s"); !errors.Is(err, state.ErrStoreClosed) {
		t.Errorf("GetLatestSequenceNum expected ErrStoreClosed, got %v", err)
	}
}

// TestChallenger_BatchAtomicRollbacks tests batch transaction atomicity when an append fails mid-batch.
func TestChallenger_BatchAtomicRollbacks(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "batch_rollback.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Initial event
	initEvt := &protocol.AgentEvent{EventID: "init-1", SessionID: "sess-roll", SequenceNum: 1, EventType: "init", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)}
	if err := store.AppendEvent(ctx, initEvt); err != nil {
		t.Fatalf("Append initial event failed: %v", err)
	}

	// Prepare a batch of 5 events where the 4th event collides with sequence_num 1 (duplicate sequence!)
	batch := []*protocol.AgentEvent{
		{EventID: "b-2", SessionID: "sess-roll", SequenceNum: 2, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
		{EventID: "b-3", SessionID: "sess-roll", SequenceNum: 3, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
		{EventID: "b-4", SessionID: "sess-roll", SequenceNum: 4, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
		{EventID: "b-dup", SessionID: "sess-roll", SequenceNum: 1, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)}, // Duplicate sequence!
		{EventID: "b-6", SessionID: "sess-roll", SequenceNum: 6, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
	}

	err = store.AppendEvents(ctx, batch)
	if !errors.Is(err, state.ErrDuplicateSequence) {
		t.Fatalf("Expected ErrDuplicateSequence, got %v", err)
	}

	// Verify ATOMICITY: None of b-2, b-3, b-4 should be persisted in DB!
	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-roll"})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Atomicity violation! Expected exactly 1 event (init-1) after batch failure, got %d", len(events))
	}
	if events[0].EventID != "init-1" {
		t.Fatalf("Expected event init-1, got %s", events[0].EventID)
	}

	// Test 2: In-batch duplicate sequence numbers (e.g. b-10 and b-11 both have sequence 10)
	batchSelfDup := []*protocol.AgentEvent{
		{EventID: "b-10a", SessionID: "sess-roll", SequenceNum: 10, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
		{EventID: "b-10b", SessionID: "sess-roll", SequenceNum: 10, EventType: "log", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
	}
	err = store.AppendEvents(ctx, batchSelfDup)
	if !errors.Is(err, state.ErrDuplicateSequence) {
		t.Fatalf("Expected ErrDuplicateSequence for internal batch collision, got %v", err)
	}

	// Confirm store still has only 1 event
	eventsAfter, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-roll"})
	if err != nil || len(eventsAfter) != 1 {
		t.Fatalf("Expected 1 event after self-duplicate batch rollback, got %d, err %v", len(eventsAfter), err)
	}
}

// TestChallenger_SQLInjectionAndSpecialChars tests resistance to injection payloads and handling of large/complex payloads.
func TestChallenger_SQLInjectionAndSpecialChars(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sqli.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	injectionSessionID := "sess' OR '1'='1'; DROP TABLE events; --"
	injectionEventType := "type' UNION SELECT 1,2,3,4,5,6 --"
	complexPayload := json.RawMessage(`{"nested":{"arr":[1,2,3],"unicode":"你好 世界 🚀 \n\t\"'\\"},"sql":"SELECT * FROM events"}`)

	evt := &protocol.AgentEvent{
		EventID:     "evt-sqli-1",
		SessionID:   injectionSessionID,
		SequenceNum: 1,
		EventType:   injectionEventType,
		Timestamp:   time.Now().UTC(),
		Payload:     complexPayload,
	}

	if err := store.AppendEvent(ctx, evt); err != nil {
		t.Fatalf("AppendEvent with SQL injection payloads failed: %v", err)
	}

	// Query specifically for this session ID
	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: injectionSessionID})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 event for injection session ID, got %d", len(events))
	}

	res := events[0]
	if res.SessionID != injectionSessionID {
		t.Errorf("SessionID mismatch: got %q, want %q", res.SessionID, injectionSessionID)
	}
	if res.EventType != injectionEventType {
		t.Errorf("EventType mismatch: got %q, want %q", res.EventType, injectionEventType)
	}
	if string(res.Payload) != string(complexPayload) {
		t.Errorf("Payload mismatch: got %s, want %s", string(res.Payload), string(complexPayload))
	}
}

// TestChallenger_HighConcurrency_ReadWriteRace tests concurrent reads and writes simultaneously under race detector.
func TestChallenger_HighConcurrency_ReadWriteRace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping high-concurrency SQLite read/write race on Windows — NTFS file locking causes SQLITE_BUSY under CI contention")
	}
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "rw_race.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5000 * time.Millisecond,
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	const numWriters = 20
	const numReaders = 20
	const opsPerWorker = 30

	var wg sync.WaitGroup
	errChan := make(chan error, (numWriters+numReaders)*opsPerWorker)

	// Writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("writer-session-%d", writerID)
			for i := 1; i <= opsPerWorker; i++ {
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("evt-w%d-s%d", writerID, i),
					SessionID:   sessionID,
					SequenceNum: int64(i),
					EventType:   "concurrency_write",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(`{"data":"race_test"}`),
				}
				if err := store.AppendEvent(ctx, evt); err != nil {
					errChan <- fmt.Errorf("writer %d op %d failed: %w", writerID, i, err)
				}
			}
		}(w)
	}

	// Readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			targetSession := fmt.Sprintf("writer-session-%d", readerID%numWriters)
			for i := 1; i <= opsPerWorker; i++ {
				_, _ = store.QueryEvents(ctx, state.EventFilter{
					SessionID: targetSession,
					Limit:     10,
				})
				_, _ = store.GetLatestSequenceNum(ctx, targetSession)
				time.Sleep(1 * time.Millisecond)
			}
		}(r)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrency read/write error: %v", err)
	}
}
