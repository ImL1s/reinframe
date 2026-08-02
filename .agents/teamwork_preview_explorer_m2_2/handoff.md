# Analysis & Handoff Report: Milestone 2 (Issue #9: Append-Only Event Store & SQLite WAL Engine)

## 1. Observation
- **Go Environment**: Go version `go1.26.0` (module target `go 1.25.0`), target OS `darwin/arm64`, `CGO_ENABLED=1`.
- **Existing Codebase**:
  - `go.mod` (lines 1–6): Defines module `github.com/reinframe/reinframe` with `github.com/santhosh-tekuri/jsonschema/v5 v5.3.1`. No SQLite driver currently added.
  - `pkg/protocol/schema.go`:
    - `AgentEvent` (lines 33–40): Contains `EventID` (string), `SessionID` (string), `SequenceNum` (int64), `EventType` (string), `Timestamp` (time.Time), `Payload` (json.RawMessage).
    - `AuditRecord` (lines 246–254): Contains `AuditID` (string), `SessionID` (string), `Actor` (string), `Category` (string), `Summary` (string), `DetailJSON` (json.RawMessage), `RecordedAt` (time.Time).
  - Existing protocol tests pass cleanly (`go test -v -race ./pkg/protocol/...`).
  - Directory `pkg/state` does not yet exist.
- **Milestone 2 Scope**: Requires building an append-only event store backed by SQLite with WAL mode (`journal_mode=WAL`), schema migrations (`001_initial_events.sql`), `AppendEvent`/`AppendEvents`/`QueryEvents`/`GetLatestSequenceNum` APIs, multi-goroutine safety, and a 50-routine concurrent race test suite.

---

## 2. Logic Chain

### 2.1 SQLite Driver Selection (`modernc.org/sqlite` vs `github.com/mattn/go-sqlite3`)
- **`modernc.org/sqlite` (Pure Go)**:
  - **Pros**: 100% CGO-free. Enables seamless cross-platform binary builds for Windows, macOS, and Linux without requiring host C compilers or cross-toolchains. Race detector operates without CGO boundary noise.
  - **Pragma Format**: Connection string URI `file:path.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)` or executed via `PRAGMA` SQL statements post-connect.
  - **Driver Name**: `"sqlite"`.
- **`github.com/mattn/go-sqlite3` (CGO C bindings)**:
  - **Pros**: Highly mature, standard C-based driver.
  - **Cons**: Requires `CGO_ENABLED=1` and platform-specific gcc/clang build tools for every target OS.
  - **Driver Name**: `"sqlite3"`.
- **Recommendation**: `modernc.org/sqlite` is recommended for Reinframe's cross-platform supervisor mission to ensure zero CGO compilation dependencies across target agent hosts.

### 2.2 SQLite WAL Engine & Concurrency Control
1. **PRAGMA Settings**:
   - `PRAGMA journal_mode=WAL;` — Enables Write-Ahead Logging. Readers do not block writers, and writers do not block readers.
   - `PRAGMA busy_timeout=5000;` — Instructs SQLite to wait up to 5000ms (5 seconds) when locked before returning `SQLITE_BUSY`.
   - `PRAGMA synchronous=NORMAL;` — Optimal performance in WAL mode; fsync occurs only at checkpointing while maintaining WAL durability.
   - `PRAGMA foreign_keys=ON;` — Enforces relational integrity across tables.
2. **`BEGIN IMMEDIATE` & Lock Escalation Prevention**:
   - Default SQLite transactions (`BEGIN DEFERRED`) acquire read locks initially and attempt to promote to write locks on `INSERT`/`UPDATE`. Concurrent goroutines attempting simultaneous lock promotion hit `SQLITE_BUSY` deadlocks.
   - Using `BEGIN IMMEDIATE` explicitly acquires a write lock at transaction initialization. Combined with Go-level `sync.RWMutex` on the `Store` instance, in-process append race conditions are completely serialized without blocking concurrent read operations (`QueryEvents`).

### 2.3 Schema & Indexing Design (`pkg/state/migrations/001_initial_events.sql`)
1. **`events` Table**:
   - `event_id` TEXT PRIMARY KEY NOT NULL
   - `session_id` TEXT NOT NULL
   - `sequence_num` INTEGER NOT NULL
   - `event_type` TEXT NOT NULL
   - `timestamp` TEXT NOT NULL (RFC3339Nano formatted)
   - `payload` TEXT NOT NULL
   - `CONSTRAINT idx_events_session_seq UNIQUE (session_id, sequence_num)`
2. **`audit_records` Table**:
   - `audit_id` TEXT PRIMARY KEY NOT NULL
   - `session_id` TEXT NOT NULL
   - `actor` TEXT NOT NULL
   - `category` TEXT NOT NULL
   - `summary` TEXT NOT NULL
   - `detail_json` TEXT
   - `recorded_at` TEXT NOT NULL (RFC3339Nano formatted)
3. **`schema_migrations` Table**:
   - `version` INTEGER PRIMARY KEY NOT NULL
   - `name` TEXT NOT NULL
   - `applied_at` TEXT NOT NULL
4. **Required Indexes**:
   - `CREATE INDEX idx_events_session_id ON events(session_id);`
   - `CREATE INDEX idx_events_event_type ON events(event_type);`
   - `CREATE INDEX idx_events_timestamp ON events(timestamp);`
   - `CREATE INDEX idx_events_sequence_num ON events(sequence_num);`
   - `CREATE INDEX idx_events_session_type_seq ON events(session_id, event_type, sequence_num);`
   - `CREATE INDEX idx_audit_records_session_id ON audit_records(session_id);`
   - `CREATE INDEX idx_audit_records_recorded_at ON audit_records(recorded_at);`

### 2.4 Query Engine & Filtering (`QueryEvents` & `GetLatestSequenceNum`)
- **`QueryEvents(ctx, filter)`**:
  - Dynamically builds SQL query based on `EventFilter` fields (`SessionID`, `EventTypes`, `StartSequence`, `EndSequence`, `StartTime`, `EndTime`).
  - Supports ordering by sequence number (`ASC` or `DESC` based on `filter.Ascending`).
  - Applies `LIMIT` and `OFFSET` pagination.
  - Reconstructs `protocol.AgentEvent` objects (parsing timestamps with `time.RFC3339Nano`).
- **`GetLatestSequenceNum(ctx, sessionID)`**:
  - Executes `SELECT COALESCE(MAX(sequence_num), 0) FROM events WHERE session_id = ?`.
  - Returns `0` if no events exist for the given session.

### 2.5 Error Handling for Constraint Violations
- Define sentinel errors in `pkg/state/store.go`:
  - `ErrDuplicateSequence` (returned when `UNIQUE(session_id, sequence_num)` is violated).
  - `ErrDuplicateEventID` (returned when `PRIMARY KEY (event_id)` is violated).
  - `ErrInvalidEvent` (returned when required fields are missing).
- Inspection logic checks for SQLite error code or message string matching `UNIQUE constraint failed`.

---

## 3. Caveats
- **In-Memory vs File-Based SQLite in WAL Mode**: SQLite in-memory databases (`:memory:`) do not support WAL mode or file locks shared across multiple connections. Tests testing WAL mode specifically should use temporary file paths (e.g. `filepath.Join(t.TempDir(), "test.db")`).
- **Driver Pragmas**: If `modernc.org/sqlite` is imported, ensure driver registration matches `import _ "modernc.org/sqlite"`. If `github.com/mattn/go-sqlite3` is preferred by the team, `import _ "github.com/mattn/go-sqlite3"` can be swapped seamlessly.
- **Timestamp Formatting**: Always format timestamps as `time.RFC3339Nano` (or `time.RFC3339`) to guarantee standard lexicographical sorting in SQL `WHERE` and `ORDER BY` clauses.

---

## 4. Conclusion & Concrete Implementation Blueprint

### Architecture & Files to Create
1. `pkg/state/migrations/001_initial_events.sql`
2. `pkg/state/migration.go`
3. `pkg/state/store.go`
4. `pkg/state/store_test.go`

### Implementation Specs for Worker

#### A. `001_initial_events.sql`
```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    event_id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL,
    sequence_num INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    payload TEXT NOT NULL,
    CONSTRAINT unq_events_session_seq UNIQUE (session_id, sequence_num)
);

CREATE TABLE IF NOT EXISTS audit_records (
    audit_id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    category TEXT NOT NULL,
    summary TEXT NOT NULL,
    detail_json TEXT,
    recorded_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_event_type ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_sequence_num ON events(sequence_num);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_session_type_seq ON events(session_id, event_type, sequence_num);

CREATE INDEX IF NOT EXISTS idx_audit_records_session_id ON audit_records(session_id);
CREATE INDEX IF NOT EXISTS idx_audit_records_recorded_at ON audit_records(recorded_at);
```

#### B. `migration.go`
- Uses `//go:embed migrations/*.sql` (`embed.FS`).
- `RunMigrations(db *sql.DB) error` reads migration files sorted by filename, executes each unapplied migration inside a transaction, and records entries in `schema_migrations`.

#### C. `store.go`
- `StoreOptions` struct:
  ```go
  type StoreOptions struct {
      DatabasePath string
      BusyTimeout  time.Duration
      MaxOpenConns int
      MaxIdleConns int
  }
  ```
- `Store` struct:
  ```go
  type Store struct {
      db *sql.DB
      mu sync.RWMutex
  }
  ```
- `NewStore(opts StoreOptions) (*Store, error)`:
  - Default `BusyTimeout`: 5000ms.
  - Apply PRAGMAs (`journal_mode=WAL`, `busy_timeout=5000`, `synchronous=NORMAL`, `foreign_keys=ON`).
  - Run embedded migrations.
- `AppendEvent(ctx context.Context, event *protocol.AgentEvent) error`:
  - Validates `event` not nil and non-empty IDs.
  - Wraps execution in `s.mu.Lock()` and `BEGIN IMMEDIATE` transaction.
  - Maps `UNIQUE constraint failed` to `ErrDuplicateSequence` / `ErrDuplicateEventID`.
- `AppendEvents(ctx context.Context, events []*protocol.AgentEvent) error`:
  - Batch insert in single transaction under `s.mu.Lock()`.
- `QueryEvents(ctx context.Context, filter EventFilter) ([]*protocol.AgentEvent, error)`:
  - Read operation using `s.mu.RLock()`.
- `GetLatestSequenceNum(ctx context.Context, sessionID string) (int64, error)`:
  - Query max sequence number under `s.mu.RLock()`.

---

## 5. Verification Method

### 5.1 Build & Test Commands
Execute the following commands to verify implementation:
1. `go mod tidy`
2. `go test -v -race ./pkg/state/...`
3. `go test -v -race ./pkg/...`

### 5.2 Verification Checklist
- [ ] SQLite connection initialized with `journal_mode=WAL` and `busy_timeout=5000ms`.
- [ ] Schema migration `001_initial_events.sql` creates tables `events`, `audit_records`, `schema_migrations` and all specified indexes.
- [ ] `AppendEvent` inserts events and returns `ErrDuplicateSequence` on sequence collisions.
- [ ] `AppendEvents` performs atomic batch insertion.
- [ ] `QueryEvents` correctly filters by session_id, event_types, sequence bounds, time bounds, limit, offset, and ascending order.
- [ ] `GetLatestSequenceNum` returns 0 for non-existent sessions and max sequence number for active sessions.
- [ ] 50-goroutine concurrent race test passes without race conditions or `SQLITE_BUSY` errors (`go test -v -race ./pkg/state/...`).
