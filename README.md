# Reinframe — Anti-Tunnel Supervision Harness (in progress)

[![CI](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml/badge.svg)](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Reinframe **aims to become** a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go.

**Today (M1 foundation + M2.0 control-loop slice):** library packages for protocol, store, adapter contracts, **minimal repeated-failure detector**, **fast/slow policy**, **supervisor orchestrator**, and a **fake-agent vertical-slice test** — **not** production Claude Code / Codex adapters, and **not** multi-deviation calibrated Anti-Tunnel E2E.

## Project Status

> **Phase: M1 Foundation + M2.0 control-loop integration (direction_fixation slice)**  
> Protocol, store, adapter contracts, LogObserver, minimal `RepeatedFailureDetector`, fast/slow policy, orchestrator wiring, and fake-agent vertical-slice tests are available.  
> **Not yet:** concrete Claude Code / Codex actuators, Git rollback runtime, multi-role live Reviewers, verification_churn / effort-calibration (M2.1), or calibrated threshold hard-gates (M3).  
> See open issues for M2.1+ backlog; M2.0 slice issues #82/#69/#70/#71 ship on this track.

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
| Minimal RepeatedFailure Detector (`pkg/detector`, #82) | ✅ Complete (provisional N=3 knobs) |
| Fast/Slow Policy Engine (`pkg/policy`, #69) | ✅ Complete (deterministic ZOOM_OUT; optional Reviewer) |
| Supervisor Orchestrator wiring (`pkg/supervisor`, #70) | ✅ Complete (composition root + fakes) |
| Hook→agent advisory vertical slice (`pkg/supervisor` tests, #71) | ✅ Control-loop integration test (fake agent; **not** full multi-deviation E2E) |
| Concrete Claude Code / Codex adapters | 🔲 Planned |
| Git Checkpoint/Rollback runtime | 🔲 Planned |
| CLI / `cmd/` binary | 🔲 Not present yet |

## Project Objective

AI coding agents risk "tunneling" (cognitive lock-in, error loops, patch churn, scope drift). Reinframe is being built toward:

1. **Supervision Levels (0–3)** — dual axes: *Integration* (handshake) vs *Intervention* (escalation). See `docs/research/level_axes_mapping.md`.
2. **Canonical Agent Event Schema** *(available)* — 22 Go types + JSON Schemas.
3. **SQLite WAL Persistence** *(available)* — append-only event store.
4. **Control plane contracts** *(available)* — HookGate, advisory delivery + ACK, fakes.
5. **M2.0 detect → defer → deliver → ACK loop** *(library + fake-agent tests)* — `pkg/supervisor` orchestrates detector + policy + delivery; **not** production harness adapters.

---

## Core Architecture Overview

```
reinframe/
├── docs/
│   ├── adr/                   # Architecture Decision Records
│   ├── research/              # Threat model, capability matrix, level axes
│   ├── specs/                 # MVP / milestone boundaries + Adaptive Task Supervisor
│   └── architecture/          # Execution DAG
├── pkg/
│   ├── protocol/              # Schemas, ValidateEvent, capability negotiation (25 flags)
│   ├── state/                 # SQLite WAL event store
│   ├── adapter/               # EventSource, Actuator, HookGate, PendingQueue, LogObserver
│   ├── detector/              # Minimal RepeatedFailureDetector (#82)
│   ├── policy/                # Fast/slow intervention policy (#69)
│   ├── supervisor/            # Orchestrator composition + vertical-slice tests (#70/#71)
│   ├── config/                # Versioned configuration schema
│   └── reviewer/              # ReviewerProvider interface + FakeProvider
├── tests/
│   └── integration/           # Protocol/store/scenario-persistence tests
├── .github/workflows/ci.yml
├── go.mod                     # module github.com/ImL1s/reinframe
└── README.md
```

### Module Responsibilities
- **`pkg/protocol`**: Canonical schemas (TaskEnvelope + TaskSubmitted/TaskContract/EvidenceLedger, interventions, …), `BuildContractFromSubmitted` + store emit helpers (`AgentEventFromTask*`), 25 capability flags (including CapAdviceDelivery, CapToolGate, CapInterventionAck, …), Level masks, negotiation. See `docs/specs/adaptive_task_supervisor.md`.
- **`pkg/state`**: SQLite WAL append-only store; persistence invariants only (not full schema validation on append).
- **`pkg/adapter`**: Bidirectional control-plane contracts; LogObserver (observe-only); fakes for tests.
- **`pkg/detector`**: Deterministic repeated-failure fingerprint detector (no LLM); provisional N=3.
- **`pkg/policy`**: Fast path = HookGate only; slow path = ZOOM_OUT from high-confidence signals (optional Reviewer on uncertain branch).
- **`pkg/supervisor`**: Thin composition root wiring detect → policy → queue → deliver → ACK; vertical-slice tests use fakes (not Claude/Codex).
- **`pkg/config`**: Versioned config schema (loader/CLI still planned).
- **`pkg/reviewer`**: Provider interface + FakeProvider (no live cloud/local HTTP yet).
- **`tests/integration`**: Foundation integration tests (not full Anti-Tunnel E2E).

### Control-plane reality check
```text
Available:   Detector → Policy → Orchestrator → HookGate/queue/delivery (fakes + LogObserver)
Not yet:     concrete Claude Code / Codex PreTool adapters, live Reviewer HTTP, calibrated thresholds
```
Vertical-slice tests prove the library loop with a **fake** target agent. Production harness wiring remains Planned.

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
