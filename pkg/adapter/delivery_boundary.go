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
// Contract for GrokACPActuator:
//   - pre-send local rejects (missing client/session, empty body, privacy) use
//     ErrorClassUnsupportedCapability → not_sent
//   - SessionPrompt failure uses ErrorClassTransport + AckStatusRejected →
//     send_attempted_unknown (host may have accepted; suppress on durable fail)
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

	switch res.ErrorClass {
	case ErrorClassUnsupportedCapability:
		// Definitive local / capability rejection before any host send.
		return BoundaryNotSent
	case ErrorClassAgentRejected:
		// Host received and rejected — terminal host-side outcome.
		return BoundaryTransportAccepted
	case ErrorClassTimeout, ErrorClassTransport:
		// Send was attempted (or may have been). Grok SessionPrompt failures use
		// ErrorClassTransport + AckStatusRejected — that is NOT not_sent.
		return BoundarySendAttemptedUnknown
	}

	// Unclassified error with a deliver error: treat as send-attempted-unknown
	// rather than silently not_sent (safer for restart suppress).
	if deliverErr != nil {
		return BoundarySendAttemptedUnknown
	}
	// Accepted=false, no error class, no deliver error → definitive not-sent.
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
