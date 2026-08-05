package classifier

import (
	"context"
	"fmt"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// ClassifierProvider is the canonical Stage-1 provider contract (#132).
// Implementations return typed success or typed failure; they never decide ALLOW|BLOCK.
type ClassifierProvider interface {
	Assess(ctx context.Context, req ProviderRequest) (ProviderResult, error)
}

// ClassifierInput is the versioned Stage 1 input.
type ClassifierInput struct {
	SchemaVersion   string
	SessionID       string
	FixtureName     string // test/fake routing
	PolicyClass     string
	RulesetID       string
	RulesetHash     string
	ProposedAction  *adapter.ProposedAction
	RecentEventIDs  []string
	RelatedEventIDs []string
	// Exception flags applied in Stage2 after raw score (#105).
	UserException       bool
	RepoPolicyException bool
	FlakyInvestigation  bool
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
// Uses the canonical ProviderRequest/ProviderResult contract (#132).
type FakeClassifierProvider struct {
	LatencyMS int64
	// ProviderKind labels Meta.Provider (default "fake").
	ProviderKind string
}

// Assess implements ClassifierProvider.
func (f FakeClassifierProvider) Assess(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	start := time.Now()
	if ctx != nil && ctx.Err() != nil {
		return ProviderResult{}, ctx.Err()
	}
	if err := ValidateProviderRequest(req); err != nil {
		return ProviderResult{}, err
	}
	in := req.Input
	kind := f.ProviderKind
	if kind == "" {
		kind = "fake"
	}
	out := RawAssessment{
		SchemaVersion: SchemaRawAssessment,
		ModelID:       "fake",
		ModelVersion:  "v0",
		PromptHash:    req.Prompt.PromptHash,
		RulesetID:     in.RulesetID,
		RulesetHash:   in.RulesetHash,
		ParseStatus:   ParseStatusOK,
		LatencyMS:     f.LatencyMS,
	}
	if out.PromptHash == "" {
		out.PromptHash = "fake-prompt"
	}
	// Prefer FixtureName; else derive from proposed action for non-fixture path.
	name := in.FixtureName
	if name == "" && in.ProposedAction != nil && adapter.FullSuiteCommand(*in.ProposedAction) {
		name = "clear_block"
	}
	switch name {
	case "clear_allow":
		out.Severity = 10
		out.ReasonCode = "NORMAL_PROGRESS"
	case "clear_block":
		out.Severity = 90
		out.ReasonCode = "OVER_SOP"
		if in.ProposedAction != nil && len(in.RecentEventIDs) > 0 {
			out.EvidenceEventIDs = append([]string(nil), in.RecentEventIDs...)
		}
	case "malformed_output":
		out.ParseStatus = ParseStatusInvalid
		out.Severity = 0
		out.ReasonCode = "UNKNOWN"
	case "user_exception", "repo_policy_exception", "flaky_investigation":
		// Raw may still be high; Stage2 applies exception.
		out.Severity = 85
		out.ReasonCode = "OVER_SOP"
	case "healthy_deep_security_work", "objective_outside_recent_tail",
		"contradictory_related_evidence":
		out.Severity = 40
		out.ReasonCode = "NORMAL_PROGRESS"
	case "":
		out.Severity = 0
		out.ReasonCode = "NORMAL_PROGRESS"
	default:
		return ProviderResult{}, fmt.Errorf("classifier fake: unknown fixture %q", name)
	}
	if out.ParseStatus == ParseStatusOK && !ValidateSeverity(out.Severity) {
		return ProviderResult{}, fmt.Errorf("severity out of range")
	}
	if out.ParseStatus == ParseStatusOK && !ValidateRawReasonCode(out.ReasonCode) {
		return ProviderResult{}, fmt.Errorf("unknown reason code")
	}
	if f.LatencyMS == 0 {
		out.LatencyMS = time.Since(start).Milliseconds()
	}
	return ProviderResult{
		Assessment: out,
		Usage:      ProviderUsage{UsagePresent: false},
		Meta: ProviderMeta{
			Provider:     kind,
			ModelID:      "fake",
			ModelVersion: "v0",
			LatencyMS:    out.LatencyMS,
			ParseStatus:  out.ParseStatus,
		},
	}, nil
}

// NewProviderRequest builds a ProviderRequest with default prompt plan and bounds.
func NewProviderRequest(in ClassifierInput) (ProviderRequest, error) {
	plan, err := BuildPromptPlan(DefaultPromptPlanMaterial(), in)
	if err != nil {
		return ProviderRequest{}, err
	}
	return ProviderRequest{
		SchemaVersion:  SchemaProviderRequest,
		Input:          in,
		Prompt:         plan,
		Timeout:        DefaultTimeout,
		MaxInputBytes:  DefaultMaxInputBytes,
		MaxOutputBytes: DefaultMaxOutputBytes,
	}, nil
}
