# Reinframe — Anti-Tunnel Supervision Harness (in progress)

[![CI](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml/badge.svg)](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Reinframe **aims to become** a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go.

**Today (M1 → M2.2 library + experimental host bridges):** control plane (detect→defer→deliver→ACK), effort-calibration library slices, offline/near-live Codex observation, Claude PreTool **fixture/CLI bridge**, FileActuator JSONL advice channel, tool-budget / hypothesis-loop detectors — **not** dual-host production install, **not** calibrated hard-gates (M3), **not** git checkpoint product runtime.

## Project Status

> **Phase: M1 + M2 library + experimental host bridges + shadow classifier + offline eval**
>
> **Still open (actual `gh` set):** epic [#80](https://github.com/ImL1s/reinframe/issues/80); **ready** [#131](https://github.com/ImL1s/reinframe/issues/131) appealable BLOCK challenges, [#132](https://github.com/ImL1s/reinframe/issues/132) classifier provider runtime, [#142](https://github.com/ImL1s/reinframe/issues/142) docs/open-set governance (this PR); **blocked** [#120](https://github.com/ImL1s/reinframe/issues/120) live Claude smoke (`BLOCKED_BY_ENVIRONMENT`) → [#108](https://github.com/ImL1s/reinframe/issues/108) advice consumer; **blocked** [#134](https://github.com/ImL1s/reinframe/issues/134)–[#139](https://github.com/ImL1s/reinframe/issues/139) native providers / exact-cache / Claude appeal; **blocked** eval follow-ups [#140](https://github.com/ImL1s/reinframe/issues/140)–[#141](https://github.com/ImL1s/reinframe/issues/141).
>
> **Closed residual library slices (narrow DoD):** ProposedAction [#115](https://github.com/ImL1s/reinframe/issues/115), PreTool semantics [#116](https://github.com/ImL1s/reinframe/issues/116), settings harden [#117](https://github.com/ImL1s/reinframe/issues/117), Codex identity [#118](https://github.com/ImL1s/reinframe/issues/118), classifier contract [#119](https://github.com/ImL1s/reinframe/issues/119), shadow classifier [#105](https://github.com/ImL1s/reinframe/issues/105), M3 benches [#100](https://github.com/ImL1s/reinframe/issues/100) (MORE-DATA, no hard-gate), managed worktree [#99](https://github.com/ImL1s/reinframe/issues/99), post-merge hygiene [#133](https://github.com/ImL1s/reinframe/pull/133).
>
> **Executable roadmap:** [`docs/roadmap/CURRENT.md`](docs/roadmap/CURRENT.md). Street map: [`docs/dev/STREET_WIRE.md`](docs/dev/STREET_WIRE.md).

| Component | Status |
|-----------|--------|
| Canonical Schema (25+ types incl. TaskSubmitted/Contract/Ledger) | ✅ Complete |
| Capability Negotiation (**25 flags**, Level 0–3) | ✅ Complete (L1 requires **CapAdviceDelivery**) |
| SQLite WAL Event Store (persistence invariants) | ✅ Complete |
| JSON Schema Validation (1MB limit, UseNumber) | ✅ Complete |
| Cross-platform CI (Linux/macOS/Windows + golangci-lint) | ✅ Complete |
| Adapter contracts (`EventSource`, `InterventionActuator`, HookGate, PendingQueue) | ✅ Complete (interfaces + fakes + FileActuator) |
| LogObserverAdapter (L0 inbound) | ✅ Complete |
| Config schema + ReviewerProvider interface | ✅ Complete (stubs/fakes) |
| RepeatedFailure Detector (#82) | ✅ Complete (provisional N=3) |
| VerificationChurn detector (#85) | ✅ Complete (provisional multi-part fingerprint) |
| Tool-budget + hypothesis-loop detectors (#98) | ✅ Library complete (provisional; not live host auto-intervention) |
| Fast/Slow + before_tool Policy (#69 / #86) | ✅ Complete; #98 modes → ZOOM_OUT on slow path |
| Supervisor Orchestrator (#70/#71) | ✅ Complete (composition root + vertical-slice tests) |
| TaskSubmitted intake mappers (#84) | ✅ Fixture/host mappers (no protocol host type names) |
| Codex EventSource offline + near-live tail (#95) | ✅ `CodexRolloutSource` / `CodexTailSource` (not process attach) |
| Claude PreTool / prompt bridge (#96) | ✅ Experimental API + `cmd/claudebridge` |
| Claude project-local install/doctor (#106) | ✅ Installer unit path; **not** live smoke (#120 open) |
| Claude settings ownership harden (#117) | ✅ Exact ownership + atomic write |
| Codex observe product surface (#107/#118) | ✅ Discovery/cursor/codexctl; observe-only L0 |
| Typed ProposedAction (#115) | ✅ ToolName ≠ Command |
| PreTool response semantics (#116) | ✅ No `continue:false` for ordinary tool deny |
| Action Alignment design (#104) + wire contract (#119) | ✅ Concept + closed schemas/FakeProvider |
| Shadow classifier runtime (#105) | ✅ Library shadow; `Enforced=false` always |
| M3 synthetic + FP benchmarks (#100) | ✅ Offline runner; disposition MORE-DATA; **no hard-gate** |
| Managed worktree checkpoint/rollback (#99) | ✅ Clean-only under managed root; not primary checkout |
| FileActuator advice channel (#97) | ✅ JSONL `reinframe.advice.v1`; write ≠ agent receipt |
| Street-wire demo (`cmd/streetwire`) | ✅ Offline Codex + M2 loops + bridge + FileActuator demos |
| Optional LLM Reviewer (OpenAI-compatible, ADR 003 local-only default) | ✅ Uncertain path only; high-confidence never calls LLM (`cmd/reviewerdemo`) |
| Live Claude ALLOW/BLOCK smoke (#120) | 🔲 Open — `BLOCKED_BY_ENVIRONMENT` |
| Real advice consumer / ACK (#108) | 🔲 Open (blocked by #120) |
| Appealable BLOCK challenges (#131) | 🔲 Open — `status:ready` (design/issue only until merged) |
| Classifier provider runtime (#132) | 🔲 Open — `status:ready` (not shipped) |
| Native classifier adapters (#134–#137) | 🔲 Open — blocked on #132 |
| Exact-assessment memoization (#138) | 🔲 Open — blocked on #132 |
| Claude appeal delivery (#139) | 🔲 Open — blocked on #131 **and** #120 |
| Challenge appeal evaluation (#140) | 🔲 Open — blocked on #131 (Lane A); #139/#132 for later lanes |
| Provider/cache economics evaluation (#141) | 🔲 Open — blocked on #132 (+ #138 / native adapters for full lanes) |
| Open-set docs governance (#142) | 🔲 Open — `status:ready` (docs PR) |
| Global host install / dual-host production supervision | 🔲 Not claimed |

## Project Objective

AI coding agents risk "tunneling" (cognitive lock-in, error loops, patch churn, scope drift). Reinframe is being built toward:

1. **Supervision Levels (0–3)** — dual axes: *Integration* (handshake) vs *Intervention* (escalation). See `docs/research/level_axes_mapping.md`.
2. **Canonical Agent Event Schema** *(available)* — 22 Go types + JSON Schemas.
3. **SQLite WAL Persistence** *(available)* — append-only event store.
4. **Control plane contracts** *(available)* — HookGate, advisory delivery + ACK, FileActuator + fakes.
5. **M2.0 detect → defer → deliver → ACK loop** *(library + tests)* — `pkg/supervisor`.
6. **M2.1 effort calibration** *(library)* — intake, verification_churn, before_tool over-SOP deny.
7. **M2.2 host bridges** *(experimental / observation)* — Codex JSONL offline+tail, Claude PreTool bridge CLI, FileActuator channel; **not** auto-installed dual-host product.
8. **Optional LLM Reviewer** *(uncertain slow path only)* — OpenAI-compatible provider from config; high-confidence ZOOM_OUT stays deterministic (no LLM). See `docs/reviewer/optional_llm_advice.md` and `go run ./cmd/reviewerdemo`.

---

## Core Architecture Overview

```
reinframe/
├── cmd/
│   ├── streetwire/            # A–F street demo (offline Codex, loops, bridge, FileActuator)
│   ├── claudebridge/          # Claude PreTool / prompt stdin→JSON (#96 experimental)
│   ├── claudeinstall/         # Project-local Claude hooks install/doctor (#106)
│   ├── codexctl/              # Codex observe-only operator surface (#107)
│   └── reviewerdemo/          # Optional LLM: fixed ZOOM_OUT vs SuggestedAdvice
├── docs/
│   ├── adapter/               # Host bridge + intake + FileActuator docs
│   ├── reviewer/              # Optional LLM advice honesty
│   ├── detector/              # #98 fingerprint rules
│   ├── dev/                   # STREET_WIRE.md street map
│   ├── roadmap/               # CURRENT.md executable queue
│   ├── adr/                   # Architecture Decision Records
│   ├── research/              # Threat model, capability matrix, level axes
│   ├── specs/                 # Adaptive Task Supervisor + Action Alignment design
│   └── architecture/          # Execution DAG
├── pkg/
│   ├── protocol/              # Schemas, ValidateEvent, capability negotiation (25 flags)
│   ├── state/                 # SQLite WAL event store
│   ├── adapter/               # EventSource, Actuators, HookGate, Claude/Codex bridges
│   ├── detector/              # RepeatedFailure, VerificationChurn, ToolBudget, HypothesisLoop
│   ├── policy/                # Fast/slow + before_tool
│   ├── supervisor/            # Orchestrator composition + vertical-slice tests
│   ├── config/                # Versioned configuration schema
│   └── reviewer/              # ReviewerProvider + OpenAI-compatible optional path
├── tests/
│   └── integration/           # Protocol/store/scenario-persistence tests
├── .github/workflows/ci.yml
├── go.mod                     # module github.com/ImL1s/reinframe
└── README.md
```

### Module Responsibilities
- **`pkg/protocol`**: Canonical schemas (TaskEnvelope + TaskSubmitted/TaskContract/EvidenceLedger, interventions, …), `BuildContractFromSubmitted` + store emit helpers (`AgentEventFromTask*`), 25 capability flags (including CapAdviceDelivery, CapToolGate, CapInterventionAck, …), Level masks, negotiation. See `docs/specs/adaptive_task_supervisor.md`.
- **`pkg/state`**: SQLite WAL append-only store; persistence invariants only (not full schema validation on append).
- **`pkg/adapter`**: Control-plane contracts; LogObserver; Codex offline/tail EventSource; Claude PreTool bridge; FileActuator + FakeActuator; HookGate / PendingQueue. Docs under `docs/adapter/`.
- **`pkg/detector`**: Deterministic detectors (no LLM): repeated-failure, verification_churn, tool-budget, hypothesis-loop (provisional thresholds).
- **`pkg/policy`**: Fast path = HookGate; before_tool over-SOP; slow path ZOOM_OUT for high-confidence signals (including #98 modes; optional Reviewer on uncertain branch).
- **`pkg/supervisor`**: Composition root: detect → policy → queue → deliver → ACK.
- **`cmd/streetwire`**: End-to-end **library** demo of residual paths (see `docs/dev/STREET_WIRE.md`).
- **`cmd/claudebridge`**: Stdin PreTool/prompt bridge for optional Claude Code hooks (experimental).
- **`cmd/claudeinstall`**: Project-local settings install/doctor (#106 unit path; live smoke is #120).
- **`cmd/codexctl`**: Codex observe-only discovery/tail helper (#107; not control).
- **`pkg/config`**: Versioned config schema (loader still thin).
- **`pkg/reviewer`**: Provider interface + optional OpenAI-compatible path (uncertain only).
- **`tests/integration`**: Foundation integration tests (not full Anti-Tunnel E2E).

### Control-plane reality check
```text
Available:  Detectors → Policy → Orchestrator → HookGate / queue / FileActuator|Fake
            Codex JSONL offline + tail EventSource; Claude PreTool fixture/CLI bridge
            Claude project-local installer (unit); codexctl observe helpers
            ProposedAction, shadow classifier (Enforced=false), offline M3 benches
            managed worktree checkpoint/rollback (clean-only under managed root)
Not claimed: global ~/.claude silent install, process-attach daemon,
             live dual-host supervision, calibrated hard-gates,
             live Claude smoke (#120), advice agent receipt (#108)
```
Vertical-slice and street-wire tests prove the **library and channel** paths. Host consumers of FileActuator / optional hook install remain operator-owned.

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

5. **Build packages and demos**:
   ```bash
   go build -v ./...
   go run ./cmd/streetwire -no-codex
   # optional: offline Codex rollout if present
   go run ./cmd/streetwire -codex "$HOME/.codex/sessions/.../rollout-….jsonl"
   # experimental Claude PreTool bridge
   echo '{"session_id":"s","tool_name":"Bash"}' | go run ./cmd/claudebridge pretool -deny-tool Bash
   # optional LLM reviewer demo (fixture HTTP; not always-on)
   go run ./cmd/reviewerdemo
   ```

### Further reading
- [`docs/dev/STREET_WIRE.md`](docs/dev/STREET_WIRE.md) — how pieces connect + honesty boundaries
- [`docs/reviewer/optional_llm_advice.md`](docs/reviewer/optional_llm_advice.md) — when LLM advice runs vs fixed ZOOM_OUT
- [`docs/adapter/claude_bridge.md`](docs/adapter/claude_bridge.md) — #96
- [`docs/adapter/file_actuator.md`](docs/adapter/file_actuator.md) — #97
- [`docs/adapter/codex_eventsource.md`](docs/adapter/codex_eventsource.md) — #95 offline/tail
- [`docs/detector/tool_budget_hypothesis.md`](docs/detector/tool_budget_hypothesis.md) — #98

---

## Contributing

Contributions welcome! Please:

1. Check the [Issue tracker](https://github.com/ImL1s/reinframe/issues) and [`docs/roadmap/CURRENT.md`](docs/roadmap/CURRENT.md) for open work (epic **#80**; ready **#131**/**#132**/**#142**; blocked **#120**→**#108**, **#134**–**#141**)
2. Follow existing code style and test patterns; keep honesty boundaries (no false product claims)
3. Run `go test -race ./...` before submitting PRs
4. All PRs require CI green on all three platforms + AI review comment before merge (project standard)

---

## License

MIT — see [LICENSE](LICENSE).
