package detector

import (
	"regexp"
	"strings"
)

// DefaultThreshold is the provisional default N for repeated identical failures.
// Threat-model table wins as N=3 until config overrides (not a calibrated hard-gate).
const DefaultThreshold = 3

// FailureModeRepeatedErrorLoop is the FailureMode value emitted on fire.
const FailureModeRepeatedErrorLoop = "repeated_error_loop"

// DetectorNameRepeatedFailure is the DetectorName on emitted TunnelSignal values.
const DetectorNameRepeatedFailure = "RepeatedFailureDetector"

var whitespaceRE = regexp.MustCompile(`\s+`)

// NormalizeFingerprint produces a stable fingerprint from a raw failure string.
//
// Rules (documented for #82):
//  1. Trim leading/trailing Unicode space.
//  2. Lowercase.
//  3. Collapse any run of whitespace to a single ASCII space.
//  4. Empty result after normalize is not a failure fingerprint (ignored).
//
// These rules are provisional knobs — not scientifically calibrated.
func NormalizeFingerprint(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.ToLower(s)
	s = whitespaceRE.ReplaceAllString(s, " ")
	return s
}
