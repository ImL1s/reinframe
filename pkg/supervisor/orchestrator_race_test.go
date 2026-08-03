package supervisor_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/supervisor"
)

// TestOrchestrator_ConcurrentMultiSessionRace drives the real Orchestrator from
// many sessions in parallel through HandleEvent → EvaluateSlow → buildZoomOut.
// Under go test -race this catches unsynchronized policy idSeq (duplicate InterventionIDs).
func TestOrchestrator_ConcurrentMultiSessionRace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	act := adapter.NewFakeActuator()
	act.AutoAck = false
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Shared detector + policy engine across sessions (production composition).
	det := detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3})
	pol := policy.NewEngine(policy.EngineConfig{})
	orch, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: det,
		Policy:   pol,
		Delivery: del,
	})
	if err != nil {
		t.Fatal(err)
	}

	const sessions = 32
	const eventsPerSession = 3 // third fires ZOOM_OUT
	var wg sync.WaitGroup
	errCh := make(chan error, sessions)
	ivCh := make(chan string, sessions)

	for s := 0; s < sessions; s++ {
		wg.Add(1)
		go func(si int) {
			defer wg.Done()
			sess := fmt.Sprintf("race-sess-%d", si)
			// Distinct failure text per session so each session fires independently.
			msg := fmt.Sprintf("fail session %d unique", si)
			var lastIV *protocol.Intervention
			for i := 1; i <= eventsPerSession; i++ {
				payload, _ := json.Marshal(map[string]string{"error": msg})
				ev := protocol.AgentEvent{
					EventID:     fmt.Sprintf("%s-e%d", sess, i),
					SessionID:   sess,
					SequenceNum: int64(i),
					EventType:   "error",
					Timestamp:   time.Now().UTC(),
					Payload:     payload,
				}
				_, iv, err := orch.HandleEvent(ctx, ev)
				if err != nil {
					errCh <- fmt.Errorf("%s event %d: %w", sess, i, err)
					return
				}
				if i < eventsPerSession && iv != nil {
					errCh <- fmt.Errorf("%s early intervention at event %d", sess, i)
					return
				}
				if i == eventsPerSession {
					if iv == nil {
						errCh <- fmt.Errorf("%s expected intervention on 3rd failure", sess)
						return
					}
					lastIV = iv
				}
			}
			// Fast path latch must defer for this session.
			dec := orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
			if dec.Action != adapter.HookActionDefer {
				errCh <- fmt.Errorf("%s PreTool action=%s want defer", sess, dec.Action)
				return
			}
			if lastIV != nil {
				ivCh <- lastIV.InterventionID
			}
		}(s)
	}
	wg.Wait()
	close(errCh)
	close(ivCh)
	for err := range errCh {
		t.Error(err)
	}

	ids := make(map[string]struct{})
	for id := range ivCh {
		if id == "" {
			t.Error("empty intervention id")
			continue
		}
		if _, dup := ids[id]; dup {
			t.Fatalf("duplicate InterventionID %q (idSeq race)", id)
		}
		ids[id] = struct{}{}
	}
	if len(ids) != sessions {
		t.Fatalf("got %d unique intervention IDs, want %d", len(ids), sessions)
	}
}
