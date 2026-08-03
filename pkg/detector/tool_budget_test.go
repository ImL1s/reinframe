package detector_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func toolEvent(sess string, seq int64, name string) protocol.AgentEvent {
	body, _ := json.Marshal(map[string]any{"tool_name": name, "source": "test"})
	return protocol.AgentEvent{
		EventID:     "e-" + name,
		SessionID:   sess,
		SequenceNum: seq,
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     body,
	}
}

func fileChangeEvent(sess string) protocol.AgentEvent {
	return protocol.AgentEvent{
		EventID:   "fc1",
		SessionID: sess,
		EventType: "file_change",
		Timestamp: time.Now().UTC(),
		Payload:   []byte(`{"file_path":"a.go"}`),
	}
}

func TestToolBudget_FiresWithoutProgress(t *testing.T) {
	t.Parallel()
	d := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{MaxToolCalls: 5})
	sess := "long-review"
	var last *protocol.TunnelSignal
	var fired bool
	for i := 1; i <= 5; i++ {
		sig, ok := d.Observe(toolEvent(sess, int64(i), "exec"))
		if i < 5 {
			if ok {
				t.Fatalf("unexpected fire at i=%d", i)
			}
			continue
		}
		if !ok || sig == nil {
			t.Fatalf("expected fire at i=%d", i)
		}
		last, fired = sig, true
	}
	if !fired {
		t.Fatal("never fired")
	}
	if last.FailureMode != detector.FailureModeToolBudgetChurn {
		t.Fatalf("mode=%s", last.FailureMode)
	}
	if last.DetectorName != detector.DetectorNameToolBudgetChurn {
		t.Fatalf("name=%s", last.DetectorName)
	}
	if last.Details["max_tool_calls"] != "5" {
		t.Fatalf("details=%v", last.Details)
	}
	if d.ToolCount(sess) != 5 {
		t.Fatalf("count=%d", d.ToolCount(sess))
	}
}

func TestToolBudget_NoFireShortSession(t *testing.T) {
	t.Parallel()
	d := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{MaxToolCalls: 30})
	sess := "short"
	for i := 1; i <= 7; i++ {
		if sig, ok := d.Observe(toolEvent(sess, int64(i), "exec")); ok {
			t.Fatalf("healthy short session must not fire: %+v", sig)
		}
	}
	if d.ToolCount(sess) != 7 {
		t.Fatalf("count=%d", d.ToolCount(sess))
	}
}

func TestToolBudget_ProgressResetsWindow(t *testing.T) {
	t.Parallel()
	d := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{MaxToolCalls: 4})
	sess := "progressing"
	// 3 tools, then progress, then 3 more — never 4 without progress.
	for i := 1; i <= 3; i++ {
		if _, ok := d.Observe(toolEvent(sess, int64(i), "exec")); ok {
			t.Fatal("early fire")
		}
	}
	if _, ok := d.Observe(fileChangeEvent(sess)); ok {
		t.Fatal("file_change should not fire signal")
	}
	if d.ToolsSinceProgress(sess) != 0 {
		t.Fatalf("after progress toolsSince=%d", d.ToolsSinceProgress(sess))
	}
	for i := 4; i <= 6; i++ {
		if sig, ok := d.Observe(toolEvent(sess, int64(i), "exec")); ok {
			t.Fatalf("progress should prevent fire: %+v", sig)
		}
	}
	// 4th tool after progress → fire
	sig, ok := d.Observe(toolEvent(sess, 7, "exec"))
	if !ok || sig == nil {
		t.Fatal("expected fire after budget without new progress")
	}
	if sig.FailureMode != detector.FailureModeToolBudgetChurn {
		t.Fatalf("mode=%s", sig.FailureMode)
	}
}

func TestToolBudget_ContractOverride(t *testing.T) {
	t.Parallel()
	d := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{MaxToolCalls: 100})
	sess := "contract"
	// TaskContract.ToolBudget.MaxToolCalls = 3
	for i := 1; i <= 2; i++ {
		if _, ok := d.ObserveWithBudget(toolEvent(sess, int64(i), "exec"), 3); ok {
			t.Fatal("early")
		}
	}
	sig, ok := d.ObserveWithBudget(toolEvent(sess, 3, "exec"), 3)
	if !ok || sig.Details["max_tool_calls"] != "3" {
		t.Fatalf("ok=%v details=%v", ok, sig)
	}
}

func TestToolBudget_DefaultMax(t *testing.T) {
	t.Parallel()
	d := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{})
	if d.MaxToolCalls() != detector.DefaultMaxToolCalls {
		t.Fatalf("default=%d", d.MaxToolCalls())
	}
}
