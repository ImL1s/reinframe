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
	if rep.InvalidAdmissionCount != 0 {
		t.Fatalf("invalid admission must be proven 0, got %d", rep.InvalidAdmissionCount)
	}
	if len(rep.Rows) < 11 {
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
	found := map[evaluation.CacheEvalMode]bool{}
	for _, r := range rep.Rows {
		found[r.Mode] = true
		switch r.Mode {
		case evaluation.ModeStage0Only:
			if r.ProviderCalls != 0 {
				t.Fatalf("stage0 calls=%d", r.ProviderCalls)
			}
		case evaluation.ModeReinframeExactHit:
			if r.ProviderCalls != 1 {
				t.Fatalf("exact hit calls=%d", r.ProviderCalls)
			}
		case evaluation.ModeSingleflightN:
			if r.ProviderCalls != 1 {
				t.Fatalf("singleflight calls=%d", r.ProviderCalls)
			}
		case evaluation.ModeRequiredMissModel, evaluation.ModeRequiredMissEvents, evaluation.ModeDynamicOnlyProvider:
			if r.ProviderCalls != 2 {
				t.Fatalf("%s calls=%d", r.Mode, r.ProviderCalls)
			}
		case evaluation.ModeProviderCacheCold:
			if r.CacheHit {
				t.Fatal("cold write must not be a hit")
			}
		case evaluation.ModeInvalidAdmission:
			if !r.OK {
				t.Fatal("invalid admission must prove zero admissions")
			}
		}
	}
	for _, m := range []evaluation.CacheEvalMode{
		evaluation.ModeStage0Only, evaluation.ModeProviderCacheCold, evaluation.ModeDynamicOnlyProvider,
		evaluation.ModeReinframeExactHit, evaluation.ModeSingleflightN,
		evaluation.ModeRequiredMissModel, evaluation.ModeRequiredMissEvents, evaluation.ModeInvalidAdmission,
	} {
		if !found[m] {
			t.Fatalf("missing mode %s", m)
		}
	}
	if rep.StaleHitRate != 0 {
		t.Fatalf("stale_hit_rate=%v", rep.StaleHitRate)
	}
}
