package detector_test

import (
	"testing"

	"github.com/ImL1s/reinframe/pkg/detector"
)

func TestHypothesisLoop_FiresOnRepeatedConclusion(t *testing.T) {
	t.Parallel()
	d := detector.NewHypothesisLoopDetector(detector.HypothesisLoopConfig{Threshold: 3})
	sess := "review"
	text := "SQL injection likely in login handler"
	for i := 1; i <= 3; i++ {
		sig, ok := d.Observe(sess, detector.HypothesisObservation{
			Text:        text,
			EvidenceIDs: []string{"auth/login.go"}, // same evidence every time
		})
		// First observation with evidence is new → count reset to 1, no fire.
		// Observations 2 and 3 re-use evidence → count 2 then 3 → fire on 3rd overall?
		// Wait: first has new evidence → count=1, return false.
		// second same evidence, no new → count=2
		// third same → count=3 fire
		if i < 3 {
			if ok {
				t.Fatalf("unexpected fire at i=%d", i)
			}
			continue
		}
		if !ok || sig == nil {
			t.Fatalf("expected fire at i=%d count=%d", i, d.Count(sess, text))
		}
		if sig.FailureMode != detector.FailureModeHypothesisLoop {
			t.Fatalf("mode=%s", sig.FailureMode)
		}
		if sig.DetectorName != detector.DetectorNameHypothesisLoop {
			t.Fatalf("name=%s", sig.DetectorName)
		}
	}
}

func TestHypothesisLoop_NoFireWhenNewEvidenceArrives(t *testing.T) {
	t.Parallel()
	d := detector.NewHypothesisLoopDetector(detector.HypothesisLoopConfig{Threshold: 3})
	sess := "deep-security"
	text := "possible IDOR on /api/users/{id}"
	// Healthy deep work: same hypothesis class but new file/evidence each probe.
	ids := []string{"handlers/users.go", "middleware/auth.go", "tests/idor_test.go", "docs/threat.md"}
	for i, id := range ids {
		sig, ok := d.Observe(sess, detector.HypothesisObservation{
			Text:        text,
			EvidenceIDs: []string{id},
		})
		if ok {
			t.Fatalf("new evidence must not fire (i=%d): %+v", i, sig)
		}
	}
}

func TestHypothesisLoop_NoFireShort(t *testing.T) {
	t.Parallel()
	d := detector.NewHypothesisLoopDetector(detector.HypothesisLoopConfig{Threshold: 3})
	// Two identical conclusions without evidence — under threshold.
	for i := 0; i < 2; i++ {
		if sig, ok := d.Observe("s", detector.HypothesisObservation{Text: "cache race in worker"}); ok {
			t.Fatalf("short must not fire: %+v", sig)
		}
	}
}

func TestHypothesisLoop_EmptyTextIgnored(t *testing.T) {
	t.Parallel()
	d := detector.NewHypothesisLoopDetector(detector.HypothesisLoopConfig{})
	if _, ok := d.Observe("s", detector.HypothesisObservation{Text: "   "}); ok {
		t.Fatal("empty")
	}
}

func TestHypothesisLoop_DefaultThreshold(t *testing.T) {
	t.Parallel()
	d := detector.NewHypothesisLoopDetector(detector.HypothesisLoopConfig{})
	if d.Threshold() != detector.DefaultHypothesisLoopThreshold {
		t.Fatalf("th=%d", d.Threshold())
	}
}
