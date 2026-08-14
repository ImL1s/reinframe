// Package policy implements fast-path and slow-path intervention policy (#69),
// and explicit dual-lane routing between delegated Codex subscription runtime
// and direct OpenAI Responses API (#186).
//
// Fast path: deterministic allow|deny|defer only (delegates to adapter.EvaluateHook).
// Never accepts or invokes a Reviewer.
//
// Slow path: maps detector signals to interventions. High-confidence
// repeated_error_loop produces ZOOM_OUT_PROMPT without a Reviewer. Uncertain
// branches may optionally call ReviewerProvider.
//
// Dual-lane routing: strictly isolates Lane A (Codex subscription OAuth / child process)
// from Lane B (OpenAI Responses API / environment API key) with zero silent fallback
// across billing boundaries.
package policy

