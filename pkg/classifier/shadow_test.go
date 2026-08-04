package classifier_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

func TestShadow_NeverEnforces(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{Provider: classifier.FakeClassifierProvider{}}
	// High severity block prediction
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		SessionID:      "s",
		Proposed:       adapter.ProposedAction{ToolName: "Bash", Command: "go test -race ./...", ToolClass: adapter.ToolClassShell},
		HookGateAction: adapter.HookActionAllow,
		RulesetID:      "fixture:clear_block",
		Threshold:      50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Resolved.Decision != classifier.DecisionBlock {
		t.Fatalf("want BLOCK predict, got %s", res.Resolved.Decision)
	}
	if res.Resolved.Enforced {
		t.Fatal("shadow must never enforce")
	}
	if !res.Disagreement {
		t.Fatal("expected disagreement with allow gate")
	}
}

func TestShadow_Stage0BlockStillNotEnforced(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		SessionID:      "s",
		Stage0Block:    true,
		Stage0Reason:   "over_sop",
		HookGateAction: adapter.HookActionDeny,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Resolved.Decision != classifier.DecisionBlock || res.Resolved.Enforced {
		t.Fatalf("%+v", res.Resolved)
	}
	if res.Resolved.ResolverReason != "stage0_block" {
		t.Fatal(res.Resolved.ResolverReason)
	}
}

func TestShadow_ProviderFailOpenProductivity(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{Provider: classifier.FakeClassifierProvider{}}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		SessionID:      "s",
		RulesetID:      "fixture:unknown_fixture_name",
		PolicyClass:    classifier.PolicyClassProductivity,
		HookGateAction: adapter.HookActionAllow,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Fake returns error for unknown → fail-open ALLOW
	if res.Resolved.Decision != classifier.DecisionAllow {
		t.Fatalf("%+v", res.Resolved)
	}
	if res.Resolved.ResolverReason != "fail_open_productivity" {
		t.Fatal(res.Resolved.ResolverReason)
	}
}

func TestShadow_AuditJSON(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{Provider: classifier.FakeClassifierProvider{}}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		RulesetID: "fixture:clear_allow", HookGateAction: adapter.HookActionAllow,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := res.AuditJSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["schema_version"] != classifier.SchemaClassifierAudit {
		t.Fatal(m["schema_version"])
	}
	if m["enforced"] != false {
		t.Fatal("enforced must be false")
	}
}

func TestStage0FullSuiteHelper(t *testing.T) {
	t.Parallel()
	pa := adapter.ProposedAction{ToolName: "Bash", ToolClass: adapter.ToolClassShell, Command: "go test -race ./..."}
	ok, reason := classifier.Stage0FullSuiteBlock(pa, true, true)
	if !ok || reason == "" {
		t.Fatal(ok, reason)
	}
	ok, _ = classifier.Stage0FullSuiteBlock(pa, false, true)
	if ok {
		t.Fatal("criteria not met")
	}
}
