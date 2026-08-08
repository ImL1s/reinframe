package evaluation_test

import (
	"testing"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func TestAttachLiveGrok167Lane_MoreDataNoRanking(t *testing.T) {
	t.Parallel()
	pin := evaluation.DefaultLiveGrok167Pin()
	rep := evaluation.AttachLiveGrok167Lane("testcommit", pin)
	if rep.Disposition != "MORE-DATA" {
		t.Fatalf("disposition=%s", rep.Disposition)
	}
	if !rep.LiveHostsUsed {
		t.Fatal("expected live hosts used")
	}
	if rep.Lane != evaluation.CrossHostLanePartialLive {
		t.Fatalf("lane=%s", rep.Lane)
	}
	if err := evaluation.ValidatePartialLiveReport(rep); err != nil {
		t.Fatal(err)
	}
	// Fake rows still present + live rows
	if len(rep.Rows) < 16+4 {
		t.Fatalf("rows=%d want fake+live", len(rep.Rows))
	}
	var liveAllow bool
	for _, row := range rep.Rows {
		if row.HostLane == evaluation.HostLaneGrokLive167 && row.ScenarioID == "harmless_allow" {
			liveAllow = row.OK && row.AllowOK
		}
		if row.ACK.Explicit != 0 {
			t.Fatal("explicit ACK forbidden")
		}
		if row.TunnelingScore != 0 {
			t.Fatal("ranking score forbidden")
		}
	}
	if !liveAllow {
		t.Fatal("live allow row missing")
	}
}

func TestValidateCrossHostReport_StillForbidsLiveInFakeCI(t *testing.T) {
	t.Parallel()
	rep := evaluation.RunCrossHostEvalFake("c")
	if err := evaluation.ValidateCrossHostReport(rep); err != nil {
		t.Fatal(err)
	}
	rep.LiveHostsUsed = true
	if err := evaluation.ValidateCrossHostReport(rep); err == nil {
		t.Fatal("CI fake validator must reject LiveHostsUsed")
	}
}

func TestValidatePartialLiveReport_RejectsLimitedGO(t *testing.T) {
	t.Parallel()
	rep := evaluation.AttachLiveGrok167Lane("c", evaluation.DefaultLiveGrok167Pin())
	rep.Disposition = "LIMITED-GO"
	if err := evaluation.ValidatePartialLiveReport(rep); err == nil {
		t.Fatal("expected error for LIMITED-GO")
	}
}
