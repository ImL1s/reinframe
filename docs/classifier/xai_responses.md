# Native xAI Responses classifier adapter (#137)

## Source pin

| Field | Value |
|-------|--------|
| Kind | `xai_responses` |
| API path | `/v1/responses` |
| Official references | https://docs.x.ai/developers/advanced-api-usage/prompt-caching · maximizing-cache-hits · usage-and-pricing |
| Retrieval date (UTC) | 2026-08-06 |

## Capability profiles

| Profile | CacheMode | Wire |
|---------|-----------|------|
| `xai-off-v1` | none | no `prompt_cache_key` |
| `xai-responses-prefix-v1` | implicit_prefix | secret-free `prompt_cache_key` (sticky routing) |

Stable policy/schema/examples first; dynamic task/events/action after. Key material: `xai_responses|model|profile|stable_prefix_hash|ruleset_hash|egress_profile` (SHA-256 truncated).

**Not sent:** Chat Completions-only `x-grok-conv-id` (distinct future profile if ever added).

## Usage

`CacheHit` is true only when provider reports **positive** cached tokens. A matching `prompt_cache_key` is **not** a hit guarantee (eviction/routing miss is tolerated).

## Non-claims

- Generic `openai_compatible` remains cache-neutral.
- OpenAI `openai_responses` is a separate kind (different host/routing semantics).
- No exact-assessment memoization (#138), no measured savings (#141).
