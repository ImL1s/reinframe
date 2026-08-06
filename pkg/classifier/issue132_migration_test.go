package classifier_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

// Fake provider migration preserves #105 shadow decision semantics.
func TestShadow_FakeMigrationPreservesSemantics(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{Provider: classifier.FakeClassifierProvider{}}
	cases := []struct {
		fixture string
		want    string
	}{
		{"clear_allow", classifier.DecisionAllow},
		{"clear_block", classifier.DecisionBlock},
		{"malformed_output", classifier.DecisionAllow}, // productivity fail-open
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.fixture, func(t *testing.T) {
			t.Parallel()
			res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
				FixtureName:    tc.fixture,
				HookGateAction: adapter.HookActionAllow,
				Threshold:      50,
				PolicyClass:    classifier.PolicyClassProductivity,
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.Resolved.Decision != tc.want {
				t.Fatalf("got %s want %s", res.Resolved.Decision, tc.want)
			}
			if res.Resolved.Enforced {
				t.Fatal("enforced must be false")
			}
		})
	}
}

func TestShadow_SecurityFailClosedOnProviderError(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{Provider: classifier.FakeClassifierProvider{}}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		FixtureName:    "unknown_fixture_xyz",
		PolicyClass:    classifier.PolicyClassSecurity,
		HookGateAction: adapter.HookActionAllow,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Resolved.Decision != classifier.DecisionBlock {
		t.Fatalf("%+v", res.Resolved)
	}
	if res.Resolved.ResolverReason != "fail_closed_security" {
		t.Fatal(res.Resolved.ResolverReason)
	}
	if res.Resolved.Enforced {
		t.Fatal("shadow never enforces")
	}
}

func TestProviderCallAudit_NoSecretsOrPrompts(t *testing.T) {
	t.Parallel()
	req, err := classifier.NewFixtureProviderRequest(classifier.ClassifierInput{
		SessionID: "sess", RulesetHash: "rh", RecentEventIDs: []string{"e1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	res := classifier.ProviderResult{
		Assessment: classifier.RawAssessment{
			Severity: 10, ReasonCode: "NORMAL_PROGRESS", ParseStatus: classifier.ParseStatusOK,
		},
		Usage: classifier.ProviderUsage{UsagePresent: true, InputTokens: 3, OutputTokens: 1},
		Meta: classifier.ProviderMeta{
			Provider: "fake", ModelID: "m", ProviderRequestID: "id", ParseStatus: classifier.ParseStatusOK,
		},
	}
	a := classifier.BuildProviderCallAudit(req, res, "corr", "cause", time.Now())
	b, err := a.AuditJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// Must not include raw stable policy text from DefaultPromptPlanMaterial.
	if strings.Contains(s, "You are Reinframe Action Alignment") {
		t.Fatal("raw prompt leaked into audit")
	}
	if strings.Contains(s, "Authorization") || strings.Contains(s, "Bearer") {
		t.Fatal("auth leaked")
	}
	if !strings.Contains(s, "prompt_hash") || !strings.Contains(s, "stable_prefix_hash") {
		t.Fatal("expected hash fields")
	}
}
