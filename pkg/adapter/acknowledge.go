package adapter

import (
	"errors"
	"fmt"
	"time"
)

// AcknowledgeRequest is a closed, source-bound ACK/reject/timeout (#200).
// Bare status strings cannot mint explicit ACK.
type AcknowledgeRequest struct {
	SchemaVersion  string    // reinframe.ack_request.v1
	InterventionID string    // required
	HostFamily     string    // e.g. grok_build
	HostVersion    string    // optional pin
	Profile        string    // e.g. reinframe.grok_build_acp.v1
	TargetSession  string    // ACP/host session identity
	SourceKind     string    // required: host_event | operator | test
	SourceEventID  string    // required non-empty correlation from host/test
	CorrelationID  string    // optional additional causation id
	AckLayer       string    // requested layer; must not exceed profile ceiling
	Status         string    // acked | rejected | timed_out
	ObservedAt     time.Time // optional; defaults to now
}

const AcknowledgeRequestSchemaV1 = "reinframe.ack_request.v1"

// ErrBareAcknowledgeExplicit is returned when Acknowledge(id,"acked") is used.
// Explicit ACK requires AcknowledgeSource with a proven source.
var ErrBareAcknowledgeExplicit = errors.New("bare Acknowledge cannot mint explicit ACK; use AcknowledgeSource")

// ErrExplicitACKNotSupported is returned when a profile forbids explicit ACK.
var ErrExplicitACKNotSupported = errors.New("explicit ACK not supported for this host profile")

// ErrSourceBoundRequired is returned when source identity is missing.
var ErrSourceBoundRequired = errors.New("source-bound ACK requires SourceKind and SourceEventID")

// ErrDurableWriteFailed is returned when the host accepted delivery but ledger commit failed.
var ErrDurableWriteFailed = errors.New("durable ledger write failed after host delivery")

// ProfileMaxACKLayer returns the strongest ACK layer a host profile may claim.
func ProfileMaxACKLayer(hostFamily, profile string) string {
	// Current Grok Build live profile ceiling is session_visible (#167/#199/#200).
	if hostFamily == GrokLiveHostFamily || profile == GrokACPProfileV1 || profile == GrokLiveAdviceProfile {
		return ACKLayerSessionVisible
	}
	// Unknown profiles: do not allow explicit by default.
	return ACKLayerSessionVisible
}

// ValidateAcknowledgeRequest checks closed request shape and profile ceiling.
func ValidateAcknowledgeRequest(req AcknowledgeRequest) error {
	if req.InterventionID == "" {
		return fmt.Errorf("intervention id required")
	}
	if req.SourceKind == "" || req.SourceEventID == "" {
		return ErrSourceBoundRequired
	}
	switch req.Status {
	case AckStatusAcked, AckStatusRejected, AckStatusTimedOut:
	default:
		return fmt.Errorf("invalid ack status %q", req.Status)
	}
	layer := req.AckLayer
	if layer == "" {
		if req.Status == AckStatusAcked {
			layer = ACKLayerSessionVisible // default non-explicit
		} else {
			layer = ACKLayerNone
		}
	}
	if layer == ACKLayerExplicit {
		max := ProfileMaxACKLayer(req.HostFamily, req.Profile)
		if max != ACKLayerExplicit {
			return ErrExplicitACKNotSupported
		}
	}
	return nil
}
