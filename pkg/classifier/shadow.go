package classifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	// RecentEventIDs bounded list for evidence validation.
	RecentEventIDs []string
	// Threshold provisional (default 50) — not calibrated.
	Threshold int
	// ProfileID for audit.
	ProfileID string
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
}

// ShadowClassifier runs Stage0 → optional Stage1 → Stage2 with Enforced=false.
type ShadowClassifier struct {
	Provider ClassifierProvider
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
		cin := ClassifierInput{
			SchemaVersion: SchemaClassifierInput,
			SessionID:     in.SessionID,
			PolicyClass:   in.PolicyClass,
			RulesetID:     in.RulesetID,
			RulesetHash:   in.RulesetHash,
			// FixtureName empty for real path; Fake can key off other fields later.
		}
		// For FakeClassifierProvider tests, allow embedding fixture in RulesetID prefix "fixture:"
		if len(in.RulesetID) > 8 && in.RulesetID[:8] == "fixture:" {
			cin.FixtureName = in.RulesetID[8:]
			cin.RulesetID = "test"
		}
		var err error
		raw, err = s.Provider.Assess(ctx, cin)
		if err != nil {
			// PRODUCTIVITY fail-open
			if in.PolicyClass == PolicyClassSecurity {
				res.Decision = DecisionBlock
				res.ResolverReason = "fail_closed_security"
				res.ReasonCode = "provider_unavailable"
				raw.ParseStatus = "error"
			} else {
				res.Decision = DecisionAllow
				res.ResolverReason = "fail_open_productivity"
				res.ReasonCode = "provider_unavailable"
				raw.ParseStatus = "error"
			}
		} else if raw.ParseStatus != "ok" {
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
			// Stage 2 deterministic threshold
			res.RawSeverity = raw.Severity
			if !ValidateSeverity(raw.Severity) {
				res.Decision = DecisionAllow
				res.ResolverReason = "fail_open_productivity"
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
		}
	}

	// Map HookGate to allow-ish vs block-ish for disagreement.
	hgBlock := in.HookGateAction == adapter.HookActionDeny || in.HookGateAction == adapter.HookActionDefer
	predBlock := res.Decision == DecisionBlock
	dis := hgBlock != predBlock

	ih := hashShadowInput(in)
	return ShadowResult{
		Raw:            raw,
		Resolved:       res,
		Disagreement:   dis,
		HookGateAction: in.HookGateAction,
		InputHash:      ih,
		CreatedAt:      now,
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

// AuditJSON returns a closed audit record blob.
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
	return json.Marshal(rec)
}

// String for debug logs.
func (r ShadowResult) String() string {
	return fmt.Sprintf("shadow decision=%s severity=%d enforced=%v disagree=%v",
		r.Resolved.Decision, r.Resolved.RawSeverity, r.Resolved.Enforced, r.Disagreement)
}
