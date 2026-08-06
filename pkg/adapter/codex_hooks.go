package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Pinned Codex hooks profile (official docs retrieved 2026-08-06).
// Sources: https://developers.openai.com/codex/hooks
const (
	CodexHooksProfileV1     = "reinframe.codex_hooks.2026-08-06.v1"
	CodexHookOwnerKey       = "reinframe_owner"
	CodexHookOwnerValue     = "reinframe"
	CodexHookSchemaKey      = "reinframe_schema_version"
	CodexHookSchemaVersion  = "reinframe.codex_hook_entry.v1"
	CodexHookProfileHashKey = "reinframe_profile_hash"
	// Bounds for stdin/stdout handling (#163).
	MaxCodexHookStdinBytes  = 1 << 20 // 1 MiB
	MaxCodexHookStdoutBytes = 64 << 10
	MaxCodexContextRunes    = 2000
	MaxCodexReasonRunes     = 500
)

// Supported Codex hook event names for the pinned profile.
const (
	CodexEventSessionStart      = "SessionStart"
	CodexEventUserPromptSubmit  = "UserPromptSubmit"
	CodexEventPreToolUse        = "PreToolUse"
	CodexEventPermissionRequest = "PermissionRequest"
	CodexEventPostToolUse       = "PostToolUse"
	CodexEventStop              = "Stop"
	CodexEventSessionEnd        = "SessionEnd"
)

// Hosted tools that do not use the local function-tool hook path (official table).
var codexHostedToolsUnsupported = map[string]struct{}{
	"WebSearch":  {},
	"web_search": {},
	"websearch":  {},
}

// CodexHookInput is the closed adapter-facing parse of a Codex command-hook stdin object.
type CodexHookInput struct {
	SessionID      string
	HookEventName  string
	Cwd            string
	Model          string
	PermissionMode string
	TurnID         string
	// ToolName is set for PreToolUse / PermissionRequest / PostToolUse when present.
	ToolName string
	// ToolInput is a redacted/bounded map of tool arguments when present.
	ToolInput map[string]any
	// Source is SessionStart source (startup|resume|clear|compact).
	Source string
	// Reason is SessionEnd reason when present.
	Reason string
	// Prompt is bounded UserPromptSubmit text (may be empty).
	Prompt string
	// UnsupportedHosted is true when the tool is known-hosted and not gateable.
	UnsupportedHosted bool
	// ParseStatus is ok | partial | unknown_shape | fail_closed.
	ParseStatus string
	// Profile is the pinned profile used for this parse.
	Profile string
	// RawEventName preserves the original hook_event_name even if unknown.
	RawEventName string
}

// CodexHookResponse is the closed PreToolUse stdout JSON for Codex command hooks.
// PermissionRequest uses map[string]any via CodexPermissionResponseFromDecision.
type CodexHookResponse struct {
	Decision           string                `json:"decision,omitempty"` // "block" legacy deny path
	HookSpecificOutput *CodexHookSpecificOut `json:"hookSpecificOutput,omitempty"`
	SystemMessage      string                `json:"systemMessage,omitempty"`
}

// CodexHookSpecificOut is PreToolUse / SessionStart-style hookSpecificOutput.
type CodexHookSpecificOut struct {
	HookEventName            string         `json:"hookEventName"`
	PermissionDecision       string         `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string         `json:"permissionDecisionReason,omitempty"`
	AdditionalContext        string         `json:"additionalContext,omitempty"`
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
}

// CodexPermissionDecision is PermissionRequest decision object.
type CodexPermissionDecision struct {
	Behavior string `json:"behavior"` // allow | deny
	Reason   string `json:"reason,omitempty"`
}

// ParseCodexHookStdin parses and bounds a Codex command-hook stdin payload.
// Malformed/oversized input returns fail_closed ParseStatus and an error.
func ParseCodexHookStdin(raw []byte) (CodexHookInput, error) {
	in := CodexHookInput{Profile: CodexHooksProfileV1, ParseStatus: "fail_closed"}
	if len(raw) == 0 {
		return in, fmt.Errorf("codex hooks: empty stdin")
	}
	if len(raw) > MaxCodexHookStdinBytes {
		return in, fmt.Errorf("codex hooks: stdin exceeds max bytes")
	}
	if !utf8.Valid(raw) {
		return in, fmt.Errorf("codex hooks: invalid utf-8")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return in, fmt.Errorf("codex hooks: malformed json: %w", err)
	}
	in.SessionID = codexFirstString(m, "session_id")
	in.RawEventName = codexFirstString(m, "hook_event_name")
	in.HookEventName = in.RawEventName
	in.Cwd = codexFirstString(m, "cwd")
	in.Model = codexFirstString(m, "model")
	in.PermissionMode = codexFirstString(m, "permission_mode")
	in.TurnID = codexFirstString(m, "turn_id")
	in.Source = codexFirstString(m, "source")
	in.Reason = codexFirstString(m, "reason")
	if p := codexFirstString(m, "prompt"); p != "" {
		in.Prompt = boundRunes(p, MaxCodexContextRunes)
	}

	// Tool fields: tool_name or tool_input.tool_name / tool.name variants.
	in.ToolName = codexFirstString(m, "tool_name", "toolName")
	if in.ToolName == "" {
		if tool, ok := m["tool"].(map[string]any); ok {
			in.ToolName = codexFirstString(tool, "name", "tool_name")
		}
	}
	if ti, ok := m["tool_input"].(map[string]any); ok {
		in.ToolInput = redactToolMap(ti)
		if in.ToolName == "" {
			in.ToolName = codexFirstString(ti, "tool_name", "name")
		}
	} else if ti, ok := m["tool_input"].(string); ok && ti != "" {
		// Some shapes may pass stringified JSON — attempt parse, else ignore.
		var nested map[string]any
		if json.Unmarshal([]byte(ti), &nested) == nil {
			in.ToolInput = redactToolMap(nested)
		}
	}

	if !isKnownCodexEvent(in.HookEventName) {
		in.ParseStatus = "unknown_shape"
		// Unknown events: fail closed for control mapping; caller may no-op allow for observability-only.
		return in, fmt.Errorf("codex hooks: unknown event %q", in.HookEventName)
	}
	if in.SessionID == "" {
		in.ParseStatus = "fail_closed"
		return in, fmt.Errorf("codex hooks: session_id required")
	}
	if _, hosted := codexHostedToolsUnsupported[in.ToolName]; hosted {
		in.UnsupportedHosted = true
	}
	// Hosted tools are not gateable — mark but still parse for honest reporting.
	in.ParseStatus = "ok"
	return in, nil
}

func isKnownCodexEvent(name string) bool {
	switch name {
	case CodexEventSessionStart, CodexEventUserPromptSubmit, CodexEventPreToolUse,
		CodexEventPermissionRequest, CodexEventPostToolUse, CodexEventStop, CodexEventSessionEnd:
		return true
	default:
		return false
	}
}

// ProposedActionFromCodexHook maps a parsed PreToolUse/PermissionRequest tool call.
func ProposedActionFromCodexHook(in CodexHookInput, opts ProposedActionOptions) (ProposedAction, error) {
	if in.SessionID == "" {
		return ProposedAction{}, fmt.Errorf("codex hooks: session_id required")
	}
	tool := in.ToolName
	if tool == "" {
		tool = "unknown"
	}
	pa := ProposedAction{
		SchemaVersion:     ProposedActionSchemaVersion,
		SessionID:         in.SessionID,
		ToolName:          tool,
		ToolClass:         classifyCodexTool(tool),
		Source:            "codex_hooks",
		WorkspaceRevision: opts.WorkspaceRevision,
		ContractRevision:  opts.ContractRevision,
		TargetScope:       append([]string(nil), opts.TargetScope...),
		ParseStatus:       "ok",
	}
	if in.UnsupportedHosted {
		pa.ParseStatus = "unknown_shape"
		pa.ToolClass = ToolClassOther
	}
	if in.ToolInput != nil {
		cmd, args, path, payload, trunc, status := extractToolInputFields(in.ToolInput)
		if path != "" {
			pa.FilePath = boundRunes(path, MaxProposedFilePathRunes)
		}
		if cmd != "" {
			pa.Command = boundRunes(cmd, MaxProposedCommandRunes)
		}
		pa.Arguments = args
		pa.RedactedPayload = payload
		pa.Truncated = trunc
		if status != "ok" {
			pa.ParseStatus = status
		}
		// apply_patch / Edit / Write often put path in tool_input.
		if pa.FilePath == "" {
			if fp := codexFirstString(in.ToolInput, "path", "file_path", "filePath"); fp != "" {
				pa.FilePath = boundRunes(fp, MaxProposedFilePathRunes)
			}
		}
	}
	if opts.ActionID != "" {
		pa.ActionID = opts.ActionID
	} else {
		pa.ActionID = deriveActionID(pa)
	}
	return pa, nil
}

func classifyCodexTool(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch {
	case n == "bash" || n == "shell" || n == "exec_command":
		return ToolClassShell
	case n == "apply_patch" || n == "edit" || n == "write":
		return ToolClassEdit
	case strings.HasPrefix(n, "mcp__"):
		// Local MCP — class other/edit/read based on name fragments.
		if strings.Contains(n, "read") || strings.Contains(n, "list") {
			return ToolClassRead
		}
		if strings.Contains(n, "write") || strings.Contains(n, "edit") {
			return ToolClassEdit
		}
		return ToolClassOther
	case n == "websearch" || n == "web_search":
		return ToolClassSearch
	default:
		return ClassifyToolName(name)
	}
}

// HookRequestFromCodexHook builds a HookRequest for EvaluateHook.
func HookRequestFromCodexHook(in CodexHookInput, proposed *ProposedAction) HookRequest {
	req := HookRequest{
		SessionID: in.SessionID,
		Phase:     "PreTool",
		ToolName:  in.ToolName,
		Proposed:  proposed,
	}
	if proposed != nil {
		req.FilePath = proposed.FilePath
		if req.ToolName == "" {
			req.ToolName = proposed.ToolName
		}
	}
	return req
}

// CodexPreToolResponseFromDecision maps HookDecision → PreToolUse stdout JSON.
// Ordinary deny never sets continue:false (not supported for PreToolUse; would fail the hook).
func CodexPreToolResponseFromDecision(in CodexHookInput, dec HookDecision, challengeID, additionalContext string) CodexHookResponse {
	reason := boundReason(dec.ReasonCode)
	if reason == "" {
		reason = boundReason(dec.Action)
	}
	out := CodexHookResponse{}
	switch dec.Action {
	case HookActionAllow:
		out.HookSpecificOutput = &CodexHookSpecificOut{
			HookEventName:      CodexEventPreToolUse,
			PermissionDecision: "allow",
		}
	case HookActionDeny, HookActionDefer:
		// Deny tool call; for defer/challenge, attach bounded additionalContext.
		out.Decision = "block"
		out.HookSpecificOutput = &CodexHookSpecificOut{
			HookEventName:            CodexEventPreToolUse,
			PermissionDecision:       "deny",
			PermissionDecisionReason: reason,
		}
		ctx := additionalContext
		if challengeID != "" {
			if ctx != "" {
				ctx = ctx + "\n"
			}
			ctx += "ChallengeID=" + challengeID + " — emit a bounded value-vs-cost justification and retry once only after acceptance; do not self-authorize."
		}
		if ctx != "" {
			out.HookSpecificOutput.AdditionalContext = boundRunes(ctx, MaxCodexContextRunes)
		}
	default:
		// Unknown action: fail closed deny.
		out.Decision = "block"
		out.HookSpecificOutput = &CodexHookSpecificOut{
			HookEventName:            CodexEventPreToolUse,
			PermissionDecision:       "deny",
			PermissionDecisionReason: "unsupported_decision",
		}
	}
	// Hosted tools: do not claim gate — return empty allow-through with system note.
	if in.UnsupportedHosted {
		out = CodexHookResponse{
			SystemMessage: "reinframe: hosted tool not covered by Codex hooks; not gated",
			HookSpecificOutput: &CodexHookSpecificOut{
				HookEventName:      CodexEventPreToolUse,
				PermissionDecision: "allow",
			},
		}
	}
	return out
}

// CodexPermissionResponseFromDecision maps to official PermissionRequest stdout.
// Shape (docs 2026-08-06):
//
//	{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow|deny","message":"..."}}}
//
// Fall-through is an empty object (no decision) so the host surfaces the approval prompt.
func CodexPermissionResponseFromDecision(dec HookDecision, fallThrough bool) map[string]any {
	if fallThrough {
		return map[string]any{}
	}
	behavior := "deny"
	msg := boundReason(dec.ReasonCode)
	switch dec.Action {
	case HookActionAllow:
		behavior = "allow"
		msg = ""
	case HookActionDeny, HookActionDefer:
		behavior = "deny"
		if msg == "" {
			msg = "denied_by_policy"
		}
	default:
		behavior = "deny"
		msg = "unsupported_decision"
	}
	decision := map[string]any{"behavior": behavior}
	if msg != "" && behavior == "deny" {
		decision["message"] = msg // official field name is message, not reason
	}
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": CodexEventPermissionRequest,
			"decision":      decision,
		},
	}
}

// CodexFailClosedResponse returns a safe no-op or deny for parse failures by event.
// PreToolUse → deny tool; other events → empty object (do not forge PreToolUse deny).
func CodexFailClosedResponse(rawEventName string) map[string]any {
	switch rawEventName {
	case CodexEventPreToolUse:
		return map[string]any{
			"decision": "block",
			"hookSpecificOutput": map[string]any{
				"hookEventName":            CodexEventPreToolUse,
				"permissionDecision":       "deny",
				"permissionDecisionReason": "parse_fail_closed",
			},
			"systemMessage": "reinframe: hook input rejected",
		}
	case CodexEventPermissionRequest:
		// Fail closed on permission: deny approval request.
		return map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName": CodexEventPermissionRequest,
				"decision": map[string]any{
					"behavior": "deny",
					"message":  "parse_fail_closed",
				},
			},
		}
	default:
		// Non-tool events: empty success (host fail-open for advisory hooks).
		return map[string]any{}
	}
}

// CodexSessionContextResponse returns SessionStart / UserPromptSubmit additionalContext JSON.
func CodexSessionContextResponse(eventName, context string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     eventName,
			"additionalContext": boundRunes(context, MaxCodexContextRunes),
		},
	}
}

// EncodeCodexHookResponse marshals a response with size bounds.
func EncodeCodexHookResponse(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxCodexHookStdoutBytes {
		return nil, fmt.Errorf("codex hooks: stdout exceeds max bytes")
	}
	return raw, nil
}

// CodexHooksCapabilityManifest is the honest control surface when hooks foundation is installed.
type CodexHooksCapabilityManifest struct {
	Profile             string   `json:"profile"`
	ObserveEvents       bool     `json:"observe_events"`
	CapEventStream      bool     `json:"cap_event_stream"`
	CapToolInspection   bool     `json:"cap_tool_inspection"`
	CapHooks            bool     `json:"cap_hooks"`
	CapToolGate         bool     `json:"cap_tool_gate"`
	CapContextInjection bool     `json:"cap_context_injection"`
	CapTurnBoundary     bool     `json:"cap_turn_boundary"`
	CapAdviceDelivery   bool     `json:"cap_advice_delivery"`  // only additionalContext/safe-boundary
	CapPause            bool     `json:"cap_pause"`            // always false for hooks
	CapInterventionAck  bool     `json:"cap_intervention_ack"` // never from hook response alone
	ExplicitAck         bool     `json:"explicit_ack"`
	NegotiatedLevel     int      `json:"negotiated_level"` // 1 tool-gate at most; never 2 from hooks
	UnsupportedHosted   []string `json:"unsupported_hosted_tools"`
	HonestyNote         string   `json:"honesty_note"`
	IntegrationVersion  string   `json:"integration_version"`
}

// CodexHooksFoundationManifest returns the foundation-only capability claim after
// profile validation (not live smoke). Never claims Level 2 or explicit ACK.
func CodexHooksFoundationManifest() CodexHooksCapabilityManifest {
	return CodexHooksCapabilityManifest{
		Profile:             CodexHooksProfileV1,
		ObserveEvents:       true, // JSONL observe remains available
		CapEventStream:      true,
		CapToolInspection:   true,
		CapHooks:            true,
		CapToolGate:         true,
		CapContextInjection: true,
		CapTurnBoundary:     true,
		CapAdviceDelivery:   true, // bounded additionalContext path only
		CapPause:            false,
		CapInterventionAck:  false,
		ExplicitAck:         false,
		NegotiatedLevel:     1,
		UnsupportedHosted:   []string{"WebSearch"},
		HonestyNote: "hook-control foundation only; live Codex proof is separate (#164); " +
			"hook response is not explicit agent ACK; CapToolGate ≠ CapPause; hosted tools not gated",
		IntegrationVersion: CodexHooksProfileV1,
	}
}

// ProfileContentHash hashes the canonical command + profile for trust diagnostics.
func ProfileContentHash(command, profile string) string {
	sum := sha256.Sum256([]byte(profile + "\n" + command))
	return hex.EncodeToString(sum[:16])
}

func codexFirstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				return strings.TrimSpace(t)
			}
		}
	}
	return ""
}

func redactToolMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		sensitive := false
		for _, p := range sensitiveKeyParts {
			if strings.Contains(lk, p) {
				sensitive = true
				break
			}
		}
		if sensitive {
			out[k] = "[redacted]"
			continue
		}
		switch t := v.(type) {
		case string:
			out[k] = boundRunes(t, MaxProposedArgRunes)
		case map[string]any:
			out[k] = redactToolMap(t)
		default:
			out[k] = t
		}
	}
	return out
}
