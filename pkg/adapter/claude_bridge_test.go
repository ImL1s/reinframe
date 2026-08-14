package adapter_test

import (
	"context"
	"encoding/json"
	"strings"
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
	// #116: tool block without continue:false (session stop). Headless defer → deny degrade.
	if resp.Decision != "block" {
		t.Fatalf("resp=%+v", resp)
	}
	if resp.Continue != nil && !*resp.Continue {
		t.Fatalf("must not set continue:false: %+v", resp)
	}
	if resp.HookSpecificOutput == nil || resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("headless defer degrades to deny: %+v", resp)
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
	// Real Claude shape: tool_name=Bash, command in tool_input (#115).
	in, err := adapter.MapClaudePreToolUseJSON([]byte(
		`{"session_id":"s4","tool_name":"Bash","tool_input":{"command":"go test -race ./..."}}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	if in.ToolName != "Bash" || in.Proposed == nil || in.Proposed.Command != "go test -race ./..." {
		t.Fatalf("mapping: tool=%s proposed=%+v", in.ToolName, in.Proposed)
	}
	eng := policy.NewEngine(policy.EngineConfig{})
	resp, dec, err := adapter.EvaluateClaudePreTool(context.Background(), in, adapter.ClaudeBridgeConfig{
		Evaluate: func(ctx context.Context, req adapter.HookRequest) adapter.HookDecision {
			if req.ToolName != "Bash" {
				t.Errorf("HookRequest.ToolName must stay Bash, got %q", req.ToolName)
			}
			if req.Proposed == nil || !adapter.FullSuiteCommand(*req.Proposed) {
				t.Errorf("expected full-suite ProposedAction on request")
			}
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

func TestMapClaudePreToolUseJSON_WithChallengeRetry(t *testing.T) {
	t.Parallel()
	// Test parsing challenge retry context from tool_input
	raw := []byte(`{
		"session_id":"s_retry_1",
		"tool_name":"Bash",
		"tool_input":{
			"command":"rm -rf build",
			"challenge_id":"ch-12345",
			"challenge_nonce":"cn-abcde",
			"justification":{
				"concrete_value":"unblocks clean build",
				"prevented_failure_or_threat":"avoids stale cache corruption",
				"estimated_cost":"5s",
				"verification_plan":"go build ./...",
				"rollback_plan":"git checkout -- build",
				"supporting_evidence_event_ids":["ev-1", "ev-2"]
			}
		},
		"hook_event_name":"PreToolUse"
	}`)

	in, err := adapter.MapClaudePreToolUseJSON(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if in.ChallengeID != "ch-12345" {
		t.Errorf("expected ChallengeID ch-12345, got %q", in.ChallengeID)
	}
	if in.ChallengeNonce != "cn-abcde" {
		t.Errorf("expected ChallengeNonce cn-abcde, got %q", in.ChallengeNonce)
	}
	if in.Justification == nil {
		t.Fatal("expected non-nil Justification")
	}
	if in.Justification.ConcreteValue != "unblocks clean build" {
		t.Errorf("expected ConcreteValue 'unblocks clean build', got %q", in.Justification.ConcreteValue)
	}
	if in.Justification.VerificationPlan != "go build ./..." {
		t.Errorf("expected VerificationPlan 'go build ./...', got %q", in.Justification.VerificationPlan)
	}
	if len(in.Justification.SupportingEvidenceEventIDs) != 2 {
		t.Errorf("expected 2 evidence IDs, got %d", len(in.Justification.SupportingEvidenceEventIDs))
	}

	// Verify HookRequest forwarding
	req := adapter.HookRequestFromClaudePreTool(in)
	if req.ChallengeID != "ch-12345" || req.ChallengeNonce != "cn-abcde" {
		t.Errorf("HookRequest did not receive challenge fields: %+v", req)
	}
	if req.Justification == nil || req.Justification.ConcreteValue != "unblocks clean build" {
		t.Errorf("HookRequest did not receive justification: %+v", req.Justification)
	}
}

func TestClaudeHookResponse_WithChallengeDelivery(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s-ch", ToolName: "Bash"}
	dec := adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: "OVER_SOP"}
	chCtx := &adapter.ClaudeChallengeContext{
		ChallengeID:         "ch-999",
		ChallengeNonce:      "cn-777",
		Reason:              "OVER_SOP",
		SuggestedFix:        "Scope action to affected files only",
		OneShotRetryAllowed: true,
	}
	opts := adapter.ClaudeResponseOptions{
		Profile:   adapter.ClaudeHookProfileV1,
		Challenge: chCtx,
	}

	resp := adapter.ClaudeHookResponseFromDecisionOpts(in, dec, opts)

	if resp.Decision != "block" {
		t.Errorf("expected decision block, got %q", resp.Decision)
	}
	if resp.Continue != nil {
		t.Errorf("continue must be omitted for tool-level blocks, got %v", *resp.Continue)
	}
	if resp.HookSpecificOutput == nil {
		t.Fatal("expected HookSpecificOutput")
	}
	if resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("expected permissionDecision deny, got %q", resp.HookSpecificOutput.PermissionDecision)
	}

	addCtx := resp.HookSpecificOutput.AdditionalContext
	if addCtx == "" {
		t.Fatal("expected non-empty AdditionalContext")
	}
	for _, expected := range []string{"ch-999", "cn-777", "OVER_SOP", "Scope action to affected files only", "one_shot_retry_allowed: true"} {
		if !strings.Contains(addCtx, expected) {
			t.Errorf("AdditionalContext missing %q:\n%s", expected, addCtx)
		}
	}

	if resp.Reinframe == nil {
		t.Fatal("expected Reinframe meta")
	}
	if resp.Reinframe.TransportLevel != adapter.ClaudeTransportHookAdditionalContext {
		t.Errorf("expected transport level %s, got %s", adapter.ClaudeTransportHookAdditionalContext, resp.Reinframe.TransportLevel)
	}
	if resp.Reinframe.Challenge == nil || resp.Reinframe.Challenge.ChallengeID != "ch-999" {
		t.Errorf("unexpected Reinframe Challenge: %+v", resp.Reinframe.Challenge)
	}

	// Verify schema validation and JSON roundtrip
	if err := adapter.ValidateClaudeHookResponseClosedSchema(resp); err != nil {
		t.Fatalf("schema validation failed: %v", err)
	}
	rawJSON, err := adapter.MarshalClaudeHookResponseJSON(resp)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(rawJSON, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if parsed["decision"] != "block" {
		t.Errorf("expected decision block in JSON, got %v", parsed["decision"])
	}
}

func TestClaudeHookResponse_TransportLevelDistinction(t *testing.T) {
	t.Parallel()
	in := adapter.ClaudePreToolInput{SessionID: "s-tl", ToolName: "Edit"}

	// 1. Direct Allow
	respAllow := adapter.ClaudeHookResponseFromDecisionOpts(in, adapter.HookDecision{Action: adapter.HookActionAllow, ReasonCode: "allow"}, adapter.ClaudeResponseOptions{})
	if respAllow.Reinframe.TransportLevel != adapter.ClaudeTransportDirectAllow {
		t.Errorf("expected direct_allow, got %s", respAllow.Reinframe.TransportLevel)
	}
	if respAllow.HookSpecificOutput.PermissionDecision != "allow" {
		t.Errorf("expected allow, got %s", respAllow.HookSpecificOutput.PermissionDecision)
	}

	// 2. Direct Deny
	respDeny := adapter.ClaudeHookResponseFromDecisionOpts(in, adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: "denied_tool"}, adapter.ClaudeResponseOptions{})
	if respDeny.Reinframe.TransportLevel != adapter.ClaudeTransportDirectDeny {
		t.Errorf("expected direct_deny, got %s", respDeny.Reinframe.TransportLevel)
	}
	if respDeny.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("expected deny, got %s", respDeny.HookSpecificOutput.PermissionDecision)
	}

	// 3. Degraded Defer (headless)
	respDegraded := adapter.ClaudeHookResponseFromDecisionOpts(in, adapter.HookDecision{Action: adapter.HookActionDefer, ReasonCode: "pending_advisory"}, adapter.ClaudeResponseOptions{
		HostMode:    adapter.ClaudeHostModeHeadless,
		NativeDefer: false,
	})
	if respDegraded.Reinframe.TransportLevel != adapter.ClaudeTransportDegradedDeny {
		t.Errorf("expected degraded_deny, got %s", respDegraded.Reinframe.TransportLevel)
	}
	if respDegraded.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("expected deny for degraded defer, got %s", respDegraded.HookSpecificOutput.PermissionDecision)
	}

	// 4. Native Defer (interactive)
	respNative := adapter.ClaudeHookResponseFromDecisionOpts(in, adapter.HookDecision{Action: adapter.HookActionDefer, ReasonCode: "pending_advisory"}, adapter.ClaudeResponseOptions{
		HostMode:    adapter.ClaudeHostModeInteractive,
		NativeDefer: true,
	})
	if respNative.Reinframe.TransportLevel != adapter.ClaudeTransportNativeDefer {
		t.Errorf("expected native_defer, got %s", respNative.Reinframe.TransportLevel)
	}
	if respNative.HookSpecificOutput.PermissionDecision != "ask" {
		t.Errorf("expected ask for native defer, got %s", respNative.HookSpecificOutput.PermissionDecision)
	}

	// 5. Challenge Context Delivery
	respChallenge := adapter.ClaudeHookResponseFromDecisionOpts(in, adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: "OVER_SOP"}, adapter.ClaudeResponseOptions{
		Challenge: &adapter.ClaudeChallengeContext{
			ChallengeID:         "ch-1",
			ChallengeNonce:      "cn-1",
			Reason:              "OVER_SOP",
			OneShotRetryAllowed: true,
		},
	})
	if respChallenge.Reinframe.TransportLevel != adapter.ClaudeTransportHookAdditionalContext {
		t.Errorf("expected hook_additional_context, got %s", respChallenge.Reinframe.TransportLevel)
	}
}

func TestValidateClaudeHookResponseClosedSchema_Bounds(t *testing.T) {
	t.Parallel()

	// Continue false rejected
	contFalse := false
	resp1 := adapter.ClaudeHookResponse{
		Continue: &contFalse,
		Decision: "block",
	}
	if err := adapter.ValidateClaudeHookResponseClosedSchema(resp1); err == nil {
		t.Error("expected error for continue:false")
	}

	// Excessive reason rejected
	resp2 := adapter.ClaudeHookResponse{
		Decision: "block",
		Reason:   strings.Repeat("a", adapter.MaxHookReasonRunes+1),
	}
	if err := adapter.ValidateClaudeHookResponseClosedSchema(resp2); err == nil {
		t.Error("expected error for oversized reason")
	}

	// Excessive context rejected
	resp3 := adapter.ClaudeHookResponse{
		Decision: "block",
		HookSpecificOutput: &adapter.ClaudeHookSpecificOutput{
			HookEventName:      "PreToolUse",
			PermissionDecision: "deny",
			AdditionalContext:  strings.Repeat("x", adapter.MaxHookContextRunes+1),
		},
	}
	if err := adapter.ValidateClaudeHookResponseClosedSchema(resp3); err == nil {
		t.Error("expected error for oversized additionalContext")
	}
}
