package adapter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestMapClaudePreToolUseJSON_DenyTool(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"session_id":"s1",
		"tool_name":"Bash",
		"tool_input":{"command":"rm -rf /"},
		"hook_event_name":"PreToolUse"
	}`)
	in, err := adapter.MapClaudePreToolUseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if in.SessionID != "s1" || in.ToolName != "Bash" {
		t.Fatalf("in=%+v", in)
	}
	resp, dec, err := adapter.EvaluateClaudePreTool(context.Background(), in, adapter.ClaudeBridgeConfig{
		Policy: adapter.HookPolicy{
			DeniedTools: map[string]struct{}{"Bash": {}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != adapter.HookActionDeny {
		t.Fatalf("dec=%+v", dec)
	}
	if resp.Decision != "block" {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.HookSpecificOutput == nil || resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("hookSpecific=%+v", resp.HookSpecificOutput)
	}
	if resp.Reinframe == nil || resp.Reinframe.ReasonCode != adapter.ReasonDeniedTool {
		t.Fatalf("meta=%+v", resp.Reinframe)
	}
}

func TestMapClaudePreToolUseJSON_Allow(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"sessionId":"s2","toolName":"Read","tool_input":{"file_path":"README.md"}}`)
	resp, dec, err := adapter.EvaluateClaudePreToolJSON(context.Background(), raw, adapter.ClaudeBridgeConfig{
		Policy: adapter.HookPolicy{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("dec=%+v", dec)
	}
	if resp.Decision != "approve" {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("perm=%s", resp.HookSpecificOutput.PermissionDecision)
	}
}

func TestEvaluateClaudePreTool_DeferPending(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s3", ToolName: "Edit", Phase: "PreTool"}
	resp, dec, err := adapter.EvaluateClaudePreTool(context.Background(), in, adapter.ClaudeBridgeConfig{
		Policy: adapter.HookPolicy{
			PendingAdvisoryInterventionID: "iv-1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("dec=%+v", dec)
	}
	if resp.Decision != "block" || resp.Continue == nil || *resp.Continue {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Reinframe.InterventionID != "iv-1" {
		t.Fatalf("iv=%s", resp.Reinframe.InterventionID)
	}
}

func TestEvaluateClaudePreTool_BeforeToolDisproportionate(t *testing.T) {
	t.Parallel()
	// Mirror M2.1 over-SOP: typo task + criteria met → full suite deny.
	intake, err := adapter.IntakeFromHost(adapter.HostTaskPayload{
		SessionID: "s4",
		Prompt:    "fix typo in README",
	}, adapter.TaskIntakeOptions{BuildContract: true})
	if err != nil {
		t.Fatal(err)
	}
	c := intake.Contract
	c.Complexity = protocol.ComplexitySimple
	c.Risk = protocol.RiskLow
	led := protocol.NewEvidenceLedger(c.TaskID, c.Revision)
	for _, cr := range c.SuccessCriteria {
		led.CriteriaStatus[cr.ID] = protocol.CriterionStatus{CriterionID: cr.ID, Status: "met"}
	}
	in, err := adapter.MapClaudePreToolUseJSON([]byte(
		`{"session_id":"s4","tool_name":"go test -race ./...","tool_input":{}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	eng := policy.NewEngine(policy.EngineConfig{})
	resp, dec, err := adapter.EvaluateClaudePreTool(context.Background(), in, adapter.ClaudeBridgeConfig{
		Evaluate: func(ctx context.Context, req adapter.HookRequest) adapter.HookDecision {
			return eng.EvaluateBeforeTool(ctx, policy.BeforeToolInput{
				Request:    req,
				Contract:   c,
				Ledger:     &led,
				BasePolicy: adapter.HookPolicy{},
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != adapter.HookActionDeny {
		t.Fatalf("want deny, got %+v", dec)
	}
	if dec.ReasonCode != adapter.ReasonDisproportionateScope {
		t.Fatalf("reason=%s", dec.ReasonCode)
	}
	if resp.Decision != "block" {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestClaudeHookResponse_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s", ToolName: "X"}
	dec := adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: adapter.ReasonDeniedTool}
	resp := adapter.ClaudeHookResponseFromDecision(in, dec)
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["decision"] != "block" {
		t.Fatalf("%s", b)
	}
}

func TestMapClaudeUserPromptBridge(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"session_id":"p1","prompt":"fix the flaky test"}`)
	out, err := adapter.MapClaudeUserPromptBridge(raw, adapter.TaskIntakeOptions{BuildContract: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Submitted.SessionID != "p1" || out.Contract == nil {
		t.Fatalf("%+v", out)
	}
}
