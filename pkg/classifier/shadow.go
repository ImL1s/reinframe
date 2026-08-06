package classifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// ShadowInput is the runtime input for shadow-mode classification (#105).
type ShadowInput struct {
	SessionID      string
	Proposed       adapter.ProposedAction
	HookGateAction string // allow|deny|defer from existing gate
	PolicyClass    string
	RulesetID      string
	RulesetHash    string
	// Stage0Block when true, skip provider and resolve BLOCK with stage0_block.
	Stage0Block  bool
	Stage0Reason string
	// RecentEventIDs bounded list for evidence validation (legacy #105 fixtures).
	RecentEventIDs []string
	// RelatedEventIDs legacy related evidence ids.
	RelatedEventIDs []string
	// Task / trajectory packet (wire §5 recent-N). When digests are set they
	// bind prompt identity and evidence allowlist; IDs are synced by NewProviderRequest.
	TaskAnchor       TaskAnchor
	ContractRevision int
	EvidenceRevision int
	RecentEvents     []EventDigest
	RelatedEvents    []EventDigest
	// Threshold provisional (default 50) — not calibrated.
	Threshold int
	// ProfileID for audit.
	ProfileID string
	// Stage2 exception flags (applied after raw score).
	UserException       bool
	RepoPolicyException bool
	FlakyInvestigation  bool
	// FixtureName for fake provider routing in tests.
	FixtureName string
}

// ResolvedDecision is the closed Stage 2 outcome.
type ResolvedDecision struct {
	SchemaVersion  string `json:"schema_version"`
	Decision       string `json:"decision"` // ALLOW | BLOCK
	RawSeverity    int    `json:"raw_severity"`
	Threshold      int    `json:"threshold"`
	ReasonCode     string `json:"reason_code"`
	ResolverReason string `json:"resolver_reason"`
	Feedback       string `json:"feedback,omitempty"`
	Enforced       bool   `json:"enforced"` // always false in shadow
	ProfileID      string `json:"profile_id"`
}

// ShadowResult is the audit package for one shadow evaluation.
type ShadowResult struct {
	Raw      RawAssessment
	Resolved ResolvedDecision
	// Disagreement is true when Resolved.Decision differs from HookGate mapping.
	Disagreement bool
	// HookGateAction echoed for audit.
	HookGateAction string
	// InputHash for audit.
	InputHash string
	// CreatedAt UTC
	CreatedAt time.Time
	// ProviderCall is the closed provider-call audit when Stage1 ran (may be nil).
	// Usage/Meta from ProviderResult are retained here rather than discarded.
	ProviderCall *ProviderCallAudit
	// ProviderResult retains full envelope when Stage1 ran (tests/telemetry).
	LastProviderResult *ProviderResult
}

// ShadowClassifier runs Stage0 → optional Stage1 → Stage2 with Enforced=false.
type ShadowClassifier struct {
	Provider ClassifierProvider
	// Observer receives best-effort provider-call audits (nil-safe).
	Observer ProviderCallObserver
	// Now for tests.
	Now func() time.Time
}

// EvaluateShadow never enforces BLOCK; HookGate remains authoritative.
func (s *ShadowClassifier) EvaluateShadow(ctx context.Context, in ShadowInput) (ShadowResult, error) {
	if in.PolicyClass == "" {
		in.PolicyClass = PolicyClassProductivity
	}
	if in.Threshold <= 0 {
		in.Threshold = 50 // provisional — not calibration
	}
	if in.ProfileID == "" {
		in.ProfileID = "provisional-default"
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}

	var raw RawAssessment
	var res ResolvedDecision
	var providerAudit *ProviderCallAudit
	var lastPres *ProviderResult
	res.SchemaVersion = SchemaResolvedDecision
	res.Threshold = in.Threshold
	res.ProfileID = in.ProfileID
	res.Enforced = false // #105 hard rule

	if in.Stage0Block {
		raw = RawAssessment{
			SchemaVersion: SchemaRawAssessment,
			Severity:      100,
			ReasonCode:    "OVER_SOP",
			ParseStatus:   "ok",
			RulesetID:     in.RulesetID,
			RulesetHash:   in.RulesetHash,
		}
		res.Decision = DecisionBlock
		res.RawSeverity = 100
		res.ReasonCode = "above_threshold"
		res.ResolverReason = "stage0_block"
		if in.Stage0Reason != "" {
			res.Feedback = in.Stage0Reason
		}
	} else if s.Provider == nil {
		raw = RawAssessment{SchemaVersion: SchemaRawAssessment, Severity: 0, ReasonCode: "NORMAL_PROGRESS", ParseStatus: "ok"}
		res.Decision = DecisionAllow
		res.RawSeverity = 0
		res.ReasonCode = "below_threshold"
		res.ResolverReason = "stage0_skip"
	} else {
		pa := in.Proposed
		cin := ClassifierInput{
			SchemaVersion:       SchemaClassifierInput,
			SessionID:           in.SessionID,
			PolicyClass:         in.PolicyClass,
			RulesetID:           in.RulesetID,
			RulesetHash:         in.RulesetHash,
			FixtureName:         in.FixtureName,
			ProposedAction:      &pa,
			TaskAnchor:          in.TaskAnchor,
			ContractRevision:    in.ContractRevision,
			EvidenceRevision:    in.EvidenceRevision,
			RecentEvents:        append([]EventDigest(nil), in.RecentEvents...),
			RelatedEvents:       append([]EventDigest(nil), in.RelatedEvents...),
			RecentEventIDs:      append([]string(nil), in.RecentEventIDs...),
			RelatedEventIDs:     append([]string(nil), in.RelatedEventIDs...),
			UserException:       in.UserException,
			RepoPolicyException: in.RepoPolicyException,
			FlakyInvestigation:  in.FlakyInvestigation,
		}
		if cin.FixtureName == "" && len(in.RulesetID) > 8 && in.RulesetID[:8] == "fixture:" {
			cin.FixtureName = in.RulesetID[8:]
			cin.RulesetID = "test"
		}
		// FixtureName / legacy-ID shadow cases use explicit fixture request builder.
		var preq ProviderRequest
		var perr error
		if cin.FixtureName != "" || needsFixtureRequest(cin) {
			preq, perr = NewFixtureProviderRequest(cin)
		} else {
			preq, perr = NewProviderRequest(cin)
		}
		var err error
		var pres ProviderResult
		if perr != nil {
			err = perr
			raw = RawAssessment{SchemaVersion: SchemaRawAssessment, ParseStatus: ParseStatusError}
		} else {
			pres, err = s.Provider.Assess(ctx, preq)
			// Nil-error results must pass request-bound validation before Stage 2.
			if err == nil {
				if verr := ValidateProviderResultForRequest(preq, pres); verr != nil {
					err = verr
					pres.Meta.ParseStatus = ParseStatusError
					pres.Meta.ErrorClass = "parse"
					pres.Meta.FallbackReason = "contract"
				}
			}
			raw = pres.Assessment
			if err != nil && raw.ParseStatus == "" {
				raw.SchemaVersion = SchemaRawAssessment
				raw.ParseStatus = ParseStatusError
			}
			presCopy := pres
			lastPres = &presCopy
			// Capture audit before optional Observer (durable public surface).
			a := BuildProviderCallAudit(preq, pres, in.SessionID, "", now)
			if err != nil {
				if isAdapterTimeout(err) {
					a.ErrorClass = "timeout"
					a.FallbackReason = "timeout"
					a.ParseStatus = ParseStatusError
				} else if isParentContextAbort(err) {
					a.ErrorClass = "canceled"
					a.FallbackReason = "canceled"
					a.ParseStatus = ParseStatusError
				} else {
					var pe *ProviderError
					if errors.As(err, &pe) && pe != nil {
						a.ErrorClass = pe.Class
						a.FallbackReason = pe.Class
					} else {
						a.ErrorClass = "transport"
						a.FallbackReason = "transport"
					}
					a.ParseStatus = ParseStatusError
				}
			}
			providerAudit = &a
			if s.Observer != nil {
				// Detached best-effort; parent cancel must not erase retained audit.
				obsCtx := context.WithoutCancel(ctx)
				_ = s.Observer.RecordProviderCall(obsCtx, a)
			}
		}
		if err != nil {
			// Parent cancel/deadline: preserve identity — never productivity fail-open ALLOW.
			// Adapter-owned timeout is ordinary provider failure (policy matrix), not cancel.
			if isParentContextAbort(err) {
				raw.ParseStatus = ParseStatusError
				res.Decision = DecisionBlock
				res.ResolverReason = "provider_context_canceled"
				res.ReasonCode = "provider_unavailable"
				ih := hashShadowInput(in)
				return ShadowResult{
					Raw: raw, Resolved: res, Disagreement: false,
					HookGateAction: in.HookGateAction, InputHash: ih, CreatedAt: now,
					ProviderCall: providerAudit, LastProviderResult: lastPres,
				}, err
			} else if in.PolicyClass == PolicyClassSecurity {
				// PRODUCTIVITY fail-open / SECURITY fail-closed — resolver owns ordinary failures
				// including adapter-owned timeout (typed ProviderError class=timeout).
				res.Decision = DecisionBlock
				res.ResolverReason = "fail_closed_security"
				res.ReasonCode = "provider_unavailable"
				raw.ParseStatus = ParseStatusError
			} else {
				res.Decision = DecisionAllow
				res.ResolverReason = "fail_open_productivity"
				res.ReasonCode = "provider_unavailable"
				raw.ParseStatus = ParseStatusError
			}
		} else if raw.ParseStatus != "ok" && raw.ParseStatus != ParseStatusOK {
			if in.PolicyClass == PolicyClassSecurity {
				res.Decision = DecisionBlock
				res.ResolverReason = "fail_closed_security"
				res.ReasonCode = "parse_invalid"
			} else {
				res.Decision = DecisionAllow
				res.ResolverReason = "fail_open_productivity"
				res.ReasonCode = "parse_invalid"
			}
		} else {
			// Stage 2 deterministic threshold then exceptions
			res.RawSeverity = raw.Severity
			if !ValidateSeverity(raw.Severity) {
				if in.PolicyClass == PolicyClassSecurity {
					res.Decision = DecisionBlock
					res.ResolverReason = "fail_closed_security"
				} else {
					res.Decision = DecisionAllow
					res.ResolverReason = "fail_open_productivity"
				}
				res.ReasonCode = "parse_invalid"
			} else if raw.Severity >= in.Threshold {
				res.Decision = DecisionBlock
				res.ReasonCode = "above_threshold"
				res.ResolverReason = "stage1_applied"
			} else {
				res.Decision = DecisionAllow
				res.ReasonCode = "below_threshold"
				res.ResolverReason = "stage1_applied"
			}
			// Exceptions after raw scoring (may flip BLOCK → ALLOW)
			if res.Decision == DecisionBlock {
				if in.UserException {
					res.Decision = DecisionAllow
					res.ResolverReason = "user_exception"
					res.ReasonCode = "user_exception"
				} else if in.RepoPolicyException {
					res.Decision = DecisionAllow
					res.ResolverReason = "repo_policy_exception"
					res.ReasonCode = "repo_policy_exception"
				} else if in.FlakyInvestigation {
					res.Decision = DecisionAllow
					res.ResolverReason = "flaky_investigation"
					res.ReasonCode = "flaky_investigation"
				}
			}
		}
	}

	// Map HookGate to allow-ish vs block-ish for disagreement.
	hgBlock := in.HookGateAction == adapter.HookActionDeny
	predBlock := res.Decision == DecisionBlock
	dis := hgBlock != predBlock

	ih := hashShadowInput(in)
	return ShadowResult{
		Raw:                raw,
		Resolved:           res,
		Disagreement:       dis,
		HookGateAction:     in.HookGateAction,
		InputHash:          ih,
		CreatedAt:          now,
		ProviderCall:       providerAudit,
		LastProviderResult: lastPres,
	}, nil
}

func hashShadowInput(in ShadowInput) string {
	b, _ := json.Marshal(struct {
		S string
		T string
		C string
		A string
	}{in.SessionID, in.Proposed.ToolName, in.Proposed.Command, in.HookGateAction})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// Stage0FullSuiteBlock is a helper: simple disproportionate full suite → Stage0 block signal.
func Stage0FullSuiteBlock(pa adapter.ProposedAction, criteriaMet bool, simpleLowRisk bool) (bool, string) {
	if !simpleLowRisk || !criteriaMet {
		return false, ""
	}
	if adapter.FullSuiteCommand(pa) {
		return true, "stage0_over_sop_full_suite"
	}
	return false, ""
}

// AuditJSON returns a closed audit record blob including provider-call telemetry.
func (r ShadowResult) AuditJSON() ([]byte, error) {
	rec := map[string]any{
		"schema_version":  SchemaClassifierAudit,
		"raw":             r.Raw,
		"resolved":        r.Resolved,
		"hookgate_action": r.HookGateAction,
		"disagreement":    r.Disagreement,
		"input_hash":      r.InputHash,
		"created_at":      r.CreatedAt.Format(time.RFC3339Nano),
		"enforced":        false,
	}
	if r.ProviderCall != nil {
		rec["provider_call"] = *r.ProviderCall
	}
	return json.Marshal(rec)
}

// needsFixtureRequest is true when only legacy ID lists are present (no digests).
func needsFixtureRequest(in ClassifierInput) bool {
	return (len(in.RecentEventIDs) > 0 || len(in.RelatedEventIDs) > 0) &&
		len(in.RecentEvents) == 0 && len(in.RelatedEvents) == 0
}

// String for debug logs.
func (r ShadowResult) String() string {
	return fmt.Sprintf("shadow decision=%s severity=%d enforced=%v disagree=%v",
		r.Resolved.Decision, r.Resolved.RawSeverity, r.Resolved.Enforced, r.Disagreement)
}
