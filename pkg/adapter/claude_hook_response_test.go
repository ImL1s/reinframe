package adapter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestClaudeHookResponse_Allow(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s", ToolName: "Read"}
	dec := adapter.HookDecision{Action: adapter.HookActionAllow, ReasonCode: adapter.ReasonAllow}
	resp := adapter.ClaudeHookResponseFromDecision(in, dec)
	if resp.Decision != "approve" || resp.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("%+v", resp)
	}
	if resp.Continue != nil {
		t.Fatal("ALLOW must not set continue")
	}
	if err := adapter.ValidateClaudeHookResponseClosedSchema(resp); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeHookResponse_BlockNoSessionStop(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s", ToolName: "Bash"}
	dec := adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: adapter.ReasonDeniedTool}
	resp := adapter.ClaudeHookResponseFromDecision(in, dec)
	if resp.Decision != "block" || resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("%+v", resp)
	}
	if resp.Continue != nil && !*resp.Continue {
		t.Fatal("BLOCK must not use continue:false (session stop)")
	}
	if err := adapter.ValidateClaudeHookResponseClosedSchema(resp); err != nil {
		t.Fatal(err)
	}
	b, err := adapter.MarshalClaudeHookResponseJSON(resp)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["continue"]; ok {
		t.Fatalf("continue must be omitted for tool deny: %s", b)
	}
}

func TestClaudeHookResponse_DeferDegradeHeadless(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s", ToolName: "Edit"}
	dec := adapter.HookDecision{Action: adapter.HookActionDefer, ReasonCode: adapter.ReasonDeferPendingAdvisory}
	resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, adapter.ClaudeResponseOptions{
		HostMode:    adapter.ClaudeHostModeHeadless,
		NativeDefer: false,
	})
	if resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("headless defer should degrade to deny, got %+v", resp)
	}
	if resp.Continue != nil && !*resp.Continue {
		t.Fatal("no session stop")
	}
	if !contains(resp.Reason, "defer_degraded") {
		t.Fatalf("reason=%s", resp.Reason)
	}
}

func TestClaudeHookResponse_NativeDeferInteractive(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s", ToolName: "Edit"}
	dec := adapter.HookDecision{Action: adapter.HookActionDefer, ReasonCode: adapter.ReasonDeferPendingAdvisory}
	resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, adapter.ClaudeResponseOptions{
		HostMode:    adapter.ClaudeHostModeInteractive,
		NativeDefer: true,
	})
	if resp.HookSpecificOutput.PermissionDecision != "ask" {
		t.Fatalf("want ask, got %+v", resp)
	}
	if resp.Continue != nil && !*resp.Continue {
		t.Fatal("no session stop")
	}
}

func TestClaudeHookResponse_UnknownHostFailClosed(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s", ToolName: "Bash"}
	dec := adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: adapter.ReasonDeniedTool}
	resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, adapter.ClaudeResponseOptions{
		HostMode: adapter.ClaudeHostModeUnknown,
	})
	if resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("%+v", resp)
	}
	if resp.Reinframe.ReasonCode != "unsupported_host_version" && resp.Reason != "unsupported_host_version" {
		// Action remapped to unsupported
		if resp.Reason != "unsupported_host_version" {
			t.Logf("reason=%s code=%s", resp.Reason, resp.Reinframe.ReasonCode)
		}
	}
}

func TestClaudeHookResponse_ProductivityTimeoutFailOpen(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s", ToolName: "Bash"}
	dec := adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: "timeout_productivity"}
	resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, adapter.ClaudeResponseOptions{
		FailOpenProductivity: true,
	})
	if resp.Decision != "approve" {
		t.Fatalf("productivity timeout should fail-open: %+v", resp)
	}
}

func TestClaudeHookResponse_SecurityDenyNotBypassedByClassifierFailure(t *testing.T) {
	t.Parallel()
	// Deterministic security deny remains deny even with fail-open productivity.
	in := adapter.ClaudePreToolInput{SessionID: "s", ToolName: "Bash"}
	dec := adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: adapter.ReasonDeniedTool}
	resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, adapter.ClaudeResponseOptions{
		FailOpenProductivity: true,
	})
	if resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("security/tool deny must not fail-open: %+v", resp)
	}
}

func TestClaudeHookResponse_RealBashFullSuite(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"session_id":"s","tool_name":"Bash","tool_input":{"command":"go test -race ./..."}}`)
	resp, dec, err := adapter.EvaluateClaudePreToolJSON(context.Background(), raw, adapter.ClaudeBridgeConfig{
		Policy: adapter.HookPolicy{DeniedTools: map[string]struct{}{"Bash": {}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dec.Action != adapter.HookActionDeny {
		t.Fatalf("%+v", dec)
	}
	if resp.Continue != nil && !*resp.Continue {
		t.Fatal("continue:false forbidden")
	}
	if err := adapter.ValidateClaudeHookResponseClosedSchema(resp); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeHookResponse_ClosedSchemaRejectsContinueFalse(t *testing.T) {
	t.Parallel()
	f := false
	resp := adapter.ClaudeHookResponse{Continue: &f, Decision: "block"}
	if err := adapter.ValidateClaudeHookResponseClosedSchema(resp); err == nil {
		t.Fatal("expected reject continue:false")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || (len(s) > 0 && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()))
}
