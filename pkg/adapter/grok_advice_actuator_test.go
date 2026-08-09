package adapter_test

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestGrokACPActuator_TransportAndSessionVisible(t *testing.T) {
	t.Parallel()
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fakeACPServer(t, serverR, serverW, nil)
	}()
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{
		StartupTimeout: 5 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, map[string]any{"name": "t"}); err != nil {
		t.Fatal(err)
	}
	sid, err := c.SessionNew(ctx, map[string]any{"cwd": "/tmp"})
	if err != nil {
		t.Fatal(err)
	}

	act := &adapter.GrokACPActuator{
		Client:          c,
		TargetSessionID: sid,
		HostVersion:     "grok 1.0.0 test",
		WaitUpdate:      2 * time.Second,
	}
	iv := protocol.Intervention{
		InterventionID: "iv-108-1",
		SessionID:      "rf-sess",
		ActionType:     "REQUEST_REPLAN",
		AdvicePrompt:   "Re-evaluate against acceptance criteria.",
		RequiresAck:    true,
		SafeBoundary:   adapter.GrokSafeBoundaryNextInput,
	}
	res, err := act.Deliver(ctx, iv)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Accepted || res.AckStatus != adapter.AckStatusPending {
		t.Fatalf("res=%+v", res)
	}
	if res.AckLayer != adapter.ACKLayerSessionVisible && res.AckLayer != adapter.ACKLayerTransport {
		t.Fatalf("ack layer=%s", res.AckLayer)
	}
	if res.AckLayer == adapter.ACKLayerExplicit {
		t.Fatal("must not claim explicit")
	}
	if res.HostFamily != adapter.GrokLiveHostFamily || res.Profile != adapter.GrokACPProfileV1 {
		t.Fatalf("host pin %+v", res)
	}
	if res.SafeBoundary != adapter.GrokSafeBoundaryNextInput {
		t.Fatalf("boundary %s", res.SafeBoundary)
	}

	// Through AdvisoryDelivery: intermediate state is SESSION_VISIBLE or TRANSPORT_ACCEPTED.
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	iv2 := iv
	iv2.InterventionID = "iv-108-2"
	del.Enqueue(iv2, time.Minute)
	item, res2, err := del.DeliverPending(ctx, "rf-sess")
	if err != nil {
		t.Fatal(err)
	}
	if item.State != adapter.StateSessionVisible && item.State != adapter.StateTransportAccepted && item.State != adapter.StateDelivering {
		t.Fatalf("state=%s", item.State)
	}
	if res2.AckLayer == adapter.ACKLayerExplicit {
		t.Fatal("explicit not allowed")
	}

	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestGrokACPActuator_SessionMismatch(t *testing.T) {
	t.Parallel()
	act := &adapter.GrokACPActuator{
		Client:          &adapter.GrokACPClient{}, // unused on mismatch
		TargetSessionID: "sess-a",
	}
	// Use nil-safe: Deliver checks client first — set Client via fake? Session mismatch runs after client check.
	// Provide a test client pipe so we pass client nil check.
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fakeACPServer(t, serverR, serverW, nil)
	}()
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{})
	ctx := context.Background()
	if _, err := c.Initialize(ctx, map[string]any{"name": "t"}); err != nil {
		t.Fatal(err)
	}
	act.Client = c
	act.TargetSessionID = "sess-a"
	res, err := act.Deliver(ctx, protocol.Intervention{
		InterventionID: "x",
		SessionID:      "rf",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "hi",
		Fingerprint:    "acp:sess-b",
	})
	if err == nil || res.AckStatus != adapter.AckStatusRejected {
		t.Fatalf("want mismatch reject got %+v err=%v", res, err)
	}
	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestGrokACPActuator_RejectsPrivateReasoningBody(t *testing.T) {
	t.Parallel()
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fakeACPServer(t, serverR, serverW, nil)
	}()
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{})
	ctx := context.Background()
	if _, err := c.Initialize(ctx, map[string]any{"name": "t"}); err != nil {
		t.Fatal(err)
	}
	act := &adapter.GrokACPActuator{Client: c, TargetSessionID: "s1"}
	res, err := act.Deliver(ctx, protocol.Intervention{
		InterventionID: "p",
		SessionID:      "rf",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "here is my chain-of-thought dump",
	})
	if err == nil || res.AckStatus != adapter.AckStatusRejected {
		t.Fatalf("want privacy reject %+v", res)
	}
	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestDurableAdviceLedger_RestartSuppressesDuplicate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.jsonl")
	l1, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	res := adapter.InterventionResult{
		InterventionID: "iv-dup",
		AckLayer:       adapter.ACKLayerSessionVisible,
		AckStatus:      adapter.AckStatusPending,
		HostFamily:     adapter.GrokLiveHostFamily,
		Profile:        adapter.GrokACPProfileV1,
		SafeBoundary:   adapter.GrokSafeBoundaryNextInput,
		Message:        "delivered",
	}
	if err := l1.RecordResult(adapter.StatePending, "rf", res, adapter.StateSessionVisible); err != nil {
		t.Fatal(err)
	}
	if !l1.AlreadyDelivered("iv-dup") {
		t.Fatal("expected seen")
	}
	// Restart
	l2, err := adapter.OpenDurableAdviceLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if !l2.AlreadyDelivered("iv-dup") {
		t.Fatal("restart must suppress duplicate delivery")
	}
	if l2.Cursor() <= 0 {
		t.Fatal("cursor")
	}
}

func TestAdvisoryDelivery_AcknowledgeFromSessionVisible(t *testing.T) {
	t.Parallel()
	act := &adapter.FakeActuator{AutoAck: false}
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := protocol.Intervention{
		InterventionID: "iv-ack",
		SessionID:      "s",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "zoom",
	}
	del.Enqueue(iv, time.Minute)
	// Force state to SESSION_VISIBLE as if Grok delivered.
	del.Queue().UpdateState("iv-ack", adapter.StateSessionVisible, &adapter.InterventionResult{
		InterventionID: "iv-ack",
		Accepted:       true,
		AckStatus:      adapter.AckStatusPending,
		AckLayer:       adapter.ACKLayerSessionVisible,
	})
	// Remove from pending delivery order is already done? NextPending moves it.
	// UpdateState alone leaves bySess; Acknowledge should work from SESSION_VISIBLE.
	if err := del.AcknowledgeSource(adapter.AcknowledgeRequest{
		InterventionID: "iv-ack", SourceKind: "test", SourceEventID: "evt-1",
		Status: adapter.AckStatusAcked, AckLayer: adapter.ACKLayerSessionVisible,
	}); err != nil {
		t.Fatal(err)
	}
	item, ok := del.Get("iv-ack")
	if !ok {
		t.Fatal("missing")
	}
	// Source-bound ack at session_visible ceiling (Grok cannot mint explicit).
	if item.State != adapter.StateSessionVisible && item.State != adapter.StateAcked {
		t.Fatalf("state=%s want SESSION_VISIBLE or ACKED", item.State)
	}
	if item.Result == nil || item.Result.AckLayer != adapter.ACKLayerSessionVisible {
		t.Fatalf("session_visible layer expected (not explicit), got %+v", item.Result)
	}
	if err := del.AcknowledgeSource(adapter.AcknowledgeRequest{
		InterventionID: "iv-ack",
		HostFamily:     adapter.GrokLiveHostFamily,
		Profile:        adapter.GrokACPProfileV1,
		SourceKind:     "test",
		SourceEventID:  "evt-explicit",
		Status:         adapter.AckStatusAcked,
		AckLayer:       adapter.ACKLayerExplicit,
	}); err == nil {
		t.Fatal("Grok profile must refuse explicit ACK")
	}
}

func TestGrokACPActuator_SecondDeliverStaysTransportWithoutNewUpdates(t *testing.T) {
	t.Parallel()
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fakeACPServer(t, serverR, serverW, nil)
	}()
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{
		StartupTimeout: 5 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, map[string]any{"name": "t"}); err != nil {
		t.Fatal(err)
	}
	sid, err := c.SessionNew(ctx, map[string]any{"cwd": "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	act := &adapter.GrokACPActuator{
		Client:          c,
		TargetSessionID: sid,
		WaitUpdate:      300 * time.Millisecond,
	}
	// First delivery may see session_visible from fake update.
	res1, err := act.Deliver(ctx, protocol.Intervention{
		InterventionID: "iv-a",
		SessionID:      "rf",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "first",
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.LastACKLayer() != adapter.ACKLayerSessionVisible && res1.AckLayer != adapter.ACKLayerSessionVisible {
		// Still ok if only transport — client may not have upgraded.
		t.Logf("first layer res=%s client=%s", res1.AckLayer, c.LastACKLayer())
	}
	// Second delivery with WaitUpdate=0 must stay transport even when client
	// LastACKLayer is already session_visible from a prior delivery.
	act.WaitUpdate = 0
	res2, err := act.Deliver(ctx, protocol.Intervention{
		InterventionID: "iv-b",
		SessionID:      "rf",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.AckLayer != adapter.ACKLayerTransport {
		t.Fatalf("second deliver must not reuse stale client LastACK; got %s (first was %s client=%s)", res2.AckLayer, res1.AckLayer, c.LastACKLayer())
	}
	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestAdvisoryDelivery_LedgerSuppressesDuplicate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	led, err := adapter.OpenDurableAdviceLedger(filepath.Join(dir, "l.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	act := &adapter.FakeActuator{AutoAck: false}
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led,
	})
	if err != nil {
		t.Fatal(err)
	}
	iv := protocol.Intervention{
		InterventionID: "iv-led",
		SessionID:      "s",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "once",
	}
	del.Enqueue(iv, time.Minute)
	item, res, err := del.DeliverPending(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if item.State != adapter.StateDelivering && item.State != adapter.StateTransportAccepted && item.State != adapter.StateSessionVisible {
		// FakeActuator AutoAck false → pending → DELIVERING
		if item.State != adapter.StateDelivering {
			t.Fatalf("state=%s res=%+v", item.State, res)
		}
	}
	if !led.AlreadyDelivered("iv-led") {
		// Fake maps to DELIVERING which is NOT suppress state — force record transport-accepted
		_ = led.RecordResult(adapter.StateDelivering, "s", adapter.InterventionResult{
			InterventionID: "iv-led",
			AckLayer:       adapter.ACKLayerTransport,
			AckStatus:      adapter.AckStatusPending,
		}, adapter.StateTransportAccepted)
	}
	// Re-enqueue same ID is suppressed by queue; use new enqueue after forget?
	// AlreadyDelivered path: put another PENDING clone isn't possible (queue dedupe).
	// Simulate restart: new queue + ledger reload.
	led2, err := adapter.OpenDurableAdviceLedger(filepath.Join(dir, "l.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	del2, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		Ledger:                 led2,
		Queue:                  adapter.NewPendingQueue(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// New queue allows re-enqueue same InterventionID as "new" only if not in byID —
	// new queue is empty so Enqueue succeeds; DeliverPending should suppress via ledger.
	del2.Enqueue(iv, time.Minute)
	item2, res2, err := del2.DeliverPending(context.Background(), "s")
	if err != nil {
		t.Fatal(err)
	}
	if item2.State != adapter.StateSuppressed {
		t.Fatalf("want SUPPRESSED got %s res=%+v", item2.State, res2)
	}
	if act.CallCount() != 1 {
		t.Fatalf("actuator calls=%d want 1 (ledger suppress)", act.CallCount())
	}
}

func TestNopAlerterStillRefusedWhenUnsupported(t *testing.T) {
	t.Parallel()
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               &adapter.FakeActuator{},
		Alerter:                adapter.NopHumanAlerter{},
		SupportsAdviceDelivery: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	del.Enqueue(protocol.Intervention{
		InterventionID: "u1",
		SessionID:      "s",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "x",
	}, time.Minute)
	_, res, err := del.DeliverPending(context.Background(), "s")
	if err != adapter.ErrNopHumanAlerter {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	if res.AckStatus != adapter.AckStatusUnsupported {
		t.Fatalf("%+v", res)
	}
}
