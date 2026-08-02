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
	ErrorClassNone                   = "none"
	ErrorClassUnsupportedCapability  = "unsupported_capability"
	ErrorClassTimeout                = "timeout"
	ErrorClassAgentRejected          = "agent_rejected"
	ErrorClassTransport              = "transport"
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
}

// DeliveryState is the pending-advisory lifecycle state (#68).
type DeliveryState string

const (
	StatePending     DeliveryState = "PENDING"
	StateDelivering  DeliveryState = "DELIVERING"
	StateAcked       DeliveryState = "ACKED"
	StateRejected    DeliveryState = "REJECTED"
	StateTimedOut    DeliveryState = "TIMED_OUT"
	StateExpired     DeliveryState = "EXPIRED"
	StateSuppressed  DeliveryState = "SUPPRESSED"
	StateFailed      DeliveryState = "FAILED"
)

// CapAdviceDelivery is the capability name required for automated advisory inject.
// Protocol bitmask may land with #65; adapter checks an explicit option until then.
const CapAdviceDelivery = "CapAdviceDelivery"
