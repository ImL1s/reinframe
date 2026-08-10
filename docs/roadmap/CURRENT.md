# Reinframe current executable roadmap

**Status:** current (2026-08-10) — **#201/#199/#200/#215/#218/#219 closed** on main; public Grok ranking disposition remains **MORE_DATA**; latest live v2 campaign on tip `3a218bd` produced harness **NO_GO** (ACP-SESSION prompt Internal error) under `docs/evidence/grok_build/runs/20260810T170640Z/`; #168 MORE-DATA; no ranking  

**Executable status source:** this file plus live GitHub issue labels. Epic #80 remains open.

## Implemented — narrow DoD, do not reopen for the same scope

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| Appealable `BLOCK` core | **#131 / PR #148** | Host-neutral; no live Claude delivery |
| Classifier provider foundation | **#132 / PR #150** | Generic OpenAI-compatible cache-neutral |
| Native OpenAI/Anthropic/Gemini/xAI | **#134–#137** | Classifier adapters only; xAI ≠ Grok host |
| Exact assessment cache | **#138** + fixes | Process-local; default off; panic-safe singleflight |
| Challenge/cache offline eval | **#140** A/B, **#141** | MORE-DATA |
| Codex EventSource | **#95/#107/#118** | JSONL observe-only L0 |
| Claude PreTool bridge | **#96/#106/#117** | Experimental; not live smoke |
| Host roadmap sync | **#170** | Public status for Codex/Grok lanes |
| Codex project-local hooks | **#163** | Foundation; live **#164** |
| Grok Build native hooks | **#165** | Foundation; host fail-open; live **#167** |
| Grok Build ACP stdio | **#166** + residual **#180**; hardening **#191** | Foundation on main; #191 fixes delegated-auth, canonical levels, official headless argv, Windows Job Object tree; live **#167** |
| Cross-host eval framework | **#168** | Fake + historical Grok pin; disposition **MORE-DATA**; no ranking; open until matched Codex/Claude live |

## Residual open work


### Evidence / delivery residuals (active)

| Issue | Notes |
|-------|-------|
| **#201** | **CLOSED** — public claim downgrade |
| **#199** | **CLOSED** — v2 gates first version |
| **#200** | **CLOSED** — source-bound ACK + durable foundation |
| **#208** | **CLOSED** (PR #210/#211 + residuals) — fail-closed sidecar, `.tmp` promote, semantic JSONL, actuator boundary |
| **#209** | **CLOSED** (PR #210 + residuals) — closed status enum, empty id NO_GO, committed-schema pre-write gate |

Completed honesty order: **#201 → #199 → #200 → #205–#207 → #208+#209 + residual follow-ups**.

### Environment / live evidence (still open)

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#164** | Interactive Codex + project trust | Live hooks smoke |
| **#120** | Interactive Claude | Live Claude smoke |
| **#139** | #120 | Claude challenge delivery |
| **#140 Claude host lane** | #139 | Manual evidence |

### Recently merged (Grok campaign) — claims bounded by residual issues

| Issue | Notes |
|-------|-------|
| **#167** / PR #193 | Historical live harness + v1 evidence on main; **unconditional GO withdrawn**; **#199** v2 gates closed; **2026-08-10** live v2 campaign on main `3a218bd` → harness **NO_GO** (see `docs/evidence/grok_build/runs/20260810T170640Z/`); public ranking still **MORE_DATA** |
| **#108** / PR #194 | Foundation consumer/ledger on main; **#200** ACK/durability closed; **live E2E composition not claimed** |
| **#191** | Grok ACP official-contract / lifecycle hardening on main |
| **#201** | Immediate public-status downgrade (closed) |

### Evaluation

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#168** live match | #164/#120 matched data + Grok session reliability | Grok historical + v2 live pins present; #168 disposition **MORE-DATA**; no ranking; Grok v2 campaign **NO_GO** does not strengthen ranking |

### Epic

| Issue | Notes |
|-------|-------|
| **#80** | Open until residual environment/product close criteria are met |

## Explicit non-claims

- Historical Grok live run present (#167 v1); latest live v2 campaign is **NO_GO** (not a GO artifact); public ranking disposition remains **MORE_DATA** (#199 gates closed; no false GO)
- No fail-closed Grok hook security (host fail-open)
- CapToolGate ≠ CapPause / Level 2
- JSON-RPC / file append / hook exit ≠ explicit agent ACK
- xAI classifier ≠ Grok Build host
- No dual-host production supervision; no hard-gate; no default cache enablement
- No cross-host tunneling ranking without matched multi-host live evidence
- No Level 2 / CapPause from hooks alone; transport ≠ explicit ACK
- No source-correlated session_visible product claim without a new live v2 report; no explicit ACK for current Grok profile
- No exactly-once delivery claim; no completed live #108 E2E composition path

## Evaluation artifacts

- `docs/evaluation/challenge_lane_a.md`
- `docs/evaluation/challenge_lane_b_model.md`
- `docs/evaluation/provider_cache_141.md`
- `docs/evaluation/cross_host_168.md`
- Adapter pins: `docs/classifier/*`, `docs/adapter/*`
