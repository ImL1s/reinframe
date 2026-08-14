# ADR 006: Delegated Codex OAuth, App Server Runtime, and Spark Model Support Boundaries

**Status:** Accepted (2026-08-15)  
**Parent:** Epic #80, Epic #182  
**Issues:** #183, #184, #185, #186, #187, #188, #189, #190  

## Context

OpenAI Codex provides multiple authentication and runtime surfaces:
1. Direct OpenAI API keys (`OPENAI_API_KEY`) targeting public API endpoints (e.g., `/v1/responses`, `/v1/chat/completions`).
2. Delegated ChatGPT OAuth subscription authentication managed natively by the Codex CLI / App Server runtime.
3. Advanced model previews such as **GPT-5.3-Codex-Spark**, launched on 2026-02-12 as an official ChatGPT Pro Codex research preview.

Previous Reinframe documentation had separate classifier provider adapters (#134) and observe-only/hook adapters (#107, #163), but did not formally codify the architectural boundaries between direct API credentials, delegated ChatGPT subscription authentication, App Server session control, and model preview qualification.

Without clear architectural boundaries, there is a risk of:
- Conflating `ClassifierProvider` (Reinframe's internal severity evaluator) with Codex host execution runtimes.
- Extracting or proxying OAuth access tokens directly (violating security boundaries and OpenAI terms).
- Silent model fallback when a requested model is unavailable in the authenticated account scope.
- Assuming ChatGPT Pro subscription access implies OpenAI API access (or vice versa).
- Treating un-qualified research preview models as supported without reproducible evidence.

## Decision

1. **Delegated Auth Ownership (#183):**
   - The host runtime (Codex CLI / Codex App Server) exclusively owns ChatGPT OAuth login, token persistence in local keyrings/config, and token refresh.
   - Reinframe operates strictly as an external supervisor and **never** extracts, intercepts, stores, or proxies ChatGPT OAuth tokens directly.

2. **Decoupled ClassifierProvider vs Codex Host Interfaces:**
   - `ClassifierProvider` (in `pkg/classifier`) is an internal severity assessment interface using explicit provider API keys (e.g. native OpenAI `/v1/responses` adapter in #134).
   - Codex host adapters (in `pkg/adapter`, `cmd/codexctl`, `cmd/codexhooks`, and App Server client in #184) supervise external agent sessions and tool gates.
   - Classifier providers must never be routed through subscription OAuth transports.

3. **Dynamic Current-Account Model Discovery (#185):**
   - Reinframe does **not** hardcode a static, authoritative list of all past, present, or future OAuth models.
   - Available models are discovered dynamically at runtime via the official Codex App Server / runtime catalog for the authenticated user session.
   - Models are classified into four explicit lifecycle states: `discovered`, `selectable`, `capability-pinned`, and `live-qualified`.

4. **Zero Silent Model Substitution & Fail-Closed Fallback Policy:**
   - Exact-model selection is mandatory. If a configuration specifies a model (such as `gpt-5.3-codex-spark` or `gpt-5.3-codex`), Reinframe will **never** silently substitute an alternative model.
   - If the requested model is not available in the authenticated scope or lacks live qualification, Reinframe fails closed with a deterministic error (`MODEL_UNAVAILABLE` / `BLOCKED_BY_CATALOG`).

5. **Dual-Lane Subscription vs API Separation (#186, #188):**
   - ChatGPT Pro subscription quota (Codex research preview limits) and OpenAI API tokens/credits are distinct lanes.
   - ChatGPT Pro Spark access does **not** grant or imply OpenAI API access.
   - Issue #188 defines a separate, opt-in API lane for design partners with direct API entitlement. Rate limits and availability differences are modeled explicitly, never bypassed.

6. **Spark Research Preview Qualification (#187):**
   - GPT-5.3-Codex-Spark support is formally tracked as *planned / not yet qualified* until live qualification (#187) closes with reproducible evidence runs under non-contaminated quota.

## Consequences

- Reinframe maintains strict compliance with OpenAI authentication architectures and terms.
- Clear separation between the 3 orthogonal operational axes (Host Integration Capability, Credential/Transport Class, Model Support State).
- No false claims of GPT-5.3-Codex-Spark support or universal OAuth model compatibility without empirical evidence.

## References

- Official Announcement: [Introducing GPT-5.3-Codex-Spark](https://openai.com/index/introducing-gpt-5-3-codex-spark/) (2026-02-12)
- Codex Auth: `https://developers.openai.com/codex/auth` (retrieved 2026-08-15)
- Codex Models: `https://developers.openai.com/codex/models` (retrieved 2026-08-15)
- Codex App Server: `https://developers.openai.com/codex/app-server` (retrieved 2026-08-15)
- Codex SDK: `https://developers.openai.com/codex/sdk` (retrieved 2026-08-15)
