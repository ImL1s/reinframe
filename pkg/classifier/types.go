package classifier

// Schema versions (closed).
const (
	SchemaClassifierInput  = "reinframe.classifier_input.v1"
	SchemaRawAssessment    = "reinframe.raw_assessment.v1"
	SchemaResolvedDecision = "reinframe.resolved_decision.v1"
	SchemaClassifierAudit  = "reinframe.classifier_audit.v1"
)

// Policy classes.
const (
	PolicyClassProductivity = "PRODUCTIVITY"
	PolicyClassSecurity     = "SECURITY"
)

// Stage 2 decisions.
const (
	DecisionAllow = "ALLOW"
	DecisionBlock = "BLOCK"
)

// Severity bounds.
const (
	SeverityMin = 0
	SeverityMax = 100
)

// ValidRawReasonCodes is the Stage 1 closed allowlist (#119).
var ValidRawReasonCodes = map[string]struct{}{
	"NORMAL_PROGRESS":    {},
	"SCOPE_DRIFT":        {},
	"VERIFICATION_CHURN": {},
	"REPEATED_FAILURE":   {},
	"HYPOTHESIS_LOOP":    {},
	"TOOL_BUDGET":        {},
	"EVIDENCE_GAP":       {},
	"OVER_SOP":           {},
	"UNKNOWN":            {},
}

// ValidateSeverity returns false if s is outside 0–100.
func ValidateSeverity(s int) bool {
	return s >= SeverityMin && s <= SeverityMax
}

// ValidateRawReasonCode reports whether code is in the closed allowlist.
func ValidateRawReasonCode(code string) bool {
	_, ok := ValidRawReasonCodes[code]
	return ok
}
