# Specification Mining & E2E Test Suite Architecture Report
**Project**: Reinframe (Anti-Tunnel Supervision Harness for AI Coding Agents)  
**Issues**: Issue #7 (Capability Manifest & Handshake Protocol) & Issue #9 (SQLite WAL Event Store)  
**Milestone**: M1 (Specification Mining & Test Suite Design)  
**Author**: teamwork_preview_spec_miner  
**Date**: 2026-08-02  

---

## 1. Executive Summary & Scope Overview

Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go. Its core system architecture relies on structured JSON-RPC 2.0 / NDJSON event streams, capability negotiation, and SQLite WAL-backed event persistence.

This specification mining report covers:
1. **Issue #7 (Capability Manifest & Handshake Protocol)**: 20 capability bitmask flags across 4 functional categories, manifest translation helpers (`ToBitmask`, `FromBitmask`, `HasCapability`), supervision level evaluation (Level 0: Observe, Level 1: Advisory, Level 2: Guarded, Level 3: Full-Control), and handshake negotiation engine (`NegotiateLevel`) with safe automatic degradation.
2. **Issue #9 (Append-Only Event Store & SQLite WAL Engine)**: Embedded SQLite storage configuration (`journal_mode=WAL`, `busy_timeout`), database schema migrations (`001_initial_events.sql`), thread-safe single and batch event appends (`AppendEvent`, `AppendEvents`), multi-dimensional event filtering (`QueryEvents`), sequence number tracking (`GetLatestSequenceNum`), and multi-goroutine concurrency control under high write load.
3. **4-Tier E2E Test Suite Design**: A comprehensive test plan containing:
   - **Tier 1**: Feature Coverage (40 test cases across 8 features).
   - **Tier 2**: Boundaries & Corner Cases (40 test cases across 8 features).
   - **Tier 3**: Cross-Feature Combinations (Pairwise interaction matrix between capability negotiation and WAL event store persistence).
   - **Tier 4**: Real-World Application Scenarios (End-to-End agent supervision session lifecycles).

---

## 2. Features Discovered

The following table synthesizes the 8 core features discovered and analyzed across Issues #7 and #9:

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Handshake Protocol | 20 Capability Bitmask Flags | uint64 bitmask representation of 20 agent capabilities across 4 functional categories | `CapabilityFlag` uint64 constants | Bitmask values (`1<<0` to `1<<19`) | Invalid flags ignored or return boolean false | Issue #7 Spec & `PROJECT.md` |
| 2 | Handshake Protocol | CapabilityManifest Helpers | Conversion methods (`ToBitmask`, `FromBitmask`, `HasCapability`) between struct and bitmask | `CapabilityManifest` struct or bitmask flag | Bitmask or `CapabilityManifest` struct | Returns false / empty manifest for zero bitmask | Issue #7 Spec & `PROJECT.md` |
| 3 | Handshake Protocol | Level Threshold Evaluator | Calculates maximum achievable supervision level (0-3) based on flag requirements | `CapabilityManifest` | `int` (Level 0, 1, 2, or 3) | Returns -1 for unsupported agent lacking Level 0 flags | Issue #7 Spec & `PROJECT.md` |
| 4 | Handshake Protocol | Handshake Negotiation Engine | Processes `HandshakeRequest` and returns `HandshakeResponse` with automatic degradation | `*HandshakeRequest` (SessionID, RequestedLevel, Manifest) | `*HandshakeResponse` (NegotiatedLevel, IsDegraded, MissingFlags) | Returns error on nil request, empty SessionID, or invalid level (<0 or >3) | Issue #7 Spec & `PROJECT.md` |
| 5 | WAL Event Store | SQL Schema & Migration Engine | Embedded SQL migration runner establishing `schema_migrations` and `events` tables | Database connection (`*sql.DB`) | Executed DDL schema, updated migration version | Returns error on locked DB or invalid SQL syntax | Issue #9 Spec & `PROJECT.md` |
| 6 | WAL Event Store | SQLite WAL Event Store Engine | Initializes SQLite with `journal_mode=WAL` and `busy_timeout`; appends single & batch events | `StoreOptions` (DatabasePath, BusyTimeout, MaxOpenConns, MaxIdleConns), `*protocol.AgentEvent` | `*Store` instance / `nil` error on successful append | Returns error on nil event, empty SessionID/EventType, or DB write failure | Issue #9 Spec & `PROJECT.md` |
| 7 | WAL Event Store | Event Query Engine | Multi-parameter filtering (`QueryEvents`) and sequence tracker (`GetLatestSequenceNum`) | `EventFilter` (SessionID, EventTypes, Sequence bounds, Time bounds, Limit, Offset, Ascending) | `[]*protocol.AgentEvent`, `int64` latest sequence | Returns error on invalid filter parameters (e.g. limit < 0) | Issue #9 Spec & `PROJECT.md` |
| 8 | Concurrency | Thread & Multi-Goroutine Safety | Concurrent read/write safety under WAL mode without `SQLITE_BUSY` errors | Multi-goroutine `AppendEvent` / `QueryEvents` calls | Consistent sequence ordering, zero lost events | Blocks up to `busy_timeout` or returns timeout error if lock unreleased | Issue #9 Spec & `PROJECT.md` |

---

## 3. Detailed Specifications & Behavioral Rules

### 3.1 Issue #7: Capability Manifest & Handshake Protocol Specification

#### Capability Flags (`CapabilityFlag uint64`)
20 distinct flags are defined as `uint64` constants using bit shifts (`1 << iota`):

1. **Inspection / Observation Category**:
   - `CapEventStream` (`1 << 0`, `0x00001`): NDJSON event stream streaming support. (Required for Level 0+).
   - `CapToolInspection` (`1 << 1`, `0x00002`): Tool execution argument & output inspection. (Required for Level 1+).
   - `CapDiffInspection` (`1 << 2`, `0x00004`): File modification diff inspection.
   - `CapCostTracking` (`1 << 3`, `0x00008`): LLM token usage and monetary cost tracking.
   - `CapHooks` (`1 << 4`, `0x00010`): Lifecycle hook execution.

2. **Execution / Control Category**:
   - `CapHeadless` (`1 << 5`, `0x00020`): Unattended headless mode execution.
   - `CapCLIControl` (`1 << 6`, `0x00040`): External CLI process intervention. (Required for Level 1+).
   - `CapPause` (`1 << 7`, `0x00080`): Session execution pause capability. (Required for Level 2+).
   - `CapCancel` (`1 << 8`, `0x00100`): Active task cancellation capability. (Required for Level 2+).
   - `CapResume` (`1 << 9`, `0x00200`): Paused task resumption capability. (Required for Level 2+).

3. **State / Rollback Category**:
   - `CapCheckpoint` (`1 << 10`, `0x00400`): Workspace git checkpoint creation. (Required for Level 2+).
   - `CapRollback` (`1 << 11`, `0x00800`): Git workspace state rollback capability. (Required for Level 2+).
   - `CapMCP` (`1 << 12`, `0x01000`): Model Context Protocol server/tool binding. (Required for Level 3).
   - `CapSubagents` (`1 << 13`, `0x02000`): Multi-agent sub-process orchestration. (Required for Level 3).
   - `CapExtensions` (`1 << 14`, `0x04000`): Custom plugin/extension execution.

4. **Provider / Model Category**:
   - `CapSwitchModel` (`1 << 15`, `0x08000`): Dynamic LLM model switching. (Required for Level 3).
   - `CapCustomProvider` (`1 << 16`, `0x10000`): Custom provider endpoint routing.
   - `CapOpenAICompat` (`1 << 17`, `0x20000`): OpenAI API-compatible interface.
   - `CapLocalModels` (`1 << 18`, `0x40000`): Local model inference support (e.g. Ollama/llama.cpp).
   - `CapSDK` (`1 << 19`, `0x80000`): Native language SDK control interface. (Required for Level 3).

#### Capability Manifest Struct & Conversion Helpers
```go
type CapabilityManifest struct {
    AgentID            string `json:"agent_id"`
    Version            string `json:"version"`
    IntegrationLevel   int    `json:"integration_level"`
    SupportsPause      bool   `json:"supports_pause"`
    SupportsCancel     bool   `json:"supports_cancel"`
    SupportsResume     bool   `json:"supports_resume"`
    SupportsCheckpoint bool   `json:"supports_checkpoint"`
    SupportsRollback   bool   `json:"supports_rollback"`
    SupportsMCP        bool   `json:"supports_mcp"`
    // Extended flags represented in bitmask helper:
    BitmaskFlags       uint64 `json:"bitmask_flags,omitempty"`
}

func (m *CapabilityManifest) ToBitmask() CapabilityFlag
func FromBitmask(mask CapabilityFlag) CapabilityManifest
func (m *CapabilityManifest) HasCapability(flag CapabilityFlag) bool
```
- `ToBitmask()` translates standard boolean fields and `BitmaskFlags` into a unified `CapabilityFlag`.
- `HasCapability(flag)` returns `(m.ToBitmask() & flag) == flag`.

#### Supervision Level Threshold Requirements
- **Level 0 (Observe)**: Requires `CapEventStream`.
- **Level 1 (Advisory)**: Requires Level 0 + `CapToolInspection` + `CapCLIControl`.
- **Level 2 (Guarded)**: Requires Level 1 + `CapPause` + `CapCancel` + `CapResume` + `CapCheckpoint` + `CapRollback`.
- **Level 3 (Full-Control)**: Requires Level 2 + `CapMCP` + `CapSubagents` + `CapSwitchModel` + `CapSDK`.

`EvaluateAchievableLevel(manifest CapabilityManifest) int`:
- Evaluates the bitmask against threshold masks.
- Returns highest level (0, 1, 2, 3) whose requirements are fully satisfied by `manifest`.
- Returns `-1` if even Level 0 requirements (`CapEventStream`) are missing.

#### Handshake Negotiation & Automatic Degradation Policy
```go
type HandshakeRequest struct {
    SessionID      string             `json:"session_id"`
    RequestedLevel int                `json:"requested_level"`
    Manifest       CapabilityManifest `json:"manifest"`
}

type HandshakeResponse struct {
    SessionID       string   `json:"session_id"`
    NegotiatedLevel int      `json:"negotiated_level"`
    IsDegraded      bool     `json:"is_degraded"`
    DegradedFrom    int      `json:"degraded_from,omitempty"`
    MissingFlags    []string `json:"missing_flags,omitempty"`
}

func NegotiateLevel(req *HandshakeRequest) (*HandshakeResponse, error)
```
- **Validation Rules**:
  - If `req == nil`, return `ErrNilRequest`.
  - If `req.SessionID == ""`, return `ErrEmptySessionID`.
  - If `req.RequestedLevel < 0` or `req.RequestedLevel > 3`, return `ErrInvalidRequestedLevel`.
- **Degradation Logic**:
  - `achievable := EvaluateAchievableLevel(req.Manifest)`
  - If `achievable < 0`: Return error `ErrUnsupportedAgent` (agent cannot even support passive event streaming).
  - If `achievable >= req.RequestedLevel`:
    - `NegotiatedLevel` = `req.RequestedLevel`
    - `IsDegraded` = `false`
    - `DegradedFrom` = `0`
    - `MissingFlags` = `nil`
  - If `achievable < req.RequestedLevel`:
    - `NegotiatedLevel` = `achievable`
    - `IsDegraded` = `true`
    - `DegradedFrom` = `req.RequestedLevel`
    - `MissingFlags` = List of string names for missing capability flags required to achieve `req.RequestedLevel`.

---

### 3.2 Issue #9: SQLite WAL Event Store Specification

#### SQLite Configuration & Connection Options
```go
type StoreOptions struct {
    DatabasePath string        // Path to SQLite db file or ":memory:"
    BusyTimeout  time.Duration // Default: 5000ms
    MaxOpenConns int           // Default: 10
    MaxIdleConns int           // Default: 5
}
```
- Must open connection using `modernc.org/sqlite` or `mattn/go-sqlite3`.
- Immediately on initialization, executes PRAGMAs:
  - `PRAGMA journal_mode = WAL;` (Enables Write-Ahead Logging for concurrent non-blocking reads).
  - `PRAGMA busy_timeout = 5000;` (Configures 5000ms lock wait timeout before throwing `SQLITE_BUSY`).
  - `PRAGMA synchronous = NORMAL;` (Provides high performance while remaining durable under WAL).
  - `PRAGMA foreign_keys = ON;`

#### Database Schema & Migration Engine (`001_initial_events.sql`)
```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS events (
    event_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    sequence_num INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    timestamp DATETIME NOT NULL,
    payload TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT uq_session_sequence UNIQUE (session_id, sequence_num)
);

CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, sequence_num);
CREATE INDEX IF NOT EXISTS idx_events_session_type ON events(session_id, event_type);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
```
- Migration engine embeds `001_initial_events.sql` and checks `schema_migrations` table version before executing. Safe for repeated runs (idempotent).

#### Event Store Core Operations

##### 1. `AppendEvent(ctx context.Context, event *protocol.AgentEvent) error`
- **Validation**: `event != nil`, `event.EventID != ""`, `event.SessionID != ""`, `event.EventType != ""`.
- **Sequence Num Auto-Assignment**:
  - If `event.SequenceNum == 0`, store calculates `nextSeq = GetLatestSequenceNum(ctx, event.SessionID) + 1` and assigns `event.SequenceNum = nextSeq`.
- **Insertion**: Inserts record into `events` table. If `(session_id, sequence_num)` violates unique constraint, returns `ErrDuplicateSequenceNum`.

##### 2. `AppendEvents(ctx context.Context, events []*protocol.AgentEvent) error`
- Wraps batch of events in a single SQLite transaction (`BEGIN TRANSACTION ... COMMIT`).
- Guarantees atomic persistence: all events succeed or transaction rolls back entirely on failure.
- Auto-increments sequence numbers sequentially across the batch if sequence numbers are zero.

##### 3. `QueryEvents(ctx context.Context, filter EventFilter) ([]*protocol.AgentEvent, error)`
```go
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
```
- Dynamic SQL query building:
  - If `SessionID != ""`: `WHERE session_id = ?`
  - If `len(EventTypes) > 0`: `AND event_type IN (?, ?, ...)`
  - If `StartSequence != nil`: `AND sequence_num >= ?`
  - If `EndSequence != nil`: `AND sequence_num <= ?`
  - If `StartTime != nil`: `AND timestamp >= ?`
  - If `EndTime != nil`: `AND timestamp <= ?`
  - Order: If `Ascending == true`, `ORDER BY sequence_num ASC`; else `ORDER BY sequence_num DESC`.
  - Pagination: If `Limit > 0`, `LIMIT ? OFFSET ?`.
- Deserializes `payload` back into `json.RawMessage` and constructs `[]*protocol.AgentEvent`.

##### 4. `GetLatestSequenceNum(ctx context.Context, sessionID string) (int64, error)`
- Executes `SELECT COALESCE(MAX(sequence_num), 0) FROM events WHERE session_id = ?`.
- Returns `0` if session has no persisted events.

---

## 4. Edge Cases, Error Behaviors & Degradation Rules

### 4.1 Edge Cases Table

| # | Feature | Input / Trigger | Observed / Expected Behavior |
|---|---------|-----------------|------------------------------|
| 1 | Handshake Protocol | `HandshakeRequest` with `nil` pointer | Return `ErrNilRequest` error immediately |
| 2 | Handshake Protocol | `HandshakeRequest` with empty `SessionID` | Return `ErrEmptySessionID` error |
| 3 | Handshake Protocol | `RequestedLevel` = -1 or 4 | Return `ErrInvalidRequestedLevel` error |
| 4 | Handshake Protocol | Manifest with zero bitmask (0 flags) | `EvaluateAchievableLevel` returns -1; `NegotiateLevel` returns `ErrUnsupportedAgent` |
| 5 | Handshake Protocol | Agent requests Level 3, has Level 2 flags + `CapMCP` + `CapSubagents`, but missing `CapSwitchModel` | Negotiates down to Level 2 (`NegotiatedLevel` = 2, `IsDegraded` = true, `DegradedFrom` = 3, `MissingFlags` = `["CapSwitchModel", "CapSDK"]`) |
| 6 | Handshake Protocol | Agent requests Level 2, has only Level 0 flags (`CapEventStream`) | Negotiates down to Level 0 (`NegotiatedLevel` = 0, `IsDegraded` = true, `DegradedFrom` = 2, `MissingFlags` lists Level 1 & 2 missing flags) |
| 7 | Handshake Protocol | Unknown extra flag bits set in bitmask (`0x8000000000000000`) | Helper functions preserve bitmask; threshold evaluator ignores unknown bits without error |
| 8 | WAL Event Store | `NewStore` with invalid directory path (`/nonexistent/path/db.sqlite`) | Return file path / IO error during DB creation |
| 9 | WAL Event Store | `AppendEvent` with `nil` event | Return `ErrNilEvent` error |
| 10 | WAL Event Store | `AppendEvent` with empty `EventID` or `EventType` | Return validation error (`ErrInvalidEvent`) |
| 11 | WAL Event Store | `AppendEvent` with duplicate `(SessionID, SequenceNum)` | SQLite unique constraint violation; returns `ErrDuplicateSequence` |
| 12 | WAL Event Store | `AppendEvents` with empty slice `[]*AgentEvent{}` | No-op, returns `nil` without opening transaction |
| 13 | WAL Event Store | `AppendEvents` where 5th event in a 10-event batch fails validation | Entire transaction rolls back; 0 events persisted |
| 14 | WAL Event Store | `QueryEvents` with `StartSequence` > `EndSequence` | Returns empty slice `[]*AgentEvent{}` (no match) without error |
| 15 | WAL Event Store | `QueryEvents` with `Limit` = 0 | Returns all matching events without pagination cap |
| 16 | WAL Event Store | `GetLatestSequenceNum` for non-existent `SessionID` | Returns sequence `0` and `nil` error |
| 17 | WAL Event Store | Concurrent `AppendEvent` from 50 goroutines simultaneously | All 50 appends succeed with unique, contiguous auto-incremented sequence numbers |
| 18 | WAL Event Store | `QueryEvents` executed while heavy background batch `AppendEvents` is running | Query completes instantly without `SQLITE_BUSY` due to WAL mode |
| 19 | WAL Event Store | Context cancelled (`ctx.Cancel()`) during `AppendEvent` / `QueryEvents` | Query/transaction aborted, returns `context.Canceled` |
| 20 | WAL Event Store | Store closed (`s.Close()`), then `AppendEvent` called | Returns `ErrStoreClosed` or `sql.ErrConnDone` |

---

## 5. 4-Tier E2E Test Suite Specification

### Tier 1: Feature Coverage (>=5 Tests per Feature across 8 Features = 40+ Tests Minimum)

#### Feature 1: 20 Capability Bitmask Flags (5 Tests)
- `TestTier1_CapFlags_BitmaskShiftValues`: Verify all 20 flag constants match expected `1 << iota` powers of 2.
- `TestTier1_CapFlags_Categories`: Verify flags correctly group into 4 categories (Observation, Execution, State, Provider).
- `TestTier1_CapFlags_BitwiseOR`: Verify combining multiple capability flags using bitwise OR produces exact bitmask.
- `TestTier1_CapFlags_BitwiseAND`: Verify isolating specific capability flags using bitwise AND.
- `TestTier1_CapFlags_StringFormatting`: Verify flag string conversion functions (`FlagNames()`) match constant names.

#### Feature 2: CapabilityManifest Helpers (5 Tests)
- `TestTier1_Manifest_ToBitmask_FullStruct`: Verify converting `CapabilityManifest` with all boolean fields `true` yields exact full bitmask.
- `TestTier1_Manifest_ToBitmask_PartialStruct`: Verify converting manifest with partial flags yields matching partial bitmask.
- `TestTier1_Manifest_FromBitmask_Roundtrip`: Verify `FromBitmask(m.ToBitmask())` reconstructs identical manifest struct.
- `TestTier1_Manifest_HasCapability_Present`: Verify `HasCapability` returns `true` for enabled flag.
- `TestTier1_Manifest_HasCapability_Absent`: Verify `HasCapability` returns `false` for disabled flag.

#### Feature 3: Level Threshold Evaluator (5 Tests)
- `TestTier1_LevelEval_Level0_Observe`: Verify manifest with `CapEventStream` only evaluates to Level 0.
- `TestTier1_LevelEval_Level1_Advisory`: Verify manifest with Level 0 + `CapToolInspection` + `CapCLIControl` evaluates to Level 1.
- `TestTier1_LevelEval_Level2_Guarded`: Verify manifest with Level 1 + `CapPause` + `CapCancel` + `CapResume` + `CapCheckpoint` + `CapRollback` evaluates to Level 2.
- `TestTier1_LevelEval_Level3_FullControl`: Verify manifest with Level 2 + `CapMCP` + `CapSubagents` + `CapSwitchModel` + `CapSDK` evaluates to Level 3.
- `TestTier1_LevelEval_SubZero_Unsupported`: Verify manifest missing `CapEventStream` evaluates to -1 (Unsupported).

#### Feature 4: Handshake Negotiation & Degradation Engine (5 Tests)
- `TestTier1_Negotiate_ExactMatch_Level3`: Verify requested Level 3 with Level 3 manifest returns `NegotiatedLevel=3`, `IsDegraded=false`.
- `TestTier1_Negotiate_ExactMatch_Level1`: Verify requested Level 1 with Level 1 manifest returns `NegotiatedLevel=1`, `IsDegraded=false`.
- `TestTier1_Negotiate_Degradation_Level3To2`: Verify requested Level 3 with Level 2 manifest returns `NegotiatedLevel=2`, `IsDegraded=true`, `DegradedFrom=3`.
- `TestTier1_Negotiate_Degradation_Level2To0`: Verify requested Level 2 with Level 0 manifest returns `NegotiatedLevel=0`, `IsDegraded=true`, `DegradedFrom=2`.
- `TestTier1_Negotiate_MissingFlagsReported`: Verify `MissingFlags` slice accurately lists flag names missing for requested level.

#### Feature 5: SQL Schema & Migration Engine (5 Tests)
- `TestTier1_Migration_FreshDB`: Verify `001_initial_events.sql` runs cleanly on fresh SQLite DB and creates tables.
- `TestTier1_Migration_Idempotency`: Verify running migration engine twice on same DB causes no errors or duplicate table errors.
- `TestTier1_Migration_SchemaVersionTracking`: Verify `schema_migrations` table records version `1` upon completion.
- `TestTier1_Migration_TableColumns`: Verify `events` table has correct columns (`event_id`, `session_id`, `sequence_num`, etc.).
- `TestTier1_Migration_IndexCreation`: Verify secondary indices (`idx_events_session_seq`, `idx_events_session_type`, etc.) exist.

#### Feature 6: SQLite WAL Event Store Engine (5 Tests)
- `TestTier1_Store_NewStore_WALMode`: Verify `NewStore` creates database file and PRAGMA `journal_mode` returns `wal`.
- `TestTier1_Store_AppendEvent_Single`: Verify appending single valid `AgentEvent` succeeds and persists to database.
- `TestTier1_Store_AppendEvent_AutoSequence`: Verify `AppendEvent` automatically assigns `sequence_num=1` when set to 0.
- `TestTier1_Store_AppendEvents_Batch`: Verify `AppendEvents` persists array of 10 events atomically in a single transaction.
- `TestTier1_Store_Close`: Verify `Close` flushes WAL and cleanly terminates database connections.

#### Feature 7: Event Query Engine & Filtering (5 Tests)
- `TestTier1_Query_BySessionID`: Verify `QueryEvents` filters events belonging strictly to target `SessionID`.
- `TestTier1_Query_ByEventType`: Verify `QueryEvents` with `EventTypes=["tool_call"]` returns only tool call events.
- `TestTier1_Query_BySequenceRange`: Verify `StartSequence` and `EndSequence` filter events by sequence boundaries.
- `TestTier1_Query_Pagination`: Verify `Limit` and `Offset` return exact paginated event subsets.
- `TestTier1_Query_GetLatestSequenceNum`: Verify `GetLatestSequenceNum` returns highest persisted sequence number for session.

#### Feature 8: Multi-Goroutine Concurrency & Race Safety (5 Tests)
- `TestTier1_Concurrency_ParallelAppends`: Verify 50 parallel goroutines calling `AppendEvent` complete without data race.
- `TestTier1_Concurrency_ParallelBatchAppends`: Verify 10 parallel goroutines calling `AppendEvents` persist all batch items cleanly.
- `TestTier1_Concurrency_ReadWhileWrite`: Verify concurrent `QueryEvents` calls during active `AppendEvent` operations do not block or fail.
- `TestTier1_Concurrency_SequenceContiguity`: Verify parallel appends produce strictly monotonic, contiguous sequence numbers.
- `TestTier1_Concurrency_RaceDetectorClean`: Verify running with `go test -race` reports zero data races across state package.

---

### Tier 2: Boundaries & Corner Cases (>=5 Tests per Feature across 8 Features = 40+ Tests Minimum)

#### Feature 1: 20 Capability Bitmask Flags (5 Boundary Tests)
- `TestTier2_CapFlags_ZeroBitmask`: Verify zero value `CapabilityFlag(0)` represents empty capabilities.
- `TestTier2_CapFlags_MaxUint64Bitmask`: Verify `CapabilityFlag(^uint64(0))` handles bitwise operations without overflow.
- `TestTier2_CapFlags_SingleBitShift20`: Verify 20th flag (`CapSDK` = `1 << 19`) shifts correctly without truncation.
- `TestTier2_CapFlags_UnknownHighBits`: Verify setting bit 63 does not crash flag evaluation functions.
- `TestTier2_CapFlags_ToggleFlag`: Verify setting and unsetting a flag via bitwise XOR/AND-NOT.

#### Feature 2: CapabilityManifest Helpers (5 Boundary Tests)
- `TestTier2_Manifest_EmptyStruct`: Verify `ToBitmask` on zero-initialized `CapabilityManifest{}` returns `0`.
- `TestTier2_Manifest_NilManifest`: Verify helper methods on nil manifest pointer handle safely or return default.
- `TestTier2_Manifest_MalformedJSON`: Verify unmarshaling invalid JSON into `CapabilityManifest` returns JSON syntax error.
- `TestTier2_Manifest_PartialBooleanMix`: Verify manifest with true `SupportsPause` but false `SupportsResume` generates exact partial bitmask.
- `TestTier2_Manifest_HasCapability_MultipleFlags`: Verify checking compound flag bitmasks returns false if any flag is missing.

#### Feature 3: Level Threshold Evaluator (5 Boundary Tests)
- `TestTier2_LevelEval_MissingOneLevel2Flag`: Verify manifest missing only `CapCheckpoint` degrades from Level 2 down to Level 1.
- `TestTier2_LevelEval_MissingOneLevel3Flag`: Verify manifest missing only `CapSDK` degrades from Level 3 down to Level 2.
- `TestTier2_LevelEval_Level0WithoutEventStream`: Verify manifest with all flags EXCEPT `CapEventStream` returns -1 (Unsupported).
- `TestTier2_LevelEval_SuperfluousLevel3FlagsAtLevel1`: Verify manifest with `CapMCP` but missing `CapPause` achieves only Level 1.
- `TestTier2_LevelEval_BoundaryAll20Flags`: Verify manifest with all 20 flags set evaluates to Level 3.

#### Feature 4: Handshake Negotiation & Degradation Engine (5 Boundary Tests)
- `TestTier2_Negotiate_NilRequest`: Verify `NegotiateLevel(nil)` returns explicit `ErrNilRequest`.
- `TestTier2_Negotiate_EmptySessionID`: Verify `NegotiateLevel` with `SessionID=""` returns `ErrEmptySessionID`.
- `TestTier2_Negotiate_InvalidRequestedLevel_Negative`: Verify `RequestedLevel = -1` returns `ErrInvalidRequestedLevel`.
- `TestTier2_Negotiate_InvalidRequestedLevel_OverMax`: Verify `RequestedLevel = 4` returns `ErrInvalidRequestedLevel`.
- `TestTier2_Negotiate_UnsupportedAgent_Error`: Verify negotiation with zero manifest returns `ErrUnsupportedAgent`.

#### Feature 5: SQL Schema & Migration Engine (5 Boundary Tests)
- `TestTier2_Migration_ReadOnlyDirectory`: Verify migration fails gracefully with permission error if DB file directory is read-only.
- `TestTier2_Migration_CorruptedMigrationTable`: Verify migration fails with explicit error if `schema_migrations` has invalid schema.
- `TestTier2_Migration_PreexistingOtherTables`: Verify migration does not alter or overwrite unrelated tables in existing SQLite database.
- `TestTier2_Migration_ClosedDBConnection`: Verify migration runner returns connection error when called on closed `*sql.DB`.
- `TestTier2_Migration_InterruptedMigrationRollback`: Verify partial migration failure rolls back schema changes cleanly.

#### Feature 6: SQLite WAL Event Store Engine (5 Boundary Tests)
- `TestTier2_Store_AppendEvent_Nil`: Verify `AppendEvent(ctx, nil)` returns `ErrNilEvent`.
- `TestTier2_Store_AppendEvent_EmptySessionID`: Verify `AppendEvent` with empty `SessionID` fails validation.
- `TestTier2_Store_AppendEvent_EmptyEventType`: Verify `AppendEvent` with empty `EventType` fails validation.
- `TestTier2_Store_AppendEvent_DuplicateSequence`: Verify manually appending event with duplicate `SequenceNum` returns unique constraint error.
- `TestTier2_Store_AppendEvents_PartialFailureRollback`: Verify failure of 5th item in batch rolls back items 1-4.

#### Feature 7: Event Query Engine & Filtering (5 Boundary Tests)
- `TestTier2_Query_EmptyStore`: Verify `QueryEvents` on store with 0 events returns empty non-nil slice `[]*AgentEvent{}`.
- `TestTier2_Query_InvertedSequenceRange`: Verify `StartSequence=10` and `EndSequence=5` returns empty slice.
- `TestTier2_Query_InvertedTimeRange`: Verify `StartTime` after `EndTime` returns empty slice.
- `TestTier2_Query_NonExistentSession`: Verify querying unknown `SessionID` returns empty slice without error.
- `TestTier2_Query_OffsetExceedsTotal`: Verify `Offset=100` on 10-event dataset returns empty slice.

#### Feature 8: Multi-Goroutine Concurrency & Race Safety (5 Boundary Tests)
- `TestTier2_Concurrency_CancelledContextAppend`: Verify `AppendEvent` with pre-cancelled `context.Context` aborts immediately.
- `TestTier2_Concurrency_CancelledContextQuery`: Verify `QueryEvents` with cancelled context returns `context.Canceled`.
- `TestTier2_Concurrency_StoreClosedDuringAppend`: Verify calling `Close()` while background goroutines are appending returns `ErrStoreClosed`.
- `TestTier2_Concurrency_BusyTimeoutExceeded`: Verify simulating locked database causes write to wait up to `BusyTimeout` before erroring.
- `TestTier2_Concurrency_HighContention500Routines`: Verify 500 concurrent goroutines perform appends without SQLite corruption or panic.

---

### Tier 3: Cross-Feature Combinations (Pairwise Interaction Matrix)

Tier 3 verifies seamless integration between Issue #7 (Handshake Negotiation Engine) and Issue #9 (SQLite WAL Event Store).

#### Pairwise Matrix
| Handshake State / Level | Persisted Event Type | Operation Flow | Expected Integrated Behavior |
|-------------------------|----------------------|----------------|------------------------------|
| Level 0 (Observe) | `agent_session`, `agent_event` | Negotiate L0 -> Append event stream | Store accepts event stream; denies tool execution interventions |
| Level 1 (Advisory) | `tool_call_event`, `review_request` | Negotiate L1 -> Append tool call | Store records tool calls; prompt advice appended to audit table |
| Level 2 (Guarded) | `checkpoint`, `rollback_result` | Negotiate L2 -> Append checkpoint | Store records Git checkpoint commit hash; rollback event persisted |
| Level 3 (Full-Control) | `mcp_event`, `provider_usage` | Negotiate L3 -> Append model switch | Store records subagent event, MCP tool call, and token usage |
| Degraded (L3 -> L2) | `intervention`, `error_fingerprint` | Negotiate L3 (Degrades to L2) -> Append intervention | Store records `HandshakeResponse` degradation event followed by L2 intervention events |

#### Test Cases (10 Pairwise Test Scenarios)
1. `TestTier3_Pairwise_HandshakeToStore_SessionInit`: Perform `NegotiateLevel` (L3), write `AgentSession` event to WAL store, verify sequence #1 queryable.
2. `TestTier3_Pairwise_DegradedHandshakeLogging`: Perform `NegotiateLevel` with missing flags (L3 -> L2 degradation), serialize `HandshakeResponse` into `AgentEvent` payload, persist to WAL store, and query degradation history.
3. `TestTier3_Pairwise_L0_ObserveOnlyPersistence`: Negotiate Level 0, stream 50 `AgentEvent` records to store, verify `QueryEvents` retrieves all 50 events in sequence.
4. `TestTier3_Pairwise_L2_GuardedCheckpointPersistence`: Negotiate Level 2, append `Checkpoint` event followed by `TestResultEvent`, verify sequence order in WAL store.
5. `TestTier3_Pairwise_L3_FullControlUsageTracking`: Negotiate Level 3, append `ProviderUsage` events alongside `ToolCallEvent` records, run `QueryEvents` with `EventTypes=["provider_usage"]`.
6. `TestTier3_Pairwise_ConcurrentNegotiationAndAppends`: 20 concurrent goroutines each perform a `NegotiateLevel` handshake and immediately append their initial `AgentSession` event to the WAL store. Verify all 20 sessions exist in DB without conflict.
7. `TestTier3_Pairwise_StoreReplayRepopulatesManifest`: Persist a serialized `HandshakeRequest` and `HandshakeResponse` to WAL store; close store; reopen store; query handshake event; unmarshal manifest and re-evaluate level.
8. `TestTier3_Pairwise_FilteredReplayByNegotiatedLevel`: Populate WAL store with events from L0, L2, and L3 sessions. Query store filtered by L2 session IDs and verify zero cross-session leakage.
9. `TestTier3_Pairwise_DegradationEventSequenceContiguity`: Trigger handshake degradation, append `TunnelSignal` and `Intervention` events. Verify sequence numbers are strictly continuous (`seq 1, 2, 3...`).
10. `TestTier3_Pairwise_StoreRecoveryAfterDegradedHandshake`: Abort database session after writing degraded handshake event. Reopen store, verify WAL journal recovery, and query persisted degradation event intact.

---

### Tier 4: Real-World Application Scenarios (Complete E2E Agent Supervision Workflows)

Tier 4 tests model complete end-to-end operational lifecycles of AI coding agent supervision sessions.

#### Scenario 1: Unattended High-Control Agent Lifecycle (L3 Full-Control)
- **Workflow**:
  1. Agent process connects and submits `HandshakeRequest` requesting Level 3 with full capability manifest (all 20 flags set).
  2. Negotiation engine returns `HandshakeResponse` with `NegotiatedLevel=3`, `IsDegraded=false`.
  3. Session init event (`AgentSession`) persisted to SQLite WAL event store (seq #1).
  4. Agent executes task, streaming 20 `ToolCallEvent` and `FileChangeEvent` records (seq #2 - #21).
  5. Harness logs `ProviderUsage` and `BudgetState` events (seq #22 - #23).
  6. Harness triggers `Checkpoint` event (seq #24).
  7. Harness queries latest sequence number (`GetLatestSequenceNum` returns 24).
  8. Store replayed via `QueryEvents(Ascending=true)` to verify exact audit trail.
- **Verification**: Zero degradation, all 24 events persisted in contiguous order, WAL store flushed and closed cleanly.

#### Scenario 2: Restricted Legacy Agent Session with Graceful Degradation (L3 -> L1)
- **Workflow**:
  1. Legacy agent submits `HandshakeRequest` requesting Level 3, but manifest only supports `CapEventStream`, `CapToolInspection`, `CapCLIControl` (Level 1 flags).
  2. Negotiation engine evaluates manifest, detects missing Level 2 & 3 flags, and gracefully degrades level: `NegotiatedLevel=1`, `IsDegraded=true`, `DegradedFrom=3`, `MissingFlags=["CapPause", "CapCancel", "CapResume", "CapCheckpoint", "CapRollback", "CapMCP", "CapSubagents", "CapSwitchModel", "CapSDK"]`.
  3. Harness appends degradation audit event (`AgentEvent{EventType: "handshake_degraded"}`) to WAL store (seq #1).
  4. Agent operates under Level 1 (Advisory mode): logs tool calls and receives prompt advice in `ReviewDecision` events (seq #2 - #10).
  5. Attempt to trigger Level 2 intervention (Pause/Rollback) is blocked by harness due to negotiated Level 1 constraint; denial event logged to store (seq #11).
  6. Query store with `EventFilter{SessionID: sid}` and verify degradation history and advisory execution trail.
- **Verification**: Handshake response correctly reflects degradation, harness respects negotiated Level 1 limits, all events queryable in WAL store.

#### Scenario 3: Anomaly Detection, Tunnel Intervention & State Rollback (L2 Guarded Workflow)
- **Workflow**:
  1. Agent negotiates Level 2 (Guarded mode) with pause/cancel/rollback capabilities.
  2. Session init persisted to WAL store (seq #1).
  3. Agent enters repeating error loop, emitting identical `FileChangeEvent` and `TestResultEvent` failures (seq #2 - #6).
  4. Anomaly detector triggers `TunnelSignal` and aggregate `TunnelAssessment` (seq #7).
  5. Harness issues `Intervention` (Level 2: Pause agent) (seq #8).
  6. Harness executes `RollbackResult` restoring Git workspace to previous `Checkpoint` commit (seq #9).
  7. Agent resumed via `Intervention` advice prompt (seq #10).
  8. Final session status updated to completed in WAL store (seq #11).
- **Verification**: Complete intervention lifecycle captured in WAL store; sequence numbers continuous; query filter retrieves full anomaly-to-rollback sequence.

#### Scenario 4: Store Crash, WAL Recovery & Replay Audit Session
- **Workflow**:
  1. Session starts with Level 2 handshake and streams 50 events to SQLite WAL store.
  2. Simulate unexpected process crash / abrupt termination while WAL journal file (`db.sqlite-wal`) has uncheckpointed frames.
  3. Re-open SQLite store using `NewStore(opts)`.
  4. Verify SQLite WAL auto-recovery restores uncheckpointed frames into main database file.
  5. Run `GetLatestSequenceNum` and verify returned sequence number is 50.
  6. Run `QueryEvents` across sequence range 1 to 50; verify zero data corruption or missing records.
- **Verification**: WAL engine guarantees crash recovery, zero data loss, exact sequence integrity restored.

---

## 6. Target Code Layout & Implementation Blueprint

To implement the test suite across the project, the following Go test file architecture must be established:

```
/Users/iml1s/Documents/mine/reinframe/
├── pkg/
│   ├── protocol/
│   │   ├── capability.go          # Issue #7: Bitmasks, Manifest, NegotiateLevel
│   │   ├── capability_test.go     # Unit tests for capability & negotiation
│   │   ├── schema.go              # Event schemas & struct models
│   │   └── schema_test.go         # Schema unit tests
│   └── state/
│       ├── migration.go           # Embedded SQL migration runner
│       ├── migrations/
│       │   └── 001_initial_events.sql # DDL table & index definitions
│       ├── store.go               # SQLite WAL Event Store implementation
│       └── store_test.go          # Unit & race tests for store
└── tests/
    └── e2e/                       # E2E Test Suite Package (package e2e_test)
        ├── capability_e2e_test.go # Tier 1 & Tier 2 E2E Capability Negotiation Tests
        ├── store_e2e_test.go      # Tier 1 & Tier 2 E2E SQLite WAL Store Tests
        ├── integration_e2e_test.go # Tier 3 Pairwise Cross-Feature Tests
        └── realworld_e2e_test.go  # Tier 4 Real-World Agent Supervision Scenarios
```

### Test Package & Execution Commands
- **Unit & Package Race Tests**:
  ```bash
  go test -v -race ./pkg/protocol/...
  go test -v -race ./pkg/state/...
  ```
- **E2E Integration & Scenario Suite**:
  ```bash
  go test -v -race ./tests/e2e/...
  ```
- **Full Project Verification**:
  ```bash
  go test -v -race ./...
  ```

---

## 7. Conclusion & Next Steps

This specification report establishes the definitive requirements, behavioral contracts, edge cases, and 4-tier test architecture for Reinframe Issues #7 and #9. 

**Summary of Deliverables**:
- **8 Core Features Discovered and Spec-Mapped**.
- **20 Edge Cases Documented**.
- **40+ Tier 1 Feature Coverage Test Cases**.
- **40+ Tier 2 Boundary & Corner Case Test Cases**.
- **10 Tier 3 Cross-Feature Pairwise Interaction Scenarios**.
- **4 Tier 4 Real-World Agent Supervision E2E Workflows**.
- **Go Test Layout & Execution Commands Defined**.

Next step: Implementers can proceed with building `pkg/protocol/capability.go`, `pkg/state/store.go`, and the corresponding unit and E2E test suites with 100% specification alignment.
