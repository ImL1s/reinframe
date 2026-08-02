# Handoff Report: Issue #9 Design & Specification Survey
**Survey 3**: Append-Only Event Store & SQLite WAL Engine Explorer
**Target Branch**: `issue-9-sqlite-wal-event-store`
**Working Directory**: `/Users/iml1s/Documents/mine/reinframe/.agents/teamwork_preview_explorer_survey_3`

---

## 1. Observation

### 1.1 Requirements Context
From `/Users/iml1s/Documents/mine/reinframe/ORIGINAL_REQUEST.md` (lines 43–44 & 59–63):
- **Requirement R2**: "Build SQLite WAL-backed event store (`pkg/state/store.go`), schema migration engine (`pkg/state/migrations/001_initial_events.sql`), `AppendEvent` and `QueryEvents` methods with multi-goroutine safety. Write unit tests in `pkg/state/store_test.go`."
- **Acceptance Criteria**:
  - SQLite connection configured with `journal_mode=WAL` and busy timeout.
  - `001_initial_events.sql` migration runs cleanly on fresh DB.
  - `AppendEvent` and `QueryEvents` pass concurrent race tests (`go test -race ./pkg/state/...`).
  - Pull Request opened for Issue #9 on branch `issue-9-sqlite-wal-event-store`.

From `/Users/iml1s/Documents/mine/reinframe/.agents/teamwork_preview_explorer_survey_3/DISPATCH.md` (lines 10–17):
- Target handoff output detailing:
  1. SQLite WAL event store requirements for `pkg/state/store.go`.
  2. Schema migration engine & SQL schema file `pkg/state/migrations/001_initial_events.sql`.
  3. Details of `AppendEvent` and `QueryEvents` methods, event data types/structs.
  4. Concurrency & multi-goroutine safety specifications (WAL mode settings, connection pooling/mutexes, transaction management).
  5. Unit test requirements in `pkg/state/store_test.go` (including race testing).
  6. Target git branch: `issue-9-sqlite-wal-event-store`.

### 1.2 Existing Codebase Observations
1. **Module & Dependencies (`go.mod`)**:
   - `go 1.25.0`
   - Module: `github.com/reinframe/reinframe`
   - Currently contains `github.com/santhosh-tekuri/jsonschema/v5 v5.3.1`.
   - SQLite driver is not yet added to `go.mod`.
2. **Canonical Event Data Model (`pkg/protocol/schema.go`, lines 33–40)**:
   ```go
   type AgentEvent struct {
       EventID     string          `json:"event_id" redact:"none"`
       SessionID   string          `json:"session_id" redact:"none"`
       SequenceNum int64           `json:"sequence_num" redact:"none"`
       EventType   string          `json:"event_type" redact:"none"`
       Timestamp   time.Time       `json:"timestamp" redact:"none"`
       Payload     json.RawMessage `json:"payload" redact:"sanitize"`
   }
   ```
3. **Directory Structure (`pkg/state`)**:
   - `pkg/state/` directory does not yet exist and needs to be created on branch `issue-9-sqlite-wal-event-store`.

---

## 2. Logic Chain

### Step 2.1: SQLite Driver Selection & Pragma Configuration
To achieve cross-platform compliance (macOS, Linux, Windows) without requiring a C compiler (CGO) in user environments, `modernc.org/sqlite` is recommended (or `github.com/mattn/go-sqlite3` with CGO).
The connection parameters must strictly configure SQLite in WAL mode:
1. `PRAGMA journal_mode = WAL;` — Enables Write-Ahead Logging for concurrent non-blocking reads during writes.
2. `PRAGMA busy_timeout = 5000;` — Instructs SQLite to wait up to 5,000ms when encountering lock contention before throwing `SQLITE_BUSY`.
3. `PRAGMA synchronous = NORMAL;` — Provides high write throughput while maintaining durability under WAL mode.
4. `PRAGMA foreign_keys = ON;` — Ensures relational integrity enforcement.

Connection string DSN format:
`file:<path>?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_foreign_keys=ON`

### Step 2.2: Schema Migration Engine & `001_initial_events.sql`
A lightweight, zero-dependency SQL migration engine must be implemented in `pkg/state/migration.go` utilizing Go 1.16+ `embed.FS` (`//go:embed migrations/*.sql`).

**Migration Tracking Table**:
```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

**SQL Schema (`pkg/state/migrations/001_initial_events.sql`)**:
```sql
-- Initial Schema Migration for Reinframe Event Store & Audit Log

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    session_id TEXT NOT NULL,
    sequence_num INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    payload TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_session_seq UNIQUE (session_id, sequence_num)
);

CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, sequence_num);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);

CREATE TABLE IF NOT EXISTS audit_records (
    audit_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    category TEXT NOT NULL,
    summary TEXT NOT NULL,
    detail_json TEXT,
    recorded_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_session ON audit_records(session_id);
```

### Step 2.3: Store Data Structures & Public API Specification
File: `pkg/state/store.go`

```go
package state

import (
    "context"
    "database/sql"
    "sync"
    "time"
    "github.com/reinframe/reinframe/pkg/protocol"
)

type Store struct {
    db *sql.DB
    mu sync.RWMutex // Protects concurrent transaction boundary if needed
}

type EventFilter struct {
    SessionID     string
    EventTypes    []string
    StartSequence *int64
    EndSequence   *int64
    StartTime     *time.Time
    EndTime       *time.Time
    Limit         int
    Offset        int
    Ascending     bool
}

type StoreOptions struct {
    DatabasePath string
    BusyTimeout  time.Duration
    MaxOpenConns int
    MaxIdleConns int
}
```

#### API Method Signatures:
1. `NewStore(opts StoreOptions) (*Store, error)`:
   - Opens SQLite connection with DSN parameters.
   - Sets connection pool parameters (`db.SetMaxOpenConns`, `db.SetMaxIdleConns`).
   - Runs embedded migrations via `RunMigrations(db)`.
2. `AppendEvent(ctx context.Context, event *protocol.AgentEvent) error`:
   - Validates `event != nil`, `event.EventID != ""`, `event.SessionID != ""`.
   - If `event.SequenceNum <= 0`, queries `MAX(sequence_num)` for `SessionID` and sets `SequenceNum = max + 1`.
   - Executes `BEGIN IMMEDIATE TRANSACTION` to acquire write lock immediately.
   - Inserts row into `events`.
   - Maps SQLite constraint violations to domain errors: `ErrDuplicateSequence`, `ErrDuplicateEventID`.
3. `AppendEvents(ctx context.Context, events []*protocol.AgentEvent) error`:
   - Batch appends multiple events within a single `BEGIN IMMEDIATE TRANSACTION` for optimal disk IO throughput.
4. `QueryEvents(ctx context.Context, filter EventFilter) ([]*protocol.AgentEvent, error)`:
   - Dynamically constructs parameterized `SELECT` query with safe `WHERE` clauses.
   - Orders by `sequence_num ASC` (default) or `DESC`.
   - Applies `LIMIT` and `OFFSET`.
   - Unmarshals `payload` string back into `json.RawMessage`.
5. `GetLatestSequenceNum(ctx context.Context, sessionID string) (int64, error)`:
   - Returns highest sequence number for a session, or `0` if no events exist.
6. `Close() error`:
   - Safely closes database connection handle.

### Step 2.4: Multi-Goroutine Concurrency & Safety Specification
1. **Single Writer / Multiple Reader Architecture**:
   - SQLite WAL mode permits arbitrary concurrent readers alongside one active writer.
   - To guarantee zero `SQLITE_BUSY` contention under high-frequency goroutine appends:
     - `Store` maintains an internal `sync.Mutex` on `AppendEvent`/`AppendEvents` write paths.
     - Transactions explicitly use `BEGIN IMMEDIATE` (via `db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelLinearizable})` or explicit `db.ExecContext(ctx, "BEGIN IMMEDIATE")`).
2. **Connection Pooling**:
   - `MaxOpenConns`: Set to `25` (or `10` for embedded harness) to support concurrent readers.
   - `MaxIdleConns`: Set to `5`.
   - `ConnMaxLifetime`: Set to `1 hour`.
3. **Context Timeout Protection**:
   - Every DB call accepts `context.Context` to handle cancellation and prevent deadlocked routines from hanging indefinitely.

### Step 2.5: Unit Test Requirements & Race Testing (`pkg/state/store_test.go`)
The test suite must cover:
1. `TestStore_Migration`: Verifies fresh database initialization automatically runs `001_initial_events.sql` and records version `1` in `schema_migrations`.
2. `TestStore_AppendAndQuery`: Tests inserting a canonical `protocol.AgentEvent` and reading it back with exact field equality (`EventID`, `SessionID`, `SequenceNum`, `EventType`, `Timestamp`, `Payload`).
3. `TestStore_BatchAppend`: Tests inserting 100 events in a single batch transaction.
4. `TestStore_MonotonicSequence`: Verifies duplicate sequence insertion returns `ErrDuplicateSequence`.
5. `TestStore_QueryFiltering`: Tests filtering events by `SessionID`, `EventTypes`, sequence ranges, time windows, and limit/offset pagination.
6. `TestStore_ConcurrentRace`:
   - Runs 50 parallel goroutines each appending 20 events to the same `Store` instance.
   - Must be verified with `go test -v -race ./pkg/state/...`.
   - Asserts total persisted event count equals 1,000 without data races or unhandled `SQLITE_BUSY` errors.
7. `TestStore_ConcurrentReadWrite`:
   - Runs 20 concurrent writer goroutines and 20 concurrent reader goroutines continuously querying events.

---

## 3. Caveats

1. **SQLite Driver CGO vs Pure Go**:
   - `modernc.org/sqlite` is pure Go and avoids CGO toolchain dependencies on Windows/macOS/Linux cross-compilation.
   - If `github.com/mattn/go-sqlite3` is preferred by project maintainers, CGO must be enabled (`CGO_ENABLED=1`) during build and test runs. Both drivers register with `database/sql`.
2. **In-Memory Concurrency Traps**:
   - Using SQLite in-memory mode (`file::memory:?mode=memory&cache=shared`) in tests requires shared cache DSN to allow multiple connections to share the same in-memory database.
3. **Monotonicity Across Process Restarts**:
   - Sequence numbers per session must be verified against SQLite `MAX(sequence_num)` upon process restart to prevent sequence collisions.

---

## 4. Conclusion

Issue #9 (Append-Only Event Store & SQLite WAL Engine) design and specification is fully defined and actionable.

### Summary of Deliverables to Implement on Branch `issue-9-sqlite-wal-event-store`:
1. `pkg/state/migrations/001_initial_events.sql` — Schema definition for `events`, `audit_records`, and `schema_migrations`.
2. `pkg/state/migration.go` — Embedded SQL migration runner.
3. `pkg/state/store.go` — SQLite WAL store implementation (`AppendEvent`, `AppendEvents`, `QueryEvents`, `GetLatestSequenceNum`, `Close`).
4. `pkg/state/store_test.go` — Complete unit and concurrent race test suite.

---

## 5. Verification Method

### How to Verify Implementation:
1. **Branch Checkout**:
   `git checkout -b issue-9-sqlite-wal-event-store`
2. **Package Test Command**:
   `go test -v -race ./pkg/state/...`
3. **Full Project Race Audit**:
   `go test -v -race ./pkg/...`
4. **Verification Pass Criteria**:
   - Zero compilation errors.
   - Clean migration run on fresh DB.
   - All concurrent append and query tests pass under Go race detector (`-race`).
