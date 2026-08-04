package classifier_test

import (
	"context"
	"testing"

	"github.com/ImL1s/reinframe/pkg/classifier"
)

func TestFakeClassifierProvider_ClearAllowBlock(t *testing.T) {
	t.Parallel()
	p := classifier.FakeClassifierProvider{LatencyMS: 1}
	a, err := p.Assess(context.Background(), classifier.ClassifierInput{
		FixtureName: "clear_allow", RulesetID: "r1", RulesetHash: "h1",
	})
	if err != nil || a.Severity != 10 || a.ReasonCode != "NORMAL_PROGRESS" || a.RulesetID != "r1" {
		t.Fatalf("%+v err=%v", a, err)
	}
	b, err := p.Assess(context.Background(), classifier.ClassifierInput{FixtureName: "clear_block"})
	if err != nil || b.Severity != 90 || b.ReasonCode != "OVER_SOP" {
		t.Fatalf("%+v err=%v", b, err)
	}
}

func TestFakeClassifierProvider_Malformed(t *testing.T) {
	t.Parallel()
	p := classifier.FakeClassifierProvider{}
	a, err := p.Assess(context.Background(), classifier.ClassifierInput{FixtureName: "malformed_output"})
	if err != nil || a.ParseStatus != "invalid" {
		t.Fatalf("%+v err=%v", a, err)
	}
}
