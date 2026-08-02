// Package adapter defines the bidirectional Target Agent control-plane contracts
// used by the Reinframe supervisor.
//
// #66 — inbound EventSource and outbound InterventionActuator interfaces, plus
// structured InterventionResult / error classes and in-memory fakes for tests.
//
// #67 — synchronous HookGate fast path (allow | deny | defer) with deterministic
// policy only; no Reviewer/LLM interfaces in the evaluation signature. Supports
// per-call timeout with fail-open or fail-closed behavior.
//
// #68 — pending advisory delivery queue with InterventionID dedupe, TTL expiry,
// Actuator delivery, ACK lifecycle states, and HumanAlerter degradation when
// CapAdviceDelivery is missing (observe-only sessions).
//
// Capability flags such as CapAdviceDelivery may land in pkg/protocol (#65).
// Until then, advisory delivery accepts an explicit SupportsAdviceDelivery option
// so observe-only degradation can be tested without protocol edits.
package adapter
