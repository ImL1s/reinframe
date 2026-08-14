package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Pinned Claude Code hook response profile for #116.
// Documented against Claude Code PreToolUse hook output shapes (permissionDecision /
// decision fields). `continue:false` is treated as whole-session stop and must not
// be used for ordinary tool deny.
const (
	ClaudeHookProfileV1 = "reinframe.claude_hook_response.v1"
	// MaxHookReasonRunes bounds reason strings (no secrets).
	MaxHookReasonRunes = 500
	// MaxHookContextRunes bounds additionalContext strings (#139).
	MaxHookContextRunes = 2000
)

// Transport level tags for capability honesty (#139).
const (
	ClaudeTransportHookAdditionalContext = "hook_additional_context"
	ClaudeTransportNativeDefer           = "native_defer"
	ClaudeTransportDegradedDeny          = "degraded_deny"
	ClaudeTransportDirectDeny            = "direct_deny"
	ClaudeTransportDirectAllow           = "direct_allow"
)

// ClaudeChallengeContext is the structured appealable challenge payload delivered
// to Claude Code in hook responses (#139).
type ClaudeChallengeContext struct {
	ChallengeID         string `json:"challenge_id"`
	ChallengeNonce      string `json:"challenge_nonce"`
	Reason              string `json:"reason"`
	SuggestedFix        string `json:"suggested_fix"`
	OneShotRetryAllowed bool   `json:"one_shot_retry_allowed"`
}

// FormatAdditionalContext formats the challenge context into human- and model-readable
// additionalContext text for Claude Code PreToolUse output.
func (c ClaudeChallengeContext) FormatAdditionalContext() string {
	if c.ChallengeID == "" {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[Reinframe Appealable Challenge]\n")
	sb.WriteString(fmt.Sprintf("challenge_id: %s\n", c.ChallengeID))
	if c.ChallengeNonce != "" {
		sb.WriteString(fmt.Sprintf("challenge_nonce: %s\n", c.ChallengeNonce))
	}
	if c.Reason != "" {
		sb.WriteString(fmt.Sprintf("reason: %s\n", c.Reason))
	}
	if c.SuggestedFix != "" {
		sb.WriteString(fmt.Sprintf("suggested_fix: %s\n", c.SuggestedFix))
	}
	sb.WriteString(fmt.Sprintf("one_shot_retry_allowed: %t\n", c.OneShotRetryAllowed))
	if c.OneShotRetryAllowed {
		sb.WriteString("To appeal and retry this action once, provide your justification with challenge_id and challenge_nonce in your next tool call.\n")
	}
	return boundContext(sb.String())
}

// ClaudeHostMode describes host defer capability.
type ClaudeHostMode string

const (
	// ClaudeHostModeInteractive supports native "ask"/defer-style decisions when configured.
	ClaudeHostModeInteractive ClaudeHostMode = "interactive"
	// ClaudeHostModeHeadless has no native defer; defer degrades to deny + feedback.
	ClaudeHostModeHeadless ClaudeHostMode = "headless"
	// ClaudeHostModeUnknown fail-closed or explicit unsupported.
	ClaudeHostModeUnknown ClaudeHostMode = "unknown"
)

// ClaudeResponseOptions configures ClaudeHookResponseFromDecision (#116).
type ClaudeResponseOptions struct {
	// Profile is the pinned response schema version (default ClaudeHookProfileV1).
	Profile string
	// HostMode selects native defer vs degrade.
	HostMode ClaudeHostMode
	// NativeDefer when true and HostMode is interactive, emit permissionDecision=ask
	// for HookActionDefer (bounded feedback). When false, defer → deny + reason.
	NativeDefer bool
	// FailOpenProductivity: on timeout-like reason codes for productivity, allow.
	// Security denies never fail-open.
	FailOpenProductivity bool
	// Challenge carries structured challenge context to deliver in additionalContext (#139).
	Challenge *ClaudeChallengeContext
	// TransportLevel overrides default transport tagging.
	TransportLevel string
}

// ClaudeHookResponseFromDecision maps core HookDecision → host response JSON shape (#116).
// Ordinary tool deny/block does NOT set continue=false (session stop).
func ClaudeHookResponseFromDecision(in ClaudePreToolInput, dec HookDecision) ClaudeHookResponse {
	return ClaudeHookResponseFromDecisionOpts(in, dec, ClaudeResponseOptions{
		Profile:     ClaudeHookProfileV1,
		HostMode:    ClaudeHostModeHeadless,
		NativeDefer: false,
	})
}

// ClaudeHookResponseFromDecisionOpts is the versioned mapper with host capability.
func ClaudeHookResponseFromDecisionOpts(in ClaudePreToolInput, dec HookDecision, opts ClaudeResponseOptions) ClaudeHookResponse {
	if opts.Profile == "" {
		opts.Profile = ClaudeHookProfileV1
	}
	if opts.HostMode == "" {
		opts.HostMode = ClaudeHostModeHeadless
	}

	// Unknown host: fail closed for non-allow.
	if opts.HostMode == ClaudeHostModeUnknown && dec.Action != HookActionAllow {
		dec = HookDecision{Action: HookActionDeny, ReasonCode: "unsupported_host_version"}
	}

	// Productivity timeout fail-open (not security).
	if opts.FailOpenProductivity && isProductivityTimeout(dec.ReasonCode) {
		dec = HookDecision{Action: HookActionAllow, ReasonCode: ReasonTimeoutFailOpen}
	}

	transportLevel := opts.TransportLevel
	if transportLevel == "" {
		switch {
		case dec.Action == HookActionAllow:
			transportLevel = ClaudeTransportDirectAllow
		case opts.Challenge != nil:
			transportLevel = ClaudeTransportHookAdditionalContext
		case dec.Action == HookActionDefer && opts.NativeDefer && opts.HostMode == ClaudeHostModeInteractive:
			transportLevel = ClaudeTransportNativeDefer
		case dec.Action == HookActionDefer:
			transportLevel = ClaudeTransportDegradedDeny
		default:
			transportLevel = ClaudeTransportDirectDeny
		}
	}

	meta := &ClaudeReinframeMeta{
		Action:         dec.Action,
		ReasonCode:     dec.ReasonCode,
		InterventionID: dec.InterventionID,
		SessionID:      in.SessionID,
		ToolName:       in.ToolName,
		TransportLevel: transportLevel,
		Challenge:      opts.Challenge,
	}
	reason := boundReason(dec.ReasonCode)
	resp := ClaudeHookResponse{Reinframe: meta, Reason: reason}

	switch dec.Action {
	case HookActionAllow:
		resp.Decision = "approve"
		resp.HookSpecificOutput = &ClaudeHookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "allow",
			PermissionDecisionReason: reason,
		}
		if opts.Challenge != nil {
			resp.HookSpecificOutput.AdditionalContext = opts.Challenge.FormatAdditionalContext()
		}
		// Explicitly do not set Continue (omit) — allow default host continue.
	case HookActionDefer:
		if opts.NativeDefer && opts.HostMode == ClaudeHostModeInteractive {
			// Native defer/ask: tool gated without session stop.
			resp.Decision = "block" // still blocks the tool call until resolved
			resp.Reason = boundReason("defer:" + dec.ReasonCode)
			resp.HookSpecificOutput = &ClaudeHookSpecificOutput{
				HookEventName:            "PreToolUse",
				PermissionDecision:       "ask",
				PermissionDecisionReason: resp.Reason,
			}
			// No continue:false
		} else {
			// Degrade: deny + bounded feedback reason (not session stop).
			resp.Decision = "block"
			resp.Reason = boundReason("defer_degraded:" + dec.ReasonCode)
			resp.HookSpecificOutput = &ClaudeHookSpecificOutput{
				HookEventName:            "PreToolUse",
				PermissionDecision:       "deny",
				PermissionDecisionReason: resp.Reason,
			}
		}
	case HookActionDeny:
		// Tool-level BLOCK only — never continue:false for ordinary deny (#116).
		resp.Decision = "block"
		resp.Reason = reason
		hso := &ClaudeHookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		}
		if opts.Challenge != nil {
			hso.AdditionalContext = opts.Challenge.FormatAdditionalContext()
		}
		resp.HookSpecificOutput = hso
	default:
		resp.Decision = "approve"
		resp.HookSpecificOutput = &ClaudeHookSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "allow",
		}
	}
	return resp
}

// ValidateClaudeHookResponseClosedSchema checks response fields are within closed set.
func ValidateClaudeHookResponseClosedSchema(resp ClaudeHookResponse) error {
	if resp.Continue != nil && !*resp.Continue {
		return fmt.Errorf("claude hook response: continue:false is forbidden for tool-level decisions (session stop)")
	}
	if resp.Decision != "" && resp.Decision != "approve" && resp.Decision != "block" {
		return fmt.Errorf("claude hook response: unknown decision %q", resp.Decision)
	}
	if resp.HookSpecificOutput != nil {
		pd := resp.HookSpecificOutput.PermissionDecision
		if pd != "" && pd != "allow" && pd != "deny" && pd != "ask" {
			return fmt.Errorf("claude hook response: unknown permissionDecision %q", pd)
		}
		if resp.HookSpecificOutput.HookEventName != "" && resp.HookSpecificOutput.HookEventName != "PreToolUse" {
			return fmt.Errorf("claude hook response: unexpected hookEventName")
		}
		if utf8.RuneCountInString(resp.HookSpecificOutput.AdditionalContext) > MaxHookContextRunes {
			return fmt.Errorf("claude hook response: additionalContext too long")
		}
	}
	if utf8.RuneCountInString(resp.Reason) > MaxHookReasonRunes {
		return fmt.Errorf("claude hook response: reason too long")
	}
	return nil
}

// MarshalClaudeHookResponseJSON encodes response for host stdin protocol.
func MarshalClaudeHookResponseJSON(resp ClaudeHookResponse) ([]byte, error) {
	if err := ValidateClaudeHookResponseClosedSchema(resp); err != nil {
		return nil, err
	}
	return json.Marshal(resp)
}

func isProductivityTimeout(reason string) bool {
	r := strings.ToLower(reason)
	return r == ReasonTimeoutFailOpen || r == "timeout" || strings.Contains(r, "timeout_productivity")
}

func boundReason(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Strip obvious secret-like fragments
	low := strings.ToLower(s)
	for _, bad := range []string{"password=", "api_key=", "authorization:", "bearer "} {
		if strings.Contains(low, bad) {
			return "redacted_reason"
		}
	}
	if utf8.RuneCountInString(s) <= MaxHookReasonRunes {
		return s
	}
	r := []rune(s)
	return string(r[:MaxHookReasonRunes]) + "…"
}

func boundContext(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Strip obvious secret-like fragments
	low := strings.ToLower(s)
	for _, bad := range []string{"password=", "api_key=", "authorization:", "bearer "} {
		if strings.Contains(low, bad) {
			return "redacted_context"
		}
	}
	if utf8.RuneCountInString(s) <= MaxHookContextRunes {
		return s
	}
	r := []rune(s)
	return string(r[:MaxHookContextRunes]) + "…"
}
