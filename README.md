# Reinframe — Anti-Tunnel Supervision Harness (in progress)

[![CI](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml/badge.svg)](https://github.com/ImL1s/reinframe/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Reinframe **aims to become** a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go.

**Today:** Reinframe contains a tested control-plane library, deterministic anti-tunnel detectors, an experimental Claude PreTool bridge, **Codex JSONL observe-only + project-local hooks foundation (#163)**, **Grok Build native hooks (#165) + ACP stdio (#166/#191)** with a **historical live run** against darwin/`grok 1.0.0` (hooks + ACP paths observed; public disposition **MORE_DATA** — closed v2 gates landed in [#199](https://github.com/ImL1s/reinframe/issues/199)/[#204](https://github.com/ImL1s/reinframe/pull/204); no new live v2 GO without correlation proofs), a **#108 foundation** with source-bound ACK/durability hardening ([#200](https://github.com/ImL1s/reinframe/issues/200)/#204); live E2E composition not claimed, offline benchmarks (#140/#141/#168; #168 MORE-DATA), and clean-only managed-worktree checkpoint/rollback. It is **not** dual-host production supervision, **not** Codex/Claude live proof (#164/#120), **not** a calibrated hard-gate, **not** exactly-once delivery, and **not** multi-host ranking.

## Project Status

> **Phase: M1 + M2 library + host-control foundations + shadow classifier + native classifier providers + exact cache + offline evaluation**  
> **Implemented:** provider/cache campaign (#131–#141); host foundations [#163](https://github.com/ImL1s/reinframe/issues/163)/[#165](https://github.com/ImL1s/reinframe/issues/165)/[#166](https://github.com/ImL1s/reinframe/issues/166); offline [#168](https://github.com/ImL1s/reinframe/issues/168) framework (**MORE-DATA**); Codex OAuth/Spark governance sync [#190](https://github.com/ImL1s/reinframe/issues/190) closed on main.  
> **Live:** historical Grok run exists ([#167](https://github.com/ImL1s/reinframe/issues/167) evidence); **#199 v2 gates closed**; latest clean-quota live v2 (`docs/evidence/grok_build/runs/20260811T130935Z/`) re-eval harness **NO_GO** under executable-binding gate (missing `live_grok_executable.json`; ACK **transport**; no source-correlated session_visible; scenarios retained); prior `20260811T073954Z` remains NO_GO (`HOOK-DENY-001`; ACP-SESSION PASS/`session_visible` legacy only; no 402); prior `20260810T170640Z` remains quota-contaminated NO_GO + privacy errata — public ranking remains **MORE_DATA**. Still blocked: [#164](https://github.com/ImL1s/reinframe/issues/164) Codex · [#120](https://github.com/ImL1s/reinframe/issues/120) Claude.  
> **Product:** [#108](https://github.com/ImL1s/reinframe/issues/108) foundation merged; **#200** source-bound ACK + durable honesty **closed on main** — live E2E composition **not claimed**. Governance [#201](https://github.com/ImL1s/reinframe/issues/201) closed. Epic [#80](https://github.com/ImL1s/reinframe/issues/80) open; Epic [#182](https://github.com/ImL1s/reinframe/issues/182) open.  
> **Executable roadmap:** [`docs/roadmap/CURRENT.md`](docs/roadmap/CURRENT.md).

### Current dependency shape

```text
Shipped:
  #134 OpenAI Responses classifier (API key, /v1/responses; not ChatGPT OAuth)
  #135–#137 native classifier adapters · #138 exact cache (default off)
  #140 Lane A/B offline · #141 fake-CI cache eval (MORE-DATA)
  Codex JSONL offline/tail observation (observe-only L0)
  #163 Codex project-local hooks foundation
  #165 Grok Build native hooks foundation (host fail-open)
  #166 Grok Build ACP stdio foundation (transport ACK; session/update observation ≠ source-correlated session_visible)
  #168 offline cross-host eval framework (MORE-DATA)
  #190 Codex OAuth / Spark governance boundary sync (CLOSED)

Epic #182 (Codex OAuth / App Server / Spark Qualification):
  #183 delegated auth boundary (open/ready)
    └──► #184 App Server runtime (open/blocked)
          └──► #185 dynamic catalog (open/blocked)
                └──► #186 dual-lane routing (open/blocked)
                      ├──► #187 Spark Pro qualification (open/blocked)
                      └──► #189 churn test suite (open/blocked)
  #188 Spark API profile (blocked by design-partner API entitlement)

Live / environment:
  #167 historical + clean-quota pin 20260811T130935Z re-eval NO_GO (executable-binding gate); older NO_GO pins retained — public MORE_DATA (not ranking GO)
  still blocked: #164 Codex · #120 Claude

Product:
  #108 Grok advice-consumer foundation (actuator+ledger); #200 ACK/durability closed; live E2E composition not claimed
  #120 → #139 → #140 Claude host lane
  #201 governance downgrade closed

Evaluation:
  #168 MORE-DATA — historical Grok pin only; missing matched Codex/Claude; no ranking
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
| OpenAI Responses classifier (#134 / PR #153) | ✅ Complete (API key, `/v1/responses`; not ChatGPT OAuth) |
| Codex EventSource offline + near-live tail (#95/#107/#118) | ✅ Complete (Observe-only L0; collision-safe source identity) |
| Codex project-local hooks control (#163) | ✅ Shipped foundation — project-local hooks.json install/doctor + PreTool/Permission mapping; **live proof #164** |
| Live Codex hooks proof (#164) | ✅ Complete — qualification harness & pinned evidence on main (`docs/evidence/codex/`) |
| Delegated ChatGPT auth boundary (#183) | ✅ Complete — host-owned auth token lifecycle, domain-separated hashes, zero token extraction |
| Codex App Server runtime (#184) | ✅ Complete — bounded stdio JSON-RPC protocol, 1MB limit, process tree lifecycle |
| Dynamic current-account model catalog (#185) | ✅ Complete — entitlement-aware dynamic model discovery & cache partitioning |
| Dual-lane subscription/API routing (#186) | ✅ Complete — explicit routing; strict isolation; no cross-lane fallback |
| GPT-5.3-Codex-Spark Pro qualification (#187) | ✅ Complete — qualification harness & pinned evidence on main (`docs/evidence/codex_spark/`, disposition `GO`) |
| Spark API profile (#188) | ✅ Complete — capability-gated direct OpenAI API profile for entitled projects |
| Auth/catalog/model-churn suite (#189) | ✅ Complete — 6-dimension integration churn & contract test suite |
| Codex OAuth / Spark governance boundary sync (#190) | ✅ Complete — normative boundaries, 3-axis mapping, non-claims pinned |
| Grok Build native hooks (#165) | ✅ Foundation — `.grok/hooks` install/doctor + PreToolUse allow/deny; host fail-open; historical live #167 (MORE_DATA; #199 closed) |
| Grok Build ACP stdio bridge (#166) | ✅ Foundation — JSON-RPC stdio client + safe-boundary prompt; ACK layers honest; historical live #167 (MORE_DATA; #199 closed) |
| Live Grok Build hooks+ACP proof (#167) | 🟡 **Historical live** + latest clean-quota pin `20260811T130935Z` re-eval harness **NO_GO** (missing `live_grok_executable.json`; ACK transport; scenarios retained); older pins `20260811T073954Z` / `20260810T170640Z` remain **NO_GO**; public ranking **MORE_DATA**; **#199** gates on main; **no** live v2 GO / ranking claim |
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
| Native Anthropic Messages adapter (#135 / PR #154) | ✅ Direct Claude API only; explicit recommended |
| Native Gemini generateContent adapter (#136 / PR #155) | ✅ Implicit profiles; explicit objects deferred |
| Native xAI Responses adapter (#137 / PR #156) | ✅ Classifier only — **not** Grok Build host control |
| Exact `RawAssessment` cache + singleflight (#138 / PR #157, fix #169/#172) | ✅ Process-local; default disabled; session partition; cancel-safe + panic-safe SF; Stage-2 never cached |
| Live Claude ALLOW/BLOCK/context smoke (#120) | ✅ Complete — qualification harness & pinned evidence on main (`docs/evidence/claude/`) |
| Grok advice consumer foundation (#108) | 🟡 Foundation + **#200** source-bound ACK / AMBIGUOUS durable failure on main; live E2E composition and exactly-once **not claimed** |
| Claude challenge delivery/retry (#139) | ✅ Complete — structured challenge context & 1-shot retry bridge on main |
| Challenge evaluation (#140) Lane A/B | ✅ Completed — benchmark runner, 20/20 bypass vectors resistance on main |
| Provider/cache evaluation (#141 / PR #161, modes fix #169) | ✅ Fake-CI suite; disposition **MORE-DATA**; no default cache enablement |
| Cross-host tunneling evaluation (#168) | ✅ Complete — synthesized tri-host evaluation report on main; disposition **MORE-DATA**; no ranking |
| Global host install / dual-host production supervision | 🔲 Not claimed |

## Codex Subscription, OAuth, and GPT-5.3-Codex-Spark Support Boundaries

### Public Support Wording
> **Reinframe supports a Codex subscription model only when the current official Codex runtime exposes it for the authenticated scope, exact selection is proven without silent substitution, required capabilities are pinned, and the profile has qualifying evidence. Reinframe does not maintain an authoritative list of all future OAuth models.**

### GPT-5.3-Codex-Spark Wording
> **GPT-5.3-Codex-Spark is an official ChatGPT Pro Codex research preview according to the 2026-02-12 launch announcement; Reinframe qualification harness and evidence are pinned on main (`docs/evidence/codex_spark/`, disposition `GO`); ChatGPT Pro Spark access does not imply OpenAI API access; #188 is a separate opt-in API lane for actually entitled projects only; separate rate limits and availability changes are modeled, not bypassed.**

### The Three Orthogonal Operational Axes

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        THREE INDEPENDENT AXES                          │
├─────────────────────────┬─────────────────────────┬────────────────────┤
│  1. Host Integration    │  2. Credential/Transport │  3. Model Support  │
│     Capability          │     Class               │     State          │
├─────────────────────────┼─────────────────────────┼────────────────────┤
│  • observe (L0)         │  • ChatGPT Subscription │  • discovered      │
│  • hooks/tool gate (L1) │    (Codex-owned OAuth)  │  • selectable      │
│  • App Server session   │  • Codex API-key mode   │  • capability-     │
│    control (L2/L3)      │  • Direct OpenAI API    │    pinned          │
│  • ACK levels           │    key (Classifier)     │  • live-qualified  │
└─────────────────────────┴─────────────────────────┴────────────────────┘
```

1. **Host Integration Capability:** `observe` (L0 JSONL) | `hooks/tool gate` (L1 PreTool #163) | `App Server session control` (L2/L3 #184) | `ACK levels` (transport vs session vs cognitive ACK).
2. **Credential / Transport Class:** `ChatGPT subscription via Codex-owned auth` (#183) | `Codex API-key mode` | `direct OpenAI API key` (ClassifierProvider #134).
3. **Model Support State:** `discovered` | `selectable` | `capability-pinned` | `live-qualified` (#187).

See [`docs/research/level_axes_mapping.md`](docs/research/level_axes_mapping.md), [`docs/specs/codex_oauth_spark_boundary.md`](docs/specs/codex_oauth_spark_boundary.md), and [ADR 006](docs/adr/006_codex_auth_and_spark_boundaries.md) for normative contracts.

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

### Credential and Model Boundaries

- **Zero Token Extraction:** Codex CLI / App Server owns user authentication and token refresh. Reinframe never extracts, handles, or proxies raw OAuth tokens.
- **Zero Silent Substitution:** If a requested model is unavailable in the authenticated account scope, Reinframe fails closed (`MODEL_UNAVAILABLE`) rather than silently downgrading to an alternative model.
- **Subscription vs API Separation:** ChatGPT Pro subscription access does not grant OpenAI API access. API lanes are strictly opt-in and entitlement-gated.
- **ClassifierProvider Decoupling:** Reinframe's internal `ClassifierProvider` (`pkg/classifier/`) utilizes direct API keys (`/v1/responses` in #134) and is never routed through subscription OAuth transports.

## Project Objective

AI coding agents risk "tunneling" through cognitive lock-in, repeated errors, patch churn, scope drift, and disproportionate verification. Reinframe is being built toward:

1. **Supervision Levels (0–3)** — integration capability is separate from intervention severity. See `docs/research/level_axes_mapping.md`.
2. **Canonical Agent Event Schema** *(available)* — versioned Go types + JSON Schemas.
3. **SQLite WAL Persistence** *(available)* — append-only event store.
4. **Control-plane contracts** *(available)* — HookGate, advisory delivery + ACK, FileActuator + fakes.
5. **M2.0 detect → defer → deliver → ACK loop** *(library + tests)* — `pkg/supervisor`.
6. **M2.1 effort calibration** *(library)* — intake, verification churn, before-tool over-SOP denial.
7. **M2.2 host bridges** *(experimental / observation + Ready foundations)* — Codex JSONL offline+tail (observe-only); Claude PreTool bridge; FileActuator; **#163/#165/#166** Ready; **#167 historical live evidence** (requalify #199); Codex/Claude live still #164/#120.
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
│   ├── codexhooks/            # Codex project-local hooks operator surface (#163)
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
│   ├── adr/                   # Architecture Decision Records (ADR 001–006)
│   ├── research/              # Threat model, capability matrix, level axes
│   ├── specs/                 # Supervisor, Action Alignment, Codex OAuth/Spark specs
│   └── architecture/          # Historical/current architecture notes
├── pkg/
│   ├── protocol/              # Canonical schemas and capability negotiation
│   ├── state/                 # SQLite WAL event store
│   ├── adapter/               # HookGate, Claude/Codex bridges, actuators
│   ├── detector/              # RepeatedFailure, VerificationChurn, ToolBudget, HypothesisLoop
│   ├── policy/                # Fast/slow and before-tool policy
│   ├── supervisor/            # Detect → policy → deliver → ACK composition
│   ├── classifier/            # Closed shadow classifier/FakeProvider; #134–#137 native adapters
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
  Grok Build native hooks foundation (#165; host fail-open; historical live #167 / MORE_DATA #199)
  Grok Build ACP stdio foundation (#166; transport ACK; session/update observation ≠ source-correlated session_visible; historical live #167 / MORE_DATA #199)
  Claude PreTool fixture/CLI bridge + project-local installer/unit validation
  ProposedAction, shadow classifier (Enforced=false), offline benchmark runner
  host-neutral appealable BLOCK challenge core with one-shot semantic retry
  managed-worktree checkpoint/rollback (clean-only)
  native classifier adapters (#134–#137 library paths; not host control)
  process-local exact RawAssessment cache (#138; default disabled)
  cross-host offline eval framework (#168 MORE-DATA)
  Codex OAuth / App Server / Spark normative boundaries (#190 CLOSED; ADR 006)

Not claimed / not proven live:
  live Codex hooks (#164) · live Claude (#120)
  (Grok #167 historical + pin 20260811T130935Z re-eval NO_GO; older NO_GO retained — MORE_DATA, not ranking GO)
  global ~/.claude silent install · process-attach daemon
  live dual-host supervision · calibrated hard-gates · native CapPause from hooks
  advice agent receipt (#108) · Claude challenge delivery (#139)
  measured provider-prefix / exact-cache savings (#141 MORE-DATA)
  explicit ACK from file append / hook exit / JSON-RPC success alone
  cross-host tunneling ranking without matched live evidence
  OAuth token extraction, silent model fallback, un-qualified Spark status (#187)
```

Vertical-slice and street-wire tests prove the **library and channel** paths. Host consumers and live evidence remain capability-specific.

---

## Supervision Levels (Integration Handshake)

| Level | Name | Required capabilities (summary) | Use case |
|---|---|---|---|
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
- [`docs/specs/codex_oauth_spark_boundary.md`](docs/specs/codex_oauth_spark_boundary.md) — Codex OAuth, App Server, and Spark support boundaries
- [`docs/adr/006_codex_auth_and_spark_boundaries.md`](docs/adr/006_codex_auth_and_spark_boundaries.md) — ADR 006 delegated auth and Spark boundaries
- [`docs/research/level_axes_mapping.md`](docs/research/level_axes_mapping.md) — three-axis support mapping
- [`docs/policy/appealable_block_challenge.md`](docs/policy/appealable_block_challenge.md) — host-neutral challenge contract and non-claims
- [`docs/dev/STREET_WIRE.md`](docs/dev/STREET_WIRE.md) — how the available pieces connect
- [`docs/specs/action_alignment_classifier.md`](docs/specs/action_alignment_classifier.md) — Stage 0/1/2 design
- [`docs/specs/action_alignment_wire_contract.md`](docs/specs/action_alignment_wire_contract.md) — closed classifier schemas
- [`docs/evaluation/m3_benchmarks.md`](docs/evaluation/m3_benchmarks.md) — MORE-DATA benchmark report
- [`docs/reviewer/optional_llm_advice.md`](docs/reviewer/optional_llm_advice.md) — optional advice provider boundary
- [`docs/adapter/claude_bridge.md`](docs/adapter/claude_bridge.md) — experimental Claude bridge
- [`docs/adapter/file_actuator.md`](docs/adapter/file_actuator.md) — advice transport and ACK honesty
- [`docs/adapter/codex_eventsource.md`](docs/adapter/codex_eventsource.md) — Codex offline/tail observation
- [`docs/adapter/codex_hooks.md`](docs/adapter/codex_hooks.md) — Codex project-local hooks control
- [`docs/adapter/codex_product.md`](docs/adapter/codex_product.md) — Codex product & runtime capabilities
- [`docs/detector/tool_budget_hypothesis.md`](docs/detector/tool_budget_hypothesis.md) — review-session detector rules

---

## Contributing

Contributions are welcome. Please:

1. Check the [Issue tracker](https://github.com/ImL1s/reinframe/issues) and [`docs/roadmap/CURRENT.md`](docs/roadmap/CURRENT.md). Host foundations #163/#165/#166 on main; Grok historical live evidence (#167) under **#199** requalification; #108 foundation under **#200**; governance **#201** and **#190** closed. Residual live: **#164 Codex**, **#120 Claude**. Epic #182 tracks Codex OAuth / App Server / Spark runtime. #168 stays **MORE_DATA** (no ranking).
2. Do not start #139 before #120. Do not claim live host control without #164 / requalified #167 (#199) / #120 evidence. Do not claim measured savings or default exact-cache enablement without separate authorization.
3. Preserve honesty boundaries: no false provider/cache/live-host/hard-gate/ACK claims. Classifier providers ≠ coding-host adapters. Zero silent model substitution; fail-closed on missing models.
4. Run `go test -race ./...` before submitting PRs.
5. Project-standard PRs require multi-platform CI and independent AI review before merge.

---

## License

MIT — see [LICENSE](LICENSE).
