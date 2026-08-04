package evaluation

// DatasetSchemaVersion is the closed fixture format for M3 benchmarks (#100).
const DatasetSchemaVersion = "reinframe.benchmark_case.v1"

// ReportSchemaVersion is the closed report format.
const ReportSchemaVersion = "reinframe.benchmark_report.v1"

// Scenario classes.
const (
	ClassPositiveDeviation = "positive_deviation"
	ClassHealthy           = "healthy_counterexample"
	ClassBoundary          = "boundary_robustness"
)

// Case kinds for runner dispatch.
const (
	KindRepeatedFailure   = "repeated_failure"
	KindVerificationChurn = "verification_churn"
	KindToolBudget        = "tool_budget"
	KindHypothesisLoop    = "hypothesis_loop"
	KindClassifierShadow  = "classifier_shadow"
)

// Case is one versioned synthetic/hand-labeled benchmark scenario.
type Case struct {
	SchemaVersion string `json:"schema_version"`
	CaseID        string `json:"case_id"`
	ScenarioClass string `json:"scenario_class"`
	PolicyClass   string `json:"policy_class"`
	Kind          string `json:"kind"`
	Source        string `json:"source"` // synthetic | hand-labeled
	Description   string `json:"description,omitempty"`

	// Detector inputs (kind-specific; unused fields ignored).
	Failures []string `json:"failures,omitempty"` // repeated_failure raw strings

	// Verification churn attempts (JSON-friendly).
	ValidationAttempts []ValidationAttemptFixture `json:"validation_attempts,omitempty"`

	// Tool budget: N tool names then optional progress mark index.
	ToolCalls     []string `json:"tool_calls,omitempty"`
	ProgressAfter int      `json:"progress_after,omitempty"` // 0 = never; else after N tools mark progress
	ToolBudgetMax int      `json:"tool_budget_max,omitempty"`

	// Hypothesis loop observations.
	Hypotheses []HypothesisFixture `json:"hypotheses,omitempty"`

	// Classifier shadow.
	ClassifierFixture   string `json:"classifier_fixture,omitempty"`
	UserException       bool   `json:"user_exception,omitempty"`
	RepoPolicyException bool   `json:"repo_policy_exception,omitempty"`
	FlakyInvestigation  bool   `json:"flaky_investigation,omitempty"`
	ProposedToolName    string `json:"proposed_tool_name,omitempty"`
	ProposedCommand     string `json:"proposed_command,omitempty"`
	HookGateAction      string `json:"hook_gate_action,omitempty"` // allow|deny
	Threshold           int    `json:"threshold,omitempty"`

	// Expected labels.
	ExpectDetectorFire   *bool  `json:"expect_detector_fire,omitempty"`
	ExpectStage2Decision string `json:"expect_stage2_decision,omitempty"` // ALLOW|BLOCK
	ExpectResolverReason string `json:"expect_resolver_reason,omitempty"`
}

// ValidationAttemptFixture is a JSON-friendly ValidationAttempt.
type ValidationAttemptFixture struct {
	Command             string   `json:"command"`
	TargetScope         []string `json:"target_scope,omitempty"`
	WorkspaceRev        string   `json:"workspace_rev,omitempty"`
	ContractRevision    int      `json:"contract_revision,omitempty"`
	Purpose             string   `json:"purpose,omitempty"`
	Succeeded           bool     `json:"succeeded"`
	FlakyInvestigation  bool     `json:"flaky_investigation,omitempty"`
	PolicyRequiresRerun bool     `json:"policy_requires_rerun,omitempty"`
	HighRiskIndependent bool     `json:"high_risk_independent,omitempty"`
	WorkspaceChanged    bool     `json:"workspace_changed,omitempty"`
}

// HypothesisFixture is one hypothesis observation.
type HypothesisFixture struct {
	Text        string   `json:"text"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
}

// CaseResult is the per-case scored output.
type CaseResult struct {
	CaseID             string `json:"case_id"`
	Kind               string `json:"kind"`
	ScenarioClass      string `json:"scenario_class"`
	DetectorFired      bool   `json:"detector_fired"`
	ExpectDetectorFire *bool  `json:"expect_detector_fire,omitempty"`
	DetectorTP         bool   `json:"detector_tp"`
	DetectorFP         bool   `json:"detector_fp"`
	DetectorFN         bool   `json:"detector_fn"`
	DetectorTN         bool   `json:"detector_tn"`
	Stage2Decision     string `json:"stage2_decision,omitempty"`
	ExpectStage2       string `json:"expect_stage2,omitempty"`
	Stage2Correct      *bool  `json:"stage2_correct,omitempty"`
	FalseBlock         bool   `json:"false_block"` // predicted BLOCK when expect ALLOW (healthy)
	FalseAllow         bool   `json:"false_allow"`
	RawSeverity        int    `json:"raw_severity,omitempty"`
	ResolverReason     string `json:"resolver_reason,omitempty"`
	ReasonCode         string `json:"reason_code,omitempty"`
	Enforced           bool   `json:"enforced"` // must always be false for classifier
	Error              string `json:"error,omitempty"`
}

// Report is the machine-readable aggregate benchmark output.
type Report struct {
	SchemaVersion     string            `json:"schema_version"`
	ReinframeCommit   string            `json:"reinframe_commit"`
	DatasetVersion    string            `json:"dataset_version"`
	DatasetHash       string            `json:"dataset_hash"`
	ClassifierModelID string            `json:"classifier_model_id"`
	RulesetID         string            `json:"ruleset_id"`
	RulesetHash       string            `json:"ruleset_hash"`
	ThresholdProfile  string            `json:"threshold_profile"`
	GOOS              string            `json:"goos"`
	GOARCH            string            `json:"goarch"`
	Disposition       string            `json:"disposition"` // NO-GO | LIMITED-GO | MORE-DATA
	DispositionNote   string            `json:"disposition_note"`
	HardGateEnabled   bool              `json:"hard_gate_enabled"` // always false for #100
	Metrics           AggregateMetrics  `json:"metrics"`
	Cases             []CaseResult      `json:"cases"`
	Meta              map[string]string `json:"meta,omitempty"`
}

// AggregateMetrics holds class-separated metrics (not one collapsed accuracy).
type AggregateMetrics struct {
	DetectorByKind   map[string]BinaryMetrics `json:"detector_by_kind"`
	ClassifierShadow BinaryMetrics            `json:"classifier_shadow"`
	FalseBlockRate   float64                  `json:"false_block_rate"`
	FalseAllowRate   float64                  `json:"false_allow_rate"`
	HealthyCases     int                      `json:"healthy_cases"`
	PositiveCases    int                      `json:"positive_cases"`
	BoundaryCases    int                      `json:"boundary_cases"`
	ParseFailCount   int                      `json:"parse_fail_count"`
	ProviderErrCount int                      `json:"provider_err_count"`
	SampleSize       int                      `json:"sample_size"`
}

// BinaryMetrics is precision/recall/F1 style counts.
type BinaryMetrics struct {
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	TN        int     `json:"tn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}
