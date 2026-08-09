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
const (
	BoundaryNotSent              = "not_sent"
	BoundarySendAttemptedUnknown = "send_attempted_unknown"
	BoundaryTransportAccepted    = "transport_accepted"
	BoundarySessionVisible       = "session_visible"
)

// ClassifyDeliveryBoundary derives the host-acceptance boundary from a Deliver result.
// Does not inspect durable/ledger errors — only the host delivery result.
func ClassifyDeliveryBoundary(res InterventionResult, deliverErr error) string {
	switch res.AckLayer {
	case ACKLayerSessionVisible, ACKLayerExplicit, ACKLayerBehavioral:
		return BoundarySessionVisible
	case ACKLayerTransport:
		return BoundaryTransportAccepted
	}

	if res.Accepted {
		// Accepted with no stronger layer still means the host took the intervention.
		return BoundaryTransportAccepted
	}

	// Definitive local / capability rejections: never sent to host acceptance path.
	switch res.ErrorClass {
	case ErrorClassUnsupportedCapability:
		return BoundaryNotSent
	case ErrorClassAgentRejected:
		// Host received and rejected — terminal host-side outcome.
		return BoundaryTransportAccepted
	case ErrorClassTimeout, ErrorClassTransport:
		return BoundarySendAttemptedUnknown
	}

	// Accepted=false with no error class: treat as definitive not-sent.
	if deliverErr != nil && res.ErrorClass == ErrorClassNone && res.AckStatus == "" {
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
