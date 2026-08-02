# Reinframe — Anti-Tunnel Supervision Harness for AI Coding Agents

Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go. It provides real-time supervision, capability negotiation, and append-only event auditing powered by SQLite WAL state persistence and JSON-RPC 2.0 / NDJSON protocol interfaces.

## Project Objective

AI coding agents running in high-autonomy environments risk "tunneling" — executing unauthorized filesystem operations, excessive API calls, or unmonitored code modifications without structured supervision checkpoints. Reinframe provides:

1. **Supervision Levels (0–3)**: Graceful degradation model from passive observation (Level 0) to full headless control (Level 3).
2. **Canonical Agent Event Schema**: 22 standardized Go struct models validated against JSON Schemas.
3. **SQLite WAL Persistence Engine**: High-concurrency append-only event store supporting parallel readers/writers with atomic transaction semantics.
4. **Integration Test Suite**: Multi-tier opaque-box integration tests verifying protocol compliance and stress performance.

---

## Core Architecture Overview

```
reinframe/
├── cmd/
│   └── reinframe/             # Entry points & CLI executable
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
- **`pkg/state`**: Manages event persistence via SQLite WAL (Write-Ahead Logging). Configures per-connection DSN pragmas (`busy_timeout=5000`, `journal_mode=WAL`, `foreign_keys=1`) and immediate transaction locks (`_txlock=immediate`).
- **`tests/integration`**: Component integration tests covering feature Tiers 1–4 with full data race detection (`-race`).

---

## Quickstart Guide

### Prerequisites
- **Go 1.25.0+**

### Building and Testing

1. **Clone & Download Dependencies**:
   ```bash
   git clone https://github.com/reinframe/reinframe.git
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

5. **Build Binaries**:
   ```bash
   go build -v ./...
   ```

---

## License

Reinframe is released under the MIT License.
