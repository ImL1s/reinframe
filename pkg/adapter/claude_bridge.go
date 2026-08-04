package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// Claude PreToolUse hook bridge (#96).
//
// Host hook type names appear only in this adapter layer and docs — never as
// pkg/protocol identifiers. This is an experimental product bridge: fixture-
// driven and optional CLI entry; not a claim of production dual-host supervision.

// ClaudePreToolInput is the harness-facing PreTool surface after JSON mapping.
// Fields are adapter-only.
type ClaudePreToolInput struct {
	SessionID string
	ToolName  string
	FilePath  string
	// Phase is always PreTool for this mapper (informational).
	Phase string
	// RawToolInput is optional tool_input object JSON for audit (truncated).
	RawToolInput string
	// Proposed is the versioned host→core action projection (#115). Prefer this
	// over stuffing shell commands into ToolName.
	Proposed *ProposedAction
}

// ClaudeHookResponse is the JSON shape written back to Claude Code hooks.
// Uses documented permissionDecision fields; see docs/adapter/claude_bridge.md.
type ClaudeHookResponse struct {
	// Continue when false stops the tool (Claude continue flag).
	Continue *bool `json:"continue,omitempty"`
	// Decision is "approve" | "block" (legacy-compatible).
	Decision string `json:"decision,omitempty"`
	// Reason is human-readable / audit reason.
	Reason string `json:"reason,omitempty"`
	// HookSpecificOutput carries permissionDecision for newer Claude Code hooks.
	HookSpecificOutput *ClaudeHookSpecificOutput `json:"hookSpecificOutput,omitempty"`
	// Reinframe embeds core decision for tests and multi-harness consumers.
	Reinframe *ClaudeReinframeMeta `json:"reinframe,omitempty"`
}

// ClaudeHookSpecificOutput is the nested permission block for PreToolUse.
type ClaudeHookSpecificOutput struct {
	HookEventName            string `json:"hookEventName,omitempty"`
	PermissionDecision       string `json:"permissionDecision,omitempty"` // allow | deny | ask
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// ClaudeReinframeMeta is non-host metadata for debugging (ignored by Claude).
type ClaudeReinframeMeta struct {
	Action         string `json:"action"`
	ReasonCode     string `json:"reason_code"`
	InterventionID string `json:"intervention_id,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	ToolName       string `json:"tool_name,omitempty"`
}

// MapClaudePreToolUseJSON maps Claude Code PreToolUse-shaped JSON to ClaudePreToolInput.
//
// Expected loose keys (any subset):
//
//	session_id|sessionId, tool_name|toolName, tool_input|toolInput (object or string),
//	file_path|filePath|path, cwd (optional path fallback), hook_event_name
func MapClaudePreToolUseJSON(raw []byte) (ClaudePreToolInput, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ClaudePreToolInput{}, fmt.Errorf("claude pretool json: %w", err)
	}
	in := ClaudePreToolInput{Phase: "PreTool"}
	in.SessionID = firstString(m, "session_id", "sessionId", "sessionID")
	// Host tool id only — never take shell command from tool_input.command as ToolName (#115).
	in.ToolName = firstString(m, "tool_name", "toolName", "tool")
	if in.ToolName == "" {
		if ti, ok := m["tool_input"].(map[string]any); ok {
			// Accept nested tool_name/name only — not "command" (that is shell text).
			in.ToolName = firstString(ti, "tool_name", "name")
			in.FilePath = firstString(ti, "file_path", "filePath", "path")
		}
	}
	in.FilePath = firstNonEmpty(in.FilePath, firstString(m, "file_path", "filePath", "path"))
	var toolInput any
	if ti, ok := m["tool_input"]; ok && ti != nil {
		toolInput = ti
		b, err := json.Marshal(ti)
		if err == nil {
			s := string(b)
			if len(s) > 400 {
				s = s[:400] + "…"
			}
			in.RawToolInput = s
		}
	} else if s := firstString(m, "tool_input", "toolInput"); s != "" {
		if len(s) > 400 {
			s = s[:400] + "…"
		}
		in.RawToolInput = s
		var parsed any
		if err := json.Unmarshal([]byte(s), &parsed); err == nil {
			toolInput = parsed
		}
	}
	if in.SessionID == "" {
		return ClaudePreToolInput{}, fmt.Errorf("claude pretool: session_id required")
	}
	if in.ToolName == "" {
		return ClaudePreToolInput{}, fmt.Errorf("claude pretool: tool_name required")
	}
	pa, err := ProposedActionFromClaudePreTool(in, toolInput, ProposedActionOptions{})
	if err != nil {
		return ClaudePreToolInput{}, err
	}
	in.Proposed = &pa
	if in.FilePath == "" && pa.FilePath != "" {
		in.FilePath = pa.FilePath
	}
	return in, nil
}

// HookRequestFromClaudePreTool converts adapter input to HookRequest for core gates.
// When Proposed is present, ToolName/FilePath come from the projection (Command is
// not placed into ToolName).
func HookRequestFromClaudePreTool(in ClaudePreToolInput) HookRequest {
	if in.Proposed != nil {
		req := HookRequestFromProposedAction(*in.Proposed, "PreTool")
		req.Proposed = in.Proposed
		return req
	}
	return HookRequest{
		SessionID: in.SessionID,
		Phase:     "PreTool",
		ToolName:  in.ToolName,
		FilePath:  in.FilePath,
	}
}

// ClaudeBridgeConfig configures EvaluateClaudePreTool.
//
// Keep policy engine out of this package (import cycle: policy → adapter).
// Callers that need EvaluateBeforeTool inject Evaluate.
type ClaudeBridgeConfig struct {
	// Policy is the deterministic HookPolicy (deny lists, pending latch, …).
	Policy HookPolicy
	// Evaluate when non-nil replaces EvaluateHook (e.g. policy.EvaluateBeforeTool wrapper).
	Evaluate func(ctx context.Context, req HookRequest) HookDecision
}

// EvaluateClaudePreTool maps host PreTool input through core EvaluateHook (or
// cfg.Evaluate) and returns a Claude-compatible response.
func EvaluateClaudePreTool(ctx context.Context, in ClaudePreToolInput, cfg ClaudeBridgeConfig) (ClaudeHookResponse, HookDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req := HookRequestFromClaudePreTool(in)
	var dec HookDecision
	if cfg.Evaluate != nil {
		dec = cfg.Evaluate(ctx, req)
	} else {
		dec = EvaluateHook(ctx, req, cfg.Policy)
	}
	return ClaudeHookResponseFromDecision(in, dec), dec, nil
}

// EvaluateClaudePreToolJSON is the stdin/fixture entry: raw PreToolUse JSON → response JSON.
func EvaluateClaudePreToolJSON(ctx context.Context, raw []byte, cfg ClaudeBridgeConfig) (ClaudeHookResponse, HookDecision, error) {
	in, err := MapClaudePreToolUseJSON(raw)
	if err != nil {
		return ClaudeHookResponse{}, HookDecision{}, err
	}
	return EvaluateClaudePreTool(ctx, in, cfg)
}

// ClaudeHookResponseFromDecision maps core HookDecision → host response JSON shape.
func ClaudeHookResponseFromDecision(in ClaudePreToolInput, dec HookDecision) ClaudeHookResponse {
	meta := &ClaudeReinframeMeta{
		Action:         dec.Action,
		ReasonCode:     dec.ReasonCode,
		InterventionID: dec.InterventionID,
		SessionID:      in.SessionID,
		ToolName:       in.ToolName,
	}
	resp := ClaudeHookResponse{Reinframe: meta, Reason: dec.ReasonCode}
	switch dec.Action {
	case HookActionAllow:
		resp.Decision = "approve"
		perm := "allow"
		resp.HookSpecificOutput = &ClaudeHookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       perm,
			PermissionDecisionReason: dec.ReasonCode,
		}
	case HookActionDeny, HookActionDefer:
		// Defer blocks the tool until advisory is delivered/acked (CapToolGate).
		cont := false
		resp.Continue = &cont
		resp.Decision = "block"
		perm := "deny"
		reason := dec.ReasonCode
		if dec.Action == HookActionDefer {
			reason = "defer:" + dec.ReasonCode
		}
		resp.Reason = reason
		resp.HookSpecificOutput = &ClaudeHookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       perm,
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

// MapClaudeUserPromptBridge reuses #84 mapper and labels the experimental bridge path.
func MapClaudeUserPromptBridge(raw []byte, opts TaskIntakeOptions) (TaskIntakeResult, error) {
	out, err := MapClaudeUserPromptSubmitJSON(raw, opts)
	if err != nil {
		return out, err
	}
	if out.Submitted.SourceHint == "" {
		out.Submitted.SourceHint = HostHintClaudeCode
	}
	return out, nil
}

// AgentEventFromClaudePreTool builds an optional tool_call AgentEvent for stores.
func AgentEventFromClaudePreTool(in ClaudePreToolInput, seq int64) protocol.AgentEvent {
	body, _ := json.Marshal(map[string]any{
		"tool_name": in.ToolName,
		"source":    "claude_pretool_bridge",
		"file_path": in.FilePath,
		"input":     in.RawToolInput,
	})
	return protocol.AgentEvent{
		EventID:     fmt.Sprintf("claude-pretool-%d", seq),
		SessionID:   in.SessionID,
		SequenceNum: seq,
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     body,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
