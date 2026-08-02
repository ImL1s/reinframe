# Project: reinframe - Store Close Race Fix & Error Mapping

## Architecture
- Module: `pkg/state` (`github.com/ImL1s/reinframe/pkg/state`)
- Target Files: `pkg/state/store.go`, `pkg/state/store_test.go`, `pkg/state/wrap_err_test.go`
- Key Abstractions: `Store`, `ErrStoreClosed`, `s.closed atomic.Bool`, `s.inFlight atomic.Int64`, `enter()`, `leave()`, `wrapDBErr()`

## Feature Inventory
| # | Feature | Description | Milestone | Source | Status |
|---|---------|-------------|-----------|--------|--------|
| 1 | In-Flight Gating & Error Wrapping | Atomic inFlight gate; wrapDBErr never rewrites domain errors; when `closed=true` maps all other non-domain errors to `ErrStoreClosed` | M1 | Survey | DONE |
| 2 | Regression Test TestStore_CloseRacesWithAppend | Close races with append return `errors.Is(err, ErrStoreClosed)` for non-domain paths | M1 | Survey | DONE |
| 3 | Package & Repository Race Verification | Pass `go test -race` on `./pkg/state/...` and `./...` | M1 | Survey | DONE |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | M1: Store Close Race Fix & Regression Test | inFlight gating + wrapDBErr + Close race tests | none | DONE |

## Interface Contracts
### Store ↔ Callers
- `AppendEvent(ctx context.Context, event *protocol.AgentEvent) error`
- `AppendEvents(ctx context.Context, events []*protocol.AgentEvent) error`
  - Persistence invariants only (non-nil, IDs/type non-empty, `SequenceNum > 0`); does not call `protocol.ValidateEvent`.
  - Writes via busy-aware `runTxWithRetry`.
  - Returns domain errors (`ErrDuplicateSequence`, `ErrDuplicateEventID`, `ErrInvalidEvent`) even if `closed` flipped mid-op; returns `ErrStoreClosed` for closed-window operational noise and for enter-after-close.
- `QueryEvents(ctx context.Context, filter EventFilter) ([]*protocol.AgentEvent, error)`
- `GetLatestSequenceNum(ctx context.Context, sessionID string) (int64, error)`
- `Close() error`: first caller sets `closed` and waits up to ~5s for `inFlight` to drain, then closes the pool; concurrent later `Close()` returns `nil` immediately (idempotent flag).

## Code Layout
- `pkg/state/store.go`: Store implementation
- `pkg/state/store_test.go`: Store tests & regression tests
- `pkg/state/wrap_err_test.go`: wrapDBErr domain vs closed-window tests
