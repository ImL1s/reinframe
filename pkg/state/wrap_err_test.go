package state

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// TestWrapDBErr_DoesNotMaskDomainErrorWhenClosedFlag exercises the shipped wrapDBErr
// mapping: a concurrent Close may flip closed=true after a domain error is produced on
// a still-open connection; that domain error must not become ErrStoreClosed.
func TestWrapDBErr_DoesNotMaskDomainErrorWhenClosedFlag(t *testing.T) {
	s := &Store{}
	s.closed.Store(true)

	dup := fmt.Errorf("%w: unique constraint", ErrDuplicateSequence)
	got := s.wrapDBErr(dup)
	if !errors.Is(got, ErrDuplicateSequence) {
		t.Fatalf("expected ErrDuplicateSequence preserved, got %v", got)
	}
	if errors.Is(got, ErrStoreClosed) {
		t.Fatalf("domain error must not be rewritten to ErrStoreClosed solely because closed=true")
	}

	dupID := fmt.Errorf("%w: primary key", ErrDuplicateEventID)
	got = s.wrapDBErr(dupID)
	if !errors.Is(got, ErrDuplicateEventID) {
		t.Fatalf("expected ErrDuplicateEventID preserved, got %v", got)
	}

	// True closed-connection errors still map to ErrStoreClosed.
	if !errors.Is(s.wrapDBErr(errors.New("sql: database is closed")), ErrStoreClosed) {
		t.Fatalf("expected database-is-closed string to map to ErrStoreClosed")
	}
	// Closed window maps all non-domain operational noise (not only interrupt/busy).
	for _, msg := range []string{
		"interrupted (9)",
		"sql: transaction has already been committed or rolled back",
		"some unexpected driver error",
	} {
		if !errors.Is(s.wrapDBErr(errors.New(msg)), ErrStoreClosed) {
			t.Fatalf("expected closed-window non-domain error %q to map to ErrStoreClosed", msg)
		}
	}
}

// TestAppendEvent_DuplicateSequenceNotMaskedByClosedFlag forces mapSQLiteError → wrapDBErr
// with closed=true while the DB is still open (simulates Close drain window): domain error
// must survive.
func TestAppendEvent_DuplicateSequenceNotMaskedByClosedFlag(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wrap_domain.db")
	store, err := NewStore(StoreOptions{
		DatabasePath: dbPath,
		BusyTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer func() {
		store.closed.Store(false)
		_ = store.Close()
	}()

	ctx := context.Background()
	base := &protocol.AgentEvent{
		EventID:     "evt-base-1",
		SessionID:   "sess-wrap",
		SequenceNum: 1,
		EventType:   "agent_session",
		Timestamp:   time.Now().UTC(),
		Payload:     []byte(`{}`),
	}
	if err := store.AppendEvent(ctx, base); err != nil {
		t.Fatalf("seed AppendEvent: %v", err)
	}

	// Flip closed without closing the pool — mirrors the window after closed.Swap(true)
	// while in-flight ops still use an open connection.
	store.closed.Store(true)

	// Bypass enter() closed check by entering as if we were already in-flight before Close,
	// then calling the write body through AppendEvents after resetting enter gate via leave/enter race.
	// Direct path: temporarily clear closed for enter, re-set before wrap — instead call wrap after
	// forcing the constraint on the open DB and map through wrapDBErr with closed true.
	// Real shipped path: use enter() by clearing flag only for the duration of enter, then set again.
	store.closed.Store(false)
	if err := store.enter(); err != nil {
		t.Fatalf("enter: %v", err)
	}
	store.closed.Store(true)

	// Perform insert on open DB (in-flight under closed flag), then map like AppendEvents does.
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		store.leave()
		t.Fatalf("BeginTx: %v", err)
	}
	_, execErr := tx.ExecContext(ctx,
		`INSERT INTO events (event_id, session_id, sequence_num, event_type, timestamp, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		"evt-dup-seq", "sess-wrap", int64(1), "agent_session", time.Now().UTC().Format(FixedTimestampLayout), `{}`,
	)
	_ = tx.Rollback()
	store.leave()

	if execErr == nil {
		t.Fatal("expected unique constraint error on duplicate sequence")
	}
	got := store.wrapDBErr(mapSQLiteError(execErr))
	if !errors.Is(got, ErrDuplicateSequence) {
		t.Fatalf("expected ErrDuplicateSequence after wrap with closed=true, got %v", got)
	}
	if errors.Is(got, ErrStoreClosed) {
		t.Fatalf("ErrDuplicateSequence must not be masked as ErrStoreClosed, got %v", got)
	}
}
