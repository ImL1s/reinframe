package classifier_test

import (
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

func TestPromptPlan_StablePrefixPreservedAcrossDynamicChange(t *testing.T) {
	t.Parallel()
	mat := classifier.DefaultPromptPlanMaterial()
	in1 := classifier.ClassifierInput{
		SessionID: "s1", RulesetID: "r", RulesetHash: "rh",
		ProposedAction: &adapter.ProposedAction{ToolName: "Bash", Command: "echo a", ToolClass: adapter.ToolClassShell},
	}
	in2 := classifier.ClassifierInput{
		SessionID: "s1", RulesetID: "r", RulesetHash: "rh",
		ProposedAction: &adapter.ProposedAction{ToolName: "Bash", Command: "echo b", ToolClass: adapter.ToolClassShell},
	}
	p1, err := classifier.BuildPromptPlan(mat, in1)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := classifier.BuildPromptPlan(mat, in2)
	if err != nil {
		t.Fatal(err)
	}
	if p1.StablePrefixHash != p2.StablePrefixHash {
		t.Fatalf("stable hash changed: %s vs %s", p1.StablePrefixHash, p2.StablePrefixHash)
	}
	if p1.InputHash == p2.InputHash {
		t.Fatal("input hash should change with dynamic action")
	}
	if p1.PromptHash == p2.PromptHash {
		t.Fatal("prompt hash should change when input hash changes")
	}
	// Byte-identical stable blocks
	if len(p1.StablePrefix) != len(p2.StablePrefix) {
		t.Fatal("stable prefix length")
	}
	for i := range p1.StablePrefix {
		if p1.StablePrefix[i] != p2.StablePrefix[i] {
			t.Fatalf("stable block %d differs", i)
		}
	}
}

func TestPromptPlan_RulesetChangeInvalidatesStable(t *testing.T) {
	t.Parallel()
	mat1 := classifier.DefaultPromptPlanMaterial()
	mat2 := mat1
	mat2.RulesetHash = "other-rules"
	in := classifier.ClassifierInput{SessionID: "s", RulesetHash: "rh"}
	p1, err := classifier.BuildPromptPlan(mat1, in)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := classifier.BuildPromptPlan(mat2, in)
	if err != nil {
		t.Fatal(err)
	}
	if p1.StablePrefixHash == p2.StablePrefixHash {
		t.Fatal("ruleset change must invalidate stable prefix hash")
	}
}

func TestPromptPlan_SchemaVersionChangeInvalidatesStable(t *testing.T) {
	t.Parallel()
	mat1 := classifier.DefaultPromptPlanMaterial()
	mat2 := mat1
	mat2.PromptSchemaVersion = "reinframe.classifier_prompt_plan.v2-test"
	in := classifier.ClassifierInput{SessionID: "s"}
	p1, _ := classifier.BuildPromptPlan(mat1, in)
	p2, _ := classifier.BuildPromptPlan(mat2, in)
	if p1.StablePrefixHash == p2.StablePrefixHash {
		t.Fatal("schema version must change stable hash")
	}
}
