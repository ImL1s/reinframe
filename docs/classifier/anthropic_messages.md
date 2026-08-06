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

| Profile | CacheMode | Wire `cache_control` | TTL |
|---------|-----------|----------------------|-----|
| `anthropic-off-v1` | none | none | — |
| `anthropic-automatic-5m-v1` | implicit_prefix | none (provider may still cache) | 5m (documented intent) |
| `anthropic-automatic-1h-v1` | implicit_prefix | none | 1h (documented intent) |
| `anthropic-explicit-prefix-5m-v1` | explicit_breakpoint | last **stable** system text block: `{"type":"ephemeral","ttl":"5m"}` | 5m |
| `anthropic-explicit-prefix-1h-v1` | explicit_breakpoint | last stable system text: `ttl=1h` | 1h |

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
