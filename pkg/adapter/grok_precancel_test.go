package adapter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// TestGrokDeliver_PreCancelContext_NotSent: ctx already canceled before SessionPrompt
// must declare BoundaryNotSent so durable-fail does not permanent-suppress.
func TestGrokDeliver_PreCancelContext_NotSent(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	act := &adapter.GrokACPActuator{
		// Client non-nil pointer not required if TargetSessionID empty returns first —
		// set TargetSessionID and Client to reach ctx.Err() check after local validation.
		Client:          &adapter.GrokACPClient{},
		TargetSessionID: "sess-1",
	}
	res, err := act.Deliver(ctx, protocol.Intervention{
		InterventionID: "iv-cancel",
		SessionID:      "rf",
		ActionType:     "ZOOM_OUT_PROMPT",
		AdvicePrompt:   "hi",
	})
	if err == nil {
		t.Fatal("expected context error")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
		// Deliver returns ctx.Err()
		t.Logf("err=%v", err)
	}
	if res.DeliveryBoundary != adapter.BoundaryNotSent {
		t.Fatalf("pre-SessionPrompt cancel want not_sent got %q", res.DeliveryBoundary)
	}
	if adapter.ShouldAmbiguousSuppress(adapter.ClassifyDeliveryBoundary(res, err)) {
		t.Fatal("pre-cancel must not suppress on durable fail")
	}
	_ = time.Second
}
