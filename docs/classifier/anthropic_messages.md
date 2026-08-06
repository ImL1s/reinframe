# Native Anthropic Messages classifier adapter (#135)

## Source pin

| Field | Value |
|-------|--------|
| Kind | `anthropic_messages` |
| API path | `/v1/messages` |
| Platform | `claude_api` only (direct Claude API) |
| Official reference | https://platform.claude.com/docs/en/build-with-claude/prompt-caching |
| Retrieval date (UTC) | 2026-08-06 |
| anthropic-version header | `2023-06-01` |

## Capability profiles

| Profile | CacheMode | Wire content-block `cache_control` | Notes |
|---------|-----------|--------------------------------------|-------|
| `anthropic-off-v1` | none | none | No Anthropic cache enablement. |
| `anthropic-automatic-5m-v1` | implicit_prefix | **none** (wire-identical to off for breakpoints) | **Not** Anthropic top-level automatic caching. Classifier single-shot calls should use **explicit** stable-prefix profiles; automatic-* reserves config identity / audit TTL metadata only and does not claim wire enablement. |
| `anthropic-automatic-1h-v1` | implicit_prefix | **none** (same honesty as 5m) | Same as above with 1h audit TTL identity. |
| `anthropic-explicit-prefix-5m-v1` | explicit_breakpoint | last **stable** system text: `{"type":"ephemeral","ttl":"5m"}` | **Recommended** production cache mode for this adapter. |
| `anthropic-explicit-prefix-1h-v1` | explicit_breakpoint | last stable system text: `ttl=1h` | Same with 1h TTL. |

**Why automatic has no wire fields:** Anthropic's documented automatic/top-level caching breakpoints through the **last message**, which for a classify request is the dynamic task/events suffix — the opposite of Reinframe's stable-prefix design. This adapter therefore **does not** send top-level automatic `cache_control`. Prefer `anthropic-explicit-prefix-*`.

Dynamic task/action/events/challenge blocks never receive `cache_control`.

Optional `egress_profile` (secret-free) is mixed into the audit `CacheKeyHash` material only — Anthropic has no request-level cache key field.

## Structured output

Forced tool use: `reinframe_raw_assessment` with closed JSON schema matching `RawAssessment`. Usage/cache metadata is read only from transport `usage` fields.

## Usage normalization

| Provider field | ProviderUsage |
|----------------|---------------|
| `input_tokens` | `UncachedInputTokens` |
| `cache_read_input_tokens` | `CacheReadTokens` |
| `cache_creation_input_tokens` | `CacheWriteTokens` |
| sum of the three | `InputTokens` (logical total) |
| `output_tokens` | `OutputTokens` |

`CacheHit` is true only when `UsagePresent && CacheReadTokens > 0`.

## Non-claims

- No Amazon Bedrock / Vertex AI / Microsoft Foundry equivalence — unsupported `platform` fails config validation.
- No prompt padding to meet minimum cacheable token thresholds.
- No pre-warm by default.
- No Reinframe exact-assessment memoization (see #138).
- No measured cost/token savings (see #141).
- No OpenAI/Gemini/xAI support in this package path.
