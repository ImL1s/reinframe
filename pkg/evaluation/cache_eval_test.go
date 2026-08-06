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
	// Exact hit must show 1 call.
	foundExact := false
	foundSF := false
	for _, r := range rep.Rows {
		if r.Mode == evaluation.ModeReinframeExactHit {
			foundExact = true
			if r.ProviderCalls != 1 {
				t.Fatalf("exact hit calls=%d", r.ProviderCalls)
			}
		}
		if r.Mode == evaluation.ModeSingleflightN {
			foundSF = true
			if r.ProviderCalls != 1 {
				t.Fatalf("singleflight calls=%d", r.ProviderCalls)
			}
		}
	}
	if !foundExact || !foundSF {
		t.Fatal("missing exact/singleflight rows")
	}
}
