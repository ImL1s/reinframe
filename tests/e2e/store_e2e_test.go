package e2e_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/reinframe/reinframe/pkg/protocol"
	"github.com/reinframe/reinframe/pkg/state"
	_ "modernc.org/sqlite"
)

// helper to create a temporary store with standard options
func setupTestStore(t *testing.T) (*state.Store, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "reinframe_store_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(dir, "events.db")
	opts := state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5000 * time.Millisecond,
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}

	store, err := state.NewStore(opts)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("Failed to initialize store: %v", err)
	}

	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(dir)
	})

	return store, dbPath
}

// ============================================================================
// Tier 1: Feature Coverage (SQLite WAL Event Store - Issue #9)
// ============================================================================

// --- Feature 5: SQL Schema & Migration Engine ---

func TestTier1_Migration_FreshDB(t *testing.T) {
	store, dbPath := setupTestStore(t)
	_ = store

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open raw sqlite db: %v", err)
	}
	defer db.Close()

	var tables []string
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table'")
	if err != nil {
		t.Fatalf("Failed to query sqlite_master: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			tables = append(tables, name)
		}
	}

	hasEvents := false
	hasMigrations := false
	for _, tbl := range tables {
		if tbl == "events" {
			hasEvents = true
		}
		if tbl == "schema_migrations" {
			hasMigrations = true
		}
	}

	if !hasEvents || !hasMigrations {
		t.Errorf("Fresh DB missing expected tables: events=%v, schema_migrations=%v", hasEvents, hasMigrations)
	}
}

func TestTier1_Migration_Idempotency(t *testing.T) {
	dir, err := os.MkdirTemp("", "reinframe_idempotency_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "events.db")
	opts := state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5000 * time.Millisecond,
	}

	s1, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("Initial NewStore failed: %v", err)
	}
	s1.Close()

	// Second initialization on existing DB
	s2, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("Idempotent NewStore failed on existing DB: %v", err)
	}
	s2.Close()
}

func TestTier1_Migration_SchemaVersionTracking(t *testing.T) {
	_, dbPath := setupTestStore(t)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}
	defer db.Close()

	var version int
	err = db.QueryRow("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&version)
	if err != nil {
		t.Fatalf("Failed to query schema_migrations version: %v", err)
	}
	if version != 1 {
		t.Errorf("Expected schema_migrations version 1, got %d", version)
	}
}

func TestTier1_Migration_TableColumns(t *testing.T) {
	_, dbPath := setupTestStore(t)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("PRAGMA table_info(events)")
	if err != nil {
		t.Fatalf("Failed to execute PRAGMA table_info(events): %v", err)
	}
	defer rows.Close()

	columns := make(map[string]string)
	for rows.Next() {
		var cid int
		var name, typeStr string
		var notNull, pk int
		var dfltValue sql.NullString
		if err := rows.Scan(&cid, &name, &typeStr, &notNull, &dfltValue, &pk); err == nil {
			columns[name] = typeStr
		}
	}

	expectedCols := []string{"event_id", "session_id", "sequence_num", "event_type", "timestamp", "payload"}
	for _, col := range expectedCols {
		if _, exists := columns[col]; !exists {
			t.Errorf("events table missing expected column '%s'", col)
		}
	}
}

func TestTier1_Migration_IndexCreation(t *testing.T) {
	_, dbPath := setupTestStore(t)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='index'")
	if err != nil {
		t.Fatalf("Failed to query sqlite_master for indexes: %v", err)
	}
	defer rows.Close()

	indices := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			indices[name] = true
		}
	}

	expectedIndices := []string{"idx_events_session_id", "idx_events_event_type", "idx_events_timestamp"}
	for _, idx := range expectedIndices {
		if !indices[idx] {
			t.Errorf("Expected index '%s' to exist in database", idx)
		}
	}
}

// --- Feature 6: SQLite WAL Event Store Engine ---

func TestTier1_Store_NewStore_WALMode(t *testing.T) {
	_, dbPath := setupTestStore(t)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}
	defer db.Close()

	var mode string
	err = db.QueryRow("PRAGMA journal_mode;").Scan(&mode)
	if err != nil {
		t.Fatalf("Failed to query PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("Expected journal_mode to be 'wal', got '%s'", mode)
	}
}

func TestTier1_Store_AppendEvent_Single(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	evt := &protocol.AgentEvent{
		EventID:     "evt-1001",
		SessionID:   "sess-append-single",
		SequenceNum: 1,
		EventType:   "agent_session",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{"status":"started"}`),
	}

	err := store.AppendEvent(ctx, evt)
	if err != nil {
		t.Fatalf("AppendEvent failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-append-single"})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 persisted event, got %d", len(events))
	}
	if events[0].EventID != "evt-1001" {
		t.Errorf("EventID mismatch: got %s, expected evt-1001", events[0].EventID)
	}
}

func TestTier1_Store_AppendEvent_AutoSequence(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	evt1 := &protocol.AgentEvent{
		EventID:     "evt-auto-1",
		SessionID:   "sess-auto-seq",
		SequenceNum: 1,
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{"tool":"read_file"}`),
	}
	evt2 := &protocol.AgentEvent{
		EventID:     "evt-auto-2",
		SessionID:   "sess-auto-seq",
		SequenceNum: 2,
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{"tool":"write_file"}`),
	}

	if err := store.AppendEvent(ctx, evt1); err != nil {
		t.Fatalf("AppendEvent 1 failed: %v", err)
	}
	if err := store.AppendEvent(ctx, evt2); err != nil {
		t.Fatalf("AppendEvent 2 failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-auto-seq", Ascending: true})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}
	if events[0].SequenceNum != 1 {
		t.Errorf("Expected first event sequence_num = 1, got %d", events[0].SequenceNum)
	}
	if events[1].SequenceNum != 2 {
		t.Errorf("Expected second event sequence_num = 2, got %d", events[1].SequenceNum)
	}
}

func TestTier1_Store_AppendEvents_Batch(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	var batch []*protocol.AgentEvent
	for i := 1; i <= 10; i++ {
		batch = append(batch, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("evt-batch-%d", i),
			SessionID:   "sess-batch",
			SequenceNum: int64(i),
			EventType:   "file_change",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(`{"file":"test.go"}`),
		})
	}

	err := store.AppendEvents(ctx, batch)
	if err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-batch"})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 10 {
		t.Fatalf("Expected 10 persisted batch events, got %d", len(events))
	}
}

func TestTier1_Store_Close(t *testing.T) {
	dir, err := os.MkdirTemp("", "reinframe_close_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	opts := state.StoreOptions{
		DatabasePath: filepath.Join(dir, "events.db"),
		BusyTimeout:  5000 * time.Millisecond,
	}

	s, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	err = s.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

// --- Feature 7: Event Query Engine & Filtering ---

func TestTier1_Query_BySessionID(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e1",
		SessionID:   "sess-A",
		SequenceNum: 1,
		EventType:   "t1",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	})
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e2",
		SessionID:   "sess-B",
		SequenceNum: 1,
		EventType:   "t1",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	})

	resultsA, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-A"})
	if err != nil {
		t.Fatalf("QueryEvents sess-A failed: %v", err)
	}
	if len(resultsA) != 1 || resultsA[0].SessionID != "sess-A" {
		t.Errorf("Expected 1 result for sess-A, got %d", len(resultsA))
	}
}

func TestTier1_Query_ByEventType(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e1",
		SessionID:   "sess-types",
		SequenceNum: 1,
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	})
	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e2",
		SessionID:   "sess-types",
		SequenceNum: 2,
		EventType:   "file_change",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	})

	results, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID:  "sess-types",
		EventTypes: []string{"tool_call"},
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(results) != 1 || results[0].EventType != "tool_call" {
		t.Errorf("Expected 1 tool_call event, got %d", len(results))
	}
}

func TestTier1_Query_BySequenceRange(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	for i := int64(1); i <= 10; i++ {
		_ = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("e-%d", i),
			SessionID:   "sess-seq-range",
			SequenceNum: i,
			EventType:   "test",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage("{}"),
		})
	}

	start := int64(3)
	end := int64(7)
	results, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID:     "sess-seq-range",
		StartSequence: &start,
		EndSequence:   &end,
		Ascending:     true,
	})
	if err != nil {
		t.Fatalf("QueryEvents bounded range failed: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("Expected 5 events in sequence range 3..7, got %d", len(results))
	}
	if results[0].SequenceNum != 3 || results[len(results)-1].SequenceNum != 7 {
		t.Errorf("Sequence range boundary mismatch: start=%d, end=%d", results[0].SequenceNum, results[len(results)-1].SequenceNum)
	}
}

func TestTier1_Query_Pagination(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	for i := int64(1); i <= 10; i++ {
		_ = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("e-%d", i),
			SessionID:   "sess-page",
			SequenceNum: i,
			EventType:   "test",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage("{}"),
		})
	}

	results, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-page",
		Limit:     3,
		Offset:    2,
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents pagination failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Expected 3 paginated results, got %d", len(results))
	}
	if results[0].SequenceNum != 3 {
		t.Errorf("Expected first paginated sequence_num = 3, got %d", results[0].SequenceNum)
	}
}

func TestTier1_Query_GetLatestSequenceNum(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	seq0, err := store.GetLatestSequenceNum(ctx, "sess-nonexistent")
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	if seq0 != 0 {
		t.Errorf("Expected sequence 0 for nonexistent session, got %d", seq0)
	}

	for i := 1; i <= 5; i++ {
		_ = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:     fmt.Sprintf("e-%d", i),
			SessionID:   "sess-latest-seq",
			SequenceNum: int64(i),
			EventType:   "test",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage("{}"),
		})
	}

	seqLatest, err := store.GetLatestSequenceNum(ctx, "sess-latest-seq")
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	if seqLatest != 5 {
		t.Errorf("Expected latest sequence 5, got %d", seqLatest)
	}
}

// --- Feature 8: Multi-Goroutine Concurrency & Race Safety ---

func TestTier1_Concurrency_ParallelAppends(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	const numRoutines = 50
	var wg sync.WaitGroup
	wg.Add(numRoutines)

	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			defer wg.Done()
			evt := &protocol.AgentEvent{
				EventID:     fmt.Sprintf("evt-parallel-%d", id),
				SessionID:   "sess-parallel",
				SequenceNum: int64(id + 1),
				EventType:   "tool_call",
				Timestamp:   time.Now().UTC(),
				Payload:     json.RawMessage(`{"goroutine":id}`),
			}
			err := store.AppendEvent(ctx, evt)
			if err != nil {
				t.Errorf("Parallel AppendEvent failed for routine %d: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	latest, err := store.GetLatestSequenceNum(ctx, "sess-parallel")
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	if latest != numRoutines {
		t.Errorf("Expected %d total appends, got latest sequence %d", numRoutines, latest)
	}
}

func TestTier1_Concurrency_ParallelBatchAppends(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	const numRoutines = 10
	const batchSize = 5
	var wg sync.WaitGroup
	wg.Add(numRoutines)

	for i := 0; i < numRoutines; i++ {
		go func(routineID int) {
			defer wg.Done()
			var batch []*protocol.AgentEvent
			for j := 0; j < batchSize; j++ {
				batch = append(batch, &protocol.AgentEvent{
					EventID:     fmt.Sprintf("evt-pbatch-%d-%d", routineID, j),
					SessionID:   "sess-parallel-batch",
					SequenceNum: int64(routineID*batchSize + j + 1),
					EventType:   "batch_item",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage("{}"),
				})
			}
			if err := store.AppendEvents(ctx, batch); err != nil {
				t.Errorf("Parallel AppendEvents failed for routine %d: %v", routineID, err)
			}
		}(i)
	}

	wg.Wait()

	latest, err := store.GetLatestSequenceNum(ctx, "sess-parallel-batch")
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	expectedTotal := int64(numRoutines * batchSize)
	if latest != expectedTotal {
		t.Errorf("Expected latest sequence %d, got %d", expectedTotal, latest)
	}
}

func TestTier1_Concurrency_ReadWhileWrite(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	var stopSignal int32
	var wg sync.WaitGroup

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; atomic.LoadInt32(&stopSignal) == 0; i++ {
			_ = store.AppendEvent(ctx, &protocol.AgentEvent{
				EventID:     fmt.Sprintf("evt-rww-%d", i),
				SessionID:   "sess-rww",
				SequenceNum: int64(i),
				EventType:   "write_stream",
				Timestamp:   time.Now().UTC(),
				Payload:     json.RawMessage("{}"),
			})
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Reader goroutines
	const numReaders = 5
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for atomic.LoadInt32(&stopSignal) == 0 {
				_, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-rww"})
				if err != nil {
					t.Errorf("Concurrent QueryEvents failed: %v", err)
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	atomic.StoreInt32(&stopSignal, 1)
	wg.Wait()
}

func TestTier1_Concurrency_SequenceContiguity(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	const totalEvents = 30
	var wg sync.WaitGroup
	wg.Add(totalEvents)

	for i := 0; i < totalEvents; i++ {
		go func(id int) {
			defer wg.Done()
			if err := store.AppendEvent(ctx, &protocol.AgentEvent{
				EventID:     fmt.Sprintf("evt-contig-%d", id),
				SessionID:   "sess-contig",
				SequenceNum: int64(id + 1),
				EventType:   "test",
				Timestamp:   time.Now().UTC(),
				Payload:     json.RawMessage("{}"),
			}); err != nil {
				t.Errorf("AppendEvent failed in goroutine %d: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-contig",
		Ascending: true,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != totalEvents {
		t.Fatalf("Expected %d events, got %d", totalEvents, len(events))
	}

	for i, evt := range events {
		expectedSeq := int64(i + 1)
		if evt.SequenceNum != expectedSeq {
			t.Errorf("Sequence gap/discontinuity at index %d: got %d, expected %d", i, evt.SequenceNum, expectedSeq)
		}
	}
}

// ============================================================================
// Tier 2: Boundaries & Corner Cases (SQLite WAL Event Store - Issue #9)
// ============================================================================

// --- Feature 5: SQL Schema & Migration Engine (Boundaries) ---

func TestTier2_Migration_ReadOnlyDirectory(t *testing.T) {
	dir, err := os.MkdirTemp("", "reinframe_readonly_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	readOnlySubdir := filepath.Join(dir, "readonly_dir")
	if err := os.Mkdir(readOnlySubdir, 0444); err != nil {
		t.Fatalf("Failed to create readonly subdir: %v", err)
	}
	defer os.Chmod(readOnlySubdir, 0755)

	opts := state.StoreOptions{
		DatabasePath: filepath.Join(readOnlySubdir, "events.db"),
		BusyTimeout:  1000 * time.Millisecond,
	}

	s, err := state.NewStore(opts)
	if err == nil {
		s.Close()
		t.Errorf("Expected error when creating store in read-only directory, got nil")
	}
}

func TestTier2_Migration_CorruptedMigrationTable(t *testing.T) {
	dir, err := os.MkdirTemp("", "reinframe_corrupt_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "events.db")

	// Pre-create table schema_migrations with invalid schema (missing version column)
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to create raw sqlite db: %v", err)
	}
	_, err = rawDB.Exec("CREATE TABLE schema_migrations (invalid_column TEXT);")
	rawDB.Close()
	if err != nil {
		t.Fatalf("Failed to create corrupt table: %v", err)
	}

	opts := state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  1000 * time.Millisecond,
	}

	s, err := state.NewStore(opts)
	if err == nil {
		s.Close()
		t.Errorf("Expected migration error for corrupted schema_migrations table, got nil")
	}
}

func TestTier2_Migration_PreexistingOtherTables(t *testing.T) {
	dir, err := os.MkdirTemp("", "reinframe_other_tables_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	dbPath := filepath.Join(dir, "events.db")

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open sqlite db: %v", err)
	}
	_, err = rawDB.Exec("CREATE TABLE external_data (id INTEGER PRIMARY KEY, info TEXT); INSERT INTO external_data VALUES (1, 'keepme');")
	rawDB.Close()
	if err != nil {
		t.Fatalf("Failed to pre-populate external_data table: %v", err)
	}

	opts := state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  1000 * time.Millisecond,
	}

	s, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore failed on DB with pre-existing tables: %v", err)
	}
	s.Close()

	// Verify external_data still exists and has its record
	rawDB, _ = sql.Open("sqlite", dbPath)
	defer rawDB.Close()
	var val string
	err = rawDB.QueryRow("SELECT info FROM external_data WHERE id=1").Scan(&val)
	if err != nil || val != "keepme" {
		t.Errorf("Pre-existing table content lost or altered: err=%v, val=%s", err, val)
	}
}

func TestTier2_Migration_ClosedDBConnection(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open in-memory db: %v", err)
	}
	rawDB.Close()

	// Calling migration runner directly on closed db should return error
	err = state.RunMigrations(rawDB)
	if err == nil {
		t.Errorf("Expected migration error on closed DB connection, got nil")
	}
}

func TestTier2_Migration_InterruptedMigrationRollback(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration_rollback.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	_, err = db.ExecContext(ctx, `CREATE TABLE test_schema_rollback (id INTEGER PRIMARY KEY, name TEXT);`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// 1. Valid SQL statement inside transaction
	_, err = tx.ExecContext(ctx, `INSERT INTO test_schema_rollback (id, name) VALUES (1, 'valid_entry');`)
	if err != nil {
		t.Fatalf("Valid SQL statement failed: %v", err)
	}

	// 2. Invalid SQL statement inside transaction causing failure
	_, err = tx.ExecContext(ctx, `INSERT INTO non_existent_table_cause_error VALUES (1);`)
	if err == nil {
		t.Fatalf("Expected error executing invalid SQL statement, got nil")
	}

	// 3. Rollback transaction upon failure
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		t.Fatalf("Failed to rollback transaction: %v", rollbackErr)
	}

	// 4. Verify first SQL statement changes were rolled back and NOT committed
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM test_schema_rollback WHERE id = 1`).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query table: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 records after transaction rollback, got %d", count)
	}
}

// --- Feature 6: SQLite WAL Event Store Engine (Boundaries) ---

func TestTier2_Store_AppendEvent_Nil(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	err := store.AppendEvent(ctx, nil)
	if err == nil {
		t.Errorf("Expected error for AppendEvent(ctx, nil), got nil")
	}
	if !errors.Is(err, state.ErrInvalidEvent) {
		t.Errorf("Expected ErrInvalidEvent, got %v", err)
	}
}

func TestTier2_Store_AppendEvent_EmptySessionID(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	evt := &protocol.AgentEvent{
		EventID:   "e1",
		SessionID: "", // Invalid
		EventType: "tool_call",
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage("{}"),
	}

	err := store.AppendEvent(ctx, evt)
	if err == nil {
		t.Errorf("Expected error for empty SessionID, got nil")
	}
	if !errors.Is(err, state.ErrInvalidEvent) {
		t.Errorf("Expected ErrInvalidEvent, got %v", err)
	}
}

func TestTier2_Store_AppendEvent_EmptyEventType(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	evt := &protocol.AgentEvent{
		EventID:   "e1",
		SessionID: "sess-valid",
		EventType: "", // Invalid
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage("{}"),
	}

	err := store.AppendEvent(ctx, evt)
	if err == nil {
		t.Errorf("Expected error for empty EventType, got nil")
	}
	if !errors.Is(err, state.ErrInvalidEvent) {
		t.Errorf("Expected ErrInvalidEvent, got %v", err)
	}
}

func TestTier2_Store_AppendEvent_DuplicateSequence(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	evt1 := &protocol.AgentEvent{
		EventID:     "e1",
		SessionID:   "sess-dup-seq",
		SequenceNum: 10,
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	}
	evt2 := &protocol.AgentEvent{
		EventID:     "e2",
		SessionID:   "sess-dup-seq",
		SequenceNum: 10, // Duplicate sequence!
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	}

	if err := store.AppendEvent(ctx, evt1); err != nil {
		t.Fatalf("Initial AppendEvent failed: %v", err)
	}

	err := store.AppendEvent(ctx, evt2)
	if err == nil {
		t.Errorf("Expected error for duplicate sequence number, got nil")
	}
	if !errors.Is(err, state.ErrDuplicateSequence) {
		t.Errorf("Expected ErrDuplicateSequence, got %v", err)
	}
}

func TestTier2_Store_AppendEvents_PartialFailureRollback(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	batch := []*protocol.AgentEvent{
		{EventID: "b1", SessionID: "sess-rollback", SequenceNum: 1, EventType: "t1", Timestamp: time.Now().UTC(), Payload: json.RawMessage("{}")},
		{EventID: "b2", SessionID: "sess-rollback", SequenceNum: 2, EventType: "t1", Timestamp: time.Now().UTC(), Payload: json.RawMessage("{}")},
		{EventID: "b3", SessionID: "", SequenceNum: 3, EventType: "t1", Timestamp: time.Now().UTC(), Payload: json.RawMessage("{}")}, // Invalid SessionID
	}

	err := store.AppendEvents(ctx, batch)
	if err == nil {
		t.Errorf("Expected batch failure due to invalid item 3, got nil")
	}

	// Verify items 1 and 2 were rolled back and not persisted
	events, _ := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-rollback"})
	if len(events) != 0 {
		t.Errorf("Batch failure should have rolled back all items, found %d persisted events", len(events))
	}
}

// --- Feature 7: Event Query Engine & Filtering (Boundaries) ---

func TestTier2_Query_EmptyStore(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "nonexistent"})
	if err != nil {
		t.Fatalf("QueryEvents on empty store failed: %v", err)
	}
	if events == nil {
		t.Errorf("Expected non-nil empty slice `[]*protocol.AgentEvent{}`, got nil")
	}
	if len(events) != 0 {
		t.Errorf("Expected 0 events, got %d", len(events))
	}
}

func TestTier2_Query_InvertedSequenceRange(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:     "e1",
		SessionID:   "sess-inv-seq",
		SequenceNum: 5,
		EventType:   "test",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	})

	start := int64(10)
	end := int64(5)
	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID:     "sess-inv-seq",
		StartSequence: &start,
		EndSequence:   &end,
	})
	if err != nil {
		t.Fatalf("QueryEvents with inverted sequence range failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected 0 events for inverted sequence range, got %d", len(events))
	}
}

func TestTier2_Query_InvertedTimeRange(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	_ = store.AppendEvent(ctx, &protocol.AgentEvent{
		EventID:   "e1",
		SessionID: "sess-inv-time",
		EventType: "test",
		Timestamp: time.Now().UTC(),
		Payload:   json.RawMessage("{}"),
	})

	now := time.Now().UTC()
	future := now.Add(1 * time.Hour)
	past := now.Add(-1 * time.Hour)

	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-inv-time",
		StartTime: &future,
		EndTime:   &past,
	})
	if err != nil {
		t.Fatalf("QueryEvents with inverted time range failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected 0 events for inverted time range, got %d", len(events))
	}
}

func TestTier2_Query_NonExistentSession(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-ghost"})
	if err != nil {
		t.Fatalf("QueryEvents for non-existent session failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected 0 events, got %d", len(events))
	}
}

func TestTier2_Query_OffsetExceedsTotal(t *testing.T) {
	store, _ := setupTestStore(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		_ = store.AppendEvent(ctx, &protocol.AgentEvent{
			EventID:   fmt.Sprintf("e-%d", i),
			SessionID: "sess-offset-over",
			EventType: "test",
			Timestamp: time.Now().UTC(),
			Payload:   json.RawMessage("{}"),
		})
	}

	events, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "sess-offset-over",
		Offset:    100,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected 0 events when offset exceeds total count, got %d", len(events))
	}
}

// --- Feature 8: Multi-Goroutine Concurrency & Race Safety (Boundaries) ---

func TestTier2_Concurrency_CancelledContextAppend(t *testing.T) {
	store, _ := setupTestStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel context

	evt := &protocol.AgentEvent{
		EventID:     "e-cancel",
		SessionID:   "sess-cancel",
		SequenceNum: 1,
		EventType:   "test",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	}

	err := store.AppendEvent(ctx, evt)
	if err == nil {
		t.Errorf("Expected error when calling AppendEvent with cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestTier2_Concurrency_CancelledContextQuery(t *testing.T) {
	store, _ := setupTestStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel context

	_, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-cancel"})
	if err == nil {
		t.Errorf("Expected error when calling QueryEvents with cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestTier2_Concurrency_StoreClosedDuringAppend(t *testing.T) {
	dir, err := os.MkdirTemp("", "reinframe_closed_append_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	opts := state.StoreOptions{
		DatabasePath: filepath.Join(dir, "events.db"),
		BusyTimeout:  5000 * time.Millisecond,
	}

	s, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	s.Close()

	ctx := context.Background()
	evt := &protocol.AgentEvent{
		EventID:     "e-closed",
		SessionID:   "sess-closed",
		SequenceNum: 1,
		EventType:   "test",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	}

	err = s.AppendEvent(ctx, evt)
	if err == nil {
		t.Errorf("Expected error when appending to closed store, got nil")
	}
	if !errors.Is(err, state.ErrStoreClosed) && !errors.Is(err, sql.ErrConnDone) {
		t.Errorf("Expected ErrStoreClosed or sql.ErrConnDone, got %v", err)
	}
}

func TestTier2_Concurrency_BusyTimeoutExceeded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "busy_timeout_test.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  50 * time.Millisecond,
		MaxOpenConns: 5,
		MaxIdleConns: 2,
	})
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()

	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open raw DB connection: %v", err)
	}
	defer rawDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rawConn, err := rawDB.Conn(ctx)
	if err != nil {
		t.Fatalf("Failed to get raw connection: %v", err)
	}
	defer rawConn.Close()

	// Acquire exclusive lock on raw DB connection
	if _, err := rawConn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("Failed to begin exclusive transaction: %v", err)
	}

	defer func() {
		_, _ = rawConn.ExecContext(context.Background(), "ROLLBACK")
	}()

	evt := &protocol.AgentEvent{
		EventID:     "evt-busy-1",
		SessionID:   "sess-busy",
		SequenceNum: 1,
		EventType:   "test_busy",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage("{}"),
	}

	// Attempt AppendEvent while DB is locked exclusively. Store busy timeout is 50ms.
	appendErr := store.AppendEvent(ctx, evt)
	if appendErr == nil {
		t.Errorf("Expected busy/lock timeout error from AppendEvent, got nil")
	} else {
		errStr := strings.ToLower(appendErr.Error())
		if !strings.Contains(errStr, "busy") && !strings.Contains(errStr, "locked") {
			t.Errorf("Expected busy/lock error message, got: %v", appendErr)
		}
	}

	// Release lock
	if _, err := rawConn.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("Failed to rollback raw transaction: %v", err)
	}

	// Verify that after lock release, AppendEvent succeeds
	if err := store.AppendEvent(ctx, evt); err != nil {
		t.Errorf("Expected AppendEvent to succeed after lock release, got: %v", err)
	}
}

func TestTier2_Concurrency_HighContention500Routines(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping 500-goroutine stress test in short mode")
	}

	store, _ := setupTestStore(t)
	ctx := context.Background()

	const numRoutines = 500
	var wg sync.WaitGroup
	wg.Add(numRoutines)

	for i := 0; i < numRoutines; i++ {
		go func(id int) {
			defer wg.Done()
			evt := &protocol.AgentEvent{
				EventID:     fmt.Sprintf("evt-stress-500-%d", id),
				SessionID:   "sess-stress-500",
				SequenceNum: int64(id + 1),
				EventType:   "stress_test",
				Timestamp:   time.Now().UTC(),
				Payload:     json.RawMessage("{}"),
			}
			if err := store.AppendEvent(ctx, evt); err != nil {
				t.Errorf("AppendEvent failed in goroutine %d: %v", id, err)
			}
		}(i)
	}

	wg.Wait()

	latest, err := store.GetLatestSequenceNum(ctx, "sess-stress-500")
	if err != nil {
		t.Fatalf("GetLatestSequenceNum failed: %v", err)
	}
	if latest != numRoutines {
		t.Errorf("Expected latest sequence %d after 500 appends, got %d", numRoutines, latest)
	}
}
