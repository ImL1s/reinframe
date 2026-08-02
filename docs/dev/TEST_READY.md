# Integration Test Suite Ready: Reinframe Issues #7 & #9

## Test Runner Command
```bash
go test -v -race ./tests/integration/...
```

Expected output: Integration / scenario-persistence cases pass with exit code 0 and zero data race warnings.

## Coverage Summary
| Tier | Count | Description |
|------|------:|-------------|
| 1. Feature Coverage | 40 | Happy path & core functionality verification across 8 main features |
| 2. Boundary & Corner Cases | 40 | Limits, zero bitmasks, null pointers, transaction rollbacks, lock contention, errors |
| 3. Cross-Feature Pairwise | 10 | Handshake negotiation + SQLite WAL store state interaction scenarios |
| 4. Scenario Persistence Workloads | 4 | Hand-built multi-event store/query scenarios (not live Detector→Policy→Git E2E; reopen ≠ process crash) |
| **Total Test Suite Count** | **94** | Protocol + store foundation coverage (harness pipeline still planned) |

## Feature Checklist
| Feature | Tier 1 | Tier 2 | Tier 3 | Tier 4 | Status |
|---------|:------:|:------:|:------:|:------:|:------:|
| 20 Capability Bitmask Flags | 5 | 5 | ✓ | ✓ | READY |
| CapabilityManifest Helpers | 5 | 5 | ✓ | ✓ | READY |
| Level Threshold Evaluator | 5 | 5 | ✓ | ✓ | READY |
| Handshake Negotiation Engine | 5 | 5 | ✓ | ✓ | READY |
| SQL Schema & Migration Engine | 5 | 5 | ✓ | ✓ | READY |
| SQLite WAL Event Store Engine | 5 | 5 | ✓ | ✓ | READY |
| Event Query Engine | 5 | 5 | ✓ | ✓ | READY |
| Multi-Goroutine & Race Safety | 5 | 5 | ✓ | ✓ | READY |

## Execution Commands
- **Run Integration Test Suite**: `go test -v -race ./tests/integration/...`
- **Run Protocol Package Unit Tests**: `go test -v -race ./pkg/protocol/...`
- **Run State Package Unit Tests**: `go test -v -race ./pkg/state/...`
- **Run All Project Tests**: `go test -v -race ./...`
