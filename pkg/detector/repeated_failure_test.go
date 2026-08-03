package detector_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestNormalizeFingerprint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"  Foo   BAR  ", "foo bar"},
		{"already clean", "already clean"},
		{"", ""},
		{"\t\nX\n\t", "x"},
	}
	for _, tc := range cases {
		got := detector.NormalizeFingerprint(tc.in)
		if got != tc.want {
			t.Fatalf("NormalizeFingerprint(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestRepeatedFailure_ThreeIdenticalFires(t *testing.T) {
	t.Parallel()
	d := detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3})
	raw := "cannot find package foo"

	for i := 1; i <= 2; i++ {
		sig, ok := d.ObserveRaw("sess-1", raw, "e"+string(rune('0'+i)))
		if ok || sig != nil {
			t.Fatalf("observation %d should not fire, got %#v", i, sig)
		}
	}
	sig, ok := d.ObserveRaw("sess-1", raw, "e3")
	if !ok || sig == nil {
		t.Fatal("third identical failure should fire")
	}
	if sig.FailureMode != detector.FailureModeRepeatedErrorLoop {
		t.Fatalf("FailureMode=%q", sig.FailureMode)
	}
	if sig.DetectorName != detector.DetectorNameRepeatedFailure {
		t.Fatalf("DetectorName=%q", sig.DetectorName)
	}
	if sig.SessionID != "sess-1" {
		t.Fatalf("SessionID=%q", sig.SessionID)
	}
	if sig.Details["fingerprint"] != detector.NormalizeFingerprint(raw) {
		t.Fatalf("details fingerprint=%q", sig.Details["fingerprint"])
	}
	if sig.Details["count"] != "3" {
		t.Fatalf("count=%q", sig.Details["count"])
	}
}

func TestRepeatedFailure_TwoIdenticalDoesNotFire(t *testing.T) {
	t.Parallel()
	d := detector.NewRepeatedFailureDetector(detector.Config{})
	raw := "panic: nil pointer"
	for i := 0; i < 2; i++ {
		if _, ok := d.ObserveRaw("s", raw, ""); ok {
			t.Fatal("should not fire on 2 identical")
		}
	}
	if d.Count("s", raw) != 2 {
		t.Fatalf("count=%d", d.Count("s", raw))
	}
}

func TestRepeatedFailure_DifferentFingerprintsDoNotFire(t *testing.T) {
	t.Parallel()
	d := detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3})
	for i, raw := range []string{"err-a", "err-b", "err-c", "err-a", "err-b"} {
		if _, ok := d.ObserveRaw("s", raw, ""); ok {
			t.Fatalf("step %d should not fire (distinct / under threshold)", i)
		}
	}
	// one more err-a → count 3 for err-a only
	sig, ok := d.ObserveRaw("s", "err-a", "")
	if !ok || sig == nil {
		t.Fatal("third err-a should fire")
	}
	if sig.Details["fingerprint"] != "err-a" {
		t.Fatalf("fp=%q", sig.Details["fingerprint"])
	}
}

func TestRepeatedFailure_WhitespaceNormalizationMerges(t *testing.T) {
	t.Parallel()
	d := detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3})
	variants := []string{
		"Build Failed: missing dep",
		"  build   failed: missing dep ",
		"BUILD FAILED: missing dep",
	}
	var fired bool
	for i, v := range variants {
		sig, ok := d.ObserveRaw("s", v, "")
		if i < 2 && ok {
			t.Fatalf("unexpected fire at %d", i)
		}
		if i == 2 {
			fired = ok
			if !ok || sig.Details["count"] != "3" {
				t.Fatalf("want fire on third normalized match, ok=%v sig=%#v", ok, sig)
			}
		}
	}
	if !fired {
		t.Fatal("expected fire")
	}
}

func TestRepeatedFailure_ObserveTestResultEvent(t *testing.T) {
	t.Parallel()
	d := detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3})
	payload, _ := json.Marshal(protocol.TestResultEvent{
		TestRunID:     "tr1",
		Command:       "go test",
		FailedCount:   1,
		FailureOutput: "FAIL: TestFoo\nexpected 1 got 2",
	})
	for i := 1; i <= 3; i++ {
		ev := protocol.AgentEvent{
			EventID:     "e" + string(rune('0'+i)),
			SessionID:   "sess-tr",
			SequenceNum: int64(i),
			EventType:   "test_result",
			Timestamp:   time.Now().UTC(),
			Payload:     payload,
		}
		sig, ok := d.Observe(ev)
		if i < 3 && ok {
			t.Fatalf("fire too early at %d", i)
		}
		if i == 3 && (!ok || sig.FailureMode != detector.FailureModeRepeatedErrorLoop) {
			t.Fatalf("expected fire on third test_result, ok=%v", ok)
		}
	}
}

func TestRepeatedFailure_DefaultThresholdIsThree(t *testing.T) {
	t.Parallel()
	d := detector.NewRepeatedFailureDetector(detector.Config{})
	if d.Threshold() != detector.DefaultThreshold {
		t.Fatalf("threshold=%d", d.Threshold())
	}
}
