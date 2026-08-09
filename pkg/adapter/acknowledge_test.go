package adapter_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestBareAcknowledgeCannotMintExplicit(t *testing.T) {
	t.Parallel()
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               adapter.NewFakeActuator(),
		SupportsAdviceDelivery: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := del.Acknowledge("x", adapter.AckStatusAcked); !errors.Is(err, adapter.ErrBareAcknowledgeExplicit) {
		// may also fail unknown intervention first — enqueue+deliver then bare
	}
	del.Enqueue(protocol.Intervention{
		InterventionID: "x", SessionID: "s", ActionType: "ZOOM_OUT_PROMPT", AdvicePrompt: "a",
	}, time.Minute)
	if _, _, err := del.DeliverPending(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	if err := del.Acknowledge("x", adapter.AckStatusAcked); !errors.Is(err, adapter.ErrBareAcknowledgeExplicit) {
		t.Fatalf("want ErrBareAcknowledgeExplicit got %v", err)
	}
}

func TestGrokProfileRefusesExplicitSourceACK(t *testing.T) {
	t.Parallel()
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               adapter.NewFakeActuator(),
		SupportsAdviceDelivery: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	del.Enqueue(protocol.Intervention{
		InterventionID: "g", SessionID: "s", ActionType: "ZOOM_OUT_PROMPT", AdvicePrompt: "a",
	}, time.Minute)
	if _, _, err := del.DeliverPending(context.Background(), "s"); err != nil {
		t.Fatal(err)
	}
	err = del.AcknowledgeSource(adapter.AcknowledgeRequest{
		InterventionID: "g",
		HostFamily:     adapter.GrokLiveHostFamily,
		Profile:        adapter.GrokACPProfileV1,
		SourceKind:     "test",
		SourceEventID:  "e1",
		Status:         adapter.AckStatusAcked,
		AckLayer:       adapter.ACKLayerExplicit,
	})
	if !errors.Is(err, adapter.ErrExplicitACKNotSupported) {
		t.Fatalf("want ErrExplicitACKNotSupported got %v", err)
	}
}

func TestLedgerSymlinkRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	real := filepath.Join(dir, "real.jsonl")
	if err := os.WriteFile(real, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlink not supported")
	}
	if _, err := adapter.OpenDurableAdviceLedger(link); err == nil {
		t.Fatal("expected symlink reject")
	}
}

func TestProfileMaxACKLayerGrok(t *testing.T) {
	t.Parallel()
	if adapter.ProfileMaxACKLayer(adapter.GrokLiveHostFamily, adapter.GrokACPProfileV1) != adapter.ACKLayerSessionVisible {
		t.Fatal("grok ceiling")
	}
}
