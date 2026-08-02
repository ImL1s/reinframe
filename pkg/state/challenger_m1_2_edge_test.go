package state_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/state"
)

// TestChallenger_MultipleConcurrentCloseWithReadersWriters tests multiple concurrent Close() calls
// racing simultaneously with active readers and writers.
func TestChallenger_MultipleConcurrentCloseWithReadersWriters(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "multi_close_race.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5000 * time.Millisecond,
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	const numWriters = 25
	const numReaders = 25
	const numClosers = 10
	const opsPerWorker = 100

	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	errChan := make(chan error, (numWriters*opsPerWorker)+(numReaders*opsPerWorker*2)+numClosers)

	// Writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			<-startSignal
			sessionID := fmt.Sprintf("multi-close-w-%d", writerID)
			for i := 1; i <= opsPerWorker; i++ {
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("evt-mc-w%d-s%d", writerID, i),
					SessionID:   sessionID,
					SequenceNum: int64(i),
					EventType:   "multi_close_append",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(`{"data":"test"}`),
				}
				if err := store.AppendEvent(ctx, evt); err != nil {
					errChan <- err
				}
			}
		}(w)
	}

	// Readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			<-startSignal
			targetSession := fmt.Sprintf("multi-close-w-%d", readerID%numWriters)
			for i := 0; i < opsPerWorker; i++ {
				_, err := store.QueryEvents(ctx, state.EventFilter{
					SessionID: targetSession,
					Limit:     5,
				})
				if err != nil {
					errChan <- err
				}
				_, err = store.GetLatestSequenceNum(ctx, targetSession)
				if err != nil {
					errChan <- err
				}
			}
		}(r)
	}

	// Closers
	for c := 0; c < numClosers; c++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startSignal
			time.Sleep(2 * time.Millisecond)
			if err := store.Close(); err != nil {
				errChan <- fmt.Errorf("Close returned unexpected error: %w", err)
			}
		}()
	}

	close(startSignal)
	wg.Wait()
	close(errChan)

	for err := range errChan {
		if !errors.Is(err, state.ErrStoreClosed) {
			t.Errorf("expected error to be ErrStoreClosed, got: %v (type %T)", err, err)
		}
	}
}

// TestChallenger_RapidOpenAppendQueryClose tests rapid succession of Open -> Append/Query -> Close
// across multiple concurrent threads.
func TestChallenger_RapidOpenAppendQueryClose(t *testing.T) {
	const numThreads = 15
	const iterations = 10

	var wg sync.WaitGroup
	errChan := make(chan error, numThreads*iterations)

	for th := 0; th < numThreads; th++ {
		wg.Add(1)
		go func(threadID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				dbPath := filepath.Join(t.TempDir(), fmt.Sprintf("rapid_%d_%d.db", threadID, i))
				st, err := state.NewStore(state.StoreOptions{
					DatabasePath: dbPath,
					BusyTimeout:  2000 * time.Millisecond,
				})
				if err != nil {
					errChan <- fmt.Errorf("thread %d iter %d NewStore failed: %w", threadID, i, err)
					continue
				}

				ctx := context.Background()
				sess := fmt.Sprintf("rapid-sess-%d", threadID)
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("evt-rapid-%d-%d", threadID, i),
					SessionID:   sess,
					SequenceNum: 1,
					EventType:   "rapid_test",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(`{"rapid":true}`),
				}

				if err := st.AppendEvent(ctx, evt); err != nil {
					errChan <- fmt.Errorf("thread %d iter %d AppendEvent failed: %w", threadID, i, err)
				}

				evts, err := st.QueryEvents(ctx, state.EventFilter{SessionID: sess})
				if err != nil {
					errChan <- fmt.Errorf("thread %d iter %d QueryEvents failed: %w", threadID, i, err)
				} else if len(evts) != 1 {
					errChan <- fmt.Errorf("thread %d iter %d QueryEvents got %d events, want 1", threadID, i, len(evts))
				}

				if _, err := st.GetLatestSequenceNum(ctx, sess); err != nil {
					errChan <- fmt.Errorf("thread %d iter %d GetLatestSequenceNum failed: %w", threadID, i, err)
				}

				if err := st.Close(); err != nil {
					errChan <- fmt.Errorf("thread %d iter %d Close failed: %w", threadID, i, err)
				}
			}
		}(th)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("rapid cycle error: %v", err)
	}
}

// TestChallenger_ContextCancelRacingWithClose tests context cancellation during store operations racing with Close().
func TestChallenger_ContextCancelRacingWithClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ctx_cancel_close.db")
	store, err := state.NewStore(state.StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  5000 * time.Millisecond,
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	})
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	const numWorkers = 30
	const opsPerWorker = 50

	var wg sync.WaitGroup
	startSignal := make(chan struct{})
	errChan := make(chan error, numWorkers*opsPerWorker)
	closeErrCh := make(chan error, 1)

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startSignal
			sessionID := fmt.Sprintf("ctx-cancel-w-%d", workerID)
			for i := 1; i <= opsPerWorker; i++ {
				// Use short timeouts or cancelled contexts
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration((i%5)+1)*time.Microsecond)
				evt := &protocol.AgentEvent{
					EventID:     fmt.Sprintf("evt-cc-w%d-s%d", workerID, i),
					SessionID:   sessionID,
					SequenceNum: int64(i),
					EventType:   "ctx_cancel_test",
					Timestamp:   time.Now().UTC(),
					Payload:     json.RawMessage(`{"data":"test"}`),
				}
				err := store.AppendEvent(ctx, evt)
				if err != nil {
					errChan <- err
				}
				cancel()
			}
		}(w)
	}

	// Close racing after short delay; wait for closer before test ends.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-startSignal
		time.Sleep(1 * time.Millisecond)
		closeErrCh <- store.Close()
	}()

	close(startSignal)
	wg.Wait()
	close(errChan)

	if err := <-closeErrCh; err != nil {
		t.Errorf("Close during race returned unexpected error: %v", err)
	}

	for err := range errChan {
		isAllowed := errors.Is(err, state.ErrStoreClosed) ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded)

		if !isAllowed {
			t.Errorf("unallowed error type received during context cancel + Close race: %v (type %T)", err, err)
		}
	}
}
