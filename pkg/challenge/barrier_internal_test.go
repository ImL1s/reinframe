package challenge

import (
	"context"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// Codex: stale Open ticket older than barrier MarkSeq cannot clear a newer denial.
func TestStaleOpenCannotClearNewerBarrier(t *testing.T) {
	st := NewStore()
	svc := NewService(ServiceConfig{Store: st})
	pa := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1",
		ActionID: "pa-rm", ToolName: "Bash", ToolClass: adapter.ToolClassShell,
		Command: "rm -rf build", ParseStatus: "ok",
		WorkspaceRevision: "ws-1", ContractRevision: 3,
	}
	// Ticket sampled before hard deny (delayed Open).
	ticket := st.Sequence()
	// Advance seq and mark hard deny under v2.
	pad := pa
	pad.SessionID = "pad"
	pad.Command = "echo pad"
	pad.ActionID = "pad1"
	_, _ = svc.Open(context.Background(), OpenRequest{
		SessionID: "pad", Proposed: pad, BlockClass: BlockClassOverSOP,
		PolicyVersion: "v2", RulesetHash: "r2",
	})
	_, err := svc.Open(context.Background(), OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: BlockClassSecretExfiltration,
		PolicyVersion: "v2", RulesetHash: "r2",
	})
	if err == nil {
		t.Fatal("expected hard deny")
	}
	fp, err := ComputeFingerprint(FingerprintInput{Proposed: pa, SessionID: pa.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	note, blocked := st.nonAppealBarrierNoteLocked(pa.SessionID, fp.Fingerprint, "v1", "r1", ticket)
	st.mu.Unlock()
	if !blocked {
		t.Fatalf("stale ticket must stay blocked, note=%q ticket=%d", note, ticket)
	}
}
