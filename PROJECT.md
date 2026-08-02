# PROJECT — Reinframe Issues #7 & #9: Capability Manifest & SQLite WAL Event Store

## Architecture
Reinframe is a cross-platform Anti-Tunnel Supervision Harness for AI coding agents written in Go.
- `pkg/protocol`: Implements data models, JSON validation engine, 20 capability flags, and Handshake Negotiation Engine supporting Level 0 (Observe), Level 1 (Advisory), Level 2 (Guarded), Level 3 (Full-control) with automatic degradation.
- `pkg/state`: Implements an append-only event store backed by SQLite with WAL mode (`journal_mode=WAL`), schema migration engine (`001_initial_events.sql`), and thread-safe `AppendEvent` / `QueryEvents` operations for high-concurrency event ingestion.

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | 20 Capability Bitmask Flags | Define `CapabilityFlag uint64` constants for 20 agent capabilities across 4 categories | M1 | survey (Issue #7) |
| 2 | CapabilityManifest Helpers | `ToBitmask()`, `FromBitmask()`, `HasCapability()` methods for `CapabilityManifest` | M1 | survey (Issue #7) |
| 3 | Level Threshold Evaluator | `EvaluateAchievableLevel()` calculating maximum achievable supervision level (Level 0-3) | M1 | survey (Issue #7) |
| 4 | Negotiation Engine | `NegotiateLevel(req *HandshakeRequest) (*HandshakeResponse, error)` with automatic degradation | M1 | survey (Issue #7) |
| 5 | Issue #7 Unit & Race Test Suite | Comprehensive tests in `pkg/protocol/capability_test.go` covering degradation and concurrency | M1 | survey (Issue #7) |
| 6 | SQL Schema & Migration Engine | `pkg/state/migrations/001_initial_events.sql` and embedded migration runner in `pkg/state/migration.go` | M2 | survey (Issue #9) |
| 7 | SQLite WAL Event Store Engine | `NewStore`, `AppendEvent`, `AppendEvents`, `Close` in `pkg/state/store.go` with WAL mode & busy timeout | M2 | survey (Issue #9) |
| 8 | Event Query Engine | `QueryEvents` with `EventFilter` (session, type, sequence, time bounds, pagination) and `GetLatestSequenceNum` | M2 | survey (Issue #9) |
| 9 | Thread & Multi-Goroutine Safety | Mutex protection and connection pooling for concurrent readers and writers without `SQLITE_BUSY` | M2 | survey (Issue #9) |
| 10 | Issue #9 Unit & Race Test Suite | Tests in `pkg/state/store_test.go` covering migrations, appends, queries, batching, and 50-routine race testing | M2 | survey (Issue #9) |
| 11 | Full Verification & Git PRs | Open PRs for `issue-7-capability-manifest-negotiation` and `issue-9-sqlite-wal-event-store` passing `go test -race ./pkg/...` | M3 | survey (Issues #7 & #9) |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Issue #7: Capability Manifest & Handshake Protocol | Implement capability flags, manifest helpers, negotiation engine with automatic degradation, unit tests on branch `issue-7-capability-manifest-negotiation` | none | PLANNED |
| M2 | Issue #9: Append-Only Event Store & SQLite WAL Engine | Implement SQLite WAL event store, migration engine, AppendEvent, QueryEvents, unit tests on branch `issue-9-sqlite-wal-event-store` | none | DONE |
| M3 | Integration Verification & Git PRs | Run `go test -race ./pkg/...`, open PRs for Issue #7 and Issue #9, deliver victory report | M1, M2 | PLANNED |

## Interface Contracts

### `pkg/protocol` Handshake API (`pkg/protocol/capability.go`)
```go
type CapabilityFlag uint64

const (
    CapEventStream CapabilityFlag = 1 << iota
    CapToolInspection
    CapDiffInspection
    CapCostTracking
    CapHooks
    CapHeadless
    CapCLIControl
    CapPause
    CapCancel
    CapResume
    CapCheckpoint
    CapRollback
    CapMCP
    CapSubagents
    CapExtensions
    CapSwitchModel
    CapCustomProvider
    CapOpenAICompat
    CapLocalModels
    CapSDK
)

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

### `pkg/state` Event Store API (`pkg/state/store.go`)
```go
type StoreOptions struct {
    DatabasePath string
    BusyTimeout  time.Duration
    MaxOpenConns int
    MaxIdleConns int
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

type Store struct { ... }

func NewStore(opts StoreOptions) (*Store, error)
func (s *Store) AppendEvent(ctx context.Context, event *protocol.AgentEvent) error
func (s *Store) AppendEvents(ctx context.Context, events []*protocol.AgentEvent) error
func (s *Store) QueryEvents(ctx context.Context, filter EventFilter) ([]*protocol.AgentEvent, error)
func (s *Store) GetLatestSequenceNum(ctx context.Context, sessionID string) (int64, error)
func (s *Store) Close() error
```

## Code Layout
```
/Users/iml1s/Documents/mine/reinframe/
├── go.mod
├── go.sum
└── pkg/
    ├── protocol/
    │   ├── schema.go
    │   ├── validator.go
    │   ├── capability.go          # Issue #7
    │   ├── capability_test.go     # Issue #7
    │   ├── schema_test.go
    │   ├── adversarial_stress_test.go
    │   ├── challenger_stress_test.go
    │   └── schemas/
    └── state/                     # Issue #9
        ├── migration.go
        ├── store.go
        ├── store_test.go
        └── migrations/
            └── 001_initial_events.sql
```
