# Reinframe — Anti-Tunnel Supervision Harness (in progress)

[![CI](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml/badge.svg)](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Reinframe **aims to become** a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go. **Today** the repository ships library foundations only: canonical protocol schemas, capability negotiation, JSON validation, and an append-only SQLite WAL event store — not a live end-to-end supervisor.

## Project Status

> **Phase: Protocol & Store Foundation (partial)** — Canonical protocol, SQLite event store, and CI are implemented library packages.
> This is **not** a production-ready end-to-end Anti-Tunnel harness yet: Detector, Reviewer, Policy, Intervention, Git rollback, and Orchestrator remain planned.

| Component | Status |
|-----------|--------|
| Canonical Schema (22 types) | ✅ Complete |
| Capability Negotiation (20 flags, Level 0–3) | ✅ Complete |
| SQLite WAL Event Store (persistence invariants) | ✅ Complete |
| JSON Schema Validation (1MB limit, UseNumber) | ✅ Complete |
| Cross-platform CI (Linux/macOS/Windows + golangci-lint) | ✅ Complete |
| Tunnel Detector | 🔲 Planned |
| Evidence Reviewer | 🔲 Planned |
| Intervention Policy Engine | 🔲 Planned |
| Git Checkpoint/Rollback | 🔲 Planned |
| Supervisor Orchestrator | 🔲 Planned |
| CLI / `cmd/` binary | 🔲 Not present yet |

## Project Objective

AI coding agents running in high-autonomy environments risk "tunneling" — executing unauthorized filesystem operations, excessive API calls, or unmonitored code modifications without structured supervision checkpoints. Reinframe is being built toward:

1. **Supervision Levels (0–3)** *(planned runtime)*: Graceful degradation from passive observation (Level 0) to full headless control (Level 3).
2. **Canonical Agent Event Schema** *(available)*: 22 standardized Go struct models validated against JSON Schemas.
3. **SQLite WAL Persistence Engine** *(available)*: Append-only event store with WAL, busy-aware writers, and persistence invariants (schema validation stays at the protocol/ingestion layer).
4. **Integration Test Suite** *(available)*: Multi-tier integration and scenario-persistence tests for protocol + store packages (not full Detector→Git E2E).

---

## Core Architecture Overview

```
reinframe/
├── docs/
│   ├── dev/                   # Specification, design, and dev tracking documents
│   └── adr/                   # Architecture Decision Records
├── pkg/
│   ├── protocol/              # Canonical schemas, validation engine, capability negotiation
│   │   ├── schemas/           # 22 JSON schema definitions
│   │   ├── capability.go      # CapabilityManifest & Handshake negotiation engine
│   │   └── schema.go          # Canonical Go types & validation engine
│   └── state/                 # SQLite WAL-backed event store engine
│       ├── store.go           # High-concurrency store implementation
│       ├── migration.go       # DSN pragma & database schema migration engine
│       └── migrations/        # SQL schema migration scripts (001_initial_events.sql)
├── tests/
│   └── integration/           # Multi-tier integration and workload tests
├── .github/
│   └── workflows/
│       └── ci.yml             # Automated CI build, lint, and test pipeline
├── .gitignore
├── go.mod
└── README.md
```

### Module Responsibilities
- **`pkg/protocol`**: Defines all 22 canonical event schemas, payload size checks (1MB limit), bitmask-based capability negotiation across 20 distinct boolean flags, and fail-fast schema compilation.
- **`pkg/state`**: Manages event persistence via SQLite WAL (Write-Ahead Logging). Configures per-connection DSN pragmas (`busy_timeout=5000`, `journal_mode=WAL`, `foreign_keys=1`) and immediate transaction locks (`_txlock=immediate`). Append paths enforce persistence invariants only; they do **not** call `protocol.ValidateEvent`.
- **`tests/integration`**: Integration and scenario-persistence tests (Tier-style suites) for capability negotiation and store behavior with `-race`.

---

## Quickstart Guide

### Prerequisites
- **Go 1.25.0+**

### Building and Testing

1. **Clone & Download Dependencies**:
   ```bash
   git clone https://github.com/ImL1s/reinframe.git
   cd reinframe
   go mod download
   ```

2. **Run Unit Tests**:
   ```bash
   go test -v -race ./pkg/...
   ```

3. **Run Integration Test Suite**:
   ```bash
   go test -v -race ./tests/integration/...
   ```

4. **Run All Project Tests**:
   ```bash
   go test -v -race ./...
   ```

5. **Compile all packages** (library packages only — there is no `package main` / CLI binary yet):
   ```bash
   go build -v ./...
   ```

---

## Supervision Levels

| Level | Name | Capabilities | Use Case |
|-------|------|-------------|----------|
| 0 | **Observe** | Event stream | Passive monitoring |
| 1 | **Advisory** | + Tool inspection | Suggestions, no control |
| 2 | **Guarded** | + Diff inspection, Pause/Cancel/Resume | Active intervention |
| 3 | **Full Control** | + Checkpoint/Rollback, MCP, Subagents, Model Switch | Full autonomy supervision |

---

## Contributing

Contributions welcome! Please:

1. Check the [Issue tracker](https://github.com/ImL1s/reinframe/issues) for available work
2. Follow the existing code style and test patterns
3. Run `go test -race ./...` before submitting PRs
4. All PRs require CI green on all three platforms

---

## License

Reinframe is released under the [MIT License](LICENSE).
