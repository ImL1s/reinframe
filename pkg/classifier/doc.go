// Package classifier implements Action Alignment wire types, shadow runtime (#105),
// and the provider-neutral Stage-1 runtime (#132).
//
// Contract: docs/specs/action_alignment_wire_contract.md (#119).
// Provider ADR: docs/adr/005-classifier-provider.md.
// Shadow mode: docs/classifier/shadow_mode.md — Enforced is always false.
//
// #132 adds:
//   - canonical ProviderRequest / ProviderResult on ClassifierProvider;
//   - deterministic PromptPlan (stable prefix / dynamic suffix);
//   - capability profiles (generic-none-v1 is cache-neutral);
//   - transport-only ProviderUsage;
//   - strict closed RawAssessment parser;
//   - generic OpenAI-compatible adapter (cache-neutral Chat Completions);
//   - provider-call audit without raw prompts or secrets.
//
// #134 adds:
//   - native OpenAI Responses adapter (openai_responses) with pinned profiles;
//   - provider-native prompt_cache_key / explicit stable-prefix breakpoint wiring;
//   - CacheHit only from positive provider-reported cache-read tokens.
//
// #135 adds:
//   - native Anthropic Messages adapter (anthropic_messages) for direct Claude API;
//   - versioned off / automatic / explicit_stable_prefix profiles with 5m|1h TTL;
//   - explicit: cache_control on last stable system block only (recommended);
//   - automatic-*: no wire cache enablement (Anthropic automatic would cache
//     through the dynamic last message — wrong for stable-prefix classify);
//   - hosted platforms fail closed; CacheHit only from positive cache_read tokens.
//
// #136 adds:
//   - native Gemini generateContent adapter (gemini_generate_content);
//   - implicit-cache-aware stable-prefix ordering; explicit cache objects deferred;
//   - model-profile min-token eligibility without padding; CacheHit from transport only.
//
// #137 adds:
//   - native xAI Responses adapter (xai_responses) with prompt_cache_key sticky routing;
//   - Chat Completions x-grok-conv-id is out of scope for this Responses profile;
//   - CacheHit only from positive provider-reported cached tokens (key match ≠ hit).
//
// Non-claims for this package revision: Gemini Interactions API, Gemini explicit cache
// object lifecycle, hosted Anthropic equivalence, exact-assessment memoization (#138),
// voting/failover, measured cache savings (#141), or hard-gate.
package classifier
