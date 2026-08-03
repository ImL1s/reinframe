package detector_test

import (
	"testing"

	"github.com/ImL1s/reinframe/pkg/detector"
)

func baseAttempt() detector.ValidationAttempt {
	return detector.ValidationAttempt{
		Command:          "go test ./pkg/foo",
		TargetScope:      []string{"pkg/foo"},
		WorkspaceRev:     "abc123",
		ContractRevision: 1,
		Purpose:          "targeted",
		Succeeded:        true,
	}
}

func TestVerificationChurn_SecondSuccessFires(t *testing.T) {
	t.Parallel()
	d := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	a := baseAttempt()
	if _, ok := d.Observe("s1", a); ok {
		t.Fatal("first success must not fire")
	}
	sig, ok := d.Observe("s1", a)
	if !ok || sig == nil {
		t.Fatal("second identical success must fire")
	}
	if sig.FailureMode != detector.FailureModeVerificationChurn {
		t.Fatalf("mode=%s", sig.FailureMode)
	}
	if sig.Details["fingerprint"] == "" {
		t.Fatal("missing fingerprint")
	}
}

func TestVerificationChurn_FailedThenSuccessNoFire(t *testing.T) {
	t.Parallel()
	d := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	a := baseAttempt()
	a.Succeeded = false
	if _, ok := d.Observe("s", a); ok {
		t.Fatal("failed must not fire")
	}
	a.Succeeded = true
	if _, ok := d.Observe("s", a); ok {
		t.Fatal("first success after fail must not fire")
	}
}

func TestVerificationChurn_WorkspaceChangeAllowsRetest(t *testing.T) {
	t.Parallel()
	d := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	a := baseAttempt()
	d.Observe("s", a)
	// Same command but workspace changed (different rev OR flag).
	b := a
	b.WorkspaceRev = "def456"
	if _, ok := d.Observe("s", b); ok {
		t.Fatal("different workspace rev is different fingerprint — first success no fire")
	}
	// Explicit workspace changed with same rev clears and records.
	c := a
	c.WorkspaceChanged = true
	if _, ok := d.Observe("s", c); ok {
		t.Fatal("workspace changed exemption must not fire")
	}
}

func TestVerificationChurn_FlakyInvestigation(t *testing.T) {
	t.Parallel()
	d := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	a := baseAttempt()
	d.Observe("s", a)
	a.FlakyInvestigation = true
	if _, ok := d.Observe("s", a); ok {
		t.Fatal("flaky investigation must not fire")
	}
}

func TestVerificationChurn_PolicyRequiresRerun(t *testing.T) {
	t.Parallel()
	d := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	a := baseAttempt()
	d.Observe("s", a)
	a.PolicyRequiresRerun = true
	if _, ok := d.Observe("s", a); ok {
		t.Fatal("policy-required re-run must not fire")
	}
}

func TestVerificationChurn_HighRiskIndependent(t *testing.T) {
	t.Parallel()
	d := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	a := baseAttempt()
	d.Observe("s", a)
	a.HighRiskIndependent = true
	if _, ok := d.Observe("s", a); ok {
		t.Fatal("high-risk independent re-validation must not fire")
	}
}

func TestVerificationChurn_DifferentPurposeNoFire(t *testing.T) {
	t.Parallel()
	d := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
	a := baseAttempt()
	d.Observe("s", a)
	b := a
	b.Purpose = "full_suite"
	if _, ok := d.Observe("s", b); ok {
		t.Fatal("different purpose = different fingerprint")
	}
}

func TestValidationFingerprint_Stable(t *testing.T) {
	t.Parallel()
	a := baseAttempt()
	fp1 := detector.ValidationFingerprint(a)
	a.Command = "  GO TEST ./PKG/FOO  "
	fp2 := detector.ValidationFingerprint(a)
	if fp1 != fp2 {
		t.Fatalf("normalize mismatch %q vs %q", fp1, fp2)
	}
}
