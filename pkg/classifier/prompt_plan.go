package classifier

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// Closed prompt block roles/types (#132).
const (
	PromptRoleSystem    = "system"
	PromptRoleUser      = "user"
	PromptRoleAssistant = "assistant"

	PromptTypePolicy      = "policy"
	PromptTypeSchema      = "schema"
	PromptTypeReasonCodes = "reason_codes"
	PromptTypeRules       = "rules"
	PromptTypeExample     = "example"
	PromptTypeTask        = "task"
	PromptTypeEvidence    = "evidence"
	PromptTypeAction      = "action"
	PromptTypeChallenge   = "challenge"
	PromptTypeMeta        = "meta"
)

// PromptBlock is one closed unit of prompt material.
type PromptBlock struct {
	Role string `json:"role"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// PromptPlan separates stable reusable policy from request-specific dynamic state.
type PromptPlan struct {
	SchemaVersion    string        `json:"schema_version"`
	StablePrefix     []PromptBlock `json:"stable_prefix"`
	DynamicSuffix    []PromptBlock `json:"dynamic_suffix"`
	StablePrefixHash string        `json:"stable_prefix_hash"`
	PromptHash       string        `json:"prompt_hash"`
	RulesetHash      string        `json:"ruleset_hash"`
	InputHash        string        `json:"input_hash"`
}

// PromptPlanMaterial is the stable policy surface for BuildPromptPlan.
// Must not include timestamps, request IDs, or session-specific values.
type PromptPlanMaterial struct {
	// PromptSchemaVersion labels the stable prompt/schema revision.
	PromptSchemaVersion string
	// PolicyText is the closed system policy.
	PolicyText string
	// OutputSchemaText describes the closed RawAssessment JSON schema.
	OutputSchemaText string
	// DecisionRulesText is deterministic Stage-1 scoring guidance (evidence only).
	DecisionRulesText string
	// StableExamples are fixed few-shot blocks (optional).
	StableExamples []PromptBlock
	// RulesetHash is the policy ruleset content hash.
	RulesetHash string
}

// DefaultPromptPlanMaterial returns the built-in stable classifier policy (#132).
func DefaultPromptPlanMaterial() PromptPlanMaterial {
	return PromptPlanMaterial{
		PromptSchemaVersion: SchemaPromptPlan,
		PolicyText: "You are Reinframe Action Alignment Stage-1 classifier. " +
			"Return exactly one JSON object matching the closed RawAssessment schema. " +
			"Do not include markdown, prose, or extra fields. " +
			"RawAssessment is evidence only; the deterministic resolver owns ALLOW|BLOCK.",
		OutputSchemaText: `{"schema_version":"reinframe.raw_assessment.v1","severity":0,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":[]}`,
		DecisionRulesText: "severity is integer 0-100; reason_code must be from the closed allowlist; " +
			"evidence_event_ids must reference only supplied event IDs.",
		RulesetHash: "builtin-ruleset-v1",
	}
}

// BuildPromptPlan constructs a deterministic PromptPlan from stable material + input.
func BuildPromptPlan(mat PromptPlanMaterial, in ClassifierInput) (PromptPlan, error) {
	if mat.PromptSchemaVersion == "" {
		mat.PromptSchemaVersion = SchemaPromptPlan
	}
	if mat.RulesetHash == "" {
		mat.RulesetHash = in.RulesetHash
	}

	// Stable prefix: policy, schema, reason codes, rules, examples — no session/action noise.
	codes := sortedReasonCodes()
	stable := []PromptBlock{
		{Role: PromptRoleSystem, Type: PromptTypePolicy, Text: mat.PolicyText},
		{Role: PromptRoleSystem, Type: PromptTypeSchema, Text: mat.OutputSchemaText},
		{Role: PromptRoleSystem, Type: PromptTypeReasonCodes, Text: encodeCanon(codes)},
		{Role: PromptRoleSystem, Type: PromptTypeRules, Text: mat.DecisionRulesText},
	}
	for _, ex := range mat.StableExamples {
		if ex.Role == "" {
			ex.Role = PromptRoleSystem
		}
		if ex.Type == "" {
			ex.Type = PromptTypeExample
		}
		stable = append(stable, ex)
	}
	// Version label is stable (not a request id).
	stable = append(stable, PromptBlock{
		Role: PromptRoleSystem,
		Type: PromptTypeMeta,
		Text: encodeCanon(map[string]string{
			"prompt_schema": mat.PromptSchemaVersion,
			"ruleset_hash":  mat.RulesetHash,
		}),
	})

	// Dynamic suffix: task/action/evidence/session-specific.
	dyn := []PromptBlock{
		{Role: PromptRoleUser, Type: PromptTypeTask, Text: encodeCanon(map[string]string{
			"session_id":   in.SessionID,
			"policy_class": in.PolicyClass,
			"ruleset_id":   in.RulesetID,
			"fixture":      in.FixtureName,
		})},
		{Role: PromptRoleUser, Type: PromptTypeEvidence, Text: encodeCanon(map[string]any{
			"recent_event_ids":  append([]string(nil), in.RecentEventIDs...),
			"related_event_ids": append([]string(nil), in.RelatedEventIDs...),
		})},
	}
	if in.ProposedAction != nil {
		dyn = append(dyn, PromptBlock{
			Role: PromptRoleUser,
			Type: PromptTypeAction,
			Text: encodeProposedAction(*in.ProposedAction),
		})
	}
	if in.UserException || in.RepoPolicyException || in.FlakyInvestigation {
		dyn = append(dyn, PromptBlock{
			Role: PromptRoleUser,
			Type: PromptTypeMeta,
			Text: encodeCanon(map[string]bool{
				"user_exception":        in.UserException,
				"repo_policy_exception": in.RepoPolicyException,
				"flaky_investigation":   in.FlakyInvestigation,
			}),
		})
	}

	plan := PromptPlan{
		SchemaVersion: SchemaPromptPlan,
		StablePrefix:  stable,
		DynamicSuffix: dyn,
		RulesetHash:   mat.RulesetHash,
	}
	var err error
	plan.StablePrefixHash, err = hashBlocks(stable)
	if err != nil {
		return PromptPlan{}, err
	}
	plan.InputHash, err = hashBlocks(dyn)
	if err != nil {
		return PromptPlan{}, err
	}
	// PromptHash binds both halves via structured encoding (not string concat).
	plan.PromptHash, err = hashCanon(map[string]string{
		"stable": plan.StablePrefixHash,
		"input":  plan.InputHash,
		"rules":  plan.RulesetHash,
		"schema": mat.PromptSchemaVersion,
	})
	if err != nil {
		return PromptPlan{}, err
	}
	return plan, nil
}

// PromptBytes returns the wire-oriented message payload size estimate (UTF-8).
func (p PromptPlan) PromptBytes() int {
	n := 0
	for _, b := range p.StablePrefix {
		n += len(b.Role) + len(b.Type) + len(b.Text)
	}
	for _, b := range p.DynamicSuffix {
		n += len(b.Role) + len(b.Type) + len(b.Text)
	}
	return n
}

// Messages in stable-then-dynamic order (never reorder).
func (p PromptPlan) Messages() []PromptBlock {
	out := make([]PromptBlock, 0, len(p.StablePrefix)+len(p.DynamicSuffix))
	out = append(out, p.StablePrefix...)
	out = append(out, p.DynamicSuffix...)
	return out
}

func sortedReasonCodes() []string {
	out := make([]string, 0, len(ValidRawReasonCodes))
	for c := range ValidRawReasonCodes {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func encodeCanon(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func encodeProposedAction(pa adapter.ProposedAction) string {
	// Deterministic subset — no secrets (ProposedAction redaction is host-side).
	return encodeCanon(map[string]any{
		"tool_name":  pa.ToolName,
		"tool_class": pa.ToolClass,
		"command":    pa.Command,
		"file_path":  pa.FilePath,
		"action_id":  pa.ActionID,
	})
}

func hashBlocks(blocks []PromptBlock) (string, error) {
	return hashCanon(blocks)
}

func hashCanon(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("classifier: hash encode: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
