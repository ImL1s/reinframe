# Reinframe — Anti-Tunnel Supervision Harness (in progress)

[![CI](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml/badge.svg)](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Reinframe **aims to become** a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go.

**Today (M1 foundation + partial M2 control-plane contracts):** library packages for protocol, store, adapter contracts (HookGate, advisory queue, LogObserver), config schema, and reviewer provider interfaces — **not** a live end-to-end supervisor that auto-interrupts agents.

## Project Status

> **Phase: M1 Foundation + early M2 control-plane interfaces**  
> Protocol, SQLite store, capability negotiation (25 flags), adapter control-plane contracts, and observe-only LogObserver are available.  
> **Not yet:** real Detector, Fast/Slow Policy engine, Supervisor Orchestrator, Claude Code / Codex actuators, or hook→agent vertical slice.  
> See [M2 epic](https://github.com/ImL1s/reinframe/issues) (Detection-to-Intervention Control Plane) for the executable backlog.

| Component | Status |
|-----------|--------|
| Canonical Schema (25+ types incl. TaskSubmitted/Contract/Ledger) | ✅ Complete |
| Capability Negotiation (**25 flags**, Level 0–3) | ✅ Complete (L1 requires **CapAdviceDelivery**) |
| SQLite WAL Event Store (persistence invariants) | ✅ Complete |
| JSON Schema Validation (1MB limit, UseNumber) | ✅ Complete |
| Cross-platform CI (Linux/macOS/Windows + golangci-lint) | ✅ Complete |
| Adapter contracts (`EventSource`, `InterventionActuator`, HookGate, PendingQueue) | ✅ Complete (interfaces + fakes) |
| LogObserverAdapter (L0 inbound) | ✅ Complete |
| Config schema + ReviewerProvider interface | ✅ Complete (stubs/fakes) |
| Minimal RepeatedFailure Detector | 🔲 M2 |
| Fast/Slow Policy Engine | 🔲 M2 (#69) |
| Supervisor Orchestrator wiring | 🔲 M2 (#70) |
| Real hook→agent advisory vertical slice | 🔲 M2 (#71) |
| Concrete Claude Code / Codex adapters | 🔲 Planned |
| Git Checkpoint/Rollback runtime | 🔲 Planned |
| CLI / `cmd/` binary | 🔲 Not present yet |

## Project Objective

AI coding agents risk "tunneling" (cognitive lock-in, error loops, patch churn, scope drift). Reinframe is being built toward:

1. **Supervision Levels (0–3)** — dual axes: *Integration* (handshake) vs *Intervention* (escalation). See `docs/research/level_axes_mapping.md`.
2. **Canonical Agent Event Schema** *(available)* — 22 Go types + JSON Schemas.
3. **SQLite WAL Persistence** *(available)* — append-only event store.
4. **Control plane contracts** *(available, not fully wired)* — HookGate, advisory delivery + ACK, fakes.
5. **Automatic detect → defer → deliver → ACK loop** *(not yet)* — requires M2 epic.

---

## Core Architecture Overview

```
reinframe/
├── docs/
│   ├── adr/                   # Architecture Decision Records
│   ├── research/              # Threat model, capability matrix, level axes
│   ├── specs/                 # MVP / milestone boundaries
│   └── architecture/          # Execution DAG
├── pkg/
│   ├── protocol/              # Schemas, ValidateEvent, capability negotiation (25 flags)
│   ├── state/                 # SQLite WAL event store
│   ├── adapter/               # EventSource, Actuator, HookGate, PendingQueue, LogObserver
│   ├── config/                # Versioned configuration schema
│   └── reviewer/              # ReviewerProvider interface + FakeProvider
├── tests/
│   └── integration/           # Protocol/store/scenario-persistence tests
├── .github/workflows/ci.yml
├── go.mod                     # module github.com/ImL1s/reinframe
└── README.md
```

### Module Responsibilities
- **`pkg/protocol`**: Canonical schemas (TaskEnvelope + TaskSubmitted/TaskContract/EvidenceLedger, interventions, …), 25 capability flags (including CapAdviceDelivery, CapToolGate, CapInterventionAck, …), Level masks, negotiation. See `docs/specs/adaptive_task_supervisor.md`.
- **`pkg/state`**: SQLite WAL append-only store; persistence invariants only (not full schema validation on append).
- **`pkg/adapter`**: Bidirectional control-plane contracts; LogObserver (observe-only); fakes for tests. **Does not auto-wire a full supervisor loop.**
- **`pkg/config`**: Versioned config schema (loader/CLI still planned).
- **`pkg/reviewer`**: Provider interface + FakeProvider (no live cloud/local HTTP yet).
- **`tests/integration`**: Foundation integration tests (not full Anti-Tunnel E2E).

### Control-plane reality check
```text
Available:   interfaces, HookGate, queue, ACK lifecycle, LogObserver, fakes
Not wired:   Detector → Policy → Orchestrator → real harness hooks
```
Manual steps (enqueue → set pending ID on HookPolicy → DeliverPending → Acknowledge) still required until M2 orchestration lands.

---

## Supervision Levels (Integration handshake)

| Level | Name | Required capabilities (summary) | Use case |
|-------|------|----------------------------------|----------|
| 0 | **Observe** | CapEventStream | Passive monitoring (e.g. LogObserver) |
| 1 | **Advisory** | + CapToolInspection + **CapAdviceDelivery** | Zoom-out / replan advice |
| 2 | **Guarded** | + Diff + **native CapPause** + Cancel + Resume | Pause/tool gate (SIGSTOP ≠ CapPause) |
| 3 | **Full Control** | + Checkpoint/Rollback, Headless, CLI, MCP, Subagents, SwitchModel | Full autonomy supervision |

Intervention escalation after detection is a **separate axis** (B0–B3). See `docs/research/level_axes_mapping.md`.

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

5. **Compile all packages** (library packages only — no `package main` yet):
   ```bash
   go build -v ./...
   ```

---

## Contributing

Contributions welcome! Please:

1. Check the [Issue tracker](https://github.com/ImL1s/reinframe/issues) for the open **M2 epic** and ready work
2. Follow existing code style and test patterns
3. Run `go test -race ./...` before submitting PRs
4. All PRs require CI green on all three platforms

---

## License

MIT — see [LICENSE](LICENSE).
