package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
	"github.com/ImL1s/reinframe/pkg/detector"
)

// Runner executes a dataset offline with deterministic fake classifier (#100).
type Runner struct {
	// Commit is Reinframe git SHA (optional; set by CLI).
	Commit string
	// DatasetVersion labels the dataset.
	DatasetVersion string
	// RulesetID / RulesetHash pinned in reports.
	RulesetID   string
	RulesetHash string
	// ThresholdProfile provisional profile id.
	ThresholdProfile string
}

// LoadCasesDir loads all *.json case files from a directory.
func LoadCasesDir(dir string) ([]Case, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var cases []Case
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var c Case
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if c.SchemaVersion != DatasetSchemaVersion {
			return nil, fmt.Errorf("%s: schema_version want %s got %q", e.Name(), DatasetSchemaVersion, c.SchemaVersion)
		}
		if c.CaseID == "" || c.Kind == "" {
			return nil, fmt.Errorf("%s: case_id and kind required", e.Name())
		}
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].CaseID < cases[j].CaseID })
	return cases, nil
}

// DatasetHash is a stable hash of sorted case JSON bytes.
func DatasetHash(cases []Case) (string, error) {
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

// Run executes all cases and returns a report. HardGateEnabled is always false.
func (r *Runner) Run(ctx context.Context, cases []Case) (Report, error) {
	if r.DatasetVersion == "" {
		r.DatasetVersion = "synthetic-m3-v1"
	}
	if r.RulesetID == "" {
		r.RulesetID = "provisional-default"
	}
	if r.RulesetHash == "" {
		r.RulesetHash = "synthetic"
	}
	if r.ThresholdProfile == "" {
		r.ThresholdProfile = "provisional-50"
	}
	dh, err := DatasetHash(cases)
	if err != nil {
		return Report{}, err
	}
	rep := Report{
		SchemaVersion:     ReportSchemaVersion,
		ReinframeCommit:   r.Commit,
		DatasetVersion:    r.DatasetVersion,
		DatasetHash:       dh,
		ClassifierModelID: "fake",
		RulesetID:         r.RulesetID,
		RulesetHash:       r.RulesetHash,
		ThresholdProfile:  r.ThresholdProfile,
		GOOS:              runtime.GOOS,
		GOARCH:            runtime.GOARCH,
		HardGateEnabled:   false,
		Metrics: AggregateMetrics{
			DetectorByKind: map[string]BinaryMetrics{},
		},
		Meta: map[string]string{
			"note": "severity threshold 50 is provisional — not calibrated hard-gate",
		},
	}

	for _, c := range cases {
		cr := r.runCase(ctx, c)
		rep.Cases = append(rep.Cases, cr)
		switch c.ScenarioClass {
		case ClassHealthy:
			rep.Metrics.HealthyCases++
		case ClassPositiveDeviation:
			rep.Metrics.PositiveCases++
		case ClassBoundary:
			rep.Metrics.BoundaryCases++
		}
	}
	rep.Metrics = aggregate(rep.Cases)
	rep.Disposition, rep.DispositionNote = disposition(rep)
	return rep, nil
}

func (r *Runner) runCase(ctx context.Context, c Case) CaseResult {
	cr := CaseResult{
		CaseID:             c.CaseID,
		Kind:               c.Kind,
		ScenarioClass:      c.ScenarioClass,
		ExpectDetectorFire: c.ExpectDetectorFire,
		ExpectStage2:       c.ExpectStage2Decision,
		Enforced:           false,
	}
	switch c.Kind {
	case KindRepeatedFailure:
		d := detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3})
		fired := false
		for i, f := range c.Failures {
			if _, ok := d.ObserveRaw(c.CaseID, f, fmt.Sprintf("e%d", i)); ok {
				fired = true
			}
		}
		cr.DetectorFired = fired
		scoreBinary(&cr)
	case KindVerificationChurn:
		d := detector.NewVerificationChurnDetector(detector.VerificationChurnConfig{})
		fired := false
		for _, a := range c.ValidationAttempts {
			att := detector.ValidationAttempt{
				Command: a.Command, TargetScope: a.TargetScope, WorkspaceRev: a.WorkspaceRev,
				ContractRevision: a.ContractRevision, Purpose: a.Purpose, Succeeded: a.Succeeded,
				FlakyInvestigation: a.FlakyInvestigation, PolicyRequiresRerun: a.PolicyRequiresRerun,
				HighRiskIndependent: a.HighRiskIndependent, WorkspaceChanged: a.WorkspaceChanged,
			}
			if _, ok := d.Observe(c.CaseID, att); ok {
				fired = true
			}
		}
		cr.DetectorFired = fired
		scoreBinary(&cr)
	case KindToolBudget:
		max := c.ToolBudgetMax
		if max <= 0 {
			max = 5
		}
		d := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{MaxToolCalls: max})
		fired := false
		for i, tool := range c.ToolCalls {
			if c.ProgressAfter > 0 && i == c.ProgressAfter {
				d.MarkProgress(c.CaseID)
			}
			if _, ok := d.ObserveToolWithBudget(c.CaseID, tool, max); ok {
				fired = true
			}
		}
		cr.DetectorFired = fired
		scoreBinary(&cr)
	case KindHypothesisLoop:
		d := detector.NewHypothesisLoopDetector(detector.HypothesisLoopConfig{Threshold: 3})
		fired := false
		for _, h := range c.Hypotheses {
			if _, ok := d.Observe(c.CaseID, detector.HypothesisObservation{Text: h.Text, EvidenceIDs: h.EvidenceIDs}); ok {
				fired = true
			}
		}
		cr.DetectorFired = fired
		scoreBinary(&cr)
	case KindClassifierShadow:
		s := &classifier.ShadowClassifier{Provider: classifier.FakeClassifierProvider{}}
		th := c.Threshold
		if th <= 0 {
			th = 50
		}
		hg := c.HookGateAction
		if hg == "" {
			hg = adapter.HookActionAllow
		}
		pa := adapter.ProposedAction{
			SchemaVersion: adapter.ProposedActionSchemaVersion,
			SessionID:     c.CaseID,
			ToolName:      c.ProposedToolName,
			Command:       c.ProposedCommand,
			ToolClass:     adapter.ClassifyToolName(c.ProposedToolName),
			Source:        "benchmark",
		}
		if pa.ToolName == "" {
			pa.ToolName = "Bash"
			pa.ToolClass = adapter.ToolClassShell
		}
		res, err := s.EvaluateShadow(ctx, classifier.ShadowInput{
			SessionID:           c.CaseID,
			Proposed:            pa,
			HookGateAction:      hg,
			FixtureName:         c.ClassifierFixture,
			UserException:       c.UserException,
			RepoPolicyException: c.RepoPolicyException,
			FlakyInvestigation:  c.FlakyInvestigation,
			Threshold:           th,
			RulesetID:           r.RulesetID,
			RulesetHash:         r.RulesetHash,
		})
		if err != nil {
			cr.Error = err.Error()
			return cr
		}
		cr.Stage2Decision = res.Resolved.Decision
		cr.RawSeverity = res.Resolved.RawSeverity
		cr.ResolverReason = res.Resolved.ResolverReason
		cr.Enforced = res.Resolved.Enforced
		if cr.Enforced {
			cr.Error = "shadow enforced=true (forbidden)"
		}
		if c.ExpectStage2Decision != "" {
			ok := cr.Stage2Decision == c.ExpectStage2Decision
			cr.Stage2Correct = &ok
			// False block: healthy/expect ALLOW but got BLOCK
			if c.ExpectStage2Decision == classifier.DecisionAllow && cr.Stage2Decision == classifier.DecisionBlock {
				cr.FalseBlock = true
			}
			if c.ExpectStage2Decision == classifier.DecisionBlock && cr.Stage2Decision == classifier.DecisionAllow {
				cr.FalseAllow = true
			}
		}
		if res.Raw.ParseStatus != "" && res.Raw.ParseStatus != "ok" {
			// counted at aggregate
		}
	default:
		cr.Error = "unknown kind: " + c.Kind
	}
	return cr
}

func scoreBinary(cr *CaseResult) {
	if cr.ExpectDetectorFire == nil {
		return
	}
	exp := *cr.ExpectDetectorFire
	got := cr.DetectorFired
	switch {
	case exp && got:
		cr.DetectorTP = true
	case !exp && got:
		cr.DetectorFP = true
	case exp && !got:
		cr.DetectorFN = true
	default:
		cr.DetectorTN = true
	}
}

func aggregate(cases []CaseResult) AggregateMetrics {
	m := AggregateMetrics{DetectorByKind: map[string]BinaryMetrics{}}
	var cTP, cFP, cFN, cTN int
	var fb, fa, classN int
	for _, cr := range cases {
		m.SampleSize++
		switch cr.ScenarioClass {
		case ClassHealthy:
			m.HealthyCases++
		case ClassPositiveDeviation:
			m.PositiveCases++
		case ClassBoundary:
			m.BoundaryCases++
		}
		if cr.Kind == KindClassifierShadow {
			classN++
			if cr.Stage2Correct != nil {
				if *cr.Stage2Correct {
					if cr.ExpectStage2 == classifier.DecisionBlock {
						cTP++
					} else {
						cTN++
					}
				} else {
					if cr.FalseBlock {
						cFP++
						fb++
					}
					if cr.FalseAllow {
						cFN++
						fa++
					}
				}
			}
			if cr.ResolverReason == "parse_invalid" || cr.ResolverReason == "fail_open_productivity" {
				m.ParseFailCount++
			}
			if cr.ResolverReason == "provider_unavailable" {
				m.ProviderErrCount++
			}
			continue
		}
		bm := m.DetectorByKind[cr.Kind]
		if cr.DetectorTP {
			bm.TP++
		}
		if cr.DetectorFP {
			bm.FP++
		}
		if cr.DetectorFN {
			bm.FN++
		}
		if cr.DetectorTN {
			bm.TN++
		}
		m.DetectorByKind[cr.Kind] = finishBinary(bm)
	}
	m.ClassifierShadow = finishBinary(BinaryMetrics{TP: cTP, FP: cFP, FN: cFN, TN: cTN})
	if classN > 0 {
		m.FalseBlockRate = float64(fb) / float64(classN)
		m.FalseAllowRate = float64(fa) / float64(classN)
	}
	// re-finish detector maps
	for k, bm := range m.DetectorByKind {
		m.DetectorByKind[k] = finishBinary(bm)
	}
	return m
}

func finishBinary(b BinaryMetrics) BinaryMetrics {
	denP := b.TP + b.FP
	denR := b.TP + b.FN
	if denP > 0 {
		b.Precision = float64(b.TP) / float64(denP)
	}
	if denR > 0 {
		b.Recall = float64(b.TP) / float64(denR)
	}
	if b.Precision+b.Recall > 0 {
		b.F1 = 2 * b.Precision * b.Recall / (b.Precision + b.Recall)
	}
	return b
}

func disposition(rep Report) (string, string) {
	// Honest: tiny synthetic sample → MORE-DATA or NO-GO for hard-gate promotion.
	n := rep.Metrics.SampleSize
	if n < 30 {
		return "MORE-DATA", fmt.Sprintf(
			"sample_size=%d is too small for scientific calibration; false_block_rate=%.3f (provisional). Keep shadow/advisory; do not enable hard-gates.",
			n, rep.Metrics.FalseBlockRate)
	}
	if rep.Metrics.FalseBlockRate > 0.05 {
		return "NO-GO", fmt.Sprintf("false_block_rate=%.3f exceeds conservative PRODUCTIVITY ceiling 0.05 on this dataset", rep.Metrics.FalseBlockRate)
	}
	return "MORE-DATA", "synthetic suite only; expand anonymized-replay before LIMITED-GO"
}
