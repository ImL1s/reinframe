// Package reviewer defines the Reviewer Provider SDK used on the policy slow path.
//
// Providers:
//   - FakeProvider — tests
//   - OpenAICompatibleProvider — OpenAI-compatible chat/completions → ReviewDecision
//
// NewProviderFromConfig maps config.Reviewer (+ session.local_only_reviewer) to a
// provider (ADR 003: remote modes blocked until local_only_reviewer=false).
//
// Cost rule: high-confidence ZOOM_OUT never calls Generate; only uncertain paths do.
package reviewer
