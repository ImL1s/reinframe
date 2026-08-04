package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ProposedActionSchemaVersion is the closed wire version for host→core action projection (#115).
const ProposedActionSchemaVersion = "reinframe.proposed_action.v1"

// Bounds for ProposedAction payloads (fail closed on oversize after truncation markers).
const (
	MaxProposedCommandRunes  = 4000
	MaxProposedArgRunes      = 1000
	MaxProposedArgs          = 32
	MaxProposedPayloadBytes  = 4096
	MaxProposedFilePathRunes = 1024
	MaxProposedActionIDRunes = 128
)

// Tool class constants for ProposedAction.ToolClass.
const (
	ToolClassShell   = "shell"
	ToolClassEdit    = "edit"
	ToolClassRead    = "read"
	ToolClassSearch  = "search"
	ToolClassUnknown = "unknown"
	ToolClassOther   = "other"
)

// ProposedAction is the canonical, versioned projection of a host tool call.
// ToolName is the host tool identifier (e.g. "Bash"); Command holds shell text when present.
// Policy and future classifiers must use Command for shell semantics — never stuff the
// full command into ToolName.
type ProposedAction struct {
	SchemaVersion     string   `json:"schema_version"`
	SessionID         string   `json:"session_id"`
	ActionID          string   `json:"action_id"`
	ToolName          string   `json:"tool_name"`
	ToolClass         string   `json:"tool_class"`
	Command           string   `json:"command,omitempty"`
	Arguments         []string `json:"arguments,omitempty"`
	FilePath          string   `json:"file_path,omitempty"`
	TargetScope       []string `json:"target_scope,omitempty"`
	WorkspaceRevision string   `json:"workspace_revision,omitempty"`
	ContractRevision  int      `json:"contract_revision,omitempty"`
	// RedactedPayload is a bounded JSON object after secret redaction (may be empty object).
	RedactedPayload json.RawMessage `json:"redacted_payload,omitempty"`
	// Source is provenance: claude_pretool | codex_rollout | codex_tail | synthetic | unknown.
	Source string `json:"source,omitempty"`
	// Truncated is true when any field was bounded.
	Truncated bool `json:"truncated,omitempty"`
	// ParseStatus is "ok" | "partial" | "unknown_shape" | "fail_closed".
	ParseStatus string `json:"parse_status,omitempty"`
}

// ProposedActionOptions configures mapping / redaction.
type ProposedActionOptions struct {
	// WorkspaceRevision / ContractRevision optional context from the supervisor.
	WorkspaceRevision string
	ContractRevision  int
	TargetScope       []string
	// ActionID when empty is derived deterministically from session+tool+command+path.
	ActionID string
}

// sensitiveKey substrings (lower) redacted from tool_input objects.
var sensitiveKeyParts = []string{
	"password", "secret", "token", "api_key", "apikey", "authorization",
	"auth", "credential", "private_key", "privatekey", "cookie", "session_key",
}

// ProposedActionFromClaudePreTool builds ProposedAction from mapped Claude PreTool input + raw tool_input.
// toolInput may be nil; when present it is redacted and bounded into RedactedPayload.
func ProposedActionFromClaudePreTool(in ClaudePreToolInput, toolInput any, opts ProposedActionOptions) (ProposedAction, error) {
	if strings.TrimSpace(in.SessionID) == "" {
		return ProposedAction{}, fmt.Errorf("proposed_action: session_id required")
	}
	if strings.TrimSpace(in.ToolName) == "" {
		return ProposedAction{}, fmt.Errorf("proposed_action: tool_name required")
	}
	pa := ProposedAction{
		SchemaVersion:     ProposedActionSchemaVersion,
		SessionID:         in.SessionID,
		ToolName:          in.ToolName,
		ToolClass:         ClassifyToolName(in.ToolName),
		FilePath:          boundRunes(in.FilePath, MaxProposedFilePathRunes),
		Source:            "claude_pretool",
		WorkspaceRevision: opts.WorkspaceRevision,
		ContractRevision:  opts.ContractRevision,
		TargetScope:       append([]string(nil), opts.TargetScope...),
		ParseStatus:       "ok",
	}
	if toolInput != nil {
		cmd, args, path, payload, trunc, status := extractToolInputFields(toolInput)
		if path != "" && pa.FilePath == "" {
			pa.FilePath = boundRunes(path, MaxProposedFilePathRunes)
		}
		pa.Command = boundRunes(cmd, MaxProposedCommandRunes)
		pa.Arguments = args
		pa.RedactedPayload = payload
		pa.Truncated = trunc || pa.Truncated
		if status != "ok" {
			pa.ParseStatus = status
		}
		// Never promote command into ToolName.
		if pa.ToolName != in.ToolName {
			return ProposedAction{}, fmt.Errorf("proposed_action: tool_name must not be overwritten")
		}
	} else if in.RawToolInput != "" {
		// Best-effort: RawToolInput may be JSON string already truncated by mapper.
		var v any
		if err := json.Unmarshal([]byte(in.RawToolInput), &v); err == nil {
			cmd, args, path, payload, trunc, status := extractToolInputFields(v)
			if path != "" && pa.FilePath == "" {
				pa.FilePath = boundRunes(path, MaxProposedFilePathRunes)
			}
			pa.Command = boundRunes(cmd, MaxProposedCommandRunes)
			pa.Arguments = args
			pa.RedactedPayload = payload
			pa.Truncated = trunc
			if status != "ok" {
				pa.ParseStatus = status
			}
		} else {
			pa.ParseStatus = "unknown_shape"
			pa.RedactedPayload = json.RawMessage(`{}`)
		}
	}
	if opts.ActionID != "" {
		pa.ActionID = boundRunes(opts.ActionID, MaxProposedActionIDRunes)
	} else {
		pa.ActionID = deriveActionID(pa)
	}
	return pa, nil
}

// ProposedActionFromCodexTool projects a Codex tool/exec observation without inventing fields.
// command may be empty when unknown; toolName should be the observed tool/name.
func ProposedActionFromCodexTool(sessionID, toolName, command, source string, opts ProposedActionOptions) ProposedAction {
	if sessionID == "" {
		sessionID = "codex-unknown"
	}
	if toolName == "" {
		toolName = "unknown"
	}
	if source == "" {
		source = "codex_rollout"
	}
	pa := ProposedAction{
		SchemaVersion:     ProposedActionSchemaVersion,
		SessionID:         sessionID,
		ToolName:          toolName,
		ToolClass:         ClassifyToolName(toolName),
		Command:           boundRunes(command, MaxProposedCommandRunes),
		Source:            source,
		WorkspaceRevision: opts.WorkspaceRevision,
		ContractRevision:  opts.ContractRevision,
		ParseStatus:       "ok",
	}
	if opts.ActionID != "" {
		pa.ActionID = boundRunes(opts.ActionID, MaxProposedActionIDRunes)
	} else {
		pa.ActionID = deriveActionID(pa)
	}
	return pa
}

// ClassifyToolName maps host tool names to ToolClass (deterministic).
func ClassifyToolName(toolName string) string {
	n := strings.TrimSpace(toolName)
	switch n {
	case "Bash", "bash", "Shell", "shell", "terminal", "Terminal":
		return ToolClassShell
	case "Edit", "Write", "MultiEdit", "NotebookEdit", "create_file", "StrReplace":
		return ToolClassEdit
	case "Read", "read_file", "NotebookRead":
		return ToolClassRead
	case "Grep", "Glob", "Search", "SemanticSearch":
		return ToolClassSearch
	case "":
		return ToolClassUnknown
	default:
		// If someone incorrectly put a full suite command in tool_name historically,
		// still classify as other — Command field holds shell text when mapped correctly.
		return ToolClassOther
	}
}

// FullSuiteCommand reports whether the proposed action is a full-repo test suite.
// Prefer Command for shell tools; never treat a bare ToolName of "Bash" as full suite.
func FullSuiteCommand(pa ProposedAction) bool {
	if pa.ToolClass == ToolClassShell || pa.Command != "" {
		return isFullSuiteText(pa.Command)
	}
	// Legacy fallback: only if ToolName itself looks like a command string (not a host tool id).
	if looksLikeCommandString(pa.ToolName) {
		return isFullSuiteText(pa.ToolName)
	}
	return false
}

func looksLikeCommandString(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// Host tool ids are short identifiers without spaces/flags typically.
	if strings.Contains(s, " ") || strings.Contains(s, "./") || strings.Contains(s, "-") && strings.Contains(s, "test") {
		return true
	}
	return false
}

func isFullSuiteText(s string) bool {
	n := strings.ToLower(strings.TrimSpace(s))
	if n == "" {
		return false
	}
	if strings.Contains(n, "./...") {
		return true
	}
	if strings.Contains(n, "go test") && strings.Contains(n, "-race") && strings.Contains(n, "./") {
		return true
	}
	return false
}

// HookRequestFromProposedAction builds the narrow HookRequest for EvaluateHook.
// ToolName remains the host tool id; FilePath from the projection.
func HookRequestFromProposedAction(pa ProposedAction, phase string) HookRequest {
	if phase == "" {
		phase = "PreTool"
	}
	return HookRequest{
		SessionID: pa.SessionID,
		Phase:     phase,
		ToolName:  pa.ToolName,
		FilePath:  pa.FilePath,
		// Command is available for policy via ProposedAction pointer on extended paths.
	}
}

func deriveActionID(pa ProposedAction) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|%s|%s", pa.SessionID, pa.ToolName, pa.Command, pa.FilePath, pa.Source)
	sum := hex.EncodeToString(h.Sum(nil))
	if len(sum) > 16 {
		sum = sum[:16]
	}
	return "pa-" + sum
}

func extractToolInputFields(toolInput any) (command string, args []string, filePath string, payload json.RawMessage, truncated bool, status string) {
	status = "ok"
	switch t := toolInput.(type) {
	case map[string]any:
		command = firstStringMap(t, "command", "cmd")
		filePath = firstStringMap(t, "file_path", "filePath", "path")
		if rawArgs, ok := t["arguments"]; ok {
			args = normalizeArgs(rawArgs)
		} else if rawArgs, ok := t["args"]; ok {
			args = normalizeArgs(rawArgs)
		}
		// Also accept command as array joined
		if command == "" {
			if arr, ok := t["command"].([]any); ok {
				parts := make([]string, 0, len(arr))
				for _, a := range arr {
					if s, ok := a.(string); ok {
						parts = append(parts, s)
					}
				}
				command = strings.Join(parts, " ")
			}
		}
		redacted, trunc := redactMap(t)
		b, err := json.Marshal(redacted)
		if err != nil {
			status = "unknown_shape"
			payload = json.RawMessage(`{}`)
			return
		}
		if len(b) > MaxProposedPayloadBytes {
			b = b[:MaxProposedPayloadBytes]
			trunc = true
			// best-effort close JSON
			b = append(b, []byte(`…"}`)...)
		}
		payload = b
		truncated = trunc
		return
	case string:
		// Opaque string tool_input: treat as command only for shell-like freeform.
		command = t
		payload = json.RawMessage(`{}`)
		status = "partial"
		return
	default:
		status = "unknown_shape"
		payload = json.RawMessage(`{}`)
		return
	}
}

func normalizeArgs(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for i, a := range t {
			if i >= MaxProposedArgs {
				break
			}
			if s, ok := a.(string); ok {
				out = append(out, boundRunes(s, MaxProposedArgRunes))
			} else {
				b, _ := json.Marshal(a)
				out = append(out, boundRunes(string(b), MaxProposedArgRunes))
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(t))
		for i, s := range t {
			if i >= MaxProposedArgs {
				break
			}
			out = append(out, boundRunes(s, MaxProposedArgRunes))
		}
		return out
	case string:
		if t == "" {
			return nil
		}
		return []string{boundRunes(t, MaxProposedArgRunes)}
	default:
		return nil
	}
}

func redactMap(m map[string]any) (map[string]any, bool) {
	out := make(map[string]any, len(m))
	trunc := false
	for k, v := range m {
		lk := strings.ToLower(k)
		if isSensitiveKey(lk) {
			out[k] = "[REDACTED]"
			continue
		}
		switch child := v.(type) {
		case map[string]any:
			cm, ct := redactMap(child)
			out[k] = cm
			trunc = trunc || ct
		case string:
			out[k] = boundRunes(child, MaxProposedCommandRunes)
			if utf8.RuneCountInString(child) > MaxProposedCommandRunes {
				trunc = true
			}
		default:
			out[k] = v
		}
	}
	return out, trunc
}

func isSensitiveKey(lk string) bool {
	for _, p := range sensitiveKeyParts {
		if strings.Contains(lk, p) {
			return true
		}
	}
	return false
}

func firstStringMap(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return t
				}
			}
		}
	}
	return ""
}

func boundRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
