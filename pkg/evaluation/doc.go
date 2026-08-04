// Package evaluation provides offline M3 synthetic/FP benchmarks (#100).
//
// It scores detectors (#82/#85/#98) and shadow classifier (#105) separately.
// Reports always set hard_gate_enabled=false. Thresholds are provisional.
// Disposition is NO-GO, LIMITED-GO recommendation, or MORE-DATA — never an
// in-repo flip from shadow to enforced mode.
package evaluation
