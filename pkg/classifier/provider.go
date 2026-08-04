package classifier

import (
	"context"
	"fmt"
)

// ClassifierProvider produces closed RawAssessment values (#119 / #105).
type ClassifierProvider interface {
	Assess(ctx context.Context, in ClassifierInput) (RawAssessment, error)
}

// ClassifierInput is the versioned Stage 1 input (wire subset for tests).
type ClassifierInput struct {
	SchemaVersion string
	SessionID     string
	FixtureName   string // test/fake only
	PolicyClass   string
	RulesetID     string
	RulesetHash   string
}

// RawAssessment is the closed Stage 1 output.
type RawAssessment struct {
	SchemaVersion    string
	Severity         int
	ReasonCode       string
	EvidenceEventIDs []string
	ModelID          string
	ModelVersion     string
	PromptHash       string
	RulesetID        string
	RulesetHash      string
	ParseStatus      string
	LatencyMS        int64
}

// FakeClassifierProvider maps golden fixture names to deterministic assessments.
type FakeClassifierProvider struct {
	// LatencyMS recorded on each Assess.
	LatencyMS int64
}

// Assess implements ClassifierProvider.
func (f FakeClassifierProvider) Assess(ctx context.Context, in ClassifierInput) (RawAssessment, error) {
	if ctx != nil && ctx.Err() != nil {
		return RawAssessment{}, ctx.Err()
	}
	out := RawAssessment{
		SchemaVersion: SchemaRawAssessment,
		ModelID:       "fake",
		ModelVersion:  "v0",
		PromptHash:    "fake-prompt",
		RulesetID:     in.RulesetID,
		RulesetHash:   in.RulesetHash,
		ParseStatus:   "ok",
		LatencyMS:     f.LatencyMS,
	}
	switch in.FixtureName {
	case "clear_allow":
		out.Severity = 10
		out.ReasonCode = "NORMAL_PROGRESS"
	case "clear_block":
		out.Severity = 90
		out.ReasonCode = "OVER_SOP"
	case "malformed_output":
		out.ParseStatus = "invalid"
		out.Severity = 0
		out.ReasonCode = "UNKNOWN"
	case "user_exception", "repo_policy_exception", "flaky_investigation",
		"healthy_deep_security_work", "objective_outside_recent_tail",
		"contradictory_related_evidence":
		out.Severity = 40
		out.ReasonCode = "NORMAL_PROGRESS"
	default:
		return RawAssessment{}, fmt.Errorf("classifier fake: unknown fixture %q", in.FixtureName)
	}
	if out.ParseStatus == "ok" && !ValidateSeverity(out.Severity) {
		return RawAssessment{}, fmt.Errorf("severity out of range")
	}
	if out.ParseStatus == "ok" && !ValidateRawReasonCode(out.ReasonCode) {
		return RawAssessment{}, fmt.Errorf("unknown reason code")
	}
	return out, nil
}
