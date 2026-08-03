package adapter_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/supervisor"
)

func TestFileActuator_DeliverWritesJSONL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "advice.jsonl")
	act := &adapter.FileActuator{Path: path}
	iv := protocol.Intervention{
		InterventionID: "iv-zoom-1",
		SessionID:      "sess-a",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "Stop and restate the goal.",
		RequiresAck:    true,
		Fingerprint:    "fp1",
	}
	res, err := act.Deliver(context.Background(), iv)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Accepted || res.AckStatus != adapter.AckStatusPending {
		t.Fatalf("res=%+v", res)
	}
	if res.DeliveryMode != adapter.DeliveryModeAdvice {
		t.Fatalf("mode=%s", res.DeliveryMode)
	}
	if act.CallCount() != 1 {
		t.Fatalf("calls=%d", act.CallCount())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env adapter.AdviceEnvelope
	if err := json.Unmarshal(data[:len(data)-1], &env); err != nil {
		t.Fatalf("parse %q: %v", data, err)
	}
	if env.Schema != "reinframe.advice.v1" || env.InterventionID != "iv-zoom-1" {
		t.Fatalf("env=%+v", env)
	}
	if env.AdvicePrompt == "" || env.ActionType != "ZOOM_OUT_PROMPT" {
		t.Fatalf("env=%+v", env)
	}
}

func TestFileActuator_VerticalSlice_DeliverThenACK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "channel.jsonl")
	act := &adapter.FileActuator{Path: path}
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	orch, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3}),
		Policy:   policy.NewEngine(policy.EngineConfig{}),
		Delivery: del,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sess := "file-act-sess"
	fail := `{"error":"exit status 1: undefined: Bar"}`
	for i := 1; i <= 3; i++ {
		ev := protocol.AgentEvent{
			EventID:     "e" + string(rune('0'+i)),
			SessionID:   sess,
			SequenceNum: int64(i),
			EventType:   "error",
			Timestamp:   time.Now().UTC(),
			Payload:     []byte(fail),
		}
		if _, _, err := orch.HandleEvent(ctx, ev); err != nil {
			t.Fatal(err)
		}
	}
	dec := orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("pre deliver: %+v", dec)
	}
	item, res, err := orch.DeliverAtSafeBoundary(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if item == nil || item.State != adapter.StateDelivering {
		t.Fatalf("item=%+v res=%+v", item, res)
	}
	if res.AckStatus != adapter.AckStatusPending {
		t.Fatalf("ack should be pending without AutoAck: %+v", res)
	}
	// Side channel evidence
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		t.Fatalf("advice file empty: %v %q", err, raw)
	}
	// PreTool still defer until ACK
	dec = orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	if dec.Action != adapter.HookActionDefer {
		t.Fatalf("anti-theater allow-before-ack: %+v", dec)
	}
	if err := orch.Acknowledge(item.Intervention.InterventionID, adapter.AckStatusAcked); err != nil {
		t.Fatal(err)
	}
	dec = orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	if dec.Action != adapter.HookActionAllow {
		t.Fatalf("after ack: %+v", dec)
	}
}

func TestFileActuator_MissingPath(t *testing.T) {
	t.Parallel()
	act := &adapter.FileActuator{}
	res, err := act.Deliver(context.Background(), protocol.Intervention{
		InterventionID: "x", ActionType: "ZOOM_OUT_PROMPT",
	})
	if err == nil || res.Accepted {
		t.Fatalf("want error, res=%+v", res)
	}
}
