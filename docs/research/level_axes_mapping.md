# Level Axes Mapping: Integration, Intervention, and Support Boundaries

**Status:** Normative planning and governance document (Aligned: 2026-08-15).  
**Problem fixed:** Prevents conflating Integration Level (A0–A3), Intervention Level (B0–B3), Credential Classes, and Model Support States, eliminating false hard-gates and unsafe silent fallbacks.

---

## 1. Core Distinction: Integration vs Intervention

| Axis | Name | Question it answers | Primary sources |
|---|---|---|---|
| **A. Integration Level** | Capability / handshake level | *What control surface does this Target Agent expose to Reinframe?* | `harness_capability_matrix.md`, `pkg/protocol/capability.go`, #7, #65 |
| **B. Intervention Level** | Escalation ladder after detection | *How aggressively may the supervisor intervene on this session?* | `anti_tunnel_threat_model.md` §4, session SM, #32, #69, #70 |

They **share numeric labels 0–3 by historical coincidence**. They are **not** the same negotiated integer unless a product rule explicitly binds them.

---

## 2. Axis A — Integration Level (handshake)

Negotiated at session start from `CapabilityManifest` flags.

| Level | Intent | Code mask today | Delivery note (#65 / #72) |
|---|---|---|---|
| **0 Observe** | Passive observation | `CapEventStream` | No advice required |
| **1 Advisory** | Can advise the agent | + `CapToolInspection` + **`CapAdviceDelivery`** | L1 request without `CapAdviceDelivery` degrades to level < 1 |
| **2 Guarded** | Can pause/cancel/gate tools | + Diff + **native** Pause + Cancel + Resume | **`CapPause` = harness-native pause only; OS SIGSTOP is NOT CapPause** (#72 option 1) |
| **3 Full-control** | Checkpoint/rollback/headless/… | L2 + Checkpoint/Rollback/Headless/CLI/MCP/Subagents/SwitchModel | Full actuator surface |

**Research tension (documented, not silently fixed in code):**  
Research L0 text emphasizes log/git/diff tailing; code L0 only requires `CapEventStream`. Treat log/git as **implementation strategies for L0 adapters**, not extra required flags, unless #7 is amended.

---

## 3. Axis B — Intervention Escalation Ladder

Applied **after** detectors/reviewers (or fast-path safety rules). May be **capped** by Axis A.

| Intervention step | Typical action | Min Integration Level usually needed | Notes |
|---|---|---|---|
| **B0 Observe** | Append events to Store / audit | A0 | Always available if events exist |
| **B1 Advisory** | `ZOOM_OUT_PROMPT` / replan advice + ACK path | A1 + CapAdviceDelivery | If A0 only → **human alert** degradation (#68) |
| **B2 Guarded** | Pause / tool defer-deny / require experiment | A2 (CapPause or tool gate) | Fast path may `defer` without full pause (#67) |
| **B3 Full-control** | Git rollback, terminate, switch model, escalate human | A3 for model-switch; A2+workspace for rollback | `action_type` includes `SWITCH_MODEL` / `ESCALATE_TO_HUMAN` (#57) |

---

## 4. The Three Orthogonal Operational Axes for Target Integrations

Beyond the general A/B level mapping, target agent integrations (such as OpenAI Codex and Claude Code) must be evaluated across **three orthogonal operational axes**:

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

### Axis 1: Host Integration Capability
- **`observe` (Level 0):** Passive JSONL rollout/event ingestion (e.g. Codex EventSource #107/#118).
- **`hooks/tool gate` (Level 1):** PreTool / Permission interception (e.g. `.codex/hooks.json` #163; live #164).
- **`App Server session control` (Level 2/3):** Programmatic session and turn management via JSON-RPC stdio (e.g. `codex app-server` #184).
- **`ACK levels`:** Transport write ACK vs source-correlated session ACK vs cognitive agent ACK.

### Axis 2: Credential and Transport Class
- **`ChatGPT subscription via Codex-owned auth`:** OAuth session tokens managed entirely within the Codex CLI / App Server host keyring. Reinframe never extracts, handles, or proxies raw OAuth tokens.
- **`Codex API-key mode`:** Codex host executing using an explicit `OPENAI_API_KEY` or custom platform endpoint.
- **`Direct OpenAI API key`:** Direct HTTPS API requests used by Reinframe's internal `ClassifierProvider` (ADR 005 / #134) or `ReviewerProvider` (ADR 003).

### Axis 3: Model Support State
- **`discovered`:** Model identifier seen in runtime discovery or announcements.
- **`selectable`:** Model verified available for the active account tier and scope without silent fallback.
- **`capability-pinned`:** Model capabilities (reasoning effort, context, tools) pinned in an immutable Reinframe profile.
- **`live-qualified`:** Verified by reproducible live test runs on `main` under uncontaminated quota.

---

## 5. Architectural Invariants & Governance Policies

### 5.1 ClassifierProvider vs Codex Host / Runtime Interfaces
- `ClassifierProvider` (`pkg/classifier/`) is Reinframe's internal severity evaluator for Action Alignment. It connects directly to model provider APIs using direct API keys.
- Codex Host Adapters (`pkg/adapter/`, `cmd/codexctl`, `cmd/codexhooks`, and App Server client) supervise external coding agents.
- **Invariant:** Subscription OAuth authentication must **never** be used for `ClassifierProvider` calls.

### 5.2 Credential Ownership & Zero-Extraction Policy
- The host runtime (Codex CLI / App Server) exclusively owns user authentication (`codex login`), token refresh, and keyring persistence.
- Reinframe **never** reads, extracts, parses, logs, or proxies raw OAuth tokens.

### 5.3 Zero Silent Model Substitution & Fallback Policy
- Reinframe supports a Codex subscription model only when the current official Codex runtime exposes it for the authenticated scope, exact selection is proven without silent substitution, required capabilities are pinned, and the profile has qualifying evidence. Reinframe does not maintain an authoritative list of all future OAuth models.
- If a requested model (e.g. `gpt-5.3-codex-spark` or `gpt-5.3-codex`) is unavailable in the current catalog, Reinframe **fails closed** (`MODEL_UNAVAILABLE` / `BLOCKED_BY_CATALOG`) and halts session initialization rather than silently falling back to another model.

### 5.4 Quota vs API Billing & Rate Limits
- **ChatGPT Pro Quota:** Governed by research preview limits, weekly caps, and subscription availability. Accessing GPT-5.3-Codex-Spark in ChatGPT Pro does **not** grant or imply OpenAI API access.
- **OpenAI API Entitlement:** Governed by pay-as-you-go credit balance and API rate limits. Issue #188 defines a separate, opt-in API lane for design partners with actual API entitlement.
- Rate limits and availability differences are modeled explicitly, never bypassed.

### 5.5 Evidence Freshness & Revalidation
- Qualifications are tied to specific runtime versions, date pins, and live run artifacts.
- Unverified models or expired research previews remain un-qualified until fresh evidence is captured.

---

## 6. Crosswalk Table (Planning)

| If harness matrix suggests… | Default Integration plan | Default max Intervention step without extra product work |
|---|---|---|
| Observe-only IDE | A0 | B0 + human notify |
| Stdio / hooks, no pause | A1 | B1 advisory (+ PreTool defer if CapToolGate) |
| Native pause/cancel / App Server | A2 | B2 guarded |
| Graph/SDK full control | A3 | B3 full-control |

---

## 7. Authoritative References & Retrieval Dates

- [Introducing GPT-5.3-Codex-Spark](https://openai.com/index/introducing-gpt-5-3-codex-spark/) (Published: 2026-02-12)
- Codex Authentication: `https://developers.openai.com/codex/auth` (Retrieved: 2026-08-15)
- Codex Models: `https://developers.openai.com/codex/models` (Retrieved: 2026-08-15)
- Codex App Server: `https://developers.openai.com/codex/app-server` (Retrieved: 2026-08-15)
- Codex SDK: `https://developers.openai.com/codex/sdk` (Retrieved: 2026-08-15)

---

## 8. Related Issues

- #7 capability handshake  
- #49 research matrix refresh  
- #65 delivery capabilities  
- #72 CapPause semantics (SIGSTOP vs native)  
- #73 dual-axis docs/ADR acceptance in architecture set  
- **Epic #80** core M2 supervision epic  
- **Epic #182** Dynamic Codex OAuth, App Server runtime, and Spark qualification  
- #183 Delegated ChatGPT auth boundary  
- #184 Codex App Server runtime  
- #185 Dynamic current-account model catalog  
- #186 Dual-lane subscription/API routing  
- #187 GPT-5.3-Codex-Spark Pro qualification  
- #188 Spark API profile (opt-in API lane)  
- #189 Auth/catalog/model-churn suite  
- #190 Sync OAuth/Spark support boundaries  
