package supervisor_test

import (
	"context"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// TestEffortCalibration_OverSOPVerticalSlice is #86:
// simple typo task → criteria met → full suite PreTool DENY (disproportionate / churn).
func TestEffortCalibration_OverSOPVerticalSlice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1) Intake (#84 path): host prompt → TaskSubmitted → contract draft
	intake, err := adapter.IntakeFromHost(adapter.HostTaskPayload{
		SessionID:  "effort-1",
		Prompt:     "fix typo in README",
		SourceHint: adapter.HostHintCLI,
	}, adapter.TaskIntakeOptions{
		BuildContract: true,
		Now:           func() time.Time { return time.Date(2026, 8, 3, 15, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	c := intake.Contract
	if c == nil {
		t.Fatal("expected contract")
	}
	// Force simple+low if heuristic differed slightly
	c.Complexity = protocol.ComplexitySimple
	c.Risk = protocol.RiskLow

	// 2) Ledger: success criteria satisfied after edit/diff
	led := protocol.NewEvidenceLedger(c.TaskID, c.Revision)
	for _, cr := range c.SuccessCriteria {
		led.CriteriaStatus[cr.ID] = protocol.CriterionStatus{CriterionID: cr.ID, Status: "met"}
	}

	// 3) Churn detector: prior successful full-suite validation, then re-run
	churn := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	full := detector.ValidationAttempt{
		Command:          "go test -race ./...",
		TargetScope:      []string{"."},
		WorkspaceRev:     "ws1",
		ContractRevision: c.Revision,
		Purpose:          "full_suite",
		Succeeded:        true,
	}
	if _, ok := churn.Observe(intake.Submitted.SessionID, full); ok {
		t.Fatal("first full suite success must not fire churn")
	}
	sig, ok := churn.Observe(intake.Submitted.SessionID, full)
	if !ok || sig == nil {
		t.Fatal("second full suite success must fire verification_churn")
	}

	eng := policy.NewEngine(policy.EngineConfig{})
	// 4) Agent attempts full suite again → before_tool DENY
	// Real host shape: ToolName=Bash, Command in ProposedAction (#115).
	pa := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion,
		SessionID:     intake.Submitted.SessionID,
		ToolName:      "Bash",
		ToolClass:     adapter.ToolClassShell,
		Command:       "go test -race ./...",
		Source:        "claude_pretool",
	}
	dec := eng.EvaluateBeforeTool(ctx, policy.BeforeToolInput{
		Request: adapter.HookRequest{
			SessionID: intake.Submitted.SessionID, Phase: "PreTool",
			ToolName: "Bash", Proposed: &pa,
		},
		BasePolicy:  adapter.HookPolicy{},
		Contract:    c,
		Ledger:      &led,
		ChurnSignal: sig,
	})
	if dec.Action != adapter.HookActionDeny {
		t.Fatalf("action=%s want deny", dec.Action)
	}
	if dec.ReasonCode != adapter.ReasonRedundantValidation && dec.ReasonCode != adapter.ReasonDisproportionateScope {
		t.Fatalf("reason=%s", dec.ReasonCode)
	}
}

// TestEffortCalibration_DisproportionateWithoutPriorFullSuite: criteria met + full suite first time still deny on simple task.
func TestEffortCalibration_DisproportionateWithoutPriorFullSuite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	intake, err := adapter.IntakeFromHost(adapter.HostTaskPayload{
		SessionID: "effort-2",
		Prompt:    "fix one typo in README",
	}, adapter.TaskIntakeOptions{BuildContract: true})
	if err != nil {
		t.Fatal(err)
	}
	c := intake.Contract
	c.Complexity = protocol.ComplexityTrivial
	c.Risk = protocol.RiskLow
	led := protocol.NewEvidenceLedger(c.TaskID, c.Revision)
	for _, cr := range c.SuccessCriteria {
		led.CriteriaStatus[cr.ID] = protocol.CriterionStatus{CriterionID: cr.ID, Status: "met"}
	}
	eng := policy.NewEngine(policy.EngineConfig{})
	pa := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion,
		SessionID:     "effort-2",
		ToolName:      "Bash",
		ToolClass:     adapter.ToolClassShell,
		Command:       "go test -race ./...",
		Source:        "claude_pretool",
	}
	dec := eng.EvaluateBeforeTool(ctx, policy.BeforeToolInput{
		Request:    adapter.HookRequest{SessionID: "effort-2", ToolName: "Bash", Proposed: &pa},
		Contract:   c,
		Ledger:     &led,
		BasePolicy: adapter.HookPolicy{},
	})
	if dec.Action != adapter.HookActionDeny || dec.ReasonCode != adapter.ReasonDisproportionateScope {
		t.Fatalf("want disproportionate deny, got action=%s reason=%s", dec.Action, dec.ReasonCode)
	}
}

func TestEffortCalibration_AllowRetestAfterWorkspaceChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	churn := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	a := detector.ValidationAttempt{
		Command: "go test ./pkg/x", TargetScope: []string{"pkg/x"},
		WorkspaceRev: "r1", ContractRevision: 1, Purpose: "targeted", Succeeded: true,
	}
	churn.Observe("s", a)
	b := a
	b.WorkspaceRev = "r2" // code changed
	if _, ok := churn.Observe("s", b); ok {
		t.Fatal("workspace change must not churn-fire on first success at new rev")
	}
	// No churn signal → targeted re-test allow under empty policy
	eng := policy.NewEngine(policy.EngineConfig{})
	dec := eng.EvaluateBeforeTool(ctx, policy.BeforeToolInput{
		Request:    adapter.HookRequest{SessionID: "s", ToolName: "go test ./pkg/x"},
		BasePolicy: adapter.HookPolicy{},
	})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("action=%s", dec.Action)
	}
}

func TestEffortCalibration_HighRiskAllowsFullSuite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := protocol.TaskContract{
		TaskID: "t", Revision: 1,
		Complexity: protocol.ComplexitySimple, Risk: protocol.RiskHigh,
		SuccessCriteria: []protocol.Criterion{{ID: "c1", Description: "done"}},
	}
	led := protocol.NewEvidenceLedger("t", 1)
	led.CriteriaStatus["c1"] = protocol.CriterionStatus{CriterionID: "c1", Status: "met"}
	eng := policy.NewEngine(policy.EngineConfig{})
	pa := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion,
		ToolName:      "Bash", ToolClass: adapter.ToolClassShell,
		Command: "go test -race ./...", Source: "claude_pretool",
	}
	dec := eng.EvaluateBeforeTool(ctx, policy.BeforeToolInput{
		Request:    adapter.HookRequest{ToolName: "Bash", Proposed: &pa},
		Contract:   &c,
		Ledger:     &led,
		BasePolicy: adapter.HookPolicy{},
	})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("high risk must allow full suite, got %s/%s", dec.Action, dec.ReasonCode)
	}
}
