# ADR 002: Core Implementation Language, Database Engine & Adapter IPC Protocol

- **Status**: Decided & Approved
- **Date**: 2026-08-02
- **Deciders**: Architecture Team
- **Technical Story**: Issue #4

---

## Context and Problem Statement
The Reinframe core supervisor requires:
1. Single binary distribution with zero runtime dependencies across Windows, macOS, and Linux.
2. Low CPU and memory footprint ($< 50\text{MB}$ RAM idle).
3. Immutable, crash-resilient event persistence.
4. Fast, standardized IPC protocol for out-of-process adapters and agent bridges.

---

## Technical Stack Evaluation

### 1. Implementation Language Choice: Go vs Rust vs TypeScript

| Criterion | Go (Selected) | Rust | TypeScript (Node/Bun) |
|---|---|---|---|
| Single Binary Packaging | Excellent (`CGO_ENABLED=0`) | Excellent | Requires packaging wrapper |
| Cross-Platform Process/Signal | Native stdlib (`os/exec`, `os/signal`) | Excellent (`nix`, `windows-sys`) | OS signal inconsistencies |
| Concurrency Model | Goroutines & Channels | Async/Tokio | Event Loop |
| Build Times & Simplicity | Extremely fast | Slow compilation | Instant script, complex bundle |
| Resource Overhead | $\sim 15-30\text{MB}$ RAM | $\sim 5-15\text{MB}$ RAM | $\sim 80-150\text{MB}$ RAM |

**Decision**: **Go** selected for optimal balance of compilation speed, concurrency model, cross-platform stdlib, and lightweight single-binary distribution.

---

### 2. Database Engine Choice: SQLite (WAL Mode) vs Embedded KV (Badger/Bolt) vs PostgreSQL

- **Decision**: **SQLite with Write-Ahead Logging (WAL)** using modern pure-Go driver (`modernc.org/sqlite` or `github.com/mattn/go-sqlite3`).
- **Rationale**:
  - Provides ACID transaction guarantees for event-sourced audit logs.
  - WAL mode enables concurrent read access by telemetry/dashboard processes while the supervisor writes events.
  - Zero external database setup or server management required.

---

### 3. Adapter IPC Protocol: JSON-RPC 2.0 over stdio / NDJSON vs gRPC vs HTTP/REST

- **Decision**: **JSON-RPC 2.0 over stdio & NDJSON (Newline Delimited JSON)**.
- **Rationale**:
  - Standardized, lightweight framing protocol compatible with MCP and language server protocols.
  - Zero network port binding conflicts on developer machines.
  - Optional WebSocket/HTTP transport supported for remote webhooks.

---

## Decision Outcome
- **Core Supervisor**: Go (Go 1.22+).
- **Database Engine**: SQLite in WAL mode.
- **IPC Protocol**: JSON-RPC 2.0 over stdio / NDJSON streams.
