# ADR 003: Provider Egress Data Classification & Local-Only Reviewer Mode

- **Status**: Decided & Approved
- **Date**: 2026-08-02
- **Deciders**: Architecture Team
- **Technical Story**: Issue #50

---

## Context and Problem Statement
Reinframe sends evidence packs, prompts, and session context to Reviewer Providers (local models, OpenAI-compatible APIs, or other cloud endpoints). Those payloads may contain:

- Secrets (API keys, tokens, passwords) from agent tool output or workspace files
- PII or customer data embedded in logs and diffs
- Proprietary source that must not leave the developer machine by default

Without an explicit egress policy, a future cloud reviewer integration could leak raw secrets over the network.

---

## Decision Drivers
- Default-safe posture for M1/M2 stubs before cloud providers ship.
- Align with protocol field `redact` tags already present on sensitive schema fields.
- Allow optional cloud review when the operator explicitly opts in and redaction is applied.
- Keep secret material out of config files (env placeholders only — see also `pkg/config`).

---

## Considered Options

### Option A: Always allow full-fidelity egress to any configured provider
- **Pros**: Maximum reviewer signal quality.
- **Cons**: High leak risk; unacceptable default for a supervisor that watches agent terminals and files.

### Option B: Local-only reviewer mode as default; classify + redact before any cloud egress (Selected)
- **Pros**: Safe default; cloud path remains available under explicit configuration and redaction gates.
- **Cons**: Local models may be weaker; operators must opt into cloud.

### Option C: Block all network reviewer calls permanently
- **Pros**: Strongest isolation.
- **Cons**: Blocks planned OpenAI-compatible / cloud reviewer tracks (#18–#19).

---

## Decision Outcome
**Selected Option**: **Option B**.

### Normative rules
1. **Default reviewer mode is local-only** (`reviewer.mode = local` / `session.local_only_reviewer = true` in config). Cloud or remote OpenAI-compatible endpoints are opt-in.
2. **Data classification** for egress payloads:
   - **Secret**: credentials, tokens, private keys, session cookies — must never leave the host in raw form.
   - **Sensitive**: prompts, rationales, advice text, large file diffs — redact or truncate before cloud egress.
   - **Operational**: IDs, scores, classifications, timestamps, token counts — may egress when cloud mode is enabled.
3. **No raw secrets to cloud without redaction**: any non-local `ReviewerProvider` implementation MUST run a redaction pass over `ReviewRequest.Prompt` (and attached evidence summaries) before network send. Secret-class matches are replaced with placeholders (e.g. `[REDACTED:secret]`).
4. **Config must not embed secret values**: API keys and similar credentials are referenced only as environment placeholders (e.g. `${REINFRAME_REVIEWER_API_KEY}`), never as literal strings in YAML/JSON config.
5. **Audit**: provider usage and egress mode (local vs remote) SHOULD be recorded via `ProviderUsage` / audit events when those paths land.

### Consequences
- **Positive**: Safe defaults; matches existing `redact` annotations on protocol types; clear gate for #17–#19 provider implementations.
- **Negative**: Cloud reviewers require explicit operator configuration and a shared redaction helper.
- **Mitigation**: Ship `FakeProvider` and local stubs first; redaction helper can land with the first remote provider PR.
