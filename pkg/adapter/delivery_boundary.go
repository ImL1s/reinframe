package adapter

// Delivery boundary classification (#208): whether a durable-write failure after
// Actuator.Deliver may leave the host in an accepted-or-unknown state that must
// never auto-redeliver.
//
// Boundaries:
//   - not_sent: definitive pre-send / local rejection; host never accepted
//   - send_attempted_unknown: transport may have started; outcome unknown
//   - transport_accepted: host admitted transport (or terminal host-side result)
//   - session_visible: stronger ACK layer observed
//
// Prefer InterventionResult.DeliveryBoundary when set by the actuator (authoritative).
// Fallback ErrorClass heuristics remain for actuators that do not set the field.
const (
	BoundaryNotSent              = "not_sent"
	BoundarySendAttemptedUnknown = "send_attempted_unknown"
	BoundaryTransportAccepted    = "transport_accepted"
	BoundarySessionVisible       = "session_visible"
)

// ClassifyDeliveryBoundary derives the host-acceptance boundary from a Deliver result.
// Does not inspect durable/ledger errors — only the host delivery result.
func ClassifyDeliveryBoundary(res InterventionResult, deliverErr error) string {
	// Actuator-declared boundary wins (FileActuator pre-send, Grok SessionPrompt, …).
	switch res.DeliveryBoundary {
	case BoundaryNotSent, BoundarySendAttemptedUnknown, BoundaryTransportAccepted, BoundarySessionVisible:
		return res.DeliveryBoundary
	}

	switch res.AckLayer {
	case ACKLayerSessionVisible, ACKLayerExplicit, ACKLayerBehavioral:
		return BoundarySessionVisible
	case ACKLayerTransport:
		return BoundaryTransportAccepted
	}

	if res.Accepted {
		return BoundaryTransportAccepted
	}

	switch res.ErrorClass {
	case ErrorClassUnsupportedCapability:
		return BoundaryNotSent
	case ErrorClassAgentRejected:
		return BoundaryTransportAccepted
	case ErrorClassTimeout, ErrorClassTransport:
		// Heuristic only when actuator did not declare DeliveryBoundary.
		// Grok SessionPrompt failures set DeliveryBoundary=send_attempted_unknown.
		// FileActuator pre-send local I/O sets DeliveryBoundary=not_sent.
		return BoundarySendAttemptedUnknown
	}

	if deliverErr != nil {
		return BoundarySendAttemptedUnknown
	}
	return BoundaryNotSent
}

// ShouldAmbiguousSuppress reports whether a durable commit failure after Deliver
// must leave a restart-safe suppress key (host acceptance possible / unknown).
func ShouldAmbiguousSuppress(boundary string) bool {
	switch boundary {
	case BoundarySendAttemptedUnknown, BoundaryTransportAccepted, BoundarySessionVisible:
		return true
	default:
		return false
	}
}
