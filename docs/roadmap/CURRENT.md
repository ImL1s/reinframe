# Reinframe current executable roadmap

**Status:** current (2026-08-06) — post-#132 / PR #150 provider-runtime merge  
**Executable status source:** this file plus live GitHub issue labels. `README.md` is the public summary and must remain capability-honest; `docs/specs/*` remain normative contracts; Epic #80 remains the tracker.

PR #148 completed the host-neutral #131 challenge core. PR #150 completed the #132 provider-neutral classifier foundation. Closed work is historical here, not an active dependency.

## Implemented — narrow DoD, do not reopen for the same scope

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| M2 control-plane library | #69–#71, #82–#86 / #88–#93 | Tested library/fake-agent loop; not dual-host production |
| Host observation/bridges | #95–#97, #106–#118 / related PRs | Experimental/observe-only surfaces; live Claude proof remains open |
| Action Alignment design + wire | #104, #119 / PR #111, #126 | Closed schemas and deterministic resolver model |
| Shadow classifier | #105 / PR #128 | `Enforced=false` always |
| Offline evaluation foundation | #100 / PR #129 | **MORE-DATA**; no calibrated hard-gate |
| Managed-worktree rollback | #99 / PR #130 | Clean-only; not primary checkout or external rollback |
| Appealable `BLOCK` core | **#131 / PR #148** | Host-neutral challenge state, justification, one-shot retry, replay; no live Claude delivery |
| Classifier provider foundation | **#132 / PR #150** | Closed request/result contracts, recent-N packet, strict parser, generic OpenAI-compatible adapter, normalized usage/audit; **no native provider cache claim** |
| Governance synchronization | #109, #121, #142 / PR #110, #122, #143, #144, #147, #149 | Source-of-truth maintenance only |

## Active backlog

### Ready — no open code dependency for the stated lane

| Issue | Pri | Scope | Boundary |
|-------|-----|-------|----------|
| **#134** | P1 | Native OpenAI Responses classifier adapter and explicit prompt-cache controls | OpenAI-native profile only; generic adapter remains cache-neutral |
| **#135** | P1 | Native Anthropic Messages adapter and `cache_control` profiles | Direct Claude API profile; no hosted-platform equivalence claim |
| **#136** | P1 | Native Gemini `generateContent` adapter and implicit-cache telemetry/eligibility | Explicit cache objects remain deferred |
| **#137** | P1 | Native xAI Responses adapter and sticky prefix-cache routing | Responses profile only; no generic OpenAI/xAI equivalence |
| **#138** | P1 | Process-local exact `RawAssessment` cache, singleflight and observability | Never cache final Stage 2 decisions or transient fallback outcomes |
| **#140 Lane A** | P2 | Deterministic challenge fixtures, bypass/replay/race/recovery evaluation | Evidence only; later model/Claude lanes remain dependency-bound |

### Blocked by environment

| Issue | Pri | Blocker | Notes |
|-------|-----|---------|-------|
| **#120** | P0 | Interactive/operator Claude Code session | Pinned project-local ALLOW/BLOCK/context smoke; `BLOCKED_BY_ENVIRONMENT` |

Closed #115/#116/#117 are implementation prerequisites, not current blockers. Do not mark #120 ready until live behavioral evidence exists.

### Blocked by remaining dependency/evidence

| Issue | Pri | Blocked by | Notes |
|-------|-----|------------|-------|
| **#108** | P0 | **#120** | Real advice consumer / SafeBoundary / honest ACK |
| **#139** | P1 | **#120** | #131 core is satisfied; Claude challenge delivery/retry still needs pinned live context/behavior evidence |
| **#140 later lanes** | P2 | model lane: one selected **#134–#137** provider; Claude lane: **#139** | Lane A is ready; #132 foundation is satisfied |
| **#141** | P2 | **#138** plus one or more selected **#134–#137** lanes | #132 foundation is satisfied; provider/exact-cache correctness and economics remain unmeasured |
| **#80** | epic | — | Residual tracker; keep open |

## Execution order

```text
Ready in parallel:
  #134 OpenAI native adapter/cache
  #135 Anthropic native adapter/cache
  #136 Gemini native adapter/cache
  #137 xAI native adapter/cache
  #138 exact assessment cache + singleflight
  #140 Lane A deterministic challenge evaluation

Environment lane:
  #120 live Claude smoke
    → #108 generic advice consumer
    → #139 Claude challenge integration
       (#131 core dependency is satisfied)

Evaluation:
  #140 Lane A now
       model-backed lane after one selected native provider
       Claude lane after #139
  #141 after #138 + selected provider/cache lanes
```

## Architectural invariants

### Classifier vs challenge

```text
Classifier / deterministic resolver: ALLOW | BLOCK
Appeal workflow metadata: none | APPEALABLE_CHALLENGE | HUMAN_REVIEW
```

`CHALLENGE` is not a third classifier decision. A justification is bounded external decision evidence, never private chain-of-thought and never automatic permission. Hard security boundaries remain non-appealable or require human review.

Only a fully validated provider result may enter threshold scoring. Provider failure, parse failure, timeout, invalid telemetry or fail-open fallback cannot mint `ALLOWED_ONCE`.

### Cache layers are distinct

```text
Stage 0 deterministic skip       → no model call
Reinframe exact assessment hit   → provider call skipped
Provider prompt/prefix cache     → provider call occurs; provider may reuse prefix work
No cache                         → normal provider path
```

The generic OpenAI-compatible adapter defaults to no vendor-specific cache capability. Native adapters own provider-specific fields. Exact cache may store only validated provider-stage assessments; deterministic Stage 2 reruns with current threshold, policy, exceptions, approval and challenge state.

## Explicit non-claims

- No calibrated classifier/detector hard-gate; #100 remains **MORE-DATA**
- No live Claude challenge-response product; #139 remains open
- No live Claude ALLOW/BLOCK/context proof until #120 evidence
- Merged #132 generic foundation ≠ native OpenAI/Anthropic/Gemini/xAI support
- No provider-prefix or exact-cache savings claim before #141 evidence
- No persistent/distributed assessment cache claim
- No silent global Claude/Codex install
- No dual-host production supervision claim
- FileActuator write or context transport ≠ explicit agent ACK
- Codex JSONL tail / codexctl ≠ bidirectional control
- Managed-worktree rollback ≠ primary checkout or external-side-effect rollback

## Evaluation

Baseline offline reports remain under [`docs/evaluation/`](../evaluation/). #100 disposition is **MORE-DATA** and hard-gates remain disabled.

- #140 Lane A can evaluate deterministic challenge quality, semantic bypass resistance, replay, concurrency, recovery and added cost.
- #140 model-backed/Claude lanes depend on a selected native provider and #139 respectively.
- #141 evaluates provider prefix caching, Reinframe exact caching, singleflight, correctness invariance and measured economics.

Any promotion or default-enable decision requires a separate issue.

## Historical sources — not executable backlog

- `docs/plans/2026-08-03-issue-queue.md` — superseded
- Closed Epic #1 — foundation archive
