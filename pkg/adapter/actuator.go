package adapter

import (
	"context"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// InterventionActuator is the outbound control surface for delivering interventions
// to a Target Agent (advice, pause, cancel, tool-gate, etc.).
//
// Deliver MUST NOT panic. Unsupported capabilities return InterventionResult with
// ErrorClassUnsupportedCapability (and typically AckStatusUnsupported) rather than
// silently dropping without a structured result.
type InterventionActuator interface {
	Deliver(ctx context.Context, intervention protocol.Intervention) (InterventionResult, error)
}

// DefaultDeliveryMode maps a protocol.Intervention.ActionType to a DeliveryMode.
// Unknown action types fall back to advice.
func DefaultDeliveryMode(actionType string) string {
	switch actionType {
	case "ZOOM_OUT_PROMPT":
		return DeliveryModeAdvice
	case "PAUSE_PROCESS":
		return DeliveryModePause
	case "CANCEL_ACTION":
		return DeliveryModeCancel
	case "GIT_ROLLBACK", "TERMINATE_SESSION":
		return DeliveryModeHumanEscalation
	default:
		return DeliveryModeAdvice
	}
}
