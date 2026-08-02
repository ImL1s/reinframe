package state_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/state"
)

func TestNewStore_Migrations(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration_test.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Open underlying database file directly to check sqlite_master tables
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open raw sqlite db: %v", err)
	}
	defer db.Close()

	expectedTables := []string{"schema_migrations", "events", "audit_records"}
	for _, tbl := range expectedTables {
		var exists bool
		query := "SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)"
		if err := db.QueryRow(query, tbl).Scan(&exists); err != nil {
			t.Fatalf("failed to check table %s existence: %v", tbl, err)
		}
		if !exists {
			t.Errorf("expected table %s to exist", tbl)
		}
	}

	var migrationCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("failed to count schema migrations: %v", err)
	}
	if migrationCount < 1 {
		t.Errorf("expected at least 1 applied migration, got %d", migrationCount)
	}
}

func TestStore_AppendAndQuery(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "append_query.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	event := &protocol.AgentEvent{
		EventID:     "evt-101",
		SessionID:   "sess-1",
		SequenceNum: 1,
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{"tool":"write_file","path":"/tmp/foo"}`),
	}

	if err := store.AppendEvent(ctx, event); err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	res := events[0]
	if res.EventID != event.EventID {
		t.Errorf("EventID = %s, want %s", res.EventID, event.EventID)
	}
	if res.SessionID != event.SessionID {
		t.Errorf("SessionID = %s, want %s", res.SessionID, event.SessionID)
	}
	if res.SequenceNum != event.SequenceNum {
		t.Errorf("SequenceNum = %d, want %d", res.SequenceNum, event.SequenceNum)
	}
	if res.EventType != event.EventType {
		t.Errorf("EventType = %s, want %s", res.EventType, event.EventType)
	}
	if string(res.Payload) != string(event.Payload) {
		t.Errorf("Payload = %s, want %s", string(res.Payload), string(event.Payload))
	}
}

func TestStore_AppendEventsBatch(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "batch.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	var batch []*protocol.AgentEvent
	for i := 1; i <= 10; i++ {
		batch = append(batch, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-batch-%d", i),
			SessionID:   "sess-batch",
			SequenceNum: int64(i),
			EventType:   "file_change",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(fmt.Sprintf(`{"index":%d}`, i)),
		})
	}

	if err := store.AppendEvents(ctx, batch); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-batch",
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 10 {
		t.Fatalf("expected 10 events, got %d", len(events))
	}

	for i, e := range events {
		expectedSeq := int64(i + 1)
		if e.SequenceNum != expectedSeq {
			t.Errorf("events[%d].SequenceNum = %d, want %d", i, e.SequenceNum, expectedSeq)
		}
	}
}

func TestStore_DuplicateSequence(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "dup_seq.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	evt1 := &protocol.AgentEvent{
		EventID:     "evt-1",
		SessionID:   "sess-dup",
		SequenceNum: 1,
		EventType:   "test_result",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{}`),
	}
	evt2 := &protocol.AgentEvent{
		EventID:     "evt-2",
		SessionID:   "sess-dup",
		SequenceNum: 1, // Same session + same sequence number -> duplicate sequence!
		EventType:   "test_result",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{}`),
	}

	if err := store.AppendEvent(ctx, evt1); err != nil {
		t.Fatalf("AppendEvent evt1 failed: %v", err)
	}

	err = store.AppendEvent(ctx, evt2)
	if err == nil {
		t.Fatal("expected error on duplicate sequence number, got nil")
	}

	if !errors.Is(err, state.ErrDuplicateSequence) {
		t.Errorf("expected ErrDuplicateSequence, got %v", err)
	}
}

func TestStore_DuplicateEventID(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "dup_id.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	evt1 := &protocol.AgentEvent{
		EventID:     "evt-unique-1",
		SessionID:   "sess-1",
		SequenceNum: 1,
		EventType:   "test_result",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{}`),
	}
	evt2 := &protocol.AgentEvent{
		EventID:     "evt-unique-1", // Same EventID -> duplicate PK!
		SessionID:   "sess-2",
		SequenceNum: 1,
		EventType:   "test_result",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{}`),
	}

	if err := store.AppendEvent(ctx, evt1); err != nil {
		t.Fatalf("AppendEvent evt1 failed: %v", err)
	}

	err = store.AppendEvent(ctx, evt2)
	if err == nil {
		t.Fatal("expected error on duplicate EventID, got nil")
	}

	if !errors.Is(err, state.ErrDuplicateEventID) {
		t.Errorf("expected ErrDuplicateEventID, got %v", err)
	}
}

func TestStore_FixedWidthTimestampLexicalOrdering(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "lexical_ts.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	baseTime := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)

	// Exact second boundary
	e1 := &protocol.AgentEvent{
		EventID:     "evt-exact-sec",
		SessionID:   "sess-ts",
		SequenceNum: 1,
		EventType:   "tool_call",
		Timestamp:   baseTime,
		Payload:     json.RawMessage(`{}`),
	}
	// Fractional second
	e2 := &protocol.AgentEvent{
		EventID:     "evt-frac-sec",
		SessionID:   "sess-ts",
		SequenceNum: 2,
		EventType:   "tool_call",
		Timestamp:   baseTime.Add(100 * time.Millisecond),
		Payload:     json.RawMessage(`{}`),
	}

	if err := store.AppendEvents(ctx, []*protocol.AgentEvent{e1, e2}); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	// Filter with StartTime at exact second
	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-ts",
		StartTime: &baseTime,
	})
	if err != nil {
		t.Fatalf("QueryEvents with StartTime failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}

	// Filter with EndTime at exact second -> should only match e1
	eventsExact, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-ts",
		EndTime:   &baseTime,
	})
	if err != nil {
		t.Fatalf("QueryEvents with EndTime failed: %v", err)
	}
	if len(eventsExact) != 1 || eventsExact[0].EventID != "evt-exact-sec" {
		t.Errorf("expected 1 event ('evt-exact-sec'), got %d", len(eventsExact))
	}
}

func TestStore_InvalidEvent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "invalid.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	invalidCases := []*protocol.AgentEvent{
		nil,
		{EventID: "", SessionID: "s", SequenceNum: 1, EventType: "e"},
		{EventID: "e", SessionID: "", SequenceNum: 1, EventType: "e"},
		{EventID: "e", SessionID: "s", SequenceNum: 0, EventType: "e"},
		{EventID: "e", SessionID: "s", SequenceNum: 1, EventType: ""},
	}

	for i, c := range invalidCases {
		err := store.AppendEvent(ctx, c)
		if !errors.Is(err, state.ErrInvalidEvent) {
			t.Errorf("case %d: expected ErrInvalidEvent, got %v", i, err)
		}
	}
}

func TestStore_QueryFilters(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "filters.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	baseTime := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)

	// Insert events for session-A and session-B
	events := []*protocol.AgentEvent{
		{EventID: "e1", SessionID: "sess-A", SequenceNum: 1, EventType: "tool_call", Timestamp: baseTime.Add(1 * time.Minute), Payload: json.RawMessage(`{}`)},
		{EventID: "e2", SessionID: "sess-A", SequenceNum: 2, EventType: "file_change", Timestamp: baseTime.Add(2 * time.Minute), Payload: json.RawMessage(`{}`)},
		{EventID: "e3", SessionID: "sess-A", SequenceNum: 3, EventType: "tool_call", Timestamp: baseTime.Add(3 * time.Minute), Payload: json.RawMessage(`{}`)},
		{EventID: "e4", SessionID: "sess-A", SequenceNum: 4, EventType: "test_result", Timestamp: baseTime.Add(4 * time.Minute), Payload: json.RawMessage(`{}`)},
		{EventID: "e5", SessionID: "sess-A", SequenceNum: 5, EventType: "file_change", Timestamp: baseTime.Add(5 * time.Minute), Payload: json.RawMessage(`{}`)},
		{EventID: "e6", SessionID: "sess-B", SequenceNum: 1, EventType: "tool_call", Timestamp: baseTime.Add(1 * time.Minute), Payload: json.RawMessage(`{}`)},
	}

	if err := store.AppendEvents(ctx, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	// 1. SessionID filter
	resA, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-A", Ascending: true})
	if err != nil || len(resA) != 5 {
		t.Fatalf("SessionID filter failed, count=%d, err=%v", len(resA), err)
	}

	// 2. EventTypes filter
	resTypes, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID:  "sess-A",
		EventTypes: []string{"tool_call", "test_result"},
		Ascending:  true,
	})
	if err != nil || len(resTypes) != 3 {
		t.Fatalf("EventTypes filter failed, count=%d, err=%v", len(resTypes), err)
	}

	// 3. Sequence bounds (StartSequence: 2, EndSequence: 4)
	startSeq := int64(2)
	endSeq := int64(4)
	resSeq, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID:     "sess-A",
		StartSequence: &startSeq,
		EndSequence:   &endSeq,
		Ascending:     true,
	})
	if err != nil || len(resSeq) != 3 {
		t.Fatalf("Sequence bounds filter failed, count=%d, err=%v", len(resSeq), err)
	}
	if resSeq[0].SequenceNum != 2 || resSeq[2].SequenceNum != 4 {
		t.Errorf("Sequence bounds returned incorrect range")
	}

	// 4. Time bounds
	startTime := baseTime.Add(2 * time.Minute)
	endTime := baseTime.Add(4 * time.Minute)
	resTime, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-A",
		StartTime: &startTime,
		EndTime:   &endTime,
		Ascending: true,
	})
	if err != nil || len(resTime) != 3 {
		t.Fatalf("Time bounds filter failed, count=%d, err=%v", len(resTime), err)
	}

	// 5. Limit and Offset pagination
	resPage, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-A",
		Ascending: true,
		Limit:     2,
		Offset:    1,
	})
	if err != nil || len(resPage) != 2 {
		t.Fatalf("Pagination failed, count=%d, err=%v", len(resPage), err)
	}
	if resPage[0].SequenceNum != 2 || resPage[1].SequenceNum != 3 {
		t.Errorf("Pagination returned incorrect sequence numbers")
	}
}

func TestStore_GetLatestSequenceNum(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "latest_seq.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Non-existent session
	seq0, err := store.GetLatestSequenceNum(ctx, "non-existent")
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	if seq0 != 0 {
		t.Errorf("expected 0 for non-existent session, got %d", seq0)
	}

	// Insert events out of order
	events := []*protocol.AgentEvent{
		{EventID: "s1", SessionID: "sess-latest", SequenceNum: 1, EventType: "init", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
		{EventID: "s3", SessionID: "sess-latest", SequenceNum: 15, EventType: "checkpoint", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
		{EventID: "s2", SessionID: "sess-latest", SequenceNum: 7, EventType: "tool_call", Timestamp: time.Now().UTC(), Payload: json.RawMessage(`{}`)},
	}
	if err := store.AppendEvents(ctx, events); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	maxSeq, err := store.GetLatestSequenceNum(ctx, "sess-latest")
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	if maxSeq != 15 {
		t.Errorf("expected max sequence number 15, got %d", maxSeq)
	}
}

func TestStore_ClosedStore(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "closed.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	evt := &protocol.AgentEvent{EventID: "e", SessionID: "s", SequenceNum: 1, EventType: "t"}
	if err := store.AppendEvent(ctx, evt); !errors.Is(err, state.ErrStoreClosed) {
		t.Errorf("AppendEvent after Close: expected ErrStoreClosed, got %v", err)
	}
	if err := store.AppendEvents(ctx, []*protocol.AgentEvent{evt}); !errors.Is(err, state.ErrStoreClosed) {
		t.Errorf("AppendEvents after Close: expected ErrStoreClosed, got %v", err)
	}
	if _, err := store.QueryEvents(ctx, state.EventFilter{}); !errors.Is(err, state.ErrStoreClosed) {
		t.Errorf("QueryEvents after Close: expected ErrStoreClosed, got %v", err)
	}
	if _, err := store.GetLatestSequenceNum(ctx, "s"); !errors.Is(err, state.ErrStoreClosed) {
		t.Errorf("GetLatestSequenceNum after Close: expected ErrStoreClosed, got %v", err)
	}
}

func TestStore_ConcurrentAppends_Race(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "race.db")
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

	const numRoutines = 50
	const eventsPerRoutine = 20

	var wg sync.WaitGroup
	errChan := make(chan error, numRoutines*eventsPerRoutine)

	for r := 0; r < numRoutines; r++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("session-%d", routineID)
			for i := 1; i <= eventsPerRoutine; i++ {
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("evt-r%d-seq%d", routineID, i),
					SessionID:   sessionID,
					SequenceNum: int64(i),
					EventType:   "concurrent_event",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(fmt.Sprintf(`{"routine":%d,"seq":%d}`, routineID, i)),
				}
				if err := store.AppendEvent(ctx, evt); err != nil {
					errChan <- fmt.Errorf("routine %d append event %d failed: %w", routineID, i, err)
				}
			}
		}(r)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent append error: %v", err)
	}

	// Verify total event count across all sessions
	totalEvents, err := store.QueryEvents(ctx, state.EventFilter{})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	expectedTotal := numRoutines * eventsPerRoutine
	if len(totalEvents) != expectedTotal {
		t.Fatalf("expected total events %d, got %d", expectedTotal, len(totalEvents))
	}

	// Verify sequence completeness per session
	for r := 0; r < numRoutines; r++ {
		sessionID := fmt.Sprintf("session-%d", r)
		maxSeq, err := store.GetLatestSequenceNum(ctx, sessionID)
		if err != nil {
			t.Fatalf("GetLatestSequenceNum for %s failed: %v", sessionID, err)
		}
		if maxSeq != eventsPerRoutine {
			t.Errorf("session %s max sequence = %d, want %d", sessionID, maxSeq, eventsPerRoutine)
		}
	}
}

func TestStore_DefaultMemoryStore_SharedCachePooling(t *testing.T) {
	ctx := context.Background()
	store, err := state.NewStore(state.StoreOptions{
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	})
	if err != nil {
		t.Fatalf("NewStore with empty DatabasePath failed: %v", err)
	}
	defer store.Close()

	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			evt := &protocol.AgentEvent{
				EventID:     fmt.Sprintf("mem-evt-%d", seq),
				SessionID:   "sess-mem",
				SequenceNum: int64(seq),
				EventType:   "mem_test",
				Timestamp:   time.Now().UTC(),
				Payload:     json.RawMessage(`{}`),
			}
			if err := store.AppendEvent(ctx, evt); err != nil {
				t.Errorf("AppendEvent failed on default memory store: %v", err)
			}
		}(i)
	}
	wg.Wait()

	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-mem"})
	if err != nil {
		t.Fatalf("QueryEvents failed on default memory store: %v", err)
	}
	if len(events) != 20 {
		t.Fatalf("expected 20 events on in-memory store, got %d", len(events))
	}
}

func TestStore_ConcurrentMigrations_Race(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrent_migrations.db")

	db, err := sql.Open("sqlite", fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate", dbPath))
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(10)

	var wg sync.WaitGroup
	errChan := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := state.RunMigrations(db); err != nil {
				errChan <- err
			}
		}()
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent RunMigrations error: %v", err)
	}
}

func TestStore_CloseRacesWithAppend(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "close_races_append.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5000 * time.Millisecond,
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	const numGoroutines = 50
	const appendsPerGoroutine = 200

	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	errChan := make(chan error, numGoroutines*appendsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			<-startSignal
			sessionID := fmt.Sprintf("race-session-%d", routineID)
			for seq := int64(1); seq <= appendsPerGoroutine; seq++ {
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("race-evt-r%d-seq%d", routineID, seq),
					SessionID:   sessionID,
					SequenceNum: seq,
					EventType:   "race_append",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(`{"data":"test"}`),
				}
				err := store.AppendEvent(ctx, evt)
				if err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	close(startSignal)
	time.Sleep(2 * time.Millisecond)

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if !errors.Is(err, state.ErrStoreClosed) {
			t.Errorf("expected error to match state.ErrStoreClosed, got raw error: %v (type %T)", err, err)
		}
	}
}

func TestStore_CloseRacesWithAllOperations(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "close_races_all_ops.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5000 * time.Millisecond,
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	// Seed initial data so Query and GetLatestSequenceNum have rows to inspect
	initialEvents := []*protocol.AgentEvent{
		{
			EventID:     "init-1",
			SessionID:   "shared-session",
			SequenceNum: 1,
			EventType:   "init",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(`{"init": true}`),
		},
		{
			EventID:     "init-2",
			SessionID:   "shared-session",
			SequenceNum: 2,
			EventType:   "init",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(`{"init": true}`),
		},
	}
	if err := store.AppendEvents(ctx, initialEvents); err != nil {
		t.Fatalf("AppendEvents initial seed failed: %v", err)
	}

	const appenders = 20
	const queryers = 20
	const sequencers = 20
	const opsPerRoutine = 100

	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	errChan := make(chan error, (appenders+queryers+sequencers)*opsPerRoutine)

	// Appender goroutines
	for i := 0; i < appenders; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			<-startSignal
			sessionID := fmt.Sprintf("appender-session-%d", routineID)
			for seq := int64(1); seq <= opsPerRoutine; seq++ {
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("allops-evt-r%d-seq%d", routineID, seq),
					SessionID:   sessionID,
					SequenceNum: seq,
					EventType:   "all_ops_append",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(`{"data":"stress"}`),
				}
				if err := store.AppendEvents(ctx, []*protocol.AgentEvent{evt}); err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	// Queryer goroutines
	for i := 0; i < queryers; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			<-startSignal
			for op := 0; op < opsPerRoutine; op++ {
				_, err := store.QueryEvents(ctx, state.EventFilter{
					SessionID: "shared-session",
					Limit:     10,
				})
				if err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	// Sequencer goroutines
	for i := 0; i < sequencers; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			<-startSignal
			for op := 0; op < opsPerRoutine; op++ {
				_, err := store.GetLatestSequenceNum(ctx, "shared-session")
				if err != nil {
					errChan <- err
				}
			}
		}(i)
	}

	close(startSignal)
	time.Sleep(3 * time.Millisecond)

	if err := store.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	wg.Wait()
	close(errChan)

	var rawClosedErrors int
	var otherUnwrappedErrors []error

	for err := range errChan {
		if !errors.Is(err, state.ErrStoreClosed) {
			if strings.Contains(err.Error(), "database is closed") || errors.Is(err, sql.ErrConnDone) {
				rawClosedErrors++
			} else {
				otherUnwrappedErrors = append(otherUnwrappedErrors, err)
			}
			t.Errorf("unwrapped or non-ErrStoreClosed error received: %v (type %T)", err, err)
		}
	}

	if rawClosedErrors > 0 {
		t.Fatalf("CRITICAL: %d raw 'sql: database is closed' errors escaped to callers!", rawClosedErrors)
	}
	if len(otherUnwrappedErrors) > 0 {
		t.Fatalf("CRITICAL: %d non-ErrStoreClosed unexpected errors escaped to callers: %v", len(otherUnwrappedErrors), otherUnwrappedErrors)
	}
}



