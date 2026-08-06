package evaluation_test

import (
	"testing"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func TestCrossHostEvalFake(t *testing.T) {
	t.Parallel()
	rep := evaluation.RunCrossHostEvalFake("test")
	if err := evaluation.ValidateCrossHostReport(rep); err != nil {
		t.Fatal(err)
	}
	if rep.Disposition != "MORE-DATA" {
		t.Fatalf("disposition %s", rep.Disposition)
	}
	if rep.LiveHostsUsed {
		t.Fatal("live hosts must be false")
	}
	if !rep.AllFakeOK || len(rep.Rows) < 12 {
		t.Fatalf("rows=%d allOK=%v", len(rep.Rows), rep.AllFakeOK)
	}
	// Grok hooks must show fail-open scenario true
	foundFailOpen := false
	for _, r := range rep.Rows {
		if r.HostLane == evaluation.HostLaneGrokHooksFake && r.ScenarioID == "host_fail_open_hook" {
			if !r.HostFailOpenSeen {
				t.Fatal("grok hooks fail-open not marked")
			}
			foundFailOpen = true
		}
		if r.ACK.Explicit != 0 {
			t.Fatal("explicit ACK claimed")
		}
	}
	if !foundFailOpen {
		t.Fatal("missing grok fail-open row")
	}
}
