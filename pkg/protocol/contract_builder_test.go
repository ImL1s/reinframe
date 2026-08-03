package protocol_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/state"
)

func sampleSubmitted(prompt string) protocol.TaskSubmitted {
	return protocol.TaskSubmitted{
		TaskID:      "task-1",
		SessionID:   "sess-1",
		Prompt:      prompt,
		SubmittedAt: time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
		SourceHint:  "test",
	}
}

func TestBuildContractFromSubmitted_SimpleTypo(t *testing.T) {
	t.Parallel()
	c := protocol.BuildContractFromSubmitted(sampleSubmitted("fix typo in README"), protocol.BuildContractOptions{
		Now: func() time.Time { return time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC) },
	})
	if c.TaskID != "task-1" {
		t.Fatalf("TaskID=%s", c.TaskID)
	}
	if c.Revision != 1 {
		t.Fatalf("Revision=%d", c.Revision)
	}
	if c.Complexity != protocol.ComplexityTrivial && c.Complexity != protocol.ComplexitySimple {
		t.Fatalf("Complexity=%s want trivial|simple", c.Complexity)
	}
	if c.Risk != protocol.RiskLow {
		t.Fatalf("Risk=%s want low", c.Risk)
	}
	if c.CreatedFrom != protocol.CreatedFromHeuristic {
		t.Fatalf("CreatedFrom=%s", c.CreatedFrom)
	}
	if c.ValidationBudget.MaxFullSuiteRuns != 0 {
		t.Fatalf("trivial/simple should not budget full suite by default: %#v", c.ValidationBudget)
	}
	// Schema-valid
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.ValidateEvent(raw, "task_contract"); err != nil {
		t.Fatalf("schema: %v", err)
	}
}

func TestBuildContractFromSubmitted_SecurityHighRisk(t *testing.T) {
	t.Parallel()
	c := protocol.BuildContractFromSubmitted(
		sampleSubmitted("fix production auth security vulnerability in login flow"),
		protocol.BuildContractOptions{},
	)
	if c.Risk != protocol.RiskHigh && c.Risk != protocol.RiskIrreversible {
		t.Fatalf("Risk=%s want high|irreversible", c.Risk)
	}
	if c.ReviewerBudget < 1 {
		t.Fatalf("ReviewerBudget=%d", c.ReviewerBudget)
	}
	found := false
	for _, e := range c.RequiredEvidence {
		if e.Kind == "test" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected test evidence requirement for elevated risk")
	}
}

func TestBuildContractFromSubmitted_ParentRevisionIncrements(t *testing.T) {
	t.Parallel()
	sub := sampleSubmitted("normal task with more words than a tiny fix needs for classification")
	sub.ParentRevision = 2
	c := protocol.BuildContractFromSubmitted(sub, protocol.BuildContractOptions{})
	if c.Revision != 3 {
		t.Fatalf("Revision=%d want 3", c.Revision)
	}
}

func TestNewEvidenceLedger(t *testing.T) {
	t.Parallel()
	led := protocol.NewEvidenceLedger("task-1", 1)
	if led.TaskID != "task-1" || led.ContractRevision != 1 {
		t.Fatalf("%#v", led)
	}
	if led.CriteriaStatus == nil || led.ToolCallCounts == nil {
		t.Fatal("maps should be non-nil")
	}
}

func TestAgentEventEmission_StoreRoundTrip(t *testing.T) {
	t.Parallel()
	sub := sampleSubmitted("rename variable in one file")
	c := protocol.BuildContractFromSubmitted(sub, protocol.BuildContractOptions{
		AllowedScope: []string{"/workspace"},
	})
	led := protocol.NewEvidenceLedger(c.TaskID, c.Revision)

	evSub, err := protocol.AgentEventFromTaskSubmitted(sub, protocol.EmitOptions{SequenceNum: 1, EventID: "e-sub"})
	if err != nil {
		t.Fatal(err)
	}
	evC, err := protocol.AgentEventFromTaskContract(sub.SessionID, c, protocol.EmitOptions{SequenceNum: 2, EventID: "e-c"})
	if err != nil {
		t.Fatal(err)
	}
	evL, err := protocol.AgentEventFromEvidenceLedger(sub.SessionID, led, protocol.EmitOptions{SequenceNum: 3, EventID: "e-l"})
	if err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "task-model.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, ev := range []protocol.AgentEvent{evSub, evC, evL} {
		if err := store.AppendEvent(ctx, &ev); err != nil {
			t.Fatalf("AppendEvent %s: %v", ev.EventType, err)
		}
	}
	got, err := store.QueryEvents(ctx, state.EventFilter{SessionID: sub.SessionID, Ascending: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("events=%d", len(got))
	}
	if got[0].EventType != protocol.EventTypeTaskSubmitted {
		t.Fatalf("0 type=%s", got[0].EventType)
	}
	if got[1].EventType != protocol.EventTypeTaskContract {
		t.Fatalf("1 type=%s", got[1].EventType)
	}
	if got[2].EventType != protocol.EventTypeEvidenceLedger {
		t.Fatalf("2 type=%s", got[2].EventType)
	}
}

