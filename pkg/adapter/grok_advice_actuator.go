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
	mode := DefaultDeliveryMode(intervention.ActionType)
	if intervention.DeliveryModeHint != "" {
		mode = intervention.DeliveryModeHint
	}
	base := InterventionResult{
		InterventionID: intervention.InterventionID,
		DeliveryMode:   mode,
		DeliveredAt:    time.Now().UTC(),
		HostFamily:     GrokLiveHostFamily,
		Profile:        GrokLiveAdviceProfile,
		SafeBoundary:   GrokSafeBoundaryNextInput,
		AckLayer:       ACKLayerNone,
		ErrorClass:     ErrorClassNone,
	}
	if g == nil || g.Client == nil {
		base.Accepted = false
		base.AckStatus = AckStatusUnsupported
		base.ErrorClass = ErrorClassUnsupportedCapability
		base.Message = "grok advice: client required"
		return base, fmt.Errorf("grok advice: client required")
	}
	now := g.now()
	base.DeliveredAt = now
	base.HostVersion = g.HostVersion
	base.TargetSessionID = g.TargetSessionID
	base.CapsDigest = g.CapsDigest
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
	// Transport success is not explicit ACK. Per-delivery layer starts at transport
	// only — never reuse client-global LastACKLayer (stale session_visible inflation).
	base.Accepted = true
	base.AckStatus = AckStatusPending
	base.AckLayer = ACKLayerTransport
	base.Message = "session/prompt transport accepted; explicit ACK not claimed"

	wait := g.WaitUpdate
	if wait <= 0 {
		return base, nil
	}
	// Only updates received *after* this SessionPrompt may upgrade this delivery.
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return base, nil
		}
		select {
		case <-ctx.Done():
			return base, nil
		case u, ok := <-g.Client.Updates():
			if !ok {
				return base, nil
			}
			if !updateMatchesTargetSession(u, g.TargetSessionID) {
				continue
			}
			kind, _ := MapSessionUpdateToSummary(u)
			if kind == "" {
				continue
			}
			// kind "unknown" still proves a post-prompt session/update envelope for this session.
			g.Client.NoteSessionVisible()
			base.AckLayer = ACKLayerSessionVisible
			base.Message = "session/prompt + post-prompt correlated session/update; strongest ACK=session_visible; explicit not claimed"
			return base, nil
		case <-time.After(50 * time.Millisecond):
			// Do not poll LastACKLayer — that reuses prior deliveries' session_visible.
		}
	}
	return base, nil
}

// updateMatchesTargetSession accepts updates that either omit sessionId or match target.
func updateMatchesTargetSession(u map[string]any, target string) bool {
	if target == "" {
		return true
	}
	sid, _ := u["sessionId"].(string)
	if sid == "" {
		if p, _ := u["params"].(map[string]any); p != nil {
			sid, _ = p["sessionId"].(string)
		}
	}
	if sid == "" {
		return true // host omitted session id — do not reject solely for that
	}
	return sid == target
}
