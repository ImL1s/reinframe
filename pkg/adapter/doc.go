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
//
// #84 — TaskSubmitted intake mappers (host → core). Claude UserPromptSubmit is a
// fixture/host mapping only; see docs/adapter/task_intake_mapping.md.
//
// #95 scaffold — CodexRolloutSource (offline JSONL) and CodexTailSource (near-live
// poll follow). Not a process-control daemon.
//
// #96 — Claude PreTool / prompt bridge (MapClaudePreToolUseJSON, EvaluateClaudePreTool,
// cmd/claudebridge). Experimental; host settings install is documented only.
//
// #97 — FileActuator: non-fake JSONL advice channel with pending ACK (no AutoAck theater).
//
// #115 — ProposedAction versioned projection (ToolName ≠ Command); Claude mapper fills
// ClaudePreToolInput.Proposed; policy over-SOP uses FullSuiteCommand on Command.
package adapter
