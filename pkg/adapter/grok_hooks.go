package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Pinned Grok Build native hooks profile (user-guide/docs retrieved 2026-08-06).
// Sources: ~/.grok/docs/user-guide/10-hooks.md, 22-permissions-and-safety.md
// Official: https://docs.x.ai/build/features/hooks
const (
	GrokHooksProfileV1     = "reinframe.grok_build_hooks.2026-08-06.v1"
	GrokHookOwnerKey       = "reinframe_owner"
	GrokHookOwnerValue     = "reinframe"
	GrokHookSchemaKey      = "reinframe_schema_version"
	GrokHookSchemaVersion  = "reinframe.grok_hook_entry.v1"
	GrokHookProfileHashKey = "reinframe_profile_hash"
	MaxGrokHookStdinBytes  = 1 << 20
	MaxGrokHookStdoutBytes = 64 << 10
	MaxGrokContextRunes    = 2000
)

// Grok hook event names (canonical). Host may send snake_case/camelCase aliases.
const (
	GrokEventSessionStart     = "SessionStart"
	GrokEventUserPromptSubmit = "UserPromptSubmit"
	GrokEventPreToolUse       = "PreToolUse"
	GrokEventPostToolUse      = "PostToolUse"
	GrokEventStop             = "Stop"
	GrokEventSessionEnd       = "SessionEnd"
)

// HostOutcome records whether the host actually enforced a deny (hooks fail-open on crash/timeout/malformed).
type HostOutcome string

const (
	HostOutcomeEnforcedDeny HostOutcome = "enforced_deny"
	HostOutcomeAllowed      HostOutcome = "allowed"
	HostOutcomeFailOpen     HostOutcome = "host_fail_open"
	HostOutcomeUnknown      HostOutcome = "unknown"
)

// GrokHookInput is the closed parse of a Grok Build command-hook stdin payload.
type GrokHookInput struct {
	SessionID      string
	HookEventName  string
	Cwd            string
	WorkspaceRoot  string
	PermissionMode string
	ToolName       string
	ToolInput      map[string]any
	ParseStatus    string
	Profile        string
	RawEventName   string
}

// GrokPreToolResponse is PreToolUse stdout JSON.
// Allow: {"decision":"allow"}  Deny: {"decision":"deny","reason":"..."}
type GrokPreToolResponse struct {
	Decision string `json:"decision"` // allow | deny
	Reason   string `json:"reason,omitempty"`
}

// ParseGrokHookStdin parses Grok Build hook stdin (camelCase or snake_case keys).
func ParseGrokHookStdin(raw []byte) (GrokHookInput, error) {
	in := GrokHookInput{Profile: GrokHooksProfileV1, ParseStatus: "fail_closed"}
	if len(raw) == 0 {
		return in, fmt.Errorf("grok hooks: empty stdin")
	}
	if len(raw) > MaxGrokHookStdinBytes {
		return in, fmt.Errorf("grok hooks: stdin exceeds max bytes")
	}
	if !utf8.Valid(raw) {
		return in, fmt.Errorf("grok hooks: invalid utf-8")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return in, fmt.Errorf("grok hooks: malformed json: %w", err)
	}
	in.SessionID = grokFirstString(m, "sessionId", "session_id")
	in.RawEventName = grokFirstString(m, "hookEventName", "hook_event_name")
	in.HookEventName = normalizeGrokEvent(in.RawEventName)
	in.Cwd = grokFirstString(m, "cwd")
	in.WorkspaceRoot = grokFirstString(m, "workspaceRoot", "workspace_root")
	in.PermissionMode = grokFirstString(m, "permissionMode", "permission_mode")
	in.ToolName = grokFirstString(m, "toolName", "tool_name")
	if ti, ok := m["toolInput"].(map[string]any); ok {
		in.ToolInput = redactToolMap(ti)
	} else if ti, ok := m["tool_input"].(map[string]any); ok {
		in.ToolInput = redactToolMap(ti)
	}
	if in.SessionID == "" {
		return in, fmt.Errorf("grok hooks: sessionId required")
	}
	if in.HookEventName == "" {
		in.ParseStatus = "unknown_shape"
		return in, fmt.Errorf("grok hooks: unknown event %q", in.RawEventName)
	}
	in.ParseStatus = "ok"
	return in, nil
}

func normalizeGrokEvent(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sessionstart", "session_start":
		return GrokEventSessionStart
	case "userpromptsubmit", "user_prompt_submit", "beforesubmitprompt":
		return GrokEventUserPromptSubmit
	case "pretooluse", "pre_tool_use", "beforeshellexecution", "beforemcpexecution":
		return GrokEventPreToolUse
	case "posttooluse", "post_tool_use", "aftershellexecution", "afterfileedit":
		return GrokEventPostToolUse
	case "stop":
		return GrokEventStop
	case "sessionend", "session_end":
		return GrokEventSessionEnd
	default:
		// Accept canonical PascalCase if already correct
		switch name {
		case GrokEventSessionStart, GrokEventUserPromptSubmit, GrokEventPreToolUse,
			GrokEventPostToolUse, GrokEventStop, GrokEventSessionEnd:
			return name
		}
		return ""
	}
}

// ProposedActionFromGrokHook maps tool events to ProposedAction.
func ProposedActionFromGrokHook(in GrokHookInput, opts ProposedActionOptions) (ProposedAction, error) {
	if in.SessionID == "" {
		return ProposedAction{}, fmt.Errorf("grok hooks: sessionId required")
	}
	tool := in.ToolName
	if tool == "" {
		tool = "unknown"
	}
	// Normalize Grok native names to classifier-friendly classes.
	pa := ProposedAction{
		SchemaVersion:     ProposedActionSchemaVersion,
		SessionID:         in.SessionID,
		ToolName:          tool,
		ToolClass:         classifyGrokTool(tool),
		Source:            "grok_hooks",
		WorkspaceRevision: opts.WorkspaceRevision,
		ContractRevision:  opts.ContractRevision,
		TargetScope:       append([]string(nil), opts.TargetScope...),
		ParseStatus:       "ok",
	}
	if in.ToolInput != nil {
		cmd, args, path, payload, trunc, status := extractToolInputFields(in.ToolInput)
		if cmd != "" {
			pa.Command = boundRunes(cmd, MaxProposedCommandRunes)
		}
		pa.Arguments = args
		if path != "" {
			pa.FilePath = boundRunes(path, MaxProposedFilePathRunes)
		}
		pa.RedactedPayload = payload
		pa.Truncated = trunc
		if status != "ok" {
			pa.ParseStatus = status
		}
	}
	if opts.ActionID != "" {
		pa.ActionID = opts.ActionID
	} else {
		pa.ActionID = deriveActionID(pa)
	}
	return pa, nil
}

func classifyGrokTool(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "bash", "run_terminal_command", "shell":
		return ToolClassShell
	case "edit", "write", "multiedit", "search_replace", "apply_patch":
		return ToolClassEdit
	case "read", "read_file":
		return ToolClassRead
	case "grep", "glob", "listdir", "list_dir":
		return ToolClassSearch
	case "websearch", "web_search":
		return ToolClassSearch
	default:
		if strings.Contains(n, "__") {
			return ToolClassOther // MCP-like server__tool
		}
		return ClassifyToolName(name)
	}
}

// HookRequestFromGrokHook builds HookRequest for EvaluateHook.
func HookRequestFromGrokHook(in GrokHookInput, proposed *ProposedAction) HookRequest {
	req := HookRequest{SessionID: in.SessionID, Phase: "PreTool", ToolName: in.ToolName, Proposed: proposed}
	if proposed != nil {
		req.FilePath = proposed.FilePath
		if req.ToolName == "" {
			req.ToolName = proposed.ToolName
		}
	}
	return req
}

// GrokPreToolResponseFromDecision maps HookDecision → native PreToolUse stdout.
// Only explicit valid deny blocks; challenge/defer also emits deny + reason with ChallengeID.
func GrokPreToolResponseFromDecision(dec HookDecision, challengeID string) GrokPreToolResponse {
	switch dec.Action {
	case HookActionAllow:
		return GrokPreToolResponse{Decision: "allow"}
	case HookActionDeny, HookActionDefer:
		reason := boundReason(dec.ReasonCode)
		if challengeID != "" {
			if reason != "" {
				reason += "; "
			}
			reason += "ChallengeID=" + challengeID + " — emit bounded value-vs-cost justification; retry once only after acceptance; do not self-authorize"
		}
		if reason == "" {
			reason = "denied_by_policy"
		}
		return GrokPreToolResponse{Decision: "deny", Reason: boundRunes(reason, MaxGrokContextRunes)}
	default:
		return GrokPreToolResponse{Decision: "deny", Reason: "unsupported_decision"}
	}
}

// RecordHostHookOutcome models host fail-open vs enforced deny for observability.
// intendedDeny is Reinframe's decision; exitCode and stdoutValid describe host execution.
// Official: timeout/crash/malformed = fail-open; only explicit valid deny blocks.
func RecordHostHookOutcome(intendedDeny bool, exitCode int, stdoutValidDeny bool) HostOutcome {
	if stdoutValidDeny {
		return HostOutcomeEnforcedDeny
	}
	// Exit 2 is documented as explicit deny for PreToolUse (stderr as feedback).
	if exitCode == 2 {
		return HostOutcomeEnforcedDeny
	}
	if intendedDeny {
		// Intended deny but host did not surface deny JSON or exit 2 -> fail-open.
		return HostOutcomeFailOpen
	}
	if exitCode == 0 {
		return HostOutcomeAllowed
	}
	// Nonzero non-2 without deny JSON: fail-open
	return HostOutcomeFailOpen
}

// EncodeGrokHookResponse marshals with size bounds.
func EncodeGrokHookResponse(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxGrokHookStdoutBytes {
		return nil, fmt.Errorf("grok hooks: stdout exceeds max bytes")
	}
	return raw, nil
}

// GrokHooksFoundationManifest is honest hook-only capability claim.
type GrokHooksFoundationManifest struct {
	Profile             string `json:"profile"`
	CapEventStream      bool   `json:"cap_event_stream"`
	CapToolInspection   bool   `json:"cap_tool_inspection"`
	CapHooks            bool   `json:"cap_hooks"`
	CapToolGate         bool   `json:"cap_tool_gate"`
	CapTurnBoundary     bool   `json:"cap_turn_boundary"`
	CapAdviceDelivery   bool   `json:"cap_advice_delivery"`
	CapContextInjection bool   `json:"cap_context_injection"`
	CapPause            bool   `json:"cap_pause"`
	CapInterventionAck  bool   `json:"cap_intervention_ack"`
	ExplicitAck         bool   `json:"explicit_ack"`
	FailClosedHooks     bool   `json:"fail_closed_hooks"` // always false — host fails open
	NegotiatedLevel     int    `json:"negotiated_level"`
	HonestyNote         string `json:"honesty_note"`
	IntegrationVersion  string `json:"integration_version"`
}

// GrokHooksFoundationManifest returns foundation claims (not live smoke).
func NewGrokHooksFoundationManifest() GrokHooksFoundationManifest {
	return GrokHooksFoundationManifest{
		Profile:             GrokHooksProfileV1,
		CapEventStream:      true,
		CapToolInspection:   true,
		CapHooks:            true,
		CapToolGate:         true, // only when explicit valid deny is returned
		CapTurnBoundary:     true,
		CapAdviceDelivery:   false, // ACP issue #166
		CapContextInjection: false,
		CapPause:            false,
		CapInterventionAck:  false,
		ExplicitAck:         false,
		FailClosedHooks:     false,
		NegotiatedLevel:     1,
		HonestyNote: "native hook foundation; host timeout/crash/malformed remain fail-open; " +
			"only explicit valid PreToolUse deny blocks; not Level 2; not explicit ACK; ACP is #166",
		IntegrationVersion: GrokHooksProfileV1,
	}
}

// GrokProfileContentHash hashes command+profile for trust diagnostics.
func GrokProfileContentHash(command, profile string) string {
	sum := sha256.Sum256([]byte(profile + "\n" + command))
	return hex.EncodeToString(sum[:16])
}

func grokFirstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
