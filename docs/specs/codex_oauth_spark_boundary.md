# Codex OAuth, App Server Runtime, and GPT-5.3-Codex-Spark Support Boundaries (#190)

**Status:** Normative Specification (2026-08-15)  
**Parent:** Epic #80, Epic #182  
**Tracks issues:** #183, #184, #185, #186, #187, #188, #189, #190  

---

## 1. Overview & Problem Statement

OpenAI Codex supports two distinct authentication modalities:
1. **Direct OpenAI API key mode** (`OPENAI_API_KEY`) targeting platform endpoints (e.g. `/v1/responses`, `/v1/chat/completions`).
2. **Delegated ChatGPT subscription authentication** (OAuth) managed natively by the official Codex CLI and App Server runtime.

On 2026-02-12, OpenAI introduced **GPT-5.3-Codex-Spark** as an official **ChatGPT Pro Codex research preview** model designed for rapid agentic code editing and high-throughput iteration.

This specification establishes strict architectural and governance boundaries across Reinframe:
- **Zero Token Extraction:** Reinframe delegates OAuth token management entirely to the Codex runtime.
- **Zero Silent Substitution:** Reinframe rejects silent fallback across models or credentials.
- **Three-Axis Separation:** Host integration, credential class, and model qualification states are modeled as orthogonal axes.
- **Dual-Lane Routing:** ChatGPT Pro subscription access does not imply OpenAI API access; API lanes are strictly opt-in and entitlement-gated.
- **Evidence-Gated Qualification:** GPT-5.3-Codex-Spark is classified as planned and un-qualified until live evidence (#187) is recorded.

---

## 2. The Three Orthogonal Operational Axes

Reinframe models target agent integrations along three independent axes rather than conflating them into a single "supported" flag.

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
Defines the control and observability surface exposed by the Codex host:
- **`observe` (Level 0):** Passive JSONL rollout tailing and event ingestion (#95/#107/#118).
- **`hooks/tool gate` (Level 1):** PreTool / Permission interception via project-local `.codex/hooks.json` (#163; live proof #164).
- **`App Server session control` (Level 2/Level 3):** Bidirectional JSON-RPC stdio session management via `codex app-server` (#184).
- **`ACK levels`:** Clear distinction between transport write ACK, source-correlated session ACK, and explicit agent cognitive ACK.

### Axis 2: Credential and Transport Class
Defines the identity, authorization, and transport mechanism:
- **`ChatGPT subscription via Codex-owned auth`:** OAuth tokens issued to the user's ChatGPT subscription (Free, Plus, Pro, Team, Enterprise). Managed solely inside Codex CLI / App Server credential stores.
- **`Codex API-key mode`:** Codex CLI / App Server executing with an explicit OpenAI API key or custom endpoint.
- **`Direct OpenAI API key`:** Direct HTTPS requests made by Reinframe's internal `ClassifierProvider` (e.g. `/v1/responses` in #134) or `ReviewerProvider` (ADR 003).

### Axis 3: Model Support State
Defines the operational readiness of a specific model ID within Reinframe:
- **`discovered`:** The model identifier appears in the runtime catalog or announcement but has not been validated for current account scope.
- **`selectable`:** The model is verified available and actively selectable for the authenticated session without triggering a fallback.
- **`capability-pinned`:** The model's token limits, reasoning effort options, tool schemas, and prompt formats are documented in an immutable Reinframe profile.
- **`live-qualified`:** Reproducible live execution runs with verifiable evidence artifacts exist on `main` under uncontaminated quota.

---

## 3. Interface Separation: `ClassifierProvider` vs Codex Host Runtimes

Reinframe strictly isolates internal evaluation components from external agent host adapters:

| Dimension | `ClassifierProvider` (`pkg/classifier`) | Codex Host Adapters (`pkg/adapter`, `cmd/codex*`) |
|---|---|---|
| **Role** | Internal severity evaluator for Action Alignment | External coding agent supervisor and tool gate |
| **Transport** | Direct HTTPS API (`/v1/responses`, `/v1/chat/completions`) | CLI stdio, JSONL tail, hooks JSON-RPC, or App Server |
| **Credential** | Explicit API keys (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`) | Codex-owned OAuth session or Codex host config |
| **OAuth Scope** | **NEVER** routed through ChatGPT OAuth subscriptions | Utilizes Codex runtime's authenticated session |
| **Contract** | ADR 005 `Assess(ctx, ClassifierInput) (RawAssessment, error)` | HookGate, EventSource, App Server session controller |

---

## 4. Epic #182 Architecture & Issue Breakdown

```text
Epic #182: Dynamic Codex OAuth, App Server runtime, and Spark qualification
├── #183 Delegated ChatGPT auth boundary (open / ready)
│    └──► #184 Codex App Server runtime (open / blocked by #183)
│          └──► #185 Dynamic current-account model catalog (open / blocked by #184)
│                └──► #186 Dual-lane subscription/API routing (open / blocked by #185)
│                      ├──► #187 GPT-5.3-Codex-Spark Pro qualification (open / blocked by #186 + env)
│                      └──► #189 Auth/catalog/model-churn suite (open / blocked by #186)
├── #188 Spark API profile (open / blocked by design-partner API entitlement)
└── #190 [Governance] Sync OAuth/Spark support boundaries (CLOSED on main)
```

### #183: Delegated ChatGPT Auth Boundary
- Reinframe delegates all authentication lifecycle actions to the official Codex CLI (`codex login`, `codex logout`).
- Tokens are stored and refreshed in the host OS keyring / `~/.codex/` configuration by Codex.
- Reinframe **never** reads, extracts, parses, logs, or proxies raw OAuth tokens.

### #184: Codex App Server Runtime
- Integrates with the official JSON-RPC stdio protocol exposed by `codex app-server`.
- Provides programmatic session lifecycle control, turn boundary synchronization, and pre-tool evaluation hooks.

### #185: Dynamic Current-Account Model Catalog
- Queries model availability dynamically from the active Codex App Server session for the authenticated account scope.
- Rejects hardcoded static lists of OAuth models.
- Adapts gracefully to account tier differences (e.g. Pro vs Plus vs Team).

### #186: Dual-Lane Subscription/API Routing
- Ensures explicit separation between `subscription` and `api` routing lanes in configuration.
- Prevents cross-contamination of credentials, endpoints, or rate limits.

### #187: GPT-5.3-Codex-Spark Pro Qualification
- Tracks empirical qualification of `gpt-5.3-codex-spark` under ChatGPT Pro subscriptions.
- Requires live smoke run with valid session artifacts, zero silent substitution proof, and uncontaminated quota.

### #188: Spark API Profile (Opt-in Design Partner Lane)
- Dedicated opt-in profile for projects with direct OpenAI API access to Spark.
- Strictly gated on actual design-partner API entitlement; never assumed from ChatGPT Pro access.

### #189: Auth/Catalog/Model-Churn Test Suite
- Comprehensive suite testing dynamic catalog updates, token expiration, model deprecation, and scope revocation.

### #190: Governance & Capability Synchronization (This Issue)
- Pinned normative boundaries in `README.md`, `CURRENT.md`, ADRs, and research docs.

---

## 5. Model Selection, Substitution, and Fallback Policy

1. **Zero Silent Substitution:**
   Reinframe supports a Codex subscription model only when:
   - The current official Codex runtime exposes it for the authenticated scope.
   - Exact selection is proven without silent substitution.
   - Required capabilities are pinned in an immutable profile.
   - The profile has qualifying evidence.
   Reinframe does not maintain an authoritative list of all future OAuth models.

2. **Deterministic Fail-Closed Fallback:**
   If a requested model (e.g. `gpt-5.3-codex-spark`) is missing from the active catalog or un-selectable, Reinframe **must not** downgrade to `gpt-5-codex`, `gpt-4o`, or any other model. Reinframe must emit a deterministic `MODEL_UNAVAILABLE` error and halt session initialization.

---

## 6. Quota Modeling vs API Usage

- **ChatGPT Pro Quota:** Governed by research preview rate limits, weekly query caps, and subscription availability. Quota exhaustion returns specific host-level rate limit responses.
- **OpenAI API Usage:** Governed by pay-as-you-go credit balance, TPM/RPM limits, and organization tiers.
- Reinframe models quota states independently and does **not** attempt to bypass or tunnel across rate limits.

---

## 7. Authoritative References & Retrieval Dates

- **Launch Announcement:** [Introducing GPT-5.3-Codex-Spark](https://openai.com/index/introducing-gpt-5-3-codex-spark/) (Published: 2026-02-12)
- **Codex Authentication:** `https://developers.openai.com/codex/auth` (Retrieved: 2026-08-15)
- **Codex Models & Capabilities:** `https://developers.openai.com/codex/models` (Retrieved: 2026-08-15)
- **Codex App Server Specification:** `https://developers.openai.com/codex/app-server` (Retrieved: 2026-08-15)
- **Codex SDK Documentation:** `https://developers.openai.com/codex/sdk` (Retrieved: 2026-08-15)
