package protocol

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Contract builder provenance values (CreatedFrom).
const (
	CreatedFromUserExplicit     = "user_explicit"
	CreatedFromRepositoryPolicy = "repository_policy"
	CreatedFromHeuristic        = "heuristic"
	CreatedFromModel            = "model"
)

// Complexity / risk vocabulary (schema enums).
const (
	ComplexityTrivial = "trivial"
	ComplexitySimple  = "simple"
	ComplexityNormal  = "normal"
	ComplexityComplex = "complex"

	RiskLow           = "low"
	RiskMedium        = "medium"
	RiskHigh          = "high"
	RiskIrreversible  = "irreversible"
)

// BuildContractOptions customizes BuildContractFromSubmitted.
// Zero value applies provisional heuristics (not calibrated science).
type BuildContractOptions struct {
	// Now overrides wall clock (tests).
	Now func() time.Time
	// AllowedScope seeds the contract scope whitelist.
	AllowedScope []string
	// CreatedFrom defaults to CreatedFromHeuristic.
	CreatedFrom string
	// ForceComplexity / ForceRisk override heuristics when non-empty.
	ForceComplexity string
	ForceRisk       string
}

// BuildContractFromSubmitted derives a revisioned TaskContract from TaskSubmitted.
//
// Heuristics are provisional knobs for M2.1 intake scaffolding:
//   - short prompts / typo-fix language → simple + low risk
//   - security / migrate / production language → higher risk
//   - long prompts → normal/complex
//
// Callers may revise the contract later (new Revision) when intent changes.
// Does not persist; use EmitTaskContract / store conventions for persistence.
func BuildContractFromSubmitted(sub TaskSubmitted, opts BuildContractOptions) TaskContract {
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	createdFrom := opts.CreatedFrom
	if createdFrom == "" {
		createdFrom = CreatedFromHeuristic
	}

	complexity, risk, confidence := heuristicComplexityRisk(sub.Prompt)
	if opts.ForceComplexity != "" {
		complexity = opts.ForceComplexity
	}
	if opts.ForceRisk != "" {
		risk = opts.ForceRisk
	}

	rev := sub.ParentRevision
	if rev < 0 {
		rev = 0
	}
	// First contract for a new task is revision 1 unless parent revision already set.
	if rev == 0 {
		rev = 1
	} else {
		rev = rev + 1
	}

	criteria := defaultCriteria(complexity, risk)
	evidence := defaultEvidence(risk)
	valBudget, toolBudget, subagent, reviewer := defaultBudgets(complexity, risk)

	return TaskContract{
		TaskID:           sub.TaskID,
		Revision:         rev,
		Complexity:       complexity,
		Risk:             risk,
		Confidence:       confidence,
		SuccessCriteria:  criteria,
		RequiredEvidence: evidence,
		AllowedScope:     append([]string(nil), opts.AllowedScope...),
		ValidationBudget: valBudget,
		ToolBudget:       toolBudget,
		SubagentBudget:   subagent,
		ReviewerBudget:   reviewer,
		CreatedFrom:      createdFrom,
		CreatedAt:        now,
	}
}

// NewEvidenceLedger creates an empty ledger for a contract revision.
func NewEvidenceLedger(taskID string, contractRevision int) EvidenceLedger {
	return EvidenceLedger{
		TaskID:           taskID,
		ContractRevision: contractRevision,
		CriteriaStatus:   make(map[string]CriterionStatus),
		ToolCallCounts:   make(map[string]int),
	}
}

func heuristicComplexityRisk(prompt string) (complexity, risk string, confidence float64) {
	p := strings.TrimSpace(prompt)
	n := utf8.RuneCountInString(p)
	lower := strings.ToLower(p)

	// Risk keywords (provisional).
	highRisk := containsAny(lower, []string{
		"security", "auth", "password", "secret", "crypto", "production",
		"migrate", "migration", "drop table", "delete all", "irreversible",
		"rm -rf", "force push", "wipe",
	})
	medRisk := containsAny(lower, []string{
		"refactor", "rewrite", "schema", "database", "deploy", "release",
	})

	// Complexity: length + multi-file / multi-step language.
	complexLang := containsAny(lower, []string{
		"architecture", "redesign", "multi-package", "entire codebase",
		"all modules", "full rewrite",
	})
	simpleLang := containsAny(lower, []string{
		"typo", "readme", "comment", "rename variable", "fix spelling",
		"one line", "single file",
	})

	switch {
	case complexLang || n > 800:
		complexity = ComplexityComplex
	case n > 280 || medRisk:
		complexity = ComplexityNormal
	case simpleLang || n < 80:
		complexity = ComplexitySimple
	default:
		complexity = ComplexityNormal
	}
	if simpleLang && n < 40 && !highRisk && !medRisk {
		complexity = ComplexityTrivial
	}

	switch {
	case highRisk && containsAny(lower, []string{"irreversible", "drop table", "wipe", "rm -rf"}):
		risk = RiskIrreversible
	case highRisk:
		risk = RiskHigh
	case medRisk:
		risk = RiskMedium
	case complexity == ComplexityTrivial || complexity == ComplexitySimple:
		risk = RiskLow
	default:
		risk = RiskMedium
	}

	// Confidence decreases as risk rises (heuristic only).
	confidence = 0.75
	if risk == RiskLow && (complexity == ComplexityTrivial || complexity == ComplexitySimple) {
		confidence = 0.9
	}
	if risk == RiskHigh || risk == RiskIrreversible {
		confidence = 0.55
	}
	return complexity, risk, confidence
}

func defaultCriteria(complexity, risk string) []Criterion {
	base := []Criterion{{
		ID:          "c-primary",
		Description: "User-stated request is satisfied",
	}}
	if complexity != ComplexityTrivial && complexity != ComplexitySimple {
		base = append(base, Criterion{
			ID:          "c-tests",
			Description: "Relevant tests pass or targeted verification completed",
		})
	}
	if risk == RiskHigh || risk == RiskIrreversible {
		base = append(base, Criterion{
			ID:          "c-safety",
			Description: "Safety-sensitive checks completed (review / evidence)",
		})
	}
	return base
}

func defaultEvidence(risk string) []EvidenceRequirement {
	out := []EvidenceRequirement{{
		ID:       "ev-diff",
		Kind:     "diff",
		Required: true,
	}}
	if risk != RiskLow {
		out = append(out, EvidenceRequirement{
			ID:       "ev-test",
			Kind:     "test",
			Required: risk == RiskHigh || risk == RiskIrreversible,
		})
	}
	return out
}

func defaultBudgets(complexity, risk string) (ValidationBudget, ToolBudget, int, int) {
	val := ValidationBudget{MaxFullSuiteRuns: 1, MaxTargetedRuns: 5}
	tool := ToolBudget{MaxToolCalls: 40}
	subagent := 1
	reviewer := 1

	switch complexity {
	case ComplexityTrivial:
		val = ValidationBudget{MaxFullSuiteRuns: 0, MaxTargetedRuns: 1}
		tool = ToolBudget{MaxToolCalls: 8}
		subagent = 0
		reviewer = 0
	case ComplexitySimple:
		val = ValidationBudget{MaxFullSuiteRuns: 0, MaxTargetedRuns: 2}
		tool = ToolBudget{MaxToolCalls: 15}
		subagent = 0
		reviewer = 0
	case ComplexityComplex:
		val = ValidationBudget{MaxFullSuiteRuns: 2, MaxTargetedRuns: 10}
		tool = ToolBudget{MaxToolCalls: 80}
		subagent = 2
		reviewer = 2
	}
	if risk == RiskHigh || risk == RiskIrreversible {
		if reviewer < 1 {
			reviewer = 1
		}
		if val.MaxTargetedRuns < 3 {
			val.MaxTargetedRuns = 3
		}
	}
	return val, tool, subagent, reviewer
}

func containsAny(hay string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(hay, n) {
			return true
		}
	}
	return false
}
