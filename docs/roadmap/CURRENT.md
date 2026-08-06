# Reinframe current executable roadmap

**Status:** current (2026-08-06) — post-#134–#138 native provider + exact-cache merge  
**Executable status source:** this file plus live GitHub issue labels. `README.md` is the public summary and must remain capability-honest; `docs/specs/*` remain normative contracts; Epic #80 remains the tracker.

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
| Classifier provider foundation | **#132 / PR #150** | Closed request/result contracts, generic OpenAI-compatible adapter (cache-neutral) |
| Native OpenAI Responses | **#134 / PR #153** | `/v1/responses` only; explicit prefix profiles |
| Native Anthropic Messages | **#135 / PR #154** | `claude_api` only; automatic-* is no wire enablement; explicit recommended |
| Native Gemini generateContent | **#136 / PR #155** | Implicit eligibility; explicit cache objects deferred |
| Native xAI Responses | **#137 / PR #156** | `prompt_cache_key` sticky routing; no Chat Completions `x-grok-conv-id` |
| Exact assessment cache | **#138 / PR #157** | Process-local LRU/TTL + singleflight; default disabled; Stage-2 never cached |
| Governance synchronization | #109, #121, #142 / PR #110, #122, #143, #144, #147, #149, #152 | Source-of-truth maintenance only |

## Active backlog

### Ready — no open code dependency for the stated lane

| Issue | Pri | Scope | Boundary |
|-------|-----|-------|----------|
| **#140 Lane A** | P2 | Deterministic challenge fixtures, bypass/replay/race/recovery evaluation | Evidence only; model/Claude lanes remain dependency-bound |
| **#141** | P2 | Cache-layer mechanics and economics across exact + native providers | Fake/CI mechanics OK; live cost claims require provider telemetry — disposition **MORE-DATA** unless measured |

### Blocked by environment

| Issue | Pri | Blocker | Notes |
|-------|-----|---------|-------|
| **#120** | P0 | Interactive/operator Claude Code session | Pinned project-local ALLOW/BLOCK/context smoke; `BLOCKED_BY_ENVIRONMENT` |

### Blocked by remaining dependency/evidence

| Issue | Pri | Blocked by | Notes |
|-------|-----|------------|-------|
| **#108** | P0 | **#120** | Real advice consumer / SafeBoundary / honest ACK |
| **#139** | P1 | **#120** | #131 core is satisfied; Claude challenge delivery still needs live evidence |
| **#140 later lanes** | P2 | model: any of **#134–#137**; Claude: **#139** | Lane A independent |
| **#80** | epic | — | Residual tracker; keep open |

## Execution order

```text
Shipped:
  #134–#137 native adapters
  #138 exact cache + singleflight

Ready evaluation:
  #140 Lane A deterministic challenge evaluation
  #141 cache economics / correctness invariance (no silent global enable)

Environment lane:
  #120 live Claude smoke
    → #108 generic advice consumer
    → #139 Claude challenge integration
```

## Architectural invariants

### Classifier vs challenge

```text
Classifier / deterministic resolver: ALLOW | BLOCK
Appeal workflow metadata: none | APPEALABLE_CHALLENGE | HUMAN_REVIEW
```

### Cache layers are distinct

```text
Stage 0 deterministic skip       → no model call
Reinframe exact assessment hit   → provider call skipped
Provider prompt/prefix cache     → provider call occurs; provider may reuse prefix work
No cache                         → normal provider path
```

## Explicit non-claims

- No calibrated classifier/detector hard-gate; #100 remains **MORE-DATA**
- No live Claude challenge-response product; #139 remains open
- No measured provider-prefix or exact-cache **savings** claim without #141 evidence
- No persistent/distributed assessment cache claim
- No dual-host production supervision claim

## Evaluation

Baseline offline reports remain under [`docs/evaluation/`](../evaluation/). Adapter docs: `docs/classifier/*`.

Any promotion or default-enable decision requires a separate issue.

## Historical sources — not executable backlog

- `docs/plans/2026-08-03-issue-queue.md` — superseded
- Closed Epic #1 — foundation archive
