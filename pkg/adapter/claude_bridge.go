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
// ClaudeJustificationInput is the adapter-facing justification payload (#139).
type ClaudeJustificationInput struct {
	ConcreteValue              string   `json:"concrete_value,omitempty"`
	PreventedFailureOrThreat   string   `json:"prevented_failure_or_threat,omitempty"`
	EstimatedCost              string   `json:"estimated_cost,omitempty"`
	AlternativesConsidered     string   `json:"alternatives_considered,omitempty"`
	ScopeLimit                 string   `json:"scope_limit,omitempty"`
	VerificationPlan           string   `json:"verification_plan,omitempty"`
	RollbackPlan               string   `json:"rollback_plan,omitempty"`
	SupportingEvidenceEventIDs []string `json:"supporting_evidence_event_ids,omitempty"`
	RawText                    string   `json:"raw_text,omitempty"`
}

// ClaudePreToolInput is the harness-facing PreTool surface after JSON mapping.
// Fields are adapter-only.
type ClaudePreToolInput struct {
	Phase        string
	SessionID    string
	ToolName     string
	ToolUseID    string
	FilePath     string
	RawToolInput string
	// Proposed is the versioned host→core action projection (#115). Prefer this
	// over stuffing shell commands into ToolName.
	Proposed *ProposedAction
	// Challenge binding fields for appealable retry turns (#139).
	ChallengeID    string
	ChallengeNonce string
	Justification  *ClaudeJustificationInput
}

// ClaudeHookResponse is the JSON shape written back to Claude Code hooks.
// Uses documented permissionDecision fields; see docs/adapter/claude_bridge.md.
//
// #116: do not set Continue=false for ordinary tool deny — that is treated as a
// whole-session stop. Tool block uses decision/permissionDecision only.
type ClaudeHookResponse struct {
	// Continue is intentionally unused for tool-level deny (#116). omitempty.
	// Setting false is rejected by ValidateClaudeHookResponseClosedSchema.
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
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// ClaudeReinframeMeta is non-host metadata for debugging (ignored by Claude).
type ClaudeReinframeMeta struct {
	Action         string                  `json:"action"`
	ReasonCode     string                  `json:"reason_code"`
	InterventionID string                  `json:"intervention_id,omitempty"`
	SessionID      string                  `json:"session_id,omitempty"`
	ToolName       string                  `json:"tool_name,omitempty"`
	TransportLevel string                  `json:"transport_level,omitempty"`
	Challenge      *ClaudeChallengeContext `json:"challenge,omitempty"`
}

// MapClaudePreToolUseJSON maps Claude Code PreToolUse-shaped JSON to ClaudePreToolInput.
//
// Expected loose keys (any subset):
//
//	session_id|sessionId, tool_name|toolName, tool_use_id|toolUseId, tool_input|toolInput (object or string),
//	file_path|filePath|path, cwd (optional path fallback), hook_event_name,
//	challenge_id|challengeId, challenge_nonce|challengeNonce, justification
func MapClaudePreToolUseJSON(raw []byte) (ClaudePreToolInput, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ClaudePreToolInput{}, fmt.Errorf("claude pretool json: %w", err)
	}
	in := ClaudePreToolInput{Phase: "PreTool"}
	in.SessionID = firstString(m, "session_id", "sessionId", "sessionID")
	// Host tool id only — never take shell command from tool_input.command as ToolName (#115).
	in.ToolName = firstString(m, "tool_name", "toolName", "tool")
	in.ToolUseID = firstString(m, "tool_use_id", "toolUseId", "id")
	in.ChallengeID = firstString(m, "challenge_id", "challengeId", "_challenge_id")
	in.ChallengeNonce = firstString(m, "challenge_nonce", "challengeNonce", "_challenge_nonce", "nonce")
	in.Justification = parseJustificationInput(m["justification"])
	if in.Justification == nil {
		in.Justification = parseJustificationInput(m["_justification"])
	}

	var tiMap map[string]any
	if ti, ok := m["tool_input"].(map[string]any); ok {
		tiMap = ti
		if in.ToolName == "" {
			in.ToolName = firstString(ti, "tool_name", "name")
		}
		in.FilePath = firstString(ti, "file_path", "filePath", "path")
		if in.ChallengeID == "" {
			in.ChallengeID = firstString(ti, "challenge_id", "challengeId", "_challenge_id")
		}
		if in.ChallengeNonce == "" {
			in.ChallengeNonce = firstString(ti, "challenge_nonce", "challengeNonce", "_challenge_nonce", "nonce")
		}
		if in.Justification == nil {
			in.Justification = parseJustificationInput(ti["justification"])
		}
		if in.Justification == nil {
			in.Justification = parseJustificationInput(ti["_justification"])
		}
	}
	in.FilePath = firstNonEmpty(in.FilePath, firstString(m, "file_path", "filePath", "path"))
	var toolInput any
	if tiMap != nil {
		toolInput = tiMap
		b, err := json.Marshal(tiMap)
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
			if pMap, ok := parsed.(map[string]any); ok {
				if in.ToolName == "" {
					in.ToolName = firstString(pMap, "tool_name", "name")
				}
				if in.FilePath == "" {
					in.FilePath = firstString(pMap, "file_path", "filePath", "path")
				}
				if in.ChallengeID == "" {
					in.ChallengeID = firstString(pMap, "challenge_id", "challengeId", "_challenge_id")
				}
				if in.ChallengeNonce == "" {
					in.ChallengeNonce = firstString(pMap, "challenge_nonce", "challengeNonce", "_challenge_nonce", "nonce")
				}
				if in.Justification == nil {
					in.Justification = parseJustificationInput(pMap["justification"])
				}
				if in.Justification == nil {
					in.Justification = parseJustificationInput(pMap["_justification"])
				}
			}
		}
	}
	if in.SessionID == "" {
		return ClaudePreToolInput{}, fmt.Errorf("claude pretool: session_id required")
	}
	if in.ToolName == "" {
		return ClaudePreToolInput{}, fmt.Errorf("claude pretool: tool_name required")
	}
	cleanToolInput := toolInput
	if tiMap != nil {
		cleanToolInput = cleanToolInputMap(tiMap)
	} else if pMap, ok := toolInput.(map[string]any); ok {
		cleanToolInput = cleanToolInputMap(pMap)
	}

	pa, err := ProposedActionFromClaudePreTool(in, cleanToolInput, ProposedActionOptions{})
	if err != nil {
		return ClaudePreToolInput{}, err
	}
	in.Proposed = &pa
	if in.FilePath == "" && pa.FilePath != "" {
		in.FilePath = pa.FilePath
	}
	return in, nil
}

func cleanToolInputMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "challenge_id" || lk == "challengeid" || lk == "_challenge_id" ||
			lk == "challenge_nonce" || lk == "challengenonce" || lk == "_challenge_nonce" || lk == "nonce" ||
			lk == "justification" || lk == "_justification" {
			continue
		}
		out[k] = v
	}
	return out
}

func parseJustificationInput(v any) *ClaudeJustificationInput {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err == nil && len(m) > 0 {
			return parseJustificationInput(m)
		}
		return &ClaudeJustificationInput{RawText: s}
	}
	if m, ok := v.(map[string]any); ok {
		out := &ClaudeJustificationInput{
			ConcreteValue:            firstString(m, "concrete_value", "concreteValue", "value"),
			PreventedFailureOrThreat: firstString(m, "prevented_failure_or_threat", "preventedFailureOrThreat", "threat", "prevented_failure"),
			EstimatedCost:            firstString(m, "estimated_cost", "estimatedCost", "cost"),
			AlternativesConsidered:   firstString(m, "alternatives_considered", "alternativesConsidered", "alternatives"),
			ScopeLimit:               firstString(m, "scope_limit", "scopeLimit", "scope"),
			VerificationPlan:         firstString(m, "verification_plan", "verificationPlan", "verification"),
			RollbackPlan:             firstString(m, "rollback_plan", "rollbackPlan", "rollback"),
		}
		if ev, ok := m["supporting_evidence_event_ids"]; ok {
			out.SupportingEvidenceEventIDs = toStringSlice(ev)
		} else if ev, ok := m["evidence_ids"]; ok {
			out.SupportingEvidenceEventIDs = toStringSlice(ev)
		} else if ev, ok := m["evidence"]; ok {
			out.SupportingEvidenceEventIDs = toStringSlice(ev)
		}
		return out
	}
	return nil
}

func toStringSlice(v any) []string {
	if v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		var out []string
		for _, elem := range s {
			if str, ok := elem.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out
	case string:
		if s != "" {
			return []string{s}
		}
	}
	return nil
}

// HookRequestFromClaudePreTool converts adapter input to HookRequest for core gates.
// When Proposed is present, ToolName/FilePath come from the projection (Command is
// not placed into ToolName).
func HookRequestFromClaudePreTool(in ClaudePreToolInput) HookRequest {
	var req HookRequest
	if in.Proposed != nil {
		req = HookRequestFromProposedAction(*in.Proposed, "PreTool")
		req.Proposed = in.Proposed
	} else {
		req = HookRequest{
			SessionID: in.SessionID,
			Phase:     "PreTool",
			ToolName:  in.ToolName,
			FilePath:  in.FilePath,
		}
	}
	req.ChallengeID = in.ChallengeID
	req.ChallengeNonce = in.ChallengeNonce
	req.Justification = in.Justification
	return req
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
	// Response options for host-versioned ALLOW/BLOCK/defer mapping (#116).
	Response ClaudeResponseOptions
	// EvaluateChallenge when non-nil handles challenge opening and retry lifecycle (#139).
	EvaluateChallenge func(ctx context.Context, in ClaudePreToolInput, req HookRequest, cfg ClaudeBridgeConfig) (ClaudeHookResponse, HookDecision, bool, error)
}

// EvaluateClaudePreTool maps host PreTool input through core EvaluateHook (or
// cfg.Evaluate) and returns a Claude-compatible response.
func EvaluateClaudePreTool(ctx context.Context, in ClaudePreToolInput, cfg ClaudeBridgeConfig) (ClaudeHookResponse, HookDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req := HookRequestFromClaudePreTool(in)

	if cfg.EvaluateChallenge != nil {
		resp, dec, handled, err := cfg.EvaluateChallenge(ctx, in, req, cfg)
		if err != nil {
			return resp, dec, err
		}
		if handled {
			return resp, dec, nil
		}
	}

	var dec HookDecision
	if cfg.Evaluate != nil {
		dec = cfg.Evaluate(ctx, req)
	} else {
		dec = EvaluateHook(ctx, req, cfg.Policy)
	}
	return ClaudeHookResponseFromDecisionOpts(in, dec, cfg.Response), dec, nil
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
// Implementation lives in claude_hook_response.go (#116): ordinary deny must not
// set continue:false (session stop).

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
