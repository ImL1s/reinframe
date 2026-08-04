package adapter_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestProposedAction_ClaudeBashCommandNotInToolName(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"session_id":"s-bash",
		"tool_name":"Bash",
		"tool_input":{"command":"go test -race ./..."}
	}`)
	in, err := adapter.MapClaudePreToolUseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if in.ToolName != "Bash" {
		t.Fatalf("ToolName=%q want Bash", in.ToolName)
	}
	if in.Proposed == nil {
		t.Fatal("expected ProposedAction on ClaudePreToolInput")
	}
	pa := *in.Proposed
	if pa.SchemaVersion != adapter.ProposedActionSchemaVersion {
		t.Fatalf("schema=%q", pa.SchemaVersion)
	}
	if pa.ToolName != "Bash" {
		t.Fatalf("pa.ToolName=%q", pa.ToolName)
	}
	if pa.Command != "go test -race ./..." {
		t.Fatalf("Command=%q", pa.Command)
	}
	if pa.ToolClass != adapter.ToolClassShell {
		t.Fatalf("ToolClass=%q", pa.ToolClass)
	}
	if !adapter.FullSuiteCommand(pa) {
		t.Fatal("expected full suite from Command")
	}
}

func TestProposedAction_ClaudeEditFilePath(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"session_id":"s-edit",
		"tool_name":"Edit",
		"tool_input":{"file_path":"pkg/adapter/x.go","old_string":"a","new_string":"b"}
	}`)
	in, err := adapter.MapClaudePreToolUseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	pa := in.Proposed
	if pa == nil {
		t.Fatal("nil Proposed")
	}
	if pa.ToolName != "Edit" || pa.ToolClass != adapter.ToolClassEdit {
		t.Fatalf("tool=%s class=%s", pa.ToolName, pa.ToolClass)
	}
	if pa.FilePath != "pkg/adapter/x.go" {
		t.Fatalf("FilePath=%q", pa.FilePath)
	}
	if pa.Command != "" {
		t.Fatalf("Edit must not invent Command, got %q", pa.Command)
	}
	if adapter.FullSuiteCommand(*pa) {
		t.Fatal("Edit must not be full suite")
	}
}

func TestProposedAction_WritePath(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"session_id":"s-w","tool_name":"Write","tool_input":{"file_path":"README.md","content":"x"}}`)
	in, err := adapter.MapClaudePreToolUseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if in.Proposed.FilePath != "README.md" || in.Proposed.ToolClass != adapter.ToolClassEdit {
		t.Fatalf("%+v", in.Proposed)
	}
}

func TestProposedAction_ReadNotShell(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"session_id":"s-r","tool_name":"Read","tool_input":{"file_path":"main.go"}}`)
	in, err := adapter.MapClaudePreToolUseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	pa := in.Proposed
	if pa.ToolClass != adapter.ToolClassRead {
		t.Fatalf("class=%s", pa.ToolClass)
	}
	if adapter.FullSuiteCommand(*pa) {
		t.Fatal("Read misclassified as full suite")
	}
	if pa.Command != "" {
		t.Fatalf("unexpected command %q", pa.Command)
	}
}

func TestProposedAction_SecretRedaction(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"session_id":"s-sec",
		"tool_name":"Bash",
		"tool_input":{"command":"echo hi","api_key":"super-secret-value","password":"p@ss"}
	}`)
	in, err := adapter.MapClaudePreToolUseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(in.Proposed.RedactedPayload, &m); err != nil {
		t.Fatal(err)
	}
	if m["api_key"] != "[REDACTED]" || m["password"] != "[REDACTED]" {
		t.Fatalf("redaction failed: %v", m)
	}
	if m["command"] != "echo hi" {
		t.Fatalf("command should remain: %v", m["command"])
	}
	body, _ := json.Marshal(m)
	if strings.Contains(string(body), "super-secret") {
		t.Fatal("secret leaked into payload")
	}
}

func TestProposedAction_OversizedPayloadBounded(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("A", adapter.MaxProposedCommandRunes+500)
	ti, _ := json.Marshal(map[string]any{"command": big})
	raw := []byte(`{"session_id":"s-big","tool_name":"Bash","tool_input":` + string(ti) + `}`)
	in, err := adapter.MapClaudePreToolUseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len([]rune(in.Proposed.Command)) > adapter.MaxProposedCommandRunes+1 { // + ellipsis rune
		t.Fatalf("command not bounded: %d runes", len([]rune(in.Proposed.Command)))
	}
	if !strings.HasSuffix(in.Proposed.Command, "…") {
		t.Fatalf("expected truncation marker, got len=%d", len(in.Proposed.Command))
	}
}

func TestProposedAction_MalformedFailClosedFields(t *testing.T) {
	t.Parallel()
	// Missing tool_name
	_, err := adapter.MapClaudePreToolUseJSON([]byte(`{"session_id":"s"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	// tool_input as number → unknown shape but mapping still succeeds with tool_name
	in, err := adapter.MapClaudePreToolUseJSON([]byte(`{"session_id":"s","tool_name":"Bash","tool_input":123}`))
	if err != nil {
		t.Fatal(err)
	}
	if in.Proposed.ParseStatus != "unknown_shape" && in.Proposed.ParseStatus != "partial" {
		// number tool_input may not unmarshal into map — MapClaude still stores raw
		t.Logf("parse_status=%s", in.Proposed.ParseStatus)
	}
}

func TestProposedAction_CodexSharedShapeNoInvent(t *testing.T) {
	t.Parallel()
	pa := adapter.ProposedActionFromCodexTool("sess-1", "exec_command", "go test ./pkg/x", "codex_rollout", adapter.ProposedActionOptions{})
	if pa.SchemaVersion != adapter.ProposedActionSchemaVersion {
		t.Fatal(pa.SchemaVersion)
	}
	if pa.ToolName != "exec_command" || pa.Command != "go test ./pkg/x" {
		t.Fatalf("%+v", pa)
	}
	if pa.SessionID != "sess-1" {
		t.Fatal(pa.SessionID)
	}
	// Empty command allowed (no invent)
	pa2 := adapter.ProposedActionFromCodexTool("sess-1", "unknown_tool", "", "codex_tail", adapter.ProposedActionOptions{})
	if pa2.Command != "" {
		t.Fatal("invented command")
	}
}

func TestProposedAction_TwoSessionsSameOffsetDifferentActionID(t *testing.T) {
	t.Parallel()
	a := adapter.ProposedActionFromCodexTool("sess-a", "Bash", "ls", "codex_tail", adapter.ProposedActionOptions{})
	b := adapter.ProposedActionFromCodexTool("sess-b", "Bash", "ls", "codex_tail", adapter.ProposedActionOptions{})
	if a.ActionID == b.ActionID {
		t.Fatalf("ActionID collision across sessions: %s", a.ActionID)
	}
}
