// Package detector implements deterministic anomaly detectors for Reinframe.
//
// #82 — Minimal RepeatedFailureDetector: counts identical normalized failure
// fingerprints per session and emits a TunnelSignal when the count reaches a
// configurable threshold (default N=3). No Reviewer or LLM is used.
package detector
