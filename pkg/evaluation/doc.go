// Package evaluation provides offline M3 synthetic/FP benchmarks (#100) and
// challenge evaluation Lane A (#140).
//
// #100 scores detectors (#82/#85/#98) and shadow classifier (#105) separately.
// #140 Lane A scores challenge open / justify / retry layers separately using
// the host-neutral challenge.Service (fake re-eval; no live Claude).
//
// Reports always set hard_gate_enabled=false. Thresholds are provisional.
// Disposition is NO-GO, LIMITED-GO recommendation, or MORE-DATA — never an
// in-repo flip from shadow to enforced mode.
package evaluation
