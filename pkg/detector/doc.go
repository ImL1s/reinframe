// Package detector implements deterministic anomaly detectors for Reinframe.
//
// #82 — Minimal RepeatedFailureDetector: counts identical normalized failure
// fingerprints per session and emits a TunnelSignal when the count reaches a
// configurable threshold (default N=3). No Reviewer or LLM is used.
//
// #85 — VerificationChurnDetector: redundant successful re-validation without
// information gain (exemptions for flaky / policy / high-risk / workspace change).
//
// #98 — ToolBudgetChurnDetector and HypothesisLoopDetector for long review
// sessions: tool usage exceeding budget without progress, and repeated
// conclusion fingerprints without new evidence IDs. Library signals only;
// thresholds are provisional (not M3 calibrated hard-gates). Does not attach
// to live Codex/Claude hosts.
package detector
