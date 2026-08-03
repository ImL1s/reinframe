// Package policy implements fast-path and slow-path intervention policy (#69).
//
// Fast path: deterministic allow|deny|defer only (delegates to adapter.EvaluateHook).
// Never accepts or invokes a Reviewer.
//
// Slow path: maps detector signals to interventions. High-confidence
// repeated_error_loop produces ZOOM_OUT_PROMPT without a Reviewer. Uncertain
// branches may optionally call ReviewerProvider.
package policy
