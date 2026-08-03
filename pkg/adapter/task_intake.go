package adapter

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// HostHint labels the origin adapter for TaskSubmitted.SourceHint.
// These are adapter-layer labels only — not protocol enums and not Claude type names.
const (
	HostHintClaudeCode = "adapter:claude_code"
	HostHintCodex      = "adapter:codex"
	HostHintAPI        = "adapter:api"
	HostHintCLI        = "adapter:cli"
	HostHintGeneric    = "adapter:generic"
)

// TaskIntakeResult is the output of host → core task intake (#84).
type TaskIntakeResult struct {
	Submitted protocol.TaskSubmitted
	// Contract is a provisional draft from BuildContractFromSubmitted when BuildContract is true.
	Contract *protocol.TaskContract
}

// TaskIntakeOptions configures IntakeFromHost.
type TaskIntakeOptions struct {
	// BuildContract when true attaches a heuristic TaskContract draft.
	BuildContract bool
	ContractOpts  protocol.BuildContractOptions
	// Now overrides SubmittedAt when zero and for contract builder.
	Now func() time.Time
}

// HostTaskPayload is the harness-agnostic intake surface for adapters.
// Host-specific mappers fill this struct; core never sees host hook type names.
type HostTaskPayload struct {
	// SessionID required.
	SessionID string
	// TaskID optional; generated when empty.
	TaskID string
	// Prompt is the user request text (required).
	Prompt string
	// ParentRevision for contract lineage (0 = first).
	ParentRevision int
	// SubmittedAt optional; defaults to now.
	SubmittedAt time.Time
	// SourceHint is an adapter label (e.g. HostHintClaudeCode).
	SourceHint string
}

// IntakeFromHost maps a generic host payload to TaskSubmitted (+ optional contract).
func IntakeFromHost(p HostTaskPayload, opts TaskIntakeOptions) (TaskIntakeResult, error) {
	if p.SessionID == "" {
		return TaskIntakeResult{}, fmt.Errorf("session_id is required")
	}
	prompt := p.Prompt
	if prompt == "" {
		return TaskIntakeResult{}, fmt.Errorf("prompt is required")
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	submittedAt := p.SubmittedAt
	if submittedAt.IsZero() {
		submittedAt = now
	}
	taskID := p.TaskID
	if taskID == "" {
		taskID = fmt.Sprintf("task-%s-%d", p.SessionID, submittedAt.UnixNano())
	}
	hint := p.SourceHint
	if hint == "" {
		hint = HostHintGeneric
	}
	sub := protocol.TaskSubmitted{
		TaskID:         taskID,
		SessionID:      p.SessionID,
		Prompt:         prompt,
		ParentRevision: p.ParentRevision,
		SubmittedAt:    submittedAt.UTC(),
		SourceHint:     hint,
	}
	out := TaskIntakeResult{Submitted: sub}
	if opts.BuildContract {
		co := opts.ContractOpts
		if co.Now == nil && opts.Now != nil {
			co.Now = opts.Now
		}
		c := protocol.BuildContractFromSubmitted(sub, co)
		out.Contract = &c
	}
	return out, nil
}

// MapClaudeUserPromptSubmitJSON maps a Claude Code UserPromptSubmit-shaped JSON
// object to core TaskSubmitted. Field names here are fixture/host-only and must
// not appear as protocol package type identifiers.
//
// Expected loose shape (any subset of common keys):
//
//	{"session_id"|"sessionId", "prompt"|"user_prompt"|"text", "cwd"?, "timestamp"?}
//
// This is a **fixture mapper** for tests and adapter stubs — not a live Claude actuator.
func MapClaudeUserPromptSubmitJSON(raw []byte, opts TaskIntakeOptions) (TaskIntakeResult, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return TaskIntakeResult{}, fmt.Errorf("claude prompt json: %w", err)
	}
	p := HostTaskPayload{SourceHint: HostHintClaudeCode}
	p.SessionID = firstString(m, "session_id", "sessionId", "sessionID")
	p.Prompt = firstString(m, "prompt", "user_prompt", "userPrompt", "text", "message")
	p.TaskID = firstString(m, "task_id", "taskId")
	if ts := firstString(m, "timestamp", "submitted_at", "submittedAt"); ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			p.SubmittedAt = t
		} else if t, err := time.Parse(time.RFC3339, ts); err == nil {
			p.SubmittedAt = t
		}
	}
	// cwd may inform AllowedScope when building a contract.
	if opts.BuildContract {
		if cwd := firstString(m, "cwd", "workspace", "workspace_path"); cwd != "" {
			opts.ContractOpts.AllowedScope = append(opts.ContractOpts.AllowedScope, cwd)
		}
	}
	return IntakeFromHost(p, opts)
}

// MapCodexUserInputJSON maps a Codex-shaped user input payload to TaskSubmitted.
func MapCodexUserInputJSON(raw []byte, opts TaskIntakeOptions) (TaskIntakeResult, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return TaskIntakeResult{}, fmt.Errorf("codex input json: %w", err)
	}
	p := HostTaskPayload{SourceHint: HostHintCodex}
	p.SessionID = firstString(m, "session_id", "sessionId", "thread_id")
	p.Prompt = firstString(m, "prompt", "input", "text", "message")
	p.TaskID = firstString(m, "task_id", "id")
	return IntakeFromHost(p, opts)
}

// MapAPITaskPayloadJSON maps a generic API task payload to TaskSubmitted.
func MapAPITaskPayloadJSON(raw []byte, opts TaskIntakeOptions) (TaskIntakeResult, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return TaskIntakeResult{}, fmt.Errorf("api task json: %w", err)
	}
	p := HostTaskPayload{SourceHint: HostHintAPI}
	p.SessionID = firstString(m, "session_id", "sessionId")
	p.Prompt = firstString(m, "prompt", "task", "text")
	p.TaskID = firstString(m, "task_id", "taskId", "id")
	return IntakeFromHost(p, opts)
}

// MapCLIInitialPrompt maps a CLI initial prompt (+ session id) to TaskSubmitted.
func MapCLIInitialPrompt(sessionID, prompt string, opts TaskIntakeOptions) (TaskIntakeResult, error) {
	return IntakeFromHost(HostTaskPayload{
		SessionID:  sessionID,
		Prompt:     prompt,
		SourceHint: HostHintCLI,
	}, opts)
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if t != "" {
					return t
				}
			}
		}
	}
	return ""
}
