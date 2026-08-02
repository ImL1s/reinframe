# Test Strategy & Analysis Report: `pkg/state/store_test.go`
**Milestone 2 — Issue #9: Append-Only Event Store & SQLite WAL Engine**

## 1. Observation
- **Target Subsystem**: `pkg/state` (`store.go`, `migration.go`, `migrations/001_initial_events.sql`, `store_test.go`).
- **Core Protocol Models** (`pkg/protocol/schema.go`, lines 33–40):
  ```go
  type AgentEvent struct {
      EventID     string          `json:"event_id"`
      SessionID   string          `json:"session_id"`
      SequenceNum int64           `json:"sequence_num"`
      EventType   string          `json:"event_type"`
      Timestamp   time.Time       `json:"timestamp"`
      Payload     json.RawMessage `json:"payload"`
  }
  ```
- **Store Interface Contract** (`PROJECT.md`, lines 77–105 & `SCOPE.md`, lines 12–19):
  - `NewStore(opts StoreOptions) (*Store, error)`
  - `AppendEvent(ctx context.Context, event *protocol.AgentEvent) error`
  - `AppendEvents(ctx context.Context, events []*protocol.AgentEvent) error`
  - `QueryEvents(ctx context.Context, filter EventFilter) ([]*protocol.AgentEvent, error)`
  - `GetLatestSequenceNum(ctx context.Context, sessionID string) (int64, error)`
  - `Close() error`
- **Required Verification Standard**: High-concurrency race verification with `go test -v -race ./pkg/state/...` passing without race conditions, deadlocks, or `SQLITE_BUSY` lock errors.

---

## 2. Logic Chain

### 2.1 Concurrency & Race Testing Strategy (50 Goroutines)
1. **Serialization vs Concurrent WAL Reads**:
   - SQLite in WAL mode (`PRAGMA journal_mode=WAL`) permits non-blocking concurrent reads while a write transaction is active. However, SQLite permits only **one active write transaction** at a time across database connections.
   - Using `BEGIN IMMEDIATE` transactions combined with an internal `sync.RWMutex` on the `Store` instance guarantees that in-process write operations are cleanly serialized, eliminating `SQLITE_BUSY` write-lock contention deadlocks.
2. **Race Suite Scenarios to Implement**:
   - **Test 1.1: 50 Parallel Independent Session Writers**:
     - 50 goroutines executing simultaneously. Each goroutine appends 20 events to its own session ID (`session-0` .. `session-49`), sequence 1..20. Total = 1,000 events inserted.
     - *Assertions*: Zero errors; total events queried across store == 1,000; each session has exactly 20 events ordered 1..20.
   - **Test 1.2: 50 Writers + 10 Readers Race Matrix**:
     - 50 writer goroutines appending events continuously while 10 reader goroutines execute `QueryEvents` and `GetLatestSequenceNum` in parallel loop.
     - *Assertions*: Verified with `go test -race`. Readers never observe partially written events, uncommitted transactions, or memory race conditions.
   - **Test 1.3: Concurrent Single-Session Sequence Collision**:
     - 10 goroutines attempting to insert event `(session_id="shared", sequence_num=1)` simultaneously.
     - *Assertions*: Exactly 1 goroutine succeeds (`err == nil`); 9 goroutines fail with `ErrDuplicateSequence`.
   - **Test 1.4: Concurrent Batch Appends (`AppendEvents`)**:
     - 20 goroutines concurrently calling `AppendEvents` with batches of 50 events per batch (1,000 total events).
     - *Assertions*: All 20 batches complete atomically without missing events or sequence mismatches.

### 2.2 Flexible Event Filter Testing Matrix
1. **Dynamic SQL Clause Verification**:
   - `QueryEvents` must convert `EventFilter` fields into a parameterized SQL `WHERE` clause:
     - `SessionID`: `session_id = ?`
     - `EventTypes`: `event_type IN (?, ?, ...)`
     - `StartSequence`: `sequence_num >= ?`
     - `EndSequence`: `sequence_num <= ?`
     - `StartTime`: `timestamp >= ?` (formatted as `time.RFC3339Nano`)
     - `EndTime`: `timestamp <= ?` (formatted as `time.RFC3339Nano`)
     - `Ascending`: `ORDER BY sequence_num ASC` vs `ORDER BY sequence_num DESC`
     - `Limit`/`Offset`: `LIMIT ? OFFSET ?`
2. **Filter Test Matrix to Implement**:
   - **Test 2.1: Empty Filter**: Returns all events sorted by sequence ascending.
   - **Test 2.2: Session Filter**: Filters precisely by `SessionID`; returns empty slice `[]*protocol.AgentEvent` (non-nil) for non-existent sessions.
   - **Test 2.3: Multi-Type Filter**: `EventTypes: []string{"tool_call", "file_change"}` generates proper `IN` clause.
   - **Test 2.4: Sequence Range Bounds**: Evaluates inclusive start/end bounds (`StartSequence <= seq <= EndSequence`), single sequence point, and out-of-range bounds.
   - **Test 2.5: Nanosecond Timestamp Bounds**: Tests `StartTime` and `EndTime` filtering using `time.RFC3339Nano` strings to ensure correct SQL text comparisons.
   - **Test 2.6: Pagination & Ordering**: Verifies `Limit` and `Offset` pagination under both `Ascending: true` and `Ascending: false`. Tests `Limit: 0` returning empty slice.
   - **Test 2.7: Full Composite Filter**: Evaluates all filters combined in a single query (Session + EventTypes + Sequence Range + Time Range + Pagination + Descending sort).

### 2.3 Constraint Enforcement & Error Mapping
1. **Sentinel Error Definitions**:
   - `ErrDuplicateSequence`: Mapped when SQLite returns `UNIQUE constraint failed: events.session_id, events.sequence_num`.
   - `ErrDuplicateEventID`: Mapped when SQLite returns `UNIQUE constraint failed: events.event_id`.
   - `ErrInvalidEvent`: Mapped when required fields (`EventID`, `SessionID`) are empty or `event == nil`.
   - `ErrStoreClosed`: Mapped when operations are invoked on a closed store.
2. **Constraint Test Scenarios to Implement**:
   - **Test 3.1: Sequence Uniqueness**: Inserting `(session_id="s1", sequence_num=1)` twice triggers `ErrDuplicateSequence`.
   - **Test 3.2: Primary Key Uniqueness**: Inserting duplicate `event_id` triggers `ErrDuplicateEventID`.
   - **Test 3.3: Batch Rollback Atomicity**: In `AppendEvents`, if item 3 of a 5-item batch violates sequence uniqueness, the entire transaction is rolled back—items 1 and 2 MUST NOT exist in the database.
   - **Test 3.4: Foreign Key PRAGMA Verification**: Validates that `PRAGMA foreign_keys = ON` is executed on connection initialization.

### 2.4 Database Closure & Lifecycle Error Handling
1. **Store Lifecycle & Resource Protection**:
   - **Test 4.1: Post-Close Rejection**: Calling `AppendEvent`, `AppendEvents`, `QueryEvents`, or `GetLatestSequenceNum` after `s.Close()` returns `ErrStoreClosed`.
   - **Test 4.2: Idempotent Close**: Calling `s.Close()` multiple times returns `nil` without panic or error.
   - **Test 4.3: Input Validation**:
     - `AppendEvent(ctx, nil)` -> returns `ErrInvalidEvent`.
     - `AppendEvent(ctx, &AgentEvent{EventID: ""})` -> returns `ErrInvalidEvent`.
     - `AppendEvent(ctx, &AgentEvent{SessionID: ""})` -> returns `ErrInvalidEvent`.
     - `AppendEvents(ctx, nil)` or `AppendEvents(ctx, []*protocol.AgentEvent{})` -> returns `nil` (no-op).
   - **Test 4.4: Initialization Failure**: `NewStore` with an uncreatable directory path returns an error and cleans up DB resources without descriptor leaks.

---

## 3. Caveats
- **SQLite In-Memory Limitation**: SQLite in-memory databases (`:memory:`) do not support multi-connection WAL mode or shared file locks. All WAL and concurrency tests MUST use temporary file databases created via `t.TempDir()`.
- **Timestamp Formatting**: SQL comparison on ISO-8601 strings requires exact RFC3339 / RFC3339Nano UTC formatting (`time.Now().UTC().Format(time.RFC3339Nano)`). Tests must format timestamps consistently to prevent lexicographical sorting bugs in SQLite string queries.
- **Race Detector Overhead**: Running 50 goroutines under `go test -race` increases execution time slightly. Set reasonable per-goroutine iteration counts (e.g. 20 events per routine) so the race suite executes in under 2 seconds.

---

## 4. Conclusion & Test Suite Implementation Blueprint

### Proposed Test File Structure (`pkg/state/store_test.go`)

```go
package state_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/reinframe/reinframe/pkg/protocol"
	"github.com/reinframe/reinframe/pkg/state"
)

// Helper function to create a clean test store in a temp directory
func setupTestStore(t *testing.T) (*state.Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_events.db")
	opts := state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5 * time.Second,
	}
	store, err := state.NewStore(opts)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() {
		store.Close()
	})
	return store, dbPath
}
```

### Complete Test Case List to Build

1. `TestStore_Migrations`: Verifies table creation and index setup on fresh DB.
2. `TestStore_AppendAndQuery_Basic`: Basic single-event append and retrieve roundtrip.
3. `TestStore_AppendEvents_Batch`: Batch insertion and atomic commit verification.
4. `TestStore_AppendEvents_RollbackOnFailure`: Verifies transaction rollback on duplicate sequence inside batch.
5. `TestStore_GetLatestSequenceNum`: Verifies sequence retrieval for empty vs active sessions.
6. `TestStore_QueryFilters_Matrix`: Comprehensive table-driven tests for session, event types, sequence bounds, time bounds, limit, offset, and sort order.
7. `TestStore_Constraint_DuplicateSequence`: Asserts `ErrDuplicateSequence` on sequence collision.
8. `TestStore_Constraint_DuplicateEventID`: Asserts `ErrDuplicateEventID` on event_id collision.
9. `TestStore_Validation_InvalidEvents`: Asserts `ErrInvalidEvent` for nil/empty fields.
10. `TestStore_Closed_Operations`: Asserts `ErrStoreClosed` post `Close()`.
11. `TestStore_Close_Idempotent`: Asserts multiple `Close()` calls succeed safely.
12. `TestStore_Concurrency_50Goroutines_Parallel`: 50 goroutines writing 20 events each simultaneously.
13. `TestStore_Concurrency_WritersAndReaders`: 50 writers + 10 continuous readers race verification.
14. `TestStore_Concurrency_SharedSession_SequenceCollision`: 10 goroutines racing to insert sequence 1 on shared session.

---

## 5. Verification Method

### 5.1 Verification Commands
Run the following commands to execute and verify the complete test suite:
```bash
# 1. Run unit and race tests for pkg/state
go test -v -race ./pkg/state/...

# 2. Run unit and race tests across all packages
go test -v -race ./pkg/...
```

### 5.2 Verification Checklist
- [ ] `go test -v -race ./pkg/state/...` completes with zero race condition warnings.
- [ ] 50-goroutine concurrent append test passes in under 3 seconds without `SQLITE_BUSY` errors.
- [ ] `QueryEvents` filter matrix correctly verifies all combinations of Session, EventTypes, Sequence Bounds, Time Bounds, Limit/Offset, and Order.
- [ ] `ErrDuplicateSequence` and `ErrDuplicateEventID` sentinel errors are properly returned and validated with `errors.Is`.
- [ ] `AppendEvents` batch rollback is verified on failure.
- [ ] Store operations post-`Close()` consistently return `ErrStoreClosed`.
