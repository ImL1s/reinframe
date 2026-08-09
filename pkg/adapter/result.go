package adapter

import "time"

// DeliveryMode identifies how an intervention was (or would be) delivered.
const (
	DeliveryModeAdvice          = "advice"
	DeliveryModeContextInject   = "context_inject"
	DeliveryModeToolGate        = "tool_gate"
	DeliveryModePause           = "pause"
	DeliveryModeCancel          = "cancel"
	DeliveryModeMCPPending      = "mcp_pending"
	DeliveryModeHumanEscalation = "human_escalation"
)

// AckStatus is the acknowledgement state reported by Deliver / advisory lifecycle.
const (
	AckStatusPending     = "pending"
	AckStatusAcked       = "acked"
	AckStatusRejected    = "rejected"
	AckStatusTimedOut    = "timed_out"
	AckStatusUnsupported = "unsupported"
)

// ErrorClass classifies Deliver failures without panicking.
const (
	ErrorClassNone                  = "none"
	ErrorClassUnsupportedCapability = "unsupported_capability"
	ErrorClassTimeout               = "timeout"
	ErrorClassAgentRejected         = "agent_rejected"
	ErrorClassTransport             = "transport"
)

// InterventionResult is the structured outcome of InterventionActuator.Deliver.
type InterventionResult struct {
	InterventionID string
	Accepted       bool
	DeliveryMode   string
	DeliveredAt    time.Time
	AckStatus      string
	AckAt          *time.Time
	ErrorClass     string
	Message        string
	// AckLayer is the strongest proven host ACK layer (#108/#167 honesty).
	// transport | session_visible | explicit | behavioral | none — never invent explicit from transport.
	AckLayer string
	// HostFamily pins the live host (e.g. grok_build); never transfer proofs across hosts.
	HostFamily string
	// HostVersion is the full CLI/version string when known.
	HostVersion string
	// Profile is the closed capability profile id (e.g. reinframe.grok_build_acp.v1).
	Profile string
	// SafeBoundary is the delivery boundary (next_input, before_tool, …).
	SafeBoundary string
	// TargetSessionID is the host session identity used for delivery.
	TargetSessionID string
	// CapsDigest is a short hash/digest of negotiated capabilities when available.
	CapsDigest string
	// DeliveryBoundary is the actuator-declared host-acceptance boundary (#208).
	// When non-empty it is authoritative for durable-fail suppress decisions:
	// not_sent | send_attempted_unknown | transport_accepted | session_visible.
	DeliveryBoundary string
}

// DeliveryState is the pending-advisory lifecycle state (#68 + #108 extensions).
type DeliveryState string

const (
	StatePending           DeliveryState = "PENDING"
	StateDelivering        DeliveryState = "DELIVERING"
	StateTransportAccepted DeliveryState = "TRANSPORT_ACCEPTED"
	StateSessionVisible    DeliveryState = "SESSION_VISIBLE"
	StateExplicitACK       DeliveryState = "EXPLICIT_ACK"
	StateBehavioralACK     DeliveryState = "BEHAVIORAL_ACK"
	StateAcked             DeliveryState = "ACKED" // legacy alias for EXPLICIT/final acked
	StateRejected          DeliveryState = "REJECTED"
	StateTimedOut          DeliveryState = "TIMED_OUT"
	StateExpired           DeliveryState = "EXPIRED"
	StateSuppressed        DeliveryState = "SUPPRESSED"
	StateFailed            DeliveryState = "FAILED"
	StateUnsupported       DeliveryState = "UNSUPPORTED"
	// StateAmbiguous: host may have accepted but durable commit failed (#200).
	// Must not auto-redeliver.
	StateAmbiguous DeliveryState = "AMBIGUOUS"
)

// CapAdviceDelivery is the capability name required for automated advisory inject.
// Protocol bitmask may land with #65; adapter checks an explicit option until then.
const CapAdviceDelivery = "CapAdviceDelivery"
