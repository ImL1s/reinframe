# Scope: Milestone 2 — Issue #9 Append-Only Event Store & SQLite WAL Engine

## Mission
Execute Milestone 2: Implement SQLite WAL event store, migration engine, `AppendEvent`, `QueryEvents`, multi-goroutine concurrency safety, unit/race tests, and open PR on branch `issue-9-sqlite-wal-event-store`.

## Requirements
1. Create/manage branch `issue-9-sqlite-wal-event-store`.
2. Implement `pkg/state/migrations/001_initial_events.sql`:
   - DDL for `events`, `audit_records`, and `schema_migrations`.
3. Implement `pkg/state/migration.go`:
   - Embedded migration runner using `//go:embed migrations/*.sql`.
4. Implement `pkg/state/store.go`:
   - `NewStore(opts StoreOptions) (*Store, error)` configuring SQLite WAL mode (`journal_mode=WAL`), busy timeout (5000ms), synchronous NORMAL, foreign keys.
   - `AppendEvent(ctx, event)` and `AppendEvents(ctx, events)` with `BEGIN IMMEDIATE` transactions and mutex protection.
   - `QueryEvents(ctx, filter)` with flexible filtering (session, event types, sequence bounds, time bounds, limit/offset pagination) and `GetLatestSequenceNum(ctx, sessionID)`.
   - `Close()` handle cleanup.
5. Implement `pkg/state/store_test.go`:
   - Migration tests, basic append/query tests, filtering tests, sequence duplication error tests.
   - 50-routine concurrent append race test verified with `go test -v -race ./pkg/state/...`.
6. Iteration loop: Explorer -> Worker -> Reviewer -> Challenger -> Auditor -> Gate.
7. Create Pull Request for Issue #9 on branch `issue-9-sqlite-wal-event-store`.

## Status
**DONE** — All requirements met, verified with `go test -v -race ./pkg/...`, Pull Request #60 opened on branch `issue-9-sqlite-wal-event-store`.

## Interface Contracts
`pkg/state/store.go` signatures specified in `PROJECT.md`.

