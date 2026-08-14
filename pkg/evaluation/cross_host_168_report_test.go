package evaluation_test

import (
	"testing"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func TestSynthesizeCrossHost168Report_TriHostSynthesis(t *testing.T) {
	t.Parallel()

	matrix := evaluation.DefaultTriHost168Matrix()
	rep := evaluation.SynthesizeCrossHost168Report("testcommit168", matrix)

	if rep.Disposition != "MORE-DATA" {
		t.Fatalf("expected disposition MORE-DATA, got %s", rep.Disposition)
	}
	if !rep.LiveHostsUsed {
		t.Fatal("expected LiveHostsUsed to be true")
	}
	if rep.Lane != evaluation.CrossHostLaneTriHostLive {
		t.Fatalf("expected lane %s, got %s", evaluation.CrossHostLaneTriHostLive, rep.Lane)
	}

	if err := evaluation.ValidateCrossHost168Report(rep); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	// Verify all 3 hosts are represented in the rows
	hostSeen := map[evaluation.HostLaneID]bool{}
	for _, row := range rep.Rows {
		hostSeen[row.HostLane] = true
		if row.ACK.Explicit != 0 {
			t.Errorf("explicit ACK claimed on %s:%s", row.HostLane, row.ScenarioID)
		}
		if row.TunnelingScore != 0 {
			t.Errorf("tunneling score non-zero on %s:%s", row.HostLane, row.ScenarioID)
		}
	}

	if !hostSeen[evaluation.HostLaneCodexLive164] {
		t.Error("missing Codex live rows in synthesized report")
	}
	if !hostSeen[evaluation.HostLaneClaudeLive120] {
		t.Error("missing Claude live rows in synthesized report")
	}
	if !hostSeen[evaluation.HostLaneGrokLive167] {
		t.Error("missing Grok live rows in synthesized report")
	}
}

func TestValidateCrossHost168Report_RejectsInvalid(t *testing.T) {
	t.Parallel()

	matrix := evaluation.DefaultTriHost168Matrix()
	rep := evaluation.SynthesizeCrossHost168Report("testcommit168", matrix)

	// Test 1: Invalid disposition
	repInvalidDisp := rep
	repInvalidDisp.Disposition = "GO"
	if err := evaluation.ValidateCrossHost168Report(repInvalidDisp); err == nil {
		t.Error("expected error for non-MORE-DATA disposition")
	}

	// Test 2: Invalid lane
	repInvalidLane := rep
	repInvalidLane.Lane = "invalid_lane"
	if err := evaluation.ValidateCrossHost168Report(repInvalidLane); err == nil {
		t.Error("expected error for invalid lane")
	}

	// Test 3: Claiming explicit ACK
	repExplicitACK := rep
	repExplicitACK.Rows = append([]evaluation.HostScenarioResult{}, rep.Rows...)
	repExplicitACK.Rows[0].ACK.Explicit = 1
	if err := evaluation.ValidateCrossHost168Report(repExplicitACK); err == nil {
		t.Error("expected error for explicit ACK claim")
	}
}
