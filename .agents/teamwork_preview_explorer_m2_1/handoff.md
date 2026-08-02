# Handoff Report — Milestone 2 (Issue #9: Append-Only Event Store & SQLite WAL Engine)

## 1. Observation

### File & Codebase Findings
1. **`go.mod`**:
   - Location: `/Users/iml1s/Documents/mine/reinframe/go.mod`
   - Content:
     ```go
     module github.com/reinframe/reinframe

     go 1.25.0

     require github.com/santhosh-tekuri/jsonschema/v5 v5.3.1 // indirect
     ```
   - Observation: SQLite driver (`github.com/mattn/go-sqlite3`) is not yet added to dependencies.

2. **`pkg/protocol/schema.go`**:
   - `AgentEvent` struct definition (lines 33-40):
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
   - `AuditRecord` struct definition (lines 246-254):
     ```go
     type AuditRecord struct {
         AuditID    string          `json:"audit_id" redact:"none"`
         SessionID  string          `json:"session_id" redact:"none"`
         Actor      string          `json:"actor" redact:"none"`
         Category   string          `json:"category" redact:"none"`
         Summary    string          `json:"summary" redact:"sensitive"`
         DetailJSON json.RawMessage `json:"detail_json,omitempty" redact:"sanitize"`
         RecordedAt time.Time       `json:"recorded_at" redact:"none"`
     }
     ```

3. **`pkg/state/` Directory**:
   - Does not exist yet. Needs creation along with `pkg/state/migrations/`.

4. **Git Branch Status**:
   - Command: `git branch -a`
   - Result:
     ```
       issue-6-canonical-agent-event-schema
     * issue-7-capability-manifest-negotiation
       main
       remotes/origin/issue-6-canonical-agent-event-schema
       remotes/origin/main
     ```
   - Observation: Target branch `issue-9-sqlite-wal-event-store` does not exist yet and must be created from `main`.

---

## 2. Logic Chain

1. **Branch Isolation**: To conform to strict issue-driven workflow requirements, branch `issue-9-sqlite-wal-event-store` must be checked out from `main`.
2. **Dependency Management**: Adding `github.com/mattn/go-sqlite3` to `go.mod` provides standard `database/sql` driver bindings for SQLite with full support for DSN URI configuration options (`_journal_mode=WAL`, `_busy_timeout=5000`, `_synchronous=NORMAL`, `_foreign_keys=ON`).
3. **DDL Schema Design (`001_initial_events.sql`)**:
   - Table `events`: Stores canonical `AgentEvent` payloads. A compound UNIQUE constraint `UNIQUE(session_id, sequence_num)` guarantees strict append-only sequence ordering per session.
   - Table `audit_records`: Persists audit log entries.
   - Table `schema_migrations`: Tracks applied migration versions.
   - Indexes on `(session_id, sequence_num)`, `(session_id, event_type)`, and `timestamp` ensure high-throughput query execution.
4. **Migration Engine (`migration.go`)**:
   - Leverages `embed.FS` to embed `migrations/*.sql`.
   - Reads SQL files sequentially, checks `schema_migrations`, executes pending migrations inside a SQL transaction, and records completed version numbers.
5. **Thread-Safe Event Store Engine (`store.go`)**:
   - `Store` struct wraps `*sql.DB` and `sync.RWMutex`.
   - `NewStore(opts StoreOptions)` builds DSN with WAL mode, busy timeout, and connection limits (`MaxOpenConns`, `MaxIdleConns`), initializes DB connection, and executes migrations.
   - `AppendEvent` & `AppendEvents`: Acquire `mu.Lock()`, initiate `BEGIN IMMEDIATE` transactions, perform parameterized `INSERT` statements, and commit.
   - `QueryEvents`: Acquires `mu.RLock()`, dynamically builds SQL query filters (session_id, event_types, start/end sequence, start/end time, limit/offset, asc/desc), and scans rows into `[]*protocol.AgentEvent`.
   - `GetLatestSequenceNum`: Acquires `mu.RLock()`, executes `SELECT COALESCE(MAX(sequence_num), 0) FROM events WHERE session_id = ?`.
6. **Concurrency & Verification (`store_test.go`)**:
   - Validates DDL migration, event appends, sequence conflict handling, query filters, and sequence number lookup.
   - Includes a 50-goroutine concurrent append race test verified via `go test -v -race ./pkg/state/...`.

---

## 3. Caveats

1. **CGO Requirement**: `github.com/mattn/go-sqlite3` requires CGO enabled for compilation.
2. **Timestamp Normalization**: `time.Time` fields must be serialized consistently to UTC RFC3339Nano (e.g. `2026-08-02T13:40:35.123456789Z`) prior to SQL insertion to ensure exact ordering and range matching in SQLite SQL query filters.
3. **SQLite WAL Concurrency Strategy**: WAL mode allows concurrent reads alongside a single write operation. Utilizing `sync.RWMutex` combined with `_busy_timeout=5000` prevents `SQLITE_BUSY` lock contention under concurrent multi-goroutine write stress.

---

## 4. Conclusion & Recommended Fix Strategy

### Phase 1: Git & Workspace Setup
1. Checkout branch `issue-9-sqlite-wal-event-store` from `main`:
   ```bash
   git checkout main
   git checkout -b issue-9-sqlite-wal-event-store
   ```
2. Add SQLite dependency:
   ```bash
   go get github.com/mattn/go-sqlite3
   ```

### Phase 2: DDL & Migration Engine
1. Create `pkg/state/migrations/001_initial_events.sql`:
   ```sql
   CREATE TABLE IF NOT EXISTS schema_migrations (
       version INTEGER PRIMARY KEY,
       name TEXT NOT NULL,
       applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
   );

   CREATE TABLE IF NOT EXISTS events (
       event_id TEXT PRIMARY KEY,
       session_id TEXT NOT NULL,
       sequence_num INTEGER NOT NULL,
       event_type TEXT NOT NULL,
       timestamp TEXT NOT NULL,
       payload TEXT NOT NULL,
       UNIQUE (session_id, sequence_num)
   );

   CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, sequence_num);
   CREATE INDEX IF NOT EXISTS idx_events_session_type ON events(session_id, event_type);
   CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);

   CREATE TABLE IF NOT EXISTS audit_records (
       audit_id TEXT PRIMARY KEY,
       session_id TEXT NOT NULL,
       actor TEXT NOT NULL,
       category TEXT NOT NULL,
       summary TEXT NOT NULL,
       detail_json TEXT,
       recorded_at TEXT NOT NULL
   );

   CREATE INDEX IF NOT EXISTS idx_audit_session_cat ON audit_records(session_id, category);
   ```

2. Create `pkg/state/migration.go`:
   - Embed `migrations/*.sql` using `//go:embed migrations/*.sql`.
   - Export `RunMigrations(db *sql.DB) error`.

### Phase 3: Store Engine Implementation
1. Create `pkg/state/store.go`:
   - Define `StoreOptions`, `EventFilter`, and `Store` struct.
   - Implement `NewStore`, `AppendEvent`, `AppendEvents`, `QueryEvents`, `GetLatestSequenceNum`, and `Close`.

### Phase 4: Unit & Race Test Suite
1. Create `pkg/state/store_test.go`:
   - `TestNewStore_Migration`
   - `TestAppendAndQueryEvents`
   - `TestSequenceDuplicationError`
   - `TestGetLatestSequenceNum`
   - `TestConcurrentAppends_Race` (50 concurrent routines writing events)

---

## 5. Verification Method

1. **Compilation & Unit Tests**:
   ```bash
   go test -v ./pkg/state/...
   ```
2. **Race Condition Verification**:
   ```bash
   go test -v -race ./pkg/state/...
   ```
3. **Full Project Race Check**:
   ```bash
   go test -v -race ./pkg/...
   ```
4. **Git Branch Verification**:
   ```bash
   git branch
   ```
