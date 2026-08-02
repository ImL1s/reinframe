# Scope: E2E Testing Track for Reinframe Issues #7 & #9

## Architecture
E2E Opaque-Box & Integration Test Suite for Reinframe Handshake Protocol (Issue #7) and SQLite WAL Event Store (Issue #9).
- `pkg/protocol`: Handshake negotiation, capability bitmasks, level calculation (Level 0-3), degradation policy.
- `pkg/state`: SQLite WAL store creation, migrations, schema validation, thread-safe append and query operations, sequence tracking.
- Test Infra / Harness: End-to-end test packages and test runner covering 4 tiers.

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | 20 Capability Bitmask Flags | CapabilityFlag uint64 constants across 4 categories | M1 | Issue #7 |
| 2 | CapabilityManifest Helpers | ToBitmask(), FromBitmask(), HasCapability() | M1 | Issue #7 |
| 3 | Level Threshold Evaluator | EvaluateAchievableLevel() mapping flags to Levels 0-3 | M1 | Issue #7 |
| 4 | Negotiation Engine | NegotiateLevel() with automatic degradation | M1 | Issue #7 |
| 5 | SQL Schema & Migration Engine | 001_initial_events.sql migration runner | M2 | Issue #9 |
| 6 | SQLite WAL Event Store Engine | NewStore, AppendEvent, AppendEvents, Close | M2 | Issue #9 |
| 7 | Event Query Engine | QueryEvents with EventFilter & GetLatestSequenceNum | M2 | Issue #9 |
| 8 | Multi-Goroutine & Race Safety | Concurrency, WAL mode, lock-free/safe reading and writing | M2 | Issue #9 |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | E2E Test Suite Specification & Infrastructure | Extract detailed specs for Issues #7 & #9, design test harness & test structures | none | DONE |
| M2 | Tier 1 & Tier 2 E2E Test Suite (Capability & WAL Store) | Build Tier 1 Feature Coverage and Tier 2 Boundary/Corner tests | M1 | DONE |
| M3 | Tier 3 & Tier 4 E2E Test Suite (Cross-Feature & Real-World) | Build Tier 3 Pairwise Combinatorial and Tier 4 Real-World E2E scenarios | M2 | DONE |
| M4 | Verification, Audit & Publication | Run go test -race on E2E tests, review & audit, publish TEST_INFRA.md and TEST_READY.md | M3 | DONE |

## Interface Contracts
- Handshake Protocol API: `NegotiateLevel`, `CapabilityManifest`, `CapabilityFlag`
- SQLite Event Store API: `NewStore`, `AppendEvent`, `AppendEvents`, `QueryEvents`, `GetLatestSequenceNum`
- E2E Test Harness: Standard Go test runner (`go test -v -race ./...`)
