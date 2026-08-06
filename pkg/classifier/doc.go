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
//   - generic OpenAI-compatible adapter (not native OpenAI/Anthropic/Gemini/xAI);
//   - provider-call audit without raw prompts or secrets.
//
// Non-claims for this package revision: native vendor adapters, provider-native
// prompt caching, exact-assessment memoization, singleflight, voting/failover,
// or calibrated hard-gate enforcement.
package classifier
