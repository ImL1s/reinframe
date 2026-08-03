// Package supervisor wires the M2.0 control-plane composition root (#70).
//
// Flow:
//
//	AgentEvent → optional Store append → RepeatedFailureDetector → Policy slow path
//	  → PendingQueue / AdvisoryDelivery → InterventionActuator → ACK
//	PreTool → Policy fast path / HookGate → allow|deny|defer
//
// Concrete Claude Code / Codex adapters are out of scope; use adapter fakes
// and LogObserver for tests.
package supervisor
