package adapter_test

import (
	"context"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestEvaluateHook_Allow(t *testing.T) {
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		Phase:     "PreTool",
		ToolName:  "read_file",
		FilePath:  "/workspace/a.go",
	}, adapter.HookPolicy{
		ScopeWhitelist: []string{"/workspace"},
	})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("Action=%s want allow; full=%+v", dec.Action, dec)
	}
	if dec.ReasonCode != adapter.ReasonAllow {
		t.Fatalf("ReasonCode=%s", dec.ReasonCode)
	}
}

func TestEvaluateHook_DenyTool(t *testing.T) {
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		ToolName:  "bash",
	}, adapter.HookPolicy{
		DeniedTools: map[string]struct{}{"bash": {}},
	})
	if dec.Action != adapter.HookActionDeny || dec.ReasonCode != adapter.ReasonDeniedTool {
		t.Fatalf("unexpected decision: %+v", dec)
	}
}

func TestEvaluateHook_DenyPathScope(t *testing.T) {
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		ToolName:  "write_file",
		FilePath:  "/etc/passwd",
	}, adapter.HookPolicy{
		ScopeWhitelist: []string{"/workspace"},
	})
	if dec.Action != adapter.HookActionDeny || dec.ReasonCode != adapter.ReasonDeniedPathScope {
		t.Fatalf("unexpected decision: %+v", dec)
	}
}

func TestEvaluateHook_DenyBudget(t *testing.T) {
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		ToolName:  "read_file",
	}, adapter.HookPolicy{BudgetExhausted: true})
	if dec.Action != adapter.HookActionDeny || dec.ReasonCode != adapter.ReasonDeniedBudget {
		t.Fatalf("unexpected decision: %+v", dec)
	}
}

func TestEvaluateHook_DenyHardLatch(t *testing.T) {
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		ToolName:  "read_file",
	}, adapter.HookPolicy{HardDenyInterventionID: "iv-hard"})
	if dec.Action != adapter.HookActionDeny {
		t.Fatalf("Action=%s", dec.Action)
	}
	if dec.InterventionID != "iv-hard" || dec.ReasonCode != adapter.ReasonDeniedHardLatch {
		t.Fatalf("unexpected decision: %+v", dec)
	}
}

func TestEvaluateHook_DeferPendingAdvisory(t *testing.T) {
	deadline := time.Now().UTC().Add(30 * time.Second)
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		ToolName:  "edit",
		FilePath:  "/workspace/x.go",
	}, adapter.HookPolicy{
		ScopeWhitelist:                []string{"/workspace"},
		PendingAdvisoryInterventionID: "iv-adv",
		DeferDeadline:                 &deadline,
	})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("Action=%s want defer", dec.Action)
	}
	if dec.ReasonCode != adapter.ReasonDeferPendingAdvisory {
		t.Fatalf("ReasonCode=%s", dec.ReasonCode)
	}
	if dec.InterventionID != "iv-adv" {
		t.Fatalf("InterventionID=%s", dec.InterventionID)
	}
	if dec.Deadline == nil || !dec.Deadline.Equal(deadline) {
		t.Fatalf("Deadline=%v want %v", dec.Deadline, deadline)
	}
}

func TestEvaluateHook_TimeoutFailOpen(t *testing.T) {
	// Never-closed wait channel forces the gate to hit the wall timeout.
	wait := make(chan struct{})
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		ToolName:  "bash",
	}, adapter.HookPolicy{
		Timeout:     15 * time.Millisecond,
		FailOpen:    true,
		DeniedTools: map[string]struct{}{"bash": {}}, // would deny if rules ran
		Wait:        wait,
	})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("Action=%s want allow (fail-open)", dec.Action)
	}
	if dec.ReasonCode != adapter.ReasonTimeoutFailOpen {
		t.Fatalf("ReasonCode=%s", dec.ReasonCode)
	}
}

func TestEvaluateHook_TimeoutFailClosed(t *testing.T) {
	wait := make(chan struct{})
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		ToolName:  "read_file",
	}, adapter.HookPolicy{
		Timeout:  15 * time.Millisecond,
		FailOpen: false,
		Wait:     wait,
	})
	if dec.Action != adapter.HookActionDeny {
		t.Fatalf("Action=%s want deny (fail-closed)", dec.Action)
	}
	if dec.ReasonCode != adapter.ReasonTimeoutFailClosed {
		t.Fatalf("ReasonCode=%s", dec.ReasonCode)
	}
}

func TestEvaluateHook_NoReviewerInterfaceRequired(t *testing.T) {
	// Compile/run guarantee: EvaluateHook signature has no Reviewer/LLM dependency.
	// This test simply exercises the public API with pure policy data.
	dec := adapter.EvaluateHook(context.Background(), adapter.HookRequest{
		SessionID: "s1",
		ToolName:  "noop",
	}, adapter.HookPolicy{})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("unexpected: %+v", dec)
	}
}
