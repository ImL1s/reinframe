package classifier_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ImL1s/reinframe/pkg/classifier"
)

func TestSeverityBounds(t *testing.T) {
	if classifier.ValidateSeverity(-1) || classifier.ValidateSeverity(101) {
		t.Fatal()
	}
	if !classifier.ValidateSeverity(0) || !classifier.ValidateSeverity(100) || !classifier.ValidateSeverity(50) {
		t.Fatal()
	}
}

func TestReasonAllowlist(t *testing.T) {
	if !classifier.ValidateRawReasonCode("OVER_SOP") {
		t.Fatal()
	}
	if classifier.ValidateRawReasonCode("NOT_A_CODE") {
		t.Fatal()
	}
}

func TestGoldenFixtureNamesPresent(t *testing.T) {
	// Drive real fixtures on disk — no test theater.
	root := filepath.Join("..", "..", "testdata", "classifier")
	required := []string{
		"clear_allow.json", "clear_block.json", "user_exception.json",
		"repo_policy_exception.json", "flaky_investigation.json",
		"healthy_deep_security_work.json", "objective_outside_recent_tail.json",
		"contradictory_related_evidence.json", "malformed_output.json",
	}
	for _, name := range required {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s json: %v", name, err)
		}
		if m["schema_version"] != "reinframe.classifier_fixture.v1" {
			t.Fatalf("%s schema", name)
		}
	}
}
