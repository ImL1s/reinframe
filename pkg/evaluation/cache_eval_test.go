package evaluation_test

import (
	"context"
	"testing"

	"github.com/ImL1s/reinframe/pkg/evaluation"
)

func TestCacheEvalFakeCI(t *testing.T) {
	t.Parallel()
	rep, err := evaluation.RunCacheEvalFakeCI(context.Background(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if rep.HardGateEnabled || rep.DefaultCacheOn {
		t.Fatal("must not enable hard-gate or default cache")
	}
	if rep.Disposition != "MORE-DATA" {
		t.Fatalf("disposition %s", rep.Disposition)
	}
	if rep.StaleHitRate != 0 {
		t.Fatal("stale hit rate")
	}
	if len(rep.Rows) < 8 {
		t.Fatalf("rows %d", len(rep.Rows))
	}
	if !rep.AllModesOK {
		for _, r := range rep.Rows {
			if !r.OK {
				t.Errorf("row fail: %+v", r)
			}
		}
		t.Fatal("all modes not ok")
	}
	foundExact, foundSF, foundMissM, foundMissE := false, false, false, false
	for _, r := range rep.Rows {
		switch r.Mode {
		case evaluation.ModeReinframeExactHit:
			foundExact = true
			if r.ProviderCalls != 1 {
				t.Fatalf("exact hit calls=%d", r.ProviderCalls)
			}
		case evaluation.ModeSingleflightN:
			foundSF = true
			if r.ProviderCalls != 1 {
				t.Fatalf("singleflight calls=%d", r.ProviderCalls)
			}
		case evaluation.ModeRequiredMissModel:
			foundMissM = true
			if r.ProviderCalls != 2 {
				t.Fatalf("model miss calls=%d", r.ProviderCalls)
			}
		case evaluation.ModeRequiredMissEvents:
			foundMissE = true
			if r.ProviderCalls != 2 {
				t.Fatalf("event miss calls=%d", r.ProviderCalls)
			}
		}
	}
	if !foundExact || !foundSF || !foundMissM || !foundMissE {
		t.Fatal("missing required mode rows")
	}
	if rep.StaleHitRate != 0 {
		t.Fatalf("stale_hit_rate=%v", rep.StaleHitRate)
	}
}
