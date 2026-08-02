package adapter_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestFakeEventSource_InjectAndReceive(t *testing.T) {
	src := adapter.NewFakeEventSource(8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := src.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}

	want := protocol.AgentEvent{
		EventID:     "e1",
		SessionID:   "s1",
		SequenceNum: 1,
		EventType:   "tool_call",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{}`),
	}
	if err := src.Inject(want); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	select {
	case got := <-ch:
		if got.EventID != want.EventID || got.SessionID != want.SessionID {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestFakeEventSource_CancelClosesConsumer(t *testing.T) {
	src := adapter.NewFakeEventSource(4)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := src.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			// May still drain; wait for close.
			for ok {
				_, ok = <-ch
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after cancel")
	}
}

func TestFakeEventSource_CloseEndsStream(t *testing.T) {
	src := adapter.NewFakeEventSource(4)
	ctx := context.Background()
	ch, err := src.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	src.Close()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for close")
	}

	if err := src.Inject(protocol.AgentEvent{EventID: "x"}); err == nil {
		t.Fatal("expected Inject error after Close")
	}
}

func TestFakeActuator_RecordsDeliverAndAutoAck(t *testing.T) {
	act := adapter.NewFakeActuator()
	iv := protocol.Intervention{
		InterventionID: "iv-1",
		SessionID:      "s1",
		Level:          1,
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "zoom out",
		Status:         "PENDING",
		ExecutedAt:     time.Now().UTC(),
	}

	res, err := act.Deliver(context.Background(), iv)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if !res.Accepted {
		t.Fatalf("expected accepted, got %+v", res)
	}
	if res.AckStatus != adapter.AckStatusAcked {
		t.Fatalf("AckStatus=%s want acked", res.AckStatus)
	}
	if res.ErrorClass != adapter.ErrorClassNone {
		t.Fatalf("ErrorClass=%s want none", res.ErrorClass)
	}
	if res.DeliveryMode != adapter.DeliveryModeAdvice {
		t.Fatalf("DeliveryMode=%s want advice", res.DeliveryMode)
	}
	if act.CallCount() != 1 {
		t.Fatalf("CallCount=%d want 1", act.CallCount())
	}
	last, ok := act.LastCall()
	if !ok || last.InterventionID != "iv-1" {
		t.Fatalf("LastCall unexpected: %+v ok=%v", last, ok)
	}
}

func TestFakeActuator_UnsupportedCapability(t *testing.T) {
	act := adapter.NewFakeActuator()
	act.Unsupported = true
	act.UnsupportedMessage = "no advice channel"

	res, err := act.Deliver(context.Background(), protocol.Intervention{
		InterventionID: "iv-u",
		SessionID:      "s1",
		ActionType:     "ZOOM_OUT_PROMPT",
	})
	if err != nil {
		t.Fatalf("Deliver err: %v", err)
	}
	if res.Accepted {
		t.Fatal("expected not accepted")
	}
	if res.ErrorClass != adapter.ErrorClassUnsupportedCapability {
		t.Fatalf("ErrorClass=%s", res.ErrorClass)
	}
	if res.AckStatus != adapter.AckStatusUnsupported {
		t.Fatalf("AckStatus=%s", res.AckStatus)
	}
	if res.Message != "no advice channel" {
		t.Fatalf("Message=%q", res.Message)
	}
}

func TestFakeActuator_ConcurrentDeliver(t *testing.T) {
	act := adapter.NewFakeActuator()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, _ = act.Deliver(context.Background(), protocol.Intervention{
				InterventionID: "iv-concurrent",
				SessionID:      "s1",
				ActionType:     "ZOOM_OUT_PROMPT",
			})
		}(i)
	}
	wg.Wait()
	if act.CallCount() != n {
		t.Fatalf("CallCount=%d want %d", act.CallCount(), n)
	}
}

func TestFakeActuator_TimeoutOnDelay(t *testing.T) {
	act := adapter.NewFakeActuator()
	act.Delay = 500 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	res, err := act.Deliver(ctx, protocol.Intervention{
		InterventionID: "iv-to",
		SessionID:      "s1",
		ActionType:     "ZOOM_OUT_PROMPT",
	})
	if err == nil {
		t.Fatal("expected context error on timeout")
	}
	if res.ErrorClass != adapter.ErrorClassTimeout {
		t.Fatalf("ErrorClass=%s want timeout", res.ErrorClass)
	}
	if res.AckStatus != adapter.AckStatusTimedOut {
		t.Fatalf("AckStatus=%s want timed_out", res.AckStatus)
	}
}

func TestDefaultDeliveryMode(t *testing.T) {
	cases := map[string]string{
		"ZOOM_OUT_PROMPT":   adapter.DeliveryModeAdvice,
		"PAUSE_PROCESS":     adapter.DeliveryModePause,
		"CANCEL_ACTION":     adapter.DeliveryModeCancel,
		"GIT_ROLLBACK":      adapter.DeliveryModeHumanEscalation,
		"TERMINATE_SESSION": adapter.DeliveryModeHumanEscalation,
		"UNKNOWN":           adapter.DeliveryModeAdvice,
	}
	for action, want := range cases {
		if got := adapter.DefaultDeliveryMode(action); got != want {
			t.Errorf("DefaultDeliveryMode(%s)=%s want %s", action, got, want)
		}
	}
}

// Compile-time interface checks.
var (
	_ adapter.EventSource           = (*adapter.FakeEventSource)(nil)
	_ adapter.InterventionActuator  = (*adapter.FakeActuator)(nil)
)
