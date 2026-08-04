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
)

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

	meta := &ClaudeReinframeMeta{
		Action:         dec.Action,
		ReasonCode:     dec.ReasonCode,
		InterventionID: dec.InterventionID,
		SessionID:      in.SessionID,
		ToolName:       in.ToolName,
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
		resp.HookSpecificOutput = &ClaudeHookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		}
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
