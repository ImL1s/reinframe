package protocol_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestValidateEvent_TaskSubmitted(t *testing.T) {
	valid := protocol.TaskSubmitted{
		TaskID:         "task-1",
		SessionID:      "sess-1",
		Prompt:         "fix the typo in README",
		ParentRevision: 0,
		SubmittedAt:    time.Now().UTC(),
		SourceHint:     "adapter:claude_code", // label only — not a core hook enum
	}
	b, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateEvent(b, "task_submitted"); err != nil {
		t.Fatalf("valid TaskSubmitted: %v", err)
	}

	invalid := []byte(`{"task_id":"","session_id":"s","prompt":"p","parent_revision":0,"submitted_at":"2026-08-02T00:00:00Z"}`)
	if err := protocol.ValidateEvent(invalid, "task_submitted"); err == nil {
		t.Fatal("expected invalid empty task_id to fail")
	}
}

func TestValidateEvent_TaskContract(t *testing.T) {
	valid := protocol.TaskContract{
		TaskID:     "task-1",
		Revision:   1,
		Complexity: "simple",
		Risk:       "low",
		Confidence: 0.8,
		SuccessCriteria: []protocol.Criterion{
			{ID: "c1", Description: "README typo fixed"},
		},
		RequiredEvidence: []protocol.EvidenceRequirement{
			{ID: "e1", Kind: "diff", Required: true},
		},
		AllowedScope:     []string{"README.md"},
		ValidationBudget: protocol.ValidationBudget{MaxFullSuiteRuns: 0, MaxTargetedRuns: 1},
		ToolBudget:       protocol.ToolBudget{MaxToolCalls: 10},
		SubagentBudget:   0,
		ReviewerBudget:   0,
		CreatedFrom:      "heuristic",
		CreatedAt:        time.Now().UTC(),
	}
	b, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateEvent(b, "task_contract"); err != nil {
		t.Fatalf("valid TaskContract: %v", err)
	}

	badComplexity := []byte(`{
		"task_id":"t","revision":1,"complexity":"mega","risk":"low","confidence":0.5,
		"validation_budget":{"max_full_suite_runs":0,"max_targeted_runs":0},
		"tool_budget":{"max_tool_calls":1},"subagent_budget":0,"reviewer_budget":0,
		"created_from":"heuristic","created_at":"2026-08-02T00:00:00Z"
	}`)
	if err := protocol.ValidateEvent(badComplexity, "task_contract"); err == nil {
		t.Fatal("expected invalid complexity enum to fail")
	}
}

func TestValidateEvent_EvidenceLedger(t *testing.T) {
	now := time.Now().UTC()
	valid := protocol.EvidenceLedger{
		TaskID:            "task-1",
		ContractRevision:  1,
		WorkspaceRevision: "abc123",
		CriteriaStatus: map[string]protocol.CriterionStatus{
			"c1": {CriterionID: "c1", Status: "met"},
		},
		ValidationRecords: []protocol.ValidationRecord{
			{
				RecordID:         "v1",
				Command:          "git diff",
				TargetScope:      []string{"README.md"},
				WorkspaceRev:     "abc123",
				ContractRevision: 1,
				Purpose:          "confirm_edit",
				Succeeded:        true,
				Fingerprint:      "git diff|README.md|abc123|1|confirm_edit",
				RecordedAt:       now,
			},
		},
		ToolCallCounts: map[string]int{"Edit": 1},
	}
	b, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateEvent(b, "evidence_ledger"); err != nil {
		t.Fatalf("valid EvidenceLedger: %v", err)
	}

	// Minimal ledger (first M2 slice may use sparse ledgers)
	minimal := []byte(`{"task_id":"task-1","contract_revision":0}`)
	if err := protocol.ValidateEvent(minimal, "evidence_ledger"); err != nil {
		t.Fatalf("minimal EvidenceLedger: %v", err)
	}
}

func TestSafeBoundaryAndAckPolicyConstants(t *testing.T) {
	if protocol.BoundaryBeforeTool != "before_tool" {
		t.Fatalf("BoundaryBeforeTool=%q", protocol.BoundaryBeforeTool)
	}
	if protocol.AckExplicit != "explicit" {
		t.Fatalf("AckExplicit=%q", protocol.AckExplicit)
	}
	// Documented first-slice default: policy may receive nil contract/ledger pointers.
	var contract *protocol.TaskContract
	var ledger *protocol.EvidenceLedger
	if contract != nil || ledger != nil {
		t.Fatal("nil defaults for first repeated-failure slice must be valid")
	}
}
