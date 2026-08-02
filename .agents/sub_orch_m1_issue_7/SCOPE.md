# Scope: Milestone 1 — Issue #7 Capability Manifest & Handshake Protocol

## Mission
Execute Milestone 1: Implement Capability Manifest, 20 capability flags, Handshake Negotiation Engine supporting Level 0-3 with automatic degradation, unit tests, and open PR on branch `issue-7-capability-manifest-negotiation`.

## Requirements
1. Create/manage branch `issue-7-capability-manifest-negotiation`.
2. Implement `pkg/protocol/capability.go`:
   - Define 20 `CapabilityFlag uint64` constants in 4 categories.
   - `CapabilityManifest` helpers (`ToBitmask()`, `FromBitmask()`, `HasCapability()`).
   - `EvaluateAchievableLevel()` calculating maximum achievable supervision level (Level 0-3).
   - `NegotiateLevel(req *HandshakeRequest) (*HandshakeResponse, error)` with automatic degradation (`IsDegraded: true`, `DegradedFrom`, `MissingFlags`).
3. Implement `pkg/protocol/capability_test.go`:
   - Unit tests for bitmask conversion, level calculation, automatic degradation, nil/invalid inputs.
   - Concurrency & race detector verification (`go test -v -race ./pkg/protocol/...`).
4. Iteration loop: Explorer -> Worker -> Reviewer -> Challenger -> Auditor -> Gate.
5. Create Pull Request for Issue #7 on branch `issue-7-capability-manifest-negotiation`.

## Interface Contracts
`pkg/protocol/capability.go` signatures specified in `PROJECT.md`.
