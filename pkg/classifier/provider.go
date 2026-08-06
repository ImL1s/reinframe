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

// ProviderCallObserver receives closed provider-call audits on real invocation paths.
// Sink failures must not alter ALLOW/BLOCK policy (best-effort recording).
type ProviderCallObserver interface {
	RecordProviderCall(ctx context.Context, audit ProviderCallAudit) error
}

// ClassifierInput is the versioned Stage 1 input (wire contract + #132 extensions).
type ClassifierInput struct {
	SchemaVersion string
	SessionID     string
	FixtureName   string // test/fake routing
	PolicyClass   string
	RulesetID     string
	RulesetHash   string

	// Task / trajectory packet (normative recent-N).
	TaskAnchor       TaskAnchor
	ContractRevision int
	EvidenceRevision int
	RecentEvents     []EventDigest
	RelatedEvents    []EventDigest
	Window           WindowMeta

	// Legacy ID lists: production rejects ID-only evidence without digests.
	// Only AllowLegacyFixtureIDs (explicit fixture builder) may use them alone.
	RecentEventIDs  []string
	RelatedEventIDs []string

	// AllowLegacyFixtureIDs enables #105 ID-only fixture compatibility.
	// Never set on real OpenAI-compatible Assess paths.
	AllowLegacyFixtureIDs bool

	ProposedAction *adapter.ProposedAction
	// Challenge is optional closed challenge/justification context (#131/#132).
	Challenge *ChallengeContext
	// Exception flags applied in Stage2 after raw score (#105).
	UserException       bool
	RepoPolicyException bool
	FlakyInvestigation  bool
}

// ChallengeContext is a closed, provider-neutral summary of challenge/justification
// state for Stage-1 assessment. No private chain-of-thought.
type ChallengeContext struct {
	ChallengeID              string
	State                    string
	BlockClass               string
	ReasonCode               string
	Appealability            string
	RequiredClaims           []string
	RetryBudget              int
	ExpiresAtSequence        int64
	OriginalActionID         string
	ActionFingerprint        string
	ConcreteValue            string
	PreventedFailureOrThreat string
	EstimatedCost            string
	AlternativesConsidered   string
	ScopeLimit               string
	VerificationPlan         string
	RollbackPlan             string
	// Claims are closed claim names only (no free-form private reasoning).
	Claims []string
	// EvidenceEventIDs are justification-supporting evidence ids.
	EvidenceEventIDs []string
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
	LatencyMS    int64
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
		// Deterministic: first sorted allowed evidence ID (never map iteration).
		allow := evidenceAllowlist(in)
		ids := SortedEvidenceIDs(allow)
		if len(ids) > 0 {
			out.EvidenceEventIDs = []string{ids[0]}
		}
	case "malformed_output":
		out.ParseStatus = ParseStatusInvalid
		out.Severity = 0
		out.ReasonCode = "UNKNOWN"
	case "user_exception", "repo_policy_exception", "flaky_investigation":
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
		SchemaVersion: SchemaProviderResult,
		Assessment:    out,
		Usage:         ProviderUsage{UsagePresent: false},
		Meta: ProviderMeta{
			Provider:     kind,
			ModelID:      "fake",
			ModelVersion: "v0",
			LatencyMS:    out.LatencyMS,
			ParseStatus:  out.ParseStatus,
		},
	}, nil
}

// ProviderRequestOptions control production vs fixture construction.
type ProviderRequestOptions struct {
	// AllowLegacyFixtureIDs enables ID-only trajectory for #105 Fake fixtures only.
	AllowLegacyFixtureIDs bool
	// Production requires strict non-empty PolicyClass and meaningful TaskAnchor when digests set.
	Production bool
}

// NewProviderRequest builds a production ProviderRequest with default prompt plan and bounds.
// Merges upstream WindowMeta provenance with local N/B bounding (never clears
// upstream Truncated merely because the already-reduced slice fits).
// Production=true enforces closed PolicyClass and model-safe ProposedAction.
func NewProviderRequest(in ClassifierInput) (ProviderRequest, error) {
	return NewProviderRequestWithOptions(in, ProviderRequestOptions{Production: true})
}

// NewFixtureProviderRequest builds a request for #105 Fake/fixture compatibility.
// Explicitly allows legacy ID-only evidence under closed bounds.
func NewFixtureProviderRequest(in ClassifierInput) (ProviderRequest, error) {
	return NewProviderRequestWithOptions(in, ProviderRequestOptions{AllowLegacyFixtureIDs: true})
}

// NewProviderRequestWithOptions builds a ProviderRequest under the given options.
func NewProviderRequestWithOptions(in ClassifierInput, opts ProviderRequestOptions) (ProviderRequest, error) {
	if in.SchemaVersion == "" {
		in.SchemaVersion = SchemaClassifierInput
	}
	in.AllowLegacyFixtureIDs = opts.AllowLegacyFixtureIDs
	// Production defaults empty policy to PRODUCTIVITY (closed allowlist only).
	if opts.Production && in.PolicyClass == "" {
		in.PolicyClass = PolicyClassProductivity
	}

	// Production model path: reject legacy ID-only trajectory.
	if !opts.AllowLegacyFixtureIDs {
		if (len(in.RecentEventIDs) > 0 || len(in.RelatedEventIDs) > 0) &&
			len(in.RecentEvents) == 0 && len(in.RelatedEvents) == 0 {
			return ProviderRequest{}, newProviderError("config", "legacy event IDs without digests rejected for production", false, 0)
		}
	}

	// Bound trajectory; merge upstream Window provenance.
	if len(in.RecentEvents) > 0 || len(in.RelatedEvents) > 0 {
		upstream := in.Window
		r, rel, local := BoundTrajectory(in.RecentEvents, in.RelatedEvents, MaxRecentEvents, MaxTrajectoryBytes)
		in.RecentEvents = r
		in.RelatedEvents = rel
		in.Window = MergeWindowMeta(upstream, local, r, rel)
		in.RecentEventIDs = digestIDs(r)
		in.RelatedEventIDs = digestIDs(rel)
	} else if opts.AllowLegacyFixtureIDs {
		if err := ValidateLegacyFixtureIDs(in.RecentEventIDs, in.RelatedEventIDs); err != nil {
			return ProviderRequest{}, err
		}
	}

	if err := ValidateClassifierInputOpts(in, opts); err != nil {
		return ProviderRequest{}, err
	}
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

func digestIDs(events []EventDigest) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.EventID)
	}
	return out
}

func evidenceAllowlist(in ClassifierInput) map[string]struct{} {
	if len(in.RecentEvents) > 0 || len(in.RelatedEvents) > 0 {
		return EvidenceAllowlistFromDigests(in.RecentEvents, in.RelatedEvents)
	}
	return AllowedEvidenceSet(in)
}
