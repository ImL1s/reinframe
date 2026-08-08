# Reinframe — Anti-Tunnel Supervision Harness (in progress)

[![CI](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml/badge.svg)](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Reinframe **aims to become** a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go.

**Today:** Reinframe contains a tested control-plane library, deterministic anti-tunnel detectors, an experimental Claude PreTool bridge, **Codex JSONL observe-only + project-local hooks foundation (#163)**, **Grok Build native hooks (#165) + ACP stdio (#166/#191) with live control evidence (#167 GO, darwin/Grok 1.0.0)**, a **first live Grok advice consumer (#108)** with honest ACK ceiling `session_visible`, a FileActuator advice channel, offline benchmarks (#140/#141/#168 frameworks; #168 MORE-DATA with one live Grok pin), and clean-only managed-worktree checkpoint/rollback. It is **not** dual-host production supervision, **not** Codex/Claude live proof (#164/#120), **not** a calibrated hard-gate, and **not** a multi-host tunneling ranking.

## Project Status

> **Phase: M1 + M2 library + host-control foundations + shadow classifier + native classifier providers + exact cache + offline evaluation**  
> **Implemented:** provider/cache campaign (#131–#141); host foundations [#163](https://github.com/ImL1s/reinframe/issues/163)/[#165](https://github.com/ImL1s/reinframe/issues/165)/[#166](https://github.com/ImL1s/reinframe/issues/166); offline [#168](https://github.com/ImL1s/reinframe/issues/168) framework (**MORE-DATA**).  
> **Live:** [#167](https://github.com/ImL1s/reinframe/issues/167) Grok hooks+ACP **GO** (darwin) · still blocked: [#164](https://github.com/ImL1s/reinframe/issues/164) Codex · [#120](https://github.com/ImL1s/reinframe/issues/120) Claude.  
> **Product:** first live host consumer [#108](https://github.com/ImL1s/reinframe/issues/108) **Grok-only** (honest ACK). Epic [#80](https://github.com/ImL1s/reinframe/issues/80) open.  
> **Executable roadmap:** [`docs/roadmap/CURRENT.md`](docs/roadmap/CURRENT.md).

### Current dependency shape

```text
Shipped:
  #134–#137 native classifier adapters · #138 exact cache (default off)
  #140 Lane A/B offline · #141 fake-CI cache eval (MORE-DATA)
  Codex JSONL offline/tail observation (observe-only L0)
  #163 Codex project-local hooks foundation
  #165 Grok Build native hooks foundation (host fail-open)
  #166 Grok Build ACP stdio foundation (transport/session_visible ACK)
  #168 offline cross-host eval framework (MORE-DATA)

Live / environment:
  #167 Grok live proof GO (darwin/Grok 1.0.0; host fail-open; session_visible ACK)
  still blocked: #164 Codex · #120 Claude

Product:
  #108 first live Grok advice consumer (ACK ceiling session_visible; multi-host not claimed)
  #120 → #139 → #140 Claude host lane

Evaluation:
  #168 MORE-DATA — live Grok pin present; missing matched Codex/Claude lanes; no ranking
```

| Component | Status |
|-----------|--------|
| Canonical Schema (25+ types incl. TaskSubmitted/Contract/Ledger) | ✅ Complete |
| Capability Negotiation (**25 flags**, Level 0–3) | ✅ Complete (L1 requires **CapAdviceDelivery**) |
| SQLite WAL Event Store (persistence invariants) | ✅ Complete |
| JSON Schema Validation (1MB limit, `UseNumber`) | ✅ Complete |
| Cross-platform CI (Linux/macOS/Windows + golangci-lint) | ✅ Complete |
| Adapter contracts (`EventSource`, `InterventionActuator`, HookGate, PendingQueue) | ✅ Complete (interfaces + fakes + FileActuator) |
| LogObserverAdapter (L0 inbound) | ✅ Complete |
| Config schema + ReviewerProvider interface | ✅ Complete (local-only default; optional path) |
| RepeatedFailure Detector (#82) | ✅ Complete (provisional N=3) |
| VerificationChurn detector (#85) | ✅ Complete (provisional multi-part fingerprint) |
| Tool-budget + hypothesis-loop detectors (#98) | ✅ Library complete (provisional; not live host auto-intervention) |
| Fast/Slow + before_tool Policy (#69 / #86) | ✅ Complete; #98 modes → ZOOM_OUT on slow path |
| Supervisor Orchestrator (#70/#71) | ✅ Complete (composition root + vertical-slice tests) |
| TaskSubmitted intake mappers (#84) | ✅ Fixture/host mappers (no protocol host type names) |
| Codex EventSource offline + near-live tail (#95/#107/#118) | ✅ Observe-only L0; collision-safe source identity |
| Codex project-local hooks control (#163) | ✅ Foundation — project-local hooks.json install/doctor + PreTool/Permission mapping; **live proof #164** |
| Grok Build native hooks (#165) | ✅ Foundation — `.grok/hooks` install/doctor + PreToolUse allow/deny; host fail-open; **live #167** |
| Grok Build ACP stdio bridge (#166) | ✅ Foundation — JSON-RPC stdio client + safe-boundary prompt; ACK layers honest; **live #167** |
| Live Codex hooks proof (#164) | 🔲 Open — blocked on interactive Codex + project trust |
| Live Grok Build hooks+ACP proof (#167) | ✅ **GO** — live evidence darwin/`grok 1.0.0`; host fail-open measured; ACK `session_visible` (not explicit); `cmd/groklive` |
| Claude PreTool / prompt bridge (#96) | ✅ Experimental API + `cmd/claudebridge` |
| Claude project-local install/doctor (#106/#117) | ✅ Installer/unit + exact ownership; **not** live smoke |
| Typed ProposedAction (#115) | ✅ ToolName ≠ Command |
| PreTool response semantics (#116) | ✅ No `continue:false` for ordinary tool deny |
| Action Alignment design (#104) + wire contract (#119) | ✅ Concept + closed schemas/FakeProvider |
| Shadow classifier runtime (#105) | ✅ Library shadow; `Enforced=false` always |
| M3 synthetic + FP benchmark foundation (#100) | ✅ Offline runner; disposition **MORE-DATA**; no hard-gate |
| Managed worktree checkpoint/rollback (#99) | ✅ Clean-only under managed root; not primary checkout |
| FileActuator advice channel (#97) | ✅ JSONL `reinframe.advice.v1`; write ≠ agent receipt |
| Street-wire demo (`cmd/streetwire`) | ✅ Offline Codex + M2 loops + bridge + FileActuator demos |
| Optional LLM Reviewer (OpenAI-compatible, ADR 003 local-only default) | ✅ Uncertain advice path only; not the classifier provider |
| Open-set governance (#142 / PR #143, PR #144, PR #147) | ✅ Closed — public/current backlog synchronization |
| Appealable BLOCK challenge core (#131 / PR #148) | ✅ Host-neutral core; one-shot retry; no live Claude claim |
| Classifier provider runtime (#132 / PR #150) | ✅ Generic OpenAI-compatible foundation (cache-neutral) |
| Native OpenAI Responses adapter (#134 / PR #153) | ✅ Pinned `/v1/responses` + explicit cache profiles |
| Native Anthropic Messages adapter (#135 / PR #154) | ✅ Direct Claude API only; explicit recommended |
| Native Gemini generateContent adapter (#136 / PR #155) | ✅ Implicit profiles; explicit objects deferred |
| Native xAI Responses adapter (#137 / PR #156) | ✅ Classifier only — **not** Grok Build host control |
| Exact `RawAssessment` cache + singleflight (#138 / PR #157, fix #169/#172) | ✅ Process-local; default disabled; session partition; cancel-safe + panic-safe SF; Stage-2 never cached |
| Live Claude ALLOW/BLOCK/context smoke (#120) | 🔲 Open — `BLOCKED_BY_ENVIRONMENT` |
| First live Grok advice consumer / ACK (#108) | ✅ Grok-only — `GrokACPActuator` + durable ledger; strongest ACK `session_visible`; multi-host not claimed |
| Claude challenge delivery/retry (#139) | 🔲 Open — #131 satisfied; blocked by #120 |
| Challenge evaluation (#140) Lane A/B | ✅ Offline deterministic + fake-native lanes (PR #159/#160); **Claude host lane open** (needs #139) |
| Provider/cache evaluation (#141 / PR #161, modes fix #169) | ✅ Fake-CI suite; disposition **MORE-DATA**; no default cache enablement |
| Cross-host tunneling evaluation (#168) | ✅ Framework + **partial live Grok pin** (MORE-DATA); missing Codex/Claude live; **no ranking** |
| Global host install / dual-host production supervision | 🔲 Not claimed |

## Core invariants

### Action Alignment stays two-valued

```text
Classifier / deterministic resolver: ALLOW | BLOCK
Appeal workflow metadata: none | APPEALABLE_CHALLENGE | HUMAN_REVIEW
```

A challenge is not a third classifier result. A visible justification is external decision evidence, not private chain-of-thought and not automatic permission. The retry is evaluated again. Hard security boundaries remain non-appealable or require human review.

### Cache layers are not interchangeable

```text
Stage 0 deterministic skip       → no model call
Reinframe exact assessment hit   → provider call skipped
Provider prompt/prefix cache     → provider call occurs; provider may reuse prefix work
No cache                         → normal provider path
```

Generic OpenAI-compatible endpoints default to **no vendor-specific cache capability**. Native provider adapters own their request fields and telemetry. Cache never owns the final decision; deterministic Stage 2 reruns with current policy, threshold, exceptions, approval, and challenge state.

## Project Objective

AI coding agents risk "tunneling" through cognitive lock-in, repeated errors, patch churn, scope drift, and disproportionate verification. Reinframe is being built toward:

1. **Supervision Levels (0–3)** — integration capability is separate from intervention severity. See `docs/research/level_axes_mapping.md`.
2. **Canonical Agent Event Schema** *(available)* — versioned Go types + JSON Schemas.
3. **SQLite WAL Persistence** *(available)* — append-only event store.
4. **Control-plane contracts** *(available)* — HookGate, advisory delivery + ACK, FileActuator + fakes.
5. **M2.0 detect → defer → deliver → ACK loop** *(library + tests)* — `pkg/supervisor`.
6. **M2.1 effort calibration** *(library)* — intake, verification churn, before-tool over-SOP denial.
7. **M2.2 host bridges** *(experimental / observation + Ready foundations)* — Codex JSONL offline+tail (observe-only); Claude PreTool bridge; FileActuator; **#163/#165/#166** Ready; **#167 Grok live GO** (darwin); Codex/Claude live still #164/#120.
8. **Action Alignment classifier** *(shadow only)* — raw severity is evidence; deterministic resolver owns `ALLOW | BLOCK`.
9. **Appealable productivity block** *(host-neutral core available in #131; live host delivery planned in #139)* — one structured justification and one semantic retry; no self-permission.
10. **Provider/cost control** *(#132 foundation available; native adapters/cache/evaluation in #134–#141)* — native adapters, provider-aware prefix caching, exact assessment memoization, and measured evaluation.

---

## Core Architecture Overview

```text
reinframe/
├── cmd/
│   ├── streetwire/            # Offline vertical-slice demos
│   ├── claudebridge/          # Claude PreTool / prompt stdin→JSON (#96 experimental)
│   ├── claudeinstall/         # Project-local hooks install/doctor (#106/#117)
│   ├── codexctl/              # Codex observe-only operator surface (#107/#118)
│   ├── bench/                 # Offline synthetic/FP evaluation (#100)
│   └── reviewerdemo/          # Optional LLM advice path
├── docs/
│   ├── adapter/               # Host bridge + FileActuator docs
│   ├── policy/                # Challenge policy and intervention boundaries
│   ├── reviewer/              # Optional LLM advice honesty
│   ├── detector/              # Detector/fingerprint rules
│   ├── evaluation/            # Offline benchmark reports
│   ├── dev/                   # STREET_WIRE.md
│   ├── roadmap/               # CURRENT.md executable queue
│   ├── adr/                   # Architecture Decision Records
│   ├── research/              # Threat model, capability matrix, level axes
│   ├── specs/                 # Supervisor + Action Alignment contracts
│   └── architecture/          # Historical/current architecture notes
├── pkg/
│   ├── protocol/              # Canonical schemas and capability negotiation
│   ├── state/                 # SQLite WAL event store
│   ├── adapter/               # HookGate, Claude/Codex bridges, actuators
│   ├── detector/              # RepeatedFailure, VerificationChurn, ToolBudget, HypothesisLoop
│   ├── policy/                # Fast/slow and before-tool policy
│   ├── supervisor/            # Detect → policy → deliver → ACK composition
│   ├── classifier/            # Closed shadow classifier/FakeProvider
│   ├── challenge/             # Host-neutral appealable BLOCK workflow (#131)
│   ├── evaluation/            # Offline benchmark runner
│   ├── workspace/             # Managed worktree checkpoint/rollback
│   ├── config/                # Versioned configuration schema
│   └── reviewer/              # Optional advice ReviewerProvider
├── tests/
│   └── integration/           # Foundation integration tests
├── .github/workflows/ci.yml
├── go.mod
└── README.md
```

### Module Responsibilities

- **`pkg/protocol`**: canonical schemas, task/contract/ledger helpers, intervention types, capability negotiation.
- **`pkg/state`**: append-only SQLite WAL store and persistence invariants.
- **`pkg/adapter`**: control-plane contracts, Codex observation, Claude PreTool bridge, FileActuator, HookGate and pending queue.
- **`pkg/detector`**: deterministic repeated-failure, verification-churn, tool-budget and hypothesis-loop signals.
- **`pkg/policy`**: fast deterministic gates and slow advisory path.
- **`pkg/supervisor`**: composition root for detect → policy → queue → deliver → ACK.
- **`pkg/classifier`**: shadow classifier; #132 runtime; native #134–#137 adapters (OpenAI/Anthropic/Gemini/xAI); #138 process-local exact cache (default off).
- **`pkg/challenge`**: versioned appealable `BLOCK` records, closed justification schema, one-shot retry, semantic fingerprinting, replay, and audit/cache identity.
- **`pkg/evaluation`**: offline fixture scoring and reports; #100 disposition remains MORE-DATA.
- **`pkg/workspace`**: clean-only checkpoint/rollback inside a verified Reinframe-owned worktree.
- **`pkg/config`**: versioned configuration and env-placeholder secret references.
- **`pkg/reviewer`**: optional LLM advice provider; separate from the classifier contract.

### Control-plane reality check

```text
Available:
  Detectors → Policy → Orchestrator → HookGate / queue / FileActuator|Fake
  Codex JSONL offline + tail EventSource (observe-only L0)
  Codex project-local hooks foundation (#163; live #164)
  Grok Build native hooks foundation (#165; host fail-open; live #167)
  Grok Build ACP stdio foundation (#166; transport/session_visible ACK; live #167)
  Claude PreTool fixture/CLI bridge + project-local installer/unit validation
  ProposedAction, shadow classifier (Enforced=false), offline benchmark runner
  host-neutral appealable BLOCK challenge core with one-shot semantic retry
  managed-worktree checkpoint/rollback (clean-only)
  native classifier adapters (#134–#137 library paths; not host control)
  process-local exact RawAssessment cache (#138; default disabled)
  cross-host offline eval framework (#168 MORE-DATA)

Not claimed / not proven live:
  live Codex hooks (#164) · live Claude (#120)
  (Grok live #167 GO on darwin — see Available / status table)
  global ~/.claude silent install · process-attach daemon
  live dual-host supervision · calibrated hard-gates · native CapPause from hooks
  advice agent receipt (#108) · Claude challenge delivery (#139)
  measured provider-prefix / exact-cache savings (#141 MORE-DATA)
  explicit ACK from file append / hook exit / JSON-RPC success alone
  cross-host tunneling ranking without matched live evidence
```

Vertical-slice and street-wire tests prove the **library and channel** paths. Host consumers and live evidence remain capability-specific.

---

## Supervision Levels (Integration Handshake)

| Level | Name | Required capabilities (summary) | Use case |
|-------|------|----------------------------------|----------|
| 0 | **Observe** | CapEventStream | Passive monitoring (for example LogObserver/Codex tail) |
| 1 | **Advisory** | + CapToolInspection + **CapAdviceDelivery** | Zoom-out / replan advice |
| 2 | **Guarded** | + Diff + native CapPause + Cancel + Resume | Pause/tool gate; OS SIGSTOP ≠ native CapPause |
| 3 | **Full Control** | + Checkpoint/Rollback, Headless, CLI, MCP, Subagents, SwitchModel | Full autonomy supervision |

Intervention escalation after detection is a **separate axis**. See `docs/research/level_axes_mapping.md`.

---

## Quickstart Guide

### Prerequisites

- Go 1.25.0+

### Building and testing

1. **Clone and download dependencies**:

   ```bash
   git clone https://github.com/ImL1s/reinframe.git
   cd reinframe
   go mod download
   ```

2. **Run package tests**:

   ```bash
   go test -v -race ./pkg/...
   ```

3. **Run integration tests**:

   ```bash
   go test -v -race ./tests/integration/...
   ```

4. **Run the full suite**:

   ```bash
   go test -v -race ./...
   ```

5. **Build packages and demos**:

   ```bash
   go build -v ./...
   go run ./cmd/streetwire -no-codex

   # Optional: offline Codex rollout if present
   go run ./cmd/streetwire -codex "$HOME/.codex/sessions/.../rollout-….jsonl"

   # Experimental Claude PreTool bridge
   echo '{"session_id":"s","tool_name":"Bash"}' \
     | go run ./cmd/claudebridge pretool -deny-tool Bash

   # Optional LLM reviewer demo (fixture HTTP; not always-on)
   go run ./cmd/reviewerdemo
   ```

### Further reading

- [`docs/roadmap/CURRENT.md`](docs/roadmap/CURRENT.md) — executable backlog and dependencies
- [`docs/policy/appealable_block_challenge.md`](docs/policy/appealable_block_challenge.md) — host-neutral challenge contract and non-claims
- [`docs/dev/STREET_WIRE.md`](docs/dev/STREET_WIRE.md) — how the available pieces connect
- [`docs/specs/action_alignment_classifier.md`](docs/specs/action_alignment_classifier.md) — Stage 0/1/2 design
- [`docs/specs/action_alignment_wire_contract.md`](docs/specs/action_alignment_wire_contract.md) — closed classifier schemas
- [`docs/evaluation/m3_benchmarks.md`](docs/evaluation/m3_benchmarks.md) — MORE-DATA benchmark report
- [`docs/reviewer/optional_llm_advice.md`](docs/reviewer/optional_llm_advice.md) — optional advice provider boundary
- [`docs/adapter/claude_bridge.md`](docs/adapter/claude_bridge.md) — experimental Claude bridge
- [`docs/adapter/file_actuator.md`](docs/adapter/file_actuator.md) — advice transport and ACK honesty
- [`docs/adapter/codex_eventsource.md`](docs/adapter/codex_eventsource.md) — Codex offline/tail observation
- [`docs/detector/tool_budget_hypothesis.md`](docs/detector/tool_budget_hypothesis.md) — review-session detector rules

---

## Contributing

Contributions are welcome. Please:

1. Check the [Issue tracker](https://github.com/ImL1s/reinframe/issues) and [`docs/roadmap/CURRENT.md`](docs/roadmap/CURRENT.md). Host foundations #163/#165/#166, live Grok **#167 GO**, Grok consumer **#108**, and #168 MORE-DATA (partial live Grok) are on main. Residual live environment: **#164 Codex**, **#120 Claude** (and multi-host #168 match).
2. Do not start #139 before #120. Do not claim live host control without #164/#167/#120 evidence. Do not claim measured savings or default exact-cache enablement without separate authorization.
3. Preserve honesty boundaries: no false provider/cache/live-host/hard-gate/ACK claims. Classifier providers ≠ coding-host adapters.
4. Run `go test -race ./...` before submitting PRs.
5. Project-standard PRs require multi-platform CI and independent AI review before merge.

---

## License

MIT — see [LICENSE](LICENSE).
