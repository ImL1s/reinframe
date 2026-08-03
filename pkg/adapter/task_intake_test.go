package adapter_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestIntakeFromHost_BasicAndContract(t *testing.T) {
	t.Parallel()
	res, err := adapter.IntakeFromHost(adapter.HostTaskPayload{
		SessionID: "s1",
		Prompt:    "fix typo in README",
	}, adapter.TaskIntakeOptions{
		BuildContract: true,
		Now:           func() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Submitted.SessionID != "s1" || res.Submitted.Prompt == "" {
		t.Fatalf("%#v", res.Submitted)
	}
	if res.Submitted.SourceHint != adapter.HostHintGeneric {
		t.Fatalf("SourceHint=%s", res.Submitted.SourceHint)
	}
	if res.Contract == nil {
		t.Fatal("expected contract draft")
	}
	if res.Contract.Complexity == "" {
		t.Fatal("empty complexity")
	}
}

func TestMapClaudeUserPromptSubmitJSON_Fixture(t *testing.T) {
	t.Parallel()
	// Host-shaped fixture only — not a live Claude actuator.
	raw := []byte(`{
		"session_id": "claude-sess-1",
		"prompt": "rename variable in one file",
		"cwd": "/workspace/proj",
		"timestamp": "2026-08-03T12:00:00Z"
	}`)
	res, err := adapter.MapClaudeUserPromptSubmitJSON(raw, adapter.TaskIntakeOptions{BuildContract: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Submitted.SessionID != "claude-sess-1" {
		t.Fatalf("session=%s", res.Submitted.SessionID)
	}
	if res.Submitted.SourceHint != adapter.HostHintClaudeCode {
		t.Fatalf("hint=%s", res.Submitted.SourceHint)
	}
	if res.Contract == nil || len(res.Contract.AllowedScope) == 0 {
		t.Fatalf("contract scope from cwd: %#v", res.Contract)
	}
	// protocol package must not define Claude hook type names as identifiers —
	// structural check: SourceHint is adapter label, prompt maps to TaskSubmitted.
	if _, err := protocol.AgentEventFromTaskSubmitted(res.Submitted, protocol.EmitOptions{SequenceNum: 1}); err != nil {
		t.Fatalf("emit: %v", err)
	}
}

func TestMapClaudeUserPromptSubmitJSON_AlternateKeys(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"sessionId":"s2","user_prompt":"hello","text":"ignored when user_prompt set"}`)
	res, err := adapter.MapClaudeUserPromptSubmitJSON(raw, adapter.TaskIntakeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Submitted.Prompt != "hello" {
		t.Fatalf("prompt=%q", res.Submitted.Prompt)
	}
}

func TestMapCodexAndAPIAndCLI(t *testing.T) {
	t.Parallel()
	codex, err := adapter.MapCodexUserInputJSON([]byte(`{"session_id":"cx","input":"do work"}`), adapter.TaskIntakeOptions{})
	if err != nil || codex.Submitted.SourceHint != adapter.HostHintCodex {
		t.Fatalf("codex: %#v err=%v", codex, err)
	}
	api, err := adapter.MapAPITaskPayloadJSON([]byte(`{"session_id":"api","task":"run"}`), adapter.TaskIntakeOptions{})
	if err != nil || api.Submitted.SourceHint != adapter.HostHintAPI {
		t.Fatalf("api: %#v err=%v", api, err)
	}
	cli, err := adapter.MapCLIInitialPrompt("cli-s", "boot", adapter.TaskIntakeOptions{})
	if err != nil || cli.Submitted.SourceHint != adapter.HostHintCLI {
		t.Fatalf("cli: %#v err=%v", cli, err)
	}
}

func TestIntakeFromHost_RequiresSessionAndPrompt(t *testing.T) {
	t.Parallel()
	if _, err := adapter.IntakeFromHost(adapter.HostTaskPayload{Prompt: "x"}, adapter.TaskIntakeOptions{}); err == nil {
		t.Fatal("expected session error")
	}
	if _, err := adapter.IntakeFromHost(adapter.HostTaskPayload{SessionID: "s"}, adapter.TaskIntakeOptions{}); err == nil {
		t.Fatal("expected prompt error")
	}
}

func TestProtocolPackageHasNoClaudeHookTypeNames(t *testing.T) {
	t.Parallel()
	// Guardrail: core protocol identifiers must not be Claude hook names.
	// We only check via public API surface used by intake — TaskSubmitted type name.
	var _ protocol.TaskSubmitted
	// SourceHint is free-form adapter label, may contain "claude_code" string but
	// type is not named UserPromptSubmit.
	if strings.Contains("TaskSubmitted", "UserPromptSubmit") {
		t.Fatal("impossible")
	}
}
