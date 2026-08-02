# E2E Test Infra: Reinframe Issues #7 & #9

## Test Philosophy
- Opaque-box, requirement-driven E2E test suite for Reinframe Anti-Tunnel Supervision Harness.
- Tests verify Capability Manifest & Handshake Negotiation (Issue #7) and SQLite WAL Event Store (Issue #9).
- Methodology: 4-Tier Category-Partition + Boundary Value Analysis + Pairwise Combinatorial + Real-World Workload Testing.

## Feature Inventory Test Mapping
| # | Feature | Unit / E2E Target | Tier 1 | Tier 2 | Tier 3 | Tier 4 | Pass/Fail Criteria |
|---|---------|-------------------|:------:|:------:|:------:|:------:|--------------------|
| 1 | 20 Capability Bitmask Flags | `capability_e2e_test.go` | 5 | 5 | ✓ | ✓ | All 20 bitwise shifts, categories, OR/AND combinations verified |
| 2 | CapabilityManifest Helpers | `capability_e2e_test.go` | 5 | 5 | ✓ | ✓ | `ToBitmask`, `FromBitmask`, `HasCapability` roundtrip parity |
| 3 | Level Threshold Evaluator | `capability_e2e_test.go` | 5 | 5 | ✓ | ✓ | Calculates Level 0-3 thresholds correctly; returns -1 for missing L0 |
| 4 | Handshake Negotiation Engine | `capability_e2e_test.go` | 5 | 5 | ✓ | ✓ | `NegotiateLevel` handles exact matches, graceful degradation, missing flags |
| 5 | SQL Schema & Migration Engine | `store_e2e_test.go` | 5 | 5 | ✓ | ✓ | `001_initial_events.sql` idempotent execution & version tracking |
| 6 | SQLite WAL Event Store Engine | `store_e2e_test.go` | 5 | 5 | ✓ | ✓ | `NewStore`, `AppendEvent`, `AppendEvents` atomic persistence under WAL |
| 7 | Event Query Engine & Filtering | `store_e2e_test.go` | 5 | 5 | ✓ | ✓ | `QueryEvents` with SessionID/EventTypes/Sequence/Time/Limit/Offset |
| 8 | Multi-Goroutine Concurrency Safety | `store_e2e_test.go` | 5 | 5 | ✓ | ✓ | 500-goroutine parallel appends with 0 race conditions or DB locks |

## Coverage Thresholds
- **Tier 1 (Feature Coverage)**: ≥40 test cases (5 per feature across 8 features)
- **Tier 2 (Boundary & Corner Cases)**: ≥40 test cases (5 per feature across 8 features)
- **Tier 3 (Cross-Feature Pairwise Interactions)**: ≥10 test scenarios
- **Tier 4 (Real-World Application Scenarios)**: ≥4 complete E2E agent supervision workflows

## Test Execution Command
```bash
go test -v -race ./tests/integration/...
```
