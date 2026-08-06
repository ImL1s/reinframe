package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ImL1s/reinframe/pkg/challenge"
)

// ChallengeRunner executes #140 Lane A deterministic challenge fixtures.
type ChallengeRunner struct {
	Commit         string
	DatasetVersion string
}

// LoadChallengeCasesDir loads *.json challenge cases.
func LoadChallengeCasesDir(dir string) ([]ChallengeCase, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var cases []ChallengeCase
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var c ChallengeCase
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if err := ValidateChallengeCase(c); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if c.Source == "" {
			c.Source = "synthetic"
		}
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].CaseID < cases[j].CaseID })
	return cases, nil
}

// ChallengeDatasetHash is a stable hash of sorted cases.
func ChallengeDatasetHash(cases []ChallengeCase) (string, error) {
	h := sha256.New()
	for _, c := range cases {
		b, err := json.Marshal(c)
		if err != nil {
			return "", err
		}
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// RunLaneA executes all cases with a fresh challenge.Service per case (isolation).
func (r *ChallengeRunner) RunLaneA(ctx context.Context, cases []ChallengeCase) (ChallengeReport, error) {
	if r.DatasetVersion == "" {
		r.DatasetVersion = "challenge-lane-a-v1"
	}
	dh, err := ChallengeDatasetHash(cases)
	if err != nil {
		return ChallengeReport{}, err
	}
	rep := ChallengeReport{
		SchemaVersion:   ChallengeReportSchemaVersion,
		Lane:            ChallengeLaneDeterministic,
		Commit:          r.Commit,
		DatasetVersion:  r.DatasetVersion,
		DatasetHash:     dh,
		FingerprintNote: "challenge.ComputeFingerprint + ClassifyRelationship (semantic, not universal)",
		HardGateEnabled: false,
		Disposition:     "MORE-DATA",
		DispositionNote: "Lane A deterministic core only; model-backed and Claude host lanes not scored. " +
			"No hard-gate enablement. Metrics are fixture-bounded, not production prevalence.",
	}
	var metrics ChallengeMetrics
	metrics.HardGateEnabled = false

	for _, c := range cases {
		if err := ctx.Err(); err != nil {
			return ChallengeReport{}, err
		}
		res := runOneChallengeCase(ctx, c)
		rep.Results = append(rep.Results, res)
		accumulateChallengeMetrics(&metrics, c, res)
	}
	metrics.CasesTotal = len(cases)
	rep.Metrics = metrics
	// Disposition stays MORE-DATA unless every case fails open path entirely.
	if metrics.CasesTotal > 0 && metrics.OpenAppealMatch+metrics.NonAppealableRoutedOK+metrics.HumanReviewRoutedOK == 0 &&
		metrics.AppealableCases+metrics.NonAppealableCases+metrics.HumanReviewCases > 0 {
		rep.Disposition = "NO-GO"
		rep.DispositionNote = "Lane A core routing failures dominate; investigate challenge service regressions."
	}
	return rep, nil
}

func runOneChallengeCase(ctx context.Context, c ChallengeCase) ChallengeCaseResult {
	out := ChallengeCaseResult{CaseID: c.CaseID, Kind: c.Kind}
	if c.ExpectNoChallenge || c.Kind == ChallengeKindHealthyNoChallenge {
		// Healthy counterexample: no Open is performed.
		out.PassOpen = true
		out.PassJustify = true
		out.PassRetry = true
		out.PassCase = true
		out.OpenMatch = true
		return out
	}

	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := c.Proposed.ToProposedAction(c.SessionID)
	rec, openErr := svc.Open(ctx, challenge.OpenRequest{
		SessionID:        c.SessionID,
		Proposed:         pa,
		BlockClass:       c.BlockClass,
		ReasonCode:       firstNonEmptyStr(c.ReasonCode, "BLOCK"),
		PolicyClass:      firstNonEmptyStr(c.PolicyClass, challenge.PolicyClassProductivity),
		PolicyVersion:    c.PolicyVersion,
		RulesetHash:      c.RulesetHash,
		Branch:           c.Branch,
		KnownEvidenceIDs: append([]string(nil), c.KnownEvidenceIDs...),
		CorrelationID:    "eval-" + c.CaseID,
	})
	out.AddedTurns++
	if openErr != nil {
		out.OpenOK = false
		out.OpenError = openErr.Error()
		out.ObservedAppeal = rec.Appealability
		out.ObservedOpenState = string(rec.State)
		// Non-appealable path returns error with filled record.
		if c.ExpectOpenError || c.ExpectAppealability == challenge.AppealNonAppealable {
			out.OpenMatch = rec.Appealability == challenge.AppealNonAppealable ||
				strings.Contains(openErr.Error(), "non-appealable")
			out.PassOpen = out.OpenMatch
		} else {
			out.OpenMatch = false
			out.PassOpen = false
		}
	} else {
		out.OpenOK = true
		out.ObservedAppeal = rec.Appealability
		out.ObservedOpenState = string(rec.State)
		out.OpenMatch = matchOpen(c, rec)
		out.PassOpen = out.OpenMatch
	}

	// Justify
	if c.Justification != nil && out.OpenOK && rec.ChallengeID != "" {
		out.JustifyAttempted = true
		out.AddedTurns++
		j := challenge.Justification{
			SchemaVersion:              challenge.SchemaJustification,
			ChallengeID:                rec.ChallengeID,
			ConcreteValue:              c.Justification.ConcreteValue,
			PreventedFailureOrThreat:   c.Justification.PreventedFailureOrThreat,
			EstimatedCost:              c.Justification.EstimatedCost,
			AlternativesConsidered:     c.Justification.AlternativesConsidered,
			ScopeLimit:                 c.Justification.ScopeLimit,
			VerificationPlan:           c.Justification.VerificationPlan,
			RollbackPlan:               c.Justification.RollbackPlan,
			SupportingEvidenceEventIDs: append([]string(nil), c.Justification.SupportingEvidenceIDs...),
		}
		rec2, jerr := svc.Justify(ctx, j, c.KnownEvidenceIDs)
		if jerr != nil {
			out.JustifyOK = false
			out.JustifyError = jerr.Error()
			if c.ExpectJustifyOK != nil && !*c.ExpectJustifyOK {
				out.JustifyMatch = true
				if c.ExpectJustifyError != "" {
					out.JustifyMatch = strings.Contains(jerr.Error(), c.ExpectJustifyError)
				}
			} else if c.ExpectJustifyOK == nil && c.Kind == ChallengeKindInvalidAppeal {
				out.JustifyMatch = true
			} else {
				out.JustifyMatch = false
			}
		} else {
			out.JustifyOK = true
			rec = rec2
			if c.ExpectJustifyOK != nil {
				out.JustifyMatch = *c.ExpectJustifyOK
			} else {
				out.JustifyMatch = true
			}
		}
		out.PassJustify = out.JustifyMatch
	} else {
		// No justify step expected.
		out.PassJustify = true
		out.JustifyMatch = true
	}

	// Retry
	if c.RetryProposed != nil && rec.ChallengeID != "" {
		out.RetryAttempted = true
		out.AddedTurns++
		rpa := c.RetryProposed.ToProposedAction(c.SessionID)
		var reEval *challenge.ReEvalContext
		if c.RetryUserException {
			reEval = &challenge.ReEvalContext{UserException: true}
		}
		res, _ := svc.AttemptRetry(ctx, challenge.RetryRequest{
			ChallengeID:   rec.ChallengeID,
			SessionID:     c.SessionID,
			Branch:        c.Branch,
			Proposed:      rpa,
			CorrelationID: "eval-retry-" + c.CaseID,
			ReEval:        reEval,
		})
		out.ObservedRelation = res.Relationship
		out.ObservedRetryState = string(res.Record.State)
		out.ObservedStage2 = res.Stage2Decision
		out.ObservedRejected = res.RejectedReason
		out.IdempotentReplay = res.IdempotentReplay
		out.RetryMatch = matchRetry(c, res)

		if c.DuplicateRetry {
			out.AddedTurns++
			res2, _ := svc.AttemptRetry(ctx, challenge.RetryRequest{
				ChallengeID:   rec.ChallengeID,
				SessionID:     c.SessionID,
				Branch:        c.Branch,
				Proposed:      rpa,
				CorrelationID: "eval-retry2-" + c.CaseID,
			})
			// Second attempt must be idempotent or non-increasing budget (business outcome once).
			if !res2.IdempotentReplay && res2.RejectedReason == "" && res.Record.State == challenge.StateAllowedOnce {
				// If first already terminal ALLOWED_ONCE, second should not re-open.
				out.RetryMatch = out.RetryMatch && (res2.IdempotentReplay || res2.RejectedReason != "" ||
					res2.Record.State == challenge.StateAllowedOnce || res2.Record.State == challenge.StateRejected)
			} else {
				out.RetryMatch = out.RetryMatch && (res2.IdempotentReplay || res2.RejectedReason != "" ||
					string(res2.Record.State) == c.ExpectRetryState)
			}
			out.IdempotentReplay = out.IdempotentReplay || res2.IdempotentReplay
		}
		out.PassRetry = out.RetryMatch
	} else {
		out.PassRetry = true
		out.RetryMatch = true
	}

	out.PassCase = out.PassOpen && out.PassJustify && out.PassRetry
	return out
}

func matchOpen(c ChallengeCase, rec challenge.ChallengeRecord) bool {
	if c.ExpectAppealability != "" && c.ExpectAppealability != "none" {
		if rec.Appealability != c.ExpectAppealability {
			return false
		}
	}
	if c.ExpectOpenState != "" && string(rec.State) != c.ExpectOpenState {
		return false
	}
	return true
}

func matchRetry(c ChallengeCase, res challenge.RetryResult) bool {
	ok := true
	if c.ExpectRelationship != "" && res.Relationship != c.ExpectRelationship {
		// Bypass attempts may be rejected before relationship is classified; accept empty relation with reject.
		if !(c.Kind == ChallengeKindBypassAttempt && res.RejectedReason != "" && res.Relationship == "") {
			ok = false
		}
	}
	if c.ExpectRetryState != "" && string(res.Record.State) != c.ExpectRetryState {
		ok = false
	}
	if c.ExpectStage2 != "" && res.Stage2Decision != c.ExpectStage2 {
		ok = false
	}
	if c.ExpectRejectedReason != "" && !strings.Contains(res.RejectedReason, c.ExpectRejectedReason) {
		ok = false
	}
	// Bypass resistance: if expect relationship bypass, final must not be ALLOWED_ONCE success without reject.
	if c.ExpectRelationship == challenge.RelBypass {
		if res.Record.State == challenge.StateAllowedOnce && res.Stage2Decision == challenge.DecisionAllow {
			ok = false
		}
	}
	return ok
}

func accumulateChallengeMetrics(m *ChallengeMetrics, c ChallengeCase, res ChallengeCaseResult) {
	m.TotalAddedTurns += res.AddedTurns
	switch c.Kind {
	case ChallengeKindLegitimateAppeal:
		m.AppealableCases++
		m.ValidAppealAttempts++
		if res.PassOpen {
			m.OpenAppealMatch++
		}
		if res.JustifyAttempted && res.JustifyOK {
			m.ValidAppealAccepted++
		}
		if res.ObservedRetryState == string(challenge.StateAllowedOnce) {
			m.AllowedOnce++
		}
		if res.ObservedRetryState == string(challenge.StateRejected) {
			m.RejectedAfterRetry++
		}
	case ChallengeKindInvalidAppeal:
		m.AppealableCases++
		m.InvalidAppealAttempts++
		if res.PassOpen {
			m.OpenAppealMatch++
		}
		if res.JustifyAttempted && !res.JustifyOK {
			m.InvalidAppealRejected++
		}
	case ChallengeKindBypassAttempt:
		m.BypassAttempts++
		if res.PassRetry {
			m.BypassBlocked++
		}
		if res.PassOpen {
			m.AppealableCases++
			m.OpenAppealMatch++
		}
	case ChallengeKindNonAppealable:
		m.NonAppealableCases++
		if res.PassOpen {
			m.NonAppealableRoutedOK++
		}
	case ChallengeKindHumanReview:
		m.HumanReviewCases++
		if res.PassOpen {
			m.HumanReviewRoutedOK++
		}
		if res.ObservedOpenState == string(challenge.StateHumanReview) {
			m.HumanEscalation++
		}
	case ChallengeKindReducedScope:
		m.ReducedScopeCases++
		if res.PassRetry {
			m.ReducedScopeOK++
		}
	case ChallengeKindDuplicateRetry:
		m.DuplicateRetryCases++
		if res.PassRetry {
			m.DuplicateIdempotentOK++
		}
	}
}

func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
