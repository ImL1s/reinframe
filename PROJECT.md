# Project: reinframe - Store Close Race Fix

## Architecture
- Module: `pkg/state`
- Target Files: `pkg/state/store.go`, `pkg/state/store_test.go`
- Key Abstractions: `Store` struct, `ErrStoreClosed`, `s.closed atomic.Bool`, `s.inFlight atomic.Int64`, `enter()`, `leave()`, `wrapDBErr()`

## Feature Inventory
| # | Feature | Description | Milestone | Source | Status |
|---|---------|-------------|-----------|--------|--------|
| 1 | In-Flight Gating & Error Wrapping | Fix check-then-use race via atomic inFlight; wrapDBErr maps only true closed/conn-done errors (not domain errors) | M1 | Survey | DONE |
| 2 | Regression Test TestStore_CloseRacesWithAppend | Add TestStore_CloseRacesWithAppend verifying errors.Is(err, ErrStoreClosed) | M1 | Survey | DONE |
| 3 | Package & Repository Race Verification | Pass go test -race -count=5 ./pkg/state/... and go test -race -count=1 ./... | M1 | Survey | DONE |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | M1: Store Close Race Fix & Regression Test | Implement atomic inFlight gating, wrapDBErr error mapping in store.go, and TestStore_CloseRacesWithAppend in store_test.go | none | DONE |

## Interface Contracts
### Store ↔ Callers
- `AppendEvents(ctx context.Context, sessionID string, events []Event) error`: returns `ErrStoreClosed` when store is closing or closed.
- `QueryEvents(ctx context.Context, filter QueryFilter) ([]Event, error)`: returns `ErrStoreClosed` when store is closing or closed.
- `GetLatestSequenceNum(ctx context.Context, sessionID string) (int64, error)`: returns `ErrStoreClosed` when store is closing or closed.
- `Close() error`: idempotently closes store connection pool, drains in-flight ops.

## Code Layout
- `pkg/state/store.go`: Store implementation
- `pkg/state/store_test.go`: Store tests & regression tests
