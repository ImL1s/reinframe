# Reinframe current executable roadmap

**Status:** current (2026-08-15) — Epic #80 open; Epic #182 Codex OAuth / App Server / Spark support boundaries synced (#190 CLOSED on main); GPT-5.6 Pro P1 waves on PR #230 branch; latest clean-quota live v2 `20260811T130935Z` re-eval **NO_GO** under executable-binding gate (strongest ACK still **transport**; scenarios retained; not a fresh live campaign); prior pins retained; public **#168 MORE_DATA**; no ranking  

**Executable status source:** this file plus live GitHub issue labels. Epic #80 and Epic #182 remain active.

## Implemented — narrow DoD, do not reopen for the same scope

| Track | Issues/PRs | Honesty boundary |
|---|---|---|
| Appealable `BLOCK` core | **#131 / PR #148** | Host-neutral; no live Claude delivery |
| Classifier provider foundation | **#132 / PR #150** | Generic OpenAI-compatible cache-neutral |
| Native OpenAI/Anthropic/Gemini/xAI | **#134–#137** | Classifier adapters only; direct API keys (/v1/responses); not ChatGPT OAuth; xAI ≠ Grok host |
| Exact assessment cache | **#138** + fixes | Process-local; default off; panic-safe singleflight |
| Challenge/cache offline eval | **#140** A/B, **#141** | Completed (#140 benchmark on main) |
| Codex EventSource | **#95/#107/#118** | JSONL observe-only L0 |
| Claude PreTool bridge | **#96/#106/#117** | Experimental; #139 appealable retry on main |
| Host roadmap sync | **#170** | Public status for Codex/Grok lanes |
| Codex project-local hooks | **#163** / **#164** | Shipped foundation & live evidence on main |
| Claude project-local hooks | **#120** / **#139** | Shipped foundation, live evidence & retry bridge on main |
| Grok Build native hooks | **#165** | Foundation; host fail-open; live **#167** |
| Grok Build ACP stdio | **#166** + residual **#180**; hardening **#191** | Foundation on main; #191 fixes delegated-auth, canonical levels, official headless argv, Windows Job Object tree; live **#167** |
| Cross-host eval framework | **#168** | Synthesized tri-host evaluation report on main; disposition **MORE-DATA**; no ranking |
| Codex OAuth/Spark governance sync | **#190** | **CLOSED** on main — normative boundaries across README, CURRENT, Epic #80/#182, 3-axis mapping; ADR 006 accepted |
| Codex App Server runtime | **#184** | **CLOSED** on main — bounded stdio JSON-RPC 2.0 runtime bridge, job objects, 1MB limit |
| Dynamic model catalog | **#185** | **CLOSED** on main — entitlement-aware dynamic model discovery & snapshots |
| Dual-lane routing | **#186** | **CLOSED** on main — strict subscription vs direct API isolation, zero cross-lane fallback |
| Spark Pro live qualification | **#187** | **CLOSED** on main — pinned live evidence & qualification report, exact identity GO |
| Spark API profile | **#188** | **CLOSED** on main — capability-gated direct OpenAI API profile for entitled projects |
| Subscription churn test suite | **#189** | **CLOSED** on main — 6-dimension integration churn test suite passing 100% |

## Residual open work

### Epic #182: Dynamic Codex OAuth, App Server runtime, and Spark qualification

```text
Epic #182 (Codex OAuth / App Server / Spark Qualification)
├── #183 Delegated ChatGPT auth boundary (open / ready)
│    └──► #184 Codex App Server runtime (open / blocked by #183)
│          └──► #185 Dynamic current-account model catalog (open / blocked by #184)
│                └──► #186 Dual-lane subscription/API routing (open / blocked by #185)
│                      ├──► #187 GPT-5.3-Codex-Spark Pro qualification (open / blocked by #186 + env)
│                      └──► #189 Auth/catalog/model-churn suite (open / blocked by #186)
├── #188 Spark API profile (open / blocked by design-partner API entitlement)
└── #190 Sync OAuth/Spark support boundaries (CLOSED on main)
```

| Issue | Status | Blocker | Notes |
|---|---|---|---|
| **#183** | Open / Ready | None | Delegated ChatGPT auth boundary; host owns OAuth tokens; Reinframe never extracts/stores tokens |
| **#184** | Open | #183 | Codex App Server runtime; JSON-RPC stdio protocol for session lifecycle and turn synchronization |
| **#185** | Open | #184 | Dynamic current-account model catalog; account/scope-aware model discovery; no static OAuth inventory |
| **#186** | Open | #185 | Dual-lane subscription/API routing; explicit routing without silent credential/transport confusion |
| **#187** | Open | #186 + environment | GPT-5.3-Codex-Spark Pro qualification; ChatGPT Pro research preview; un-qualified until live evidence on main |
| **#188** | Open | Design-partner API entitlement | Spark API profile; separate opt-in API lane for actually entitled projects; not implied by ChatGPT Pro |
| **#189** | Open | #186 | Auth/catalog/model-churn test suite; synthetic/mocked catalog discovery, token expiration, and revocation churn |
| **#190** | **CLOSED** | None | Governance sync: 3-axis mapping, exact-model substitution, fail-closed fallback, ADR 006 on main |

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
| **#168** live match | #164/#120 matched data + Grok session reliability | Grok historical + v2 live pins present; #168 disposition **MORE-DATA**; no ranking; latest clean-quota pin `20260811T130935Z` re-eval **NO_GO** under executable-binding gate (older pins remain **NO_GO**); none of these strengthen ranking |

### Epics

| Issue | Notes |
|-------|-------|
| **#80** | Core external supervision epic; open until residual environment/product close criteria are met |
| **#182** | Dynamic Codex OAuth, App Server runtime, and Spark qualification epic; #190 closed on main |

## Explicit non-claims

- No ChatGPT OAuth token extraction, interception, storage, or proxying (Codex CLI / App Server owns auth lifecycle)
- No silent model fallback or substitution (missing models fail closed with `MODEL_UNAVAILABLE`)
- ChatGPT Pro subscription access to GPT-5.3-Codex-Spark does not grant OpenAI API access
- GPT-5.3-Codex-Spark is not live-qualified until #187 closes with fresh, un-contaminated evidence on main
- No static, authoritative inventory of all past/future OAuth models (dynamic runtime discovery only)
- Historical Grok live run present (#167 v1); latest clean-quota live v2 pin `20260811T130935Z` re-eval **NO_GO** under executable-binding gate (not a false LIMITED_GO); older pins `20260811T073954Z` / `20260810T170640Z` remain **NO_GO**; public ranking disposition remains **MORE_DATA** (#199 gates closed; no false GO)
- No fail-closed Grok hook security (host fail-open)
- CapToolGate ≠ CapPause / Level 2
- JSON-RPC / file append / hook exit ≠ explicit agent ACK
- xAI classifier ≠ Grok Build host
- No dual-host production supervision; no hard-gate; no default cache enablement
- No cross-host tunneling ranking without matched multi-host live evidence
- No Level 2 / CapPause from hooks alone; transport ≠ explicit ACK
- Latest pin `20260811T130935Z` keeps ACK at **transport** (session-matched updates without request/intervention/challenge identity do not upgrade); no product-wide explicit ACK / ranking upgrade
- No exactly-once delivery claim; no completed live #108 E2E composition path

## Evaluation artifacts

- `docs/evaluation/challenge_lane_a.md`
- `docs/evaluation/challenge_lane_b_model.md`
- `docs/evaluation/provider_cache_141.md`
- `docs/evaluation/cross_host_168.md`
- Adapter pins: `docs/classifier/*`, `docs/adapter/*`
- Codex OAuth/Spark specification: `docs/specs/codex_oauth_spark_boundary.md`
