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
//   - cache_control on last stable system block only; hosted platforms fail closed;
//   - CacheHit only from positive cache_read_input_tokens.
//
// Non-claims for this package revision: Gemini/xAI native adapters, hosted
// Anthropic equivalence (Bedrock/Vertex/Azure), exact-assessment memoization
// (#138), voting/failover, measured cache savings (#141), or hard-gate enforcement.
package classifier
