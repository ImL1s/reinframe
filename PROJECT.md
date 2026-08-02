# Reinframe P0/P1 Issue Resolution — PROJECT.md

## Architecture
Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go, powered by SQLite WAL state persistence and JSON-RPC 2.0 / NDJSON protocol interfaces.

### Core Modules
- `pkg/state/`: SQLite WAL-backed append-only event store and migration engine.
- `pkg/protocol/`: Canonical event schemas, JSON validation engine, CapabilityManifest, and negotiation engine.
- `tests/integration/`: Component integration tests and real-world workload test suites.
- `.github/workflows/ci.yml`: Multi-platform automated build, test, and static analysis pipeline.

## Feature Inventory
| # | Feature / Requirement | Description | Milestone | Source |
|---|------------------------|-------------|-----------|--------|
| 1 | DSN Pragma Configuration | Move pragmas (busy_timeout, journal_mode WAL, foreign_keys) to DSN connection string | M1 | R1.1 |
| 2 | Remove Global Mutex | Remove sync.RWMutex in store.go, manage closed state via atomic.Bool | M1 | R1.2 |
| 3 | db.BeginTx & _txlock=immediate | Use db.BeginTx(ctx, nil) with _txlock=immediate instead of manual BEGIN IMMEDIATE | M1 | R1.3 |
| 4 | Shared Cache In-Memory URI | Change default :memory: DSN to shared cache URI to fix multi-connection pooling | M1 | R1.4 |
| 5 | Migration Transaction Placement | Move SELECT EXISTS check inside db.BeginTx transaction block in migration.go | M1 | R1.5 |
| 6 | ToBitmask Permission Fix | Build bitmask exclusively from 20 explicit boolean fields, removing auto-granting | M2 | R2.1 |
| 7 | Level Contract Re-alignment | Move Pause/Cancel/Resume from Level 1 Required Mask to Level 2 (Guarded) | M2 | R2.2 |
| 8 | Complete 20 Capability Fields | Add missing 14 boolean capability flags to CapabilityManifest struct & JSON schema | M2 | R2.3 |
| 9 | ValidateEvent Hardening | Add payload size limit check (1MB) and use json.Decoder.UseNumber() | M2 | R2.4 |
| 10| AgentSession RESUME Enum | Add "RESUME" to AgentSession status enum in agent_session.json schema | M2 | R2.5 |
| 11| max_depth Schema Constraint | Add "maximum": 1 to task_envelope.json max_depth schema | M2 | R2.6 |
| 12| Schema Init Fail-Fast | Replace sync.Once schema compilation with Go init() fail-fast panic | M2 | R2.7 |
| 13| Go Version Alignment | Align CI go-version with go.mod via go-version-file: 'go.mod' | M3 | R3.1 |
| 14| Root Governance Files | Create root README.md and .gitignore excluding *.db, .DS_Store, .agents/ | M3 | R3.2 |
| 15| Move Dev Specs to docs/dev/ | Move ORIGINAL_REQUEST.md, PROJECT.md, TEST_INFRA.md, TEST_READY.md to docs/dev/ | M3 | R3.3 |
| 16| CI golangci-lint Step | Add golangci-lint-action to CI workflow | M3 | R3.4 |
| 17| Issue Tracker Cleanup | Verify and mark completed Issues (#6, #7, #9, #39) as closed in Epic checklist | M3 | R3.5 |
| 18| Rename tests/e2e | Rename tests/e2e/ to tests/integration/ and update package to integration_test | M3 | R3.8 |
| 19| Flaky BusyTimeout Fix | Increase test BusyTimeout to 500ms in store_e2e_test.go | M3 | R3.9 |
| 20| Test t.TempDir Cleanups | Replace os.MkdirTemp + defer os.RemoveAll with t.TempDir() in integration tests | M3 | R3.10|
| 21| Capability Test Rewrite | Rewrite capability tests to match non-auto-granting semantics & add JSON lossy test | M4 | R4.1 |
| 22| High-Concurrency Stress Test | Run 100 writers / 50 readers & 500 goroutine stress test with go test -race -count=5 | M5 | R5.1 |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | SQLite Concurrent Architecture Fixes | pkg/state/ (store.go, migration.go, store_test.go) | none | PLANNED |
| M2 | Capability & Schema Fixes | pkg/protocol/ (capability.go, schema.go, schemas/*.json) | none | PLANNED |
| M3 | Governance, CI & Directory Refactoring | go.mod, .github/workflows/ci.yml, README.md, .gitignore, docs/dev/, tests/ | none | PLANNED |
| M4 | Capability Test Suite Rewrite | pkg/protocol/ (capability_test.go, challenger2_stress_test.go) | M2 | PLANNED |
| M5 | Stress Test Verification & Full E2E | pkg/state/ (challenger_stress_test.go), tests/integration/ | M1, M2, M3, M4 | PLANNED |

## Interface Contracts
### `pkg/state` ↔ `pkg/protocol`
- `Store.AppendEvent(ctx context.Context, event *protocol.AgentEvent) error`
  - Validates `event` using `protocol.ValidateEvent`.
  - Executes within SQLite transaction using `db.BeginTx(ctx, nil)`.
- `Store.QueryEvents(ctx context.Context, filter protocol.EventFilter) ([]protocol.AgentEvent, error)`
  - Reads from SQLite without Go-level mutex blocking.

## Code Layout
```
reinframe/
├── cmd/
│   └── reinframe/
├── docs/
│   └── dev/                  # Specification and dev tracking docs
├── pkg/
│   ├── protocol/             # Protocol schemas, capability negotiation, validation engine
│   │   ├── schemas/          # 22 canonical JSON schema files
│   │   ├── capability.go
│   │   ├── capability_test.go
│   │   ├── schema.go
│   │   └── schema_test.go
│   └── state/                # SQLite WAL event store and migrations
│       ├── store.go
│       ├── store_test.go
│       ├── migration.go
│       └── migrations/
├── tests/
│   └── integration/          # Integration tests (formerly tests/e2e)
├── .github/
│   └── workflows/
│       └── ci.yml            # CI build & test workflow
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```
