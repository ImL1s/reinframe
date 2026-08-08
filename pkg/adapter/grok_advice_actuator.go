package adapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// Grok live-host profile pins from #167 evidence (do not invent Level 2 / CapPause).
const (
	GrokLiveHostFamily        = "grok_build"
	GrokLiveAdviceProfile     = GrokACPProfileV1 // reinframe.grok_build_acp.v1
	GrokSafeBoundaryNextInput = "next_input"
)

// GrokACPActuator delivers bounded advice via a live or test Grok ACP session (#108 + #167).
//
// Honest ACK ceiling from #167 live proof:
//   - transport: session/prompt JSON-RPC success
//   - session_visible: correlated session/update after delivery
//   - explicit: never claimed from transport alone
//
// Does not read auth.json. Does not inject mid-tool. Safe boundary is next_input only.
type GrokACPActuator struct {
	// Client is an initialized (+ preferably authenticated) Grok ACP client.
	Client *GrokACPClient
	// TargetSessionID is the ACP session/new identity.
	TargetSessionID string
	// HostVersion is the full grok --version string when known.
	HostVersion string
	// CapsDigest is optional negotiated caps digest from the live profile.
	CapsDigest string
	// WaitUpdate is how long to wait for session/update after prompt (0 = transport only).
	WaitUpdate time.Duration
	// Now overrides clock (tests).
	Now func() time.Time
}

func (g *GrokACPActuator) now() time.Time {
	if g != nil && g.Now != nil {
		return g.Now().UTC()
	}
	return time.Now().UTC()
}

// Deliver implements InterventionActuator for Grok ACP session/prompt.
func (g *GrokACPActuator) Deliver(ctx context.Context, intervention protocol.Intervention) (InterventionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := g.now()
	mode := DefaultDeliveryMode(intervention.ActionType)
	if intervention.DeliveryModeHint != "" {
		mode = intervention.DeliveryModeHint
	}
	base := InterventionResult{
		InterventionID:  intervention.InterventionID,
		DeliveryMode:    mode,
		DeliveredAt:     now,
		HostFamily:      GrokLiveHostFamily,
		HostVersion:     g.HostVersion,
		Profile:         GrokLiveAdviceProfile,
		SafeBoundary:    GrokSafeBoundaryNextInput,
		TargetSessionID: g.TargetSessionID,
		CapsDigest:      g.CapsDigest,
		AckLayer:        ACKLayerNone,
		ErrorClass:      ErrorClassNone,
	}

	if g == nil || g.Client == nil {
		base.Accepted = false
		base.AckStatus = AckStatusUnsupported
		base.ErrorClass = ErrorClassUnsupportedCapability
		base.Message = "grok advice: client required"
		return base, fmt.Errorf("grok advice: client required")
	}
	if g.TargetSessionID == "" {
		base.Accepted = false
		base.AckStatus = AckStatusUnsupported
		base.ErrorClass = ErrorClassUnsupportedCapability
		base.Message = "grok advice: TargetSessionID required"
		return base, fmt.Errorf("grok advice: TargetSessionID required")
	}
	// Session mismatch: never inject into another Reinframe session's host binding
	// when intervention carries a host session pin in Fingerprint prefix "acp:".
	if strings.HasPrefix(intervention.Fingerprint, "acp:") {
		want := strings.TrimPrefix(intervention.Fingerprint, "acp:")
		if want != "" && want != g.TargetSessionID {
			base.Accepted = false
			base.AckStatus = AckStatusRejected
			base.ErrorClass = ErrorClassTransport
			base.Message = "session/host mismatch: intervention pin does not match TargetSessionID"
			return base, fmt.Errorf("grok advice: session mismatch")
		}
	}
	if intervention.AdvicePrompt == "" && intervention.ActionType == "" {
		base.Accepted = false
		base.AckStatus = AckStatusRejected
		base.ErrorClass = ErrorClassTransport
		base.Message = "empty advice"
		return base, fmt.Errorf("grok advice: empty intervention")
	}
	// Bound: never inject raw reviewer/model CoT — only closed advice fields.
	kind := intervention.ActionType
	if kind == "" {
		kind = "ZOOM_OUT_PROMPT"
	}
	body := intervention.AdvicePrompt
	if body == "" {
		body = "Re-evaluate the current approach against the stated acceptance criteria."
	}
	// Reject obvious private-reasoning markers.
	low := strings.ToLower(body)
	for _, bad := range []string{"<thinking>", "chain-of-thought", "system prompt dump"} {
		if strings.Contains(low, bad) {
			base.Accepted = false
			base.AckStatus = AckStatusRejected
			base.ErrorClass = ErrorClassTransport
			base.Message = "refusing private-reasoning-shaped advice body"
			return base, fmt.Errorf("grok advice: privacy reject")
		}
	}
	prompt := BuildAdvicePrompt(kind, body, intervention.InterventionID, "")
	if err := ctx.Err(); err != nil {
		base.Accepted = false
		base.AckStatus = AckStatusTimedOut
		base.ErrorClass = ErrorClassTimeout
		base.Message = "context done before deliver"
		return base, err
	}
	if err := g.Client.SessionPrompt(ctx, g.TargetSessionID, prompt, intervention.InterventionID, ""); err != nil {
		base.Accepted = false
		base.AckStatus = AckStatusRejected
		base.ErrorClass = ErrorClassTransport
		base.Message = "session/prompt: " + boundRunes(err.Error(), 200)
		base.AckLayer = ACKLayerNone
		return base, err
	}
	// Transport success is not explicit ACK.
	base.Accepted = true
	base.AckStatus = AckStatusPending
	base.AckLayer = ACKLayerTransport
	base.Message = "session/prompt transport accepted; explicit ACK not claimed"

	wait := g.WaitUpdate
	if wait <= 0 {
		// Still return transport; optional update wait skipped.
		return base, nil
	}
	deadline := time.After(wait)
	for {
		select {
		case <-ctx.Done():
			// Transport already succeeded; context end does not revoke transport layer.
			return base, nil
		case <-deadline:
			return base, nil
		case u, ok := <-g.Client.Updates():
			if !ok {
				return base, nil
			}
			kind, _ := MapSessionUpdateToSummary(u)
			if kind == "" {
				continue
			}
			g.Client.NoteSessionVisible()
			base.AckLayer = ACKLayerSessionVisible
			base.Message = "session/prompt + correlated session/update; strongest ACK=session_visible; explicit not claimed"
			return base, nil
		case <-time.After(50 * time.Millisecond):
			if g.Client.LastACKLayer() == ACKLayerSessionVisible {
				base.AckLayer = ACKLayerSessionVisible
				base.Message = "session_visible via client ACK layer; explicit not claimed"
				return base, nil
			}
		}
	}
}
