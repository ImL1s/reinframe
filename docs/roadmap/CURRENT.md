# Reinframe current executable roadmap

**Status:** current (2026-08-06) — provider/cache campaign complete on main (through PR **#172** panic-safe singleflight)  
**Executable status source:** this file plus live GitHub issue labels. Epic #80 remains open for residual environment-bound work.

## Implemented — narrow DoD, do not reopen for the same scope

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| Appealable `BLOCK` core | **#131 / PR #148** | Host-neutral; no live Claude delivery |
| Classifier provider foundation | **#132 / PR #150** | Generic OpenAI-compatible cache-neutral |
| Native OpenAI Responses | **#134 / PR #153** | `/v1/responses`; explicit profiles |
| Native Anthropic Messages | **#135 / PR #154** | `claude_api` only; explicit recommended |
| Native Gemini generateContent | **#136 / PR #155** | Implicit; explicit objects deferred |
| Native xAI Responses | **#137 / PR #156** | `prompt_cache_key`; no `x-grok-conv-id` |
| Exact assessment cache | **#138 / PR #157** + **#169** + **#172** | Process-local; default disabled; session partition; cancel-safe + panic-safe singleflight |
| Challenge eval Lane A | **#140 / PR #159** | Deterministic fixtures/metrics; MORE-DATA |
| Challenge eval Lane B offline | **#140 / PR #160** | Fake native transports only |
| Provider/cache eval | **#141 / PR #161** + **#169** | Fake CI modes incl. stage0/cold/dynamic/invalid-admission; disposition **MORE-DATA** |
| Offline detector/classifier foundation | **#100 / PR #129** | MORE-DATA; no hard-gate |

## Residual open work

### Environment-bound

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#120** | Interactive Claude session | Live ALLOW/BLOCK/context smoke |
| **#108** | #120 | Real advice consumer / ACK |
| **#139** | #120 | Live Claude challenge delivery |
| **#140 Claude host lane** | #139 | Manual evidence only; not machine-upgraded |

### Epic

| Issue | Notes |
|-------|-------|
| **#80** | Keep open until residual environment/product close criteria are met |

## Explicit non-claims

- No live Claude smoke (#120), ACK (#108), or challenge delivery (#139)
- No calibrated hard-gate; #100/#140/#141 remain **MORE-DATA** where stated
- No default exact-cache or provider-cache enablement from #141
- No universal provider savings percentage / fabricated metrics
- No persistent/distributed exact cache
- No dual-host production supervision

## Evaluation artifacts

- `docs/evaluation/challenge_lane_a.md`
- `docs/evaluation/challenge_lane_b_model.md`
- `docs/evaluation/provider_cache_141.md`
- Adapter pins: `docs/classifier/*`
