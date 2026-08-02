package state_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/reinframe/reinframe/pkg/protocol"
	"github.com/reinframe/reinframe/pkg/state"
)

// TestChallenger_ConcurrentReadWriteStress tests 100 writers and 50 readers running concurrently against a SQLite WAL store.
func TestChallenger_ConcurrentReadWriteStress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping heavy concurrent stress test on Windows — NTFS file locking causes SQLITE_BUSY under CI contention")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "stress_read_write.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  10000 * time.Millisecond,
		MaxOpenConns: 20,
		MaxIdleConns: 10,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	const numWriters = 100
	const numReaders = 50
	const eventsPerWriter = 20

	var wg sync.WaitGroup
	errChan := make(chan error, (numWriters*eventsPerWriter)+numReaders*10)

	var writeCount int64
	var readCount int64

	// Launch 100 writer goroutines
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("stress-writer-%d", writerID)

			for i := 1; i <= eventsPerWriter; i++ {
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("evt-w%d-s%d", writerID, i),
					SessionID:   sessionID,
					SequenceNum: int64(i),
					EventType:   "stress_event",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(`{"stress":true}`),
				}
				if err := store.AppendEvent(ctx, evt); err != nil {
					errChan <- fmt.Errorf("writer %d event %d error: %w", writerID, i, err)
					return
				}
				atomic.AddInt64(&writeCount, 1)
			}
		}(w)
	}

	// Launch 50 reader goroutines querying continuously
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			targetSession := fmt.Sprintf("stress-writer-%d", readerID%numWriters)

			for i := 0; i < 10; i++ {
				_, err := store.QueryEvents(ctx, state.EventFilter{
					SessionID: targetSession,
					Ascending: true,
				})
				if err != nil && !errors.Is(err, context.Canceled) {
					errChan <- fmt.Errorf("reader %d pass %d error: %w", readerID, i, err)
					return
				}
				atomic.AddInt64(&readCount, 1)
				time.Sleep(2 * time.Millisecond)
			}
		}(r)
	}

	wg.Wait()
	close(errChan)

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		t.Fatalf("Encountered %d errors during concurrent read/write stress: %v", len(errs), errs[0])
	}

	expectedWrites := int64(numWriters * eventsPerWriter)
	if writeCount != expectedWrites {
		t.Errorf("write count = %d, want %d", writeCount, expectedWrites)
	}

	t.Logf("Read/Write stress completed successfully: %d writes, %d reads", writeCount, readCount)
}

// TestChallenger_BatchAtomicityOnFailure verifies that if AppendEvents fails mid-batch, no events from the batch are persisted.
func TestChallenger_BatchAtomicityOnFailure(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "atomicity.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	// Seed event 1 for session-atom
	seedEvt := &protocol.AgentEvent{
		EventID:     "existing-evt-5",
		SessionID:   "session-atom",
		SequenceNum: 5,
		EventType:   "seed",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{}`),
	}
	if err := store.AppendEvent(ctx, seedEvt); err != nil {
		t.Fatalf("failed to insert seed event: %v", err)
	}

	// Create a batch of 10 events where event index 6 collides with sequence 5
	batch := make([]*protocol.AgentEvent, 10)
	for i := 0; i < 10; i++ {
		seq := int64(i + 1)
		if i == 6 {
			seq = 5 // Duplicate sequence with seed event!
		}
		batch[i] = &protocol.AgentEvent{
			EventID:     fmt.Sprintf("batch-evt-%d", i),
			SessionID:   "session-atom",
			SequenceNum: seq,
			EventType:   "batch_atomicity",
			Timestamp:   time.Now().UTC(),
			Payload:     json.RawMessage(`{}`),
		}
	}

	err = store.AppendEvents(ctx, batch)
	if err == nil {
		t.Fatal("expected AppendEvents to fail due to duplicate sequence, but it succeeded")
	}

	// Verify that none of batch-evt-0..9 exist in the store
	events, err := store.QueryEvents(ctx, state.EventFilter{SessionID: "session-atom"})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected exactly 1 event (the seed event) in store, got %d", len(events))
	}

	if events[0].EventID != "existing-evt-5" {
		t.Errorf("expected seed event 'existing-evt-5', got '%s'", events[0].EventID)
	}
}

// TestChallenger_ContextCancellation verifies store methods respect canceled contexts gracefully without hanging.
func TestChallenger_ContextCancellation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "context_cancel.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	evt := &protocol.AgentEvent{
		EventID:     "evt-cancel-1",
		SessionID:   "sess-cancel",
		SequenceNum: 1,
		EventType:   "test",
		Timestamp:   time.Now().UTC(),
		Payload:     json.RawMessage(`{}`),
	}

	err = store.AppendEvent(ctx, evt)
	if err == nil {
		t.Error("expected error when calling AppendEvent with canceled context, got nil")
	}

	_, err = store.QueryEvents(ctx, state.EventFilter{SessionID: "sess-cancel"})
	if err == nil {
		t.Error("expected error when calling QueryEvents with canceled context, got nil")
	}
}

// TestChallenger_NanosecondTimestampPrecision tests timestamp sub-microsecond nanosecond handling.
func TestChallenger_NanosecondTimestampPrecision(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "nano_time.db")
	store, err := state.NewStore(state.StoreOptions{DatabasePath: dbPath})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	t1 := now.Truncate(time.Second).Add(123456789 * time.Nanosecond)
	t2 := now.Truncate(time.Second).Add(987654321 * time.Nanosecond)

	evt1 := &protocol.AgentEvent{
		EventID:     "e-nano-1",
		SessionID:   "s-nano",
		SequenceNum: 1,
		EventType:   "nano",
		Timestamp:   t1,
		Payload:     json.RawMessage(`{}`),
	}
	evt2 := &protocol.AgentEvent{
		EventID:     "e-nano-2",
		SessionID:   "s-nano",
		SequenceNum: 2,
		EventType:   "nano",
		Timestamp:   t2,
		Payload:     json.RawMessage(`{}`),
	}

	if err := store.AppendEvents(ctx, []*protocol.AgentEvent{evt1, evt2}); err != nil {
		t.Fatalf("AppendEvents failed: %v", err)
	}

	// Query events with startTime = t1 + 100ms
	midTime := t1.Add(100 * time.Millisecond)
	res, err := store.QueryEvents(ctx, state.EventFilter{
		SessionID: "s-nano",
		StartTime: &midTime,
	})
	if err != nil {
		t.Fatalf("QueryEvents failed: %v", err)
	}

	if len(res) != 1 {
		t.Fatalf("expected 1 event after midTime, got %d", len(res))
	}
	if res[0].EventID != "e-nano-2" {
		t.Errorf("expected event e-nano-2, got %s", res[0].EventID)
	}
}

// TestChallenger_Extreme500GoroutinesStress stress-tests SQLite WAL store with 500 concurrent goroutines (350 writers + 150 readers).
func TestChallenger_Extreme500GoroutinesStress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping extreme stress test on Windows — NTFS file locking causes SQLITE_BUSY under heavy contention")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	dbPath := filepath.Join(t.TempDir(), "extreme_500_stress.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  10000 * time.Millisecond,
		MaxOpenConns: 25,
		MaxIdleConns: 10,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer store.Close()

	const numWriters = 350
	const numReaders = 150
	const eventsPerWriter = 10

	var wg sync.WaitGroup
	errChan := make(chan error, (numWriters*eventsPerWriter)+numReaders*10)

	var writeCount int64
	var readCount int64

	// Launch 350 writer goroutines across multiple sessions
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			sessionID := fmt.Sprintf("500-writer-%d", writerID%50) // Shared among sets of writers for lock contention

			for i := 1; i <= eventsPerWriter; i++ {
				// To avoid duplicate sequence in shared session, use unique sequence or atomic per session
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("evt-w%d-seq%d-%d", writerID, i, time.Now().UnixNano()),
					SessionID:   sessionID,
					SequenceNum: int64((writerID/50)*eventsPerWriter + i),
					EventType:   "extreme_stress_write",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(`{"extreme":true}`),
				}
				if err := store.AppendEvent(ctx, evt); err != nil {
					errChan <- fmt.Errorf("extreme writer %d event %d error: %w", writerID, i, err)
					return
				}
				atomic.AddInt64(&writeCount, 1)
			}
		}(w)
	}

	// Launch 150 reader goroutines querying continuously
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			targetSession := fmt.Sprintf("500-writer-%d", readerID%50)

			for i := 0; i < 10; i++ {
				_, err := store.QueryEvents(ctx, state.EventFilter{
					SessionID: targetSession,
					Ascending: true,
				})
				if err != nil && !errors.Is(err, context.Canceled) {
					errChan <- fmt.Errorf("extreme reader %d pass %d error: %w", readerID, i, err)
					return
				}
				_, _ = store.GetLatestSequenceNum(ctx, targetSession)
				atomic.AddInt64(&readCount, 1)
				time.Sleep(1 * time.Millisecond)
			}
		}(r)
	}

	wg.Wait()
	close(errChan)

	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		t.Fatalf("Encountered %d errors during 500-goroutine stress test: %v", len(errs), errs[0])
	}

	expectedWrites := int64(numWriters * eventsPerWriter)
	if writeCount != expectedWrites {
		t.Errorf("write count = %d, want %d", writeCount, expectedWrites)
	}

	t.Logf("500-goroutine stress completed successfully: %d writes, %d reads across 500 goroutines", writeCount, readCount)
}

