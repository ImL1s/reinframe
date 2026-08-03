package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

var (
	// ErrDuplicateSequence is returned when inserting an event with a duplicate sequence_num for the same session.
	ErrDuplicateSequence = errors.New("duplicate sequence number for session")
	// ErrDuplicateEventID is returned when inserting an event with a duplicate event_id.
	ErrDuplicateEventID = errors.New("duplicate event ID")
	// ErrInvalidEvent is returned when an event is missing required fields.
	ErrInvalidEvent = errors.New("invalid agent event")
	// ErrStoreClosed is returned when an operation is performed on a closed store.
	ErrStoreClosed = errors.New("store is closed")
)

// FixedTimestampLayout ensures fixed-width ISO 8601 formatting for lexically sortable SQLite text comparison.
const FixedTimestampLayout = "2006-01-02T15:04:05.000000000Z"

// StoreOptions defines configuration parameters for initializing the SQLite Store.
type StoreOptions struct {
	DatabasePath string
	BusyTimeout  time.Duration
	MaxOpenConns int
	MaxIdleConns int
}

// EventFilter specifies search criteria for querying events from the store.
type EventFilter struct {
	SessionID     string
	EventTypes    []string
	StartSequence *int64
	EndSequence   *int64
	StartTime     *time.Time
	EndTime       *time.Time
	Limit         int
	Offset        int
	Ascending     bool
}

var memDBSeq uint64

// Store represents an append-only event store backed by SQLite with WAL mode.
type Store struct {
	db       *sql.DB
	closed   atomic.Bool
	inFlight atomic.Int64
}

// NewStore initializes a new SQLite WAL event store with embedded schema migrations applied.
func NewStore(opts StoreOptions) (*Store, error) {
	dbPath := opts.DatabasePath
	isMemory := dbPath == "" || dbPath == ":memory:" || strings.Contains(dbPath, "mode=memory")
	if dbPath == "" || dbPath == ":memory:" {
		dbPath = fmt.Sprintf("file:reinframe-memory-%d?mode=memory&cache=shared", atomic.AddUint64(&memDBSeq, 1))
	}

	busyTimeoutMs := 5000
	if opts.BusyTimeout > 0 {
		busyTimeoutMs = int(opts.BusyTimeout.Milliseconds())
	}

	pragmas := fmt.Sprintf("_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate", busyTimeoutMs)

	var dsn string
	if strings.Contains(dbPath, "?") {
		dsn = fmt.Sprintf("%s&%s", dbPath, pragmas)
	} else {
		dsn = fmt.Sprintf("%s?%s", dbPath, pragmas)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	maxOpen := 10
	if opts.MaxOpenConns > 0 {
		maxOpen = opts.MaxOpenConns
	}

	if isMemory {
		maxOpen = 1
	}

	maxIdle := 5
	if opts.MaxIdleConns > 0 {
		maxIdle = opts.MaxIdleConns
	}
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)

	// Run embedded migrations
	if err := RunMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &Store{
		db: db,
	}, nil
}

// AppendEvent inserts a single agent event using a busy-aware BEGIN IMMEDIATE transaction.
// It enforces persistence invariants only (non-nil event, non-empty IDs/type, positive sequence).
// Full protocol schema validation (protocol.ValidateEvent) is the caller's / ingestion layer responsibility.
func (s *Store) AppendEvent(ctx context.Context, event *protocol.AgentEvent) error {
	if event == nil {
		return ErrInvalidEvent
	}
	if event.EventID == "" || event.SessionID == "" || event.EventType == "" || event.SequenceNum <= 0 {
		return ErrInvalidEvent
	}

	return s.AppendEvents(ctx, []*protocol.AgentEvent{event})
}

// AppendEvents inserts multiple agent events atomically with SQLite busy retry on the write path.
// Same persistence-invariant checks as AppendEvent; does not call protocol.ValidateEvent.
func (s *Store) AppendEvents(ctx context.Context, events []*protocol.AgentEvent) error {
	if err := s.enter(); err != nil {
		return err
	}
	defer s.leave()

	if len(events) == 0 {
		return nil
	}

	for _, e := range events {
		if e == nil || e.EventID == "" || e.SessionID == "" || e.EventType == "" || e.SequenceNum <= 0 {
			return ErrInvalidEvent
		}
	}

	err := runTxWithRetry(ctx, s.db, func(tx *sql.Tx) error {
		stmtSQL := `INSERT INTO events (event_id, session_id, sequence_num, event_type, timestamp, payload) VALUES (?, ?, ?, ?, ?, ?)`
		stmt, err := tx.PrepareContext(ctx, stmtSQL)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, e := range events {
			tsStr := e.Timestamp.UTC().Format(FixedTimestampLayout)
			payloadStr := string(e.Payload)
			if payloadStr == "" {
				payloadStr = "{}"
			}

			if _, err := stmt.ExecContext(ctx, e.EventID, e.SessionID, e.SequenceNum, e.EventType, tsStr, payloadStr); err != nil {
				return err
			}
		}

		return tx.Commit()
	})
	if err != nil {
		return s.mapWriteErr(ctx, err)
	}

	return nil
}

// QueryEvents retrieves events from the store matching the provided EventFilter criteria.
func (s *Store) QueryEvents(ctx context.Context, filter EventFilter) ([]*protocol.AgentEvent, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.leave()

	var queryBuilder strings.Builder
	queryBuilder.WriteString("SELECT event_id, session_id, sequence_num, event_type, timestamp, payload FROM events WHERE 1=1")

	var args []interface{}

	if filter.SessionID != "" {
		queryBuilder.WriteString(" AND session_id = ?")
		args = append(args, filter.SessionID)
	}

	if len(filter.EventTypes) > 0 {
		placeholders := make([]string, len(filter.EventTypes))
		for i, et := range filter.EventTypes {
			placeholders[i] = "?"
			args = append(args, et)
		}
		fmt.Fprintf(&queryBuilder, " AND event_type IN (%s)", strings.Join(placeholders, ","))
	}

	if filter.StartSequence != nil {
		queryBuilder.WriteString(" AND sequence_num >= ?")
		args = append(args, *filter.StartSequence)
	}

	if filter.EndSequence != nil {
		queryBuilder.WriteString(" AND sequence_num <= ?")
		args = append(args, *filter.EndSequence)
	}

	if filter.StartTime != nil {
		queryBuilder.WriteString(" AND timestamp >= ?")
		args = append(args, filter.StartTime.UTC().Format(FixedTimestampLayout))
	}

	if filter.EndTime != nil {
		queryBuilder.WriteString(" AND timestamp <= ?")
		args = append(args, filter.EndTime.UTC().Format(FixedTimestampLayout))
	}

	if filter.Ascending {
		queryBuilder.WriteString(" ORDER BY sequence_num ASC")
	} else {
		queryBuilder.WriteString(" ORDER BY sequence_num DESC")
	}

	if filter.Limit > 0 {
		queryBuilder.WriteString(" LIMIT ?")
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			queryBuilder.WriteString(" OFFSET ?")
			args = append(args, filter.Offset)
		}
	} else if filter.Offset > 0 {
		queryBuilder.WriteString(" LIMIT -1 OFFSET ?")
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, queryBuilder.String(), args...)
	if err != nil {
		return nil, s.wrapDBErr(fmt.Errorf("failed to query events: %w", err))
	}
	defer rows.Close()

	events := []*protocol.AgentEvent{}
	for rows.Next() {
		var e protocol.AgentEvent
		var tsStr string
		var payloadStr string

		if err := rows.Scan(&e.EventID, &e.SessionID, &e.SequenceNum, &e.EventType, &tsStr, &payloadStr); err != nil {
			return nil, s.wrapDBErr(fmt.Errorf("failed to scan event row: %w", err))
		}

		t, err := parseTimestamp(tsStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse event timestamp %q: %w", tsStr, err)
		}
		e.Timestamp = t
		e.Payload = json.RawMessage(payloadStr)

		events = append(events, &e)
	}

	if err := rows.Err(); err != nil {
		return nil, s.wrapDBErr(fmt.Errorf("error iterating event rows: %w", err))
	}

	return events, nil
}

// GetLatestSequenceNum returns the highest sequence_num for the given session ID, or 0 if no events exist.
func (s *Store) GetLatestSequenceNum(ctx context.Context, sessionID string) (int64, error) {
	if err := s.enter(); err != nil {
		return 0, err
	}
	defer s.leave()

	var maxSeq sql.NullInt64
	query := "SELECT MAX(sequence_num) FROM events WHERE session_id = ?"
	if err := s.db.QueryRowContext(ctx, query, sessionID).Scan(&maxSeq); err != nil {
		return 0, s.wrapDBErr(fmt.Errorf("failed to query max sequence number: %w", err))
	}

	if !maxSeq.Valid {
		return 0, nil
	}

	return maxSeq.Int64, nil
}

// Close closes the database connection pool after draining in-flight operations.
func (s *Store) Close() error {
	if s.closed.Swap(true) {
		return nil
	}

	deadline := time.Now().Add(5 * time.Second)
	for s.inFlight.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond)
	}

	return s.db.Close()
}

func (s *Store) enter() error {
	if s.closed.Load() {
		return ErrStoreClosed
	}
	s.inFlight.Add(1)
	if s.closed.Load() {
		s.inFlight.Add(-1)
		return ErrStoreClosed
	}
	return nil
}

func (s *Store) leave() {
	s.inFlight.Add(-1)
}

// mapWriteErr maps append/write failures after busy-retry.
// Prefer context errors when the driver surfaces SQLITE_INTERRUPT under a dead ctx;
// otherwise apply wrapDBErr (domain / closed-window mapping).
func (s *Store) mapWriteErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	mapped := mapSQLiteError(err)
	// Domain errors must surface unchanged even under cancel/close races.
	if errors.Is(mapped, ErrDuplicateSequence) || errors.Is(mapped, ErrDuplicateEventID) || errors.Is(mapped, ErrInvalidEvent) {
		return mapped
	}
	if ctx != nil && ctx.Err() != nil && isSQLiteInterrupt(err) {
		return ctx.Err()
	}
	return s.wrapDBErr(mapped)
}

// isSQLiteInterrupt reports SQLITE_INTERRUPT-class driver errors (code 9).
func isSQLiteInterrupt(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "interrupted") || strings.Contains(msg, "interrupt")
}

// wrapDBErr maps connection/shutdown failures to ErrStoreClosed.
// Domain errors (duplicate sequence/event ID, invalid event) are never rewritten
// solely because s.closed is true — a concurrent Close can flip that flag after a
// constraint violation was already produced on a still-open connection.
// Once closed=true, every other non-domain error is treated as store-closed so
// close-race drain noise (interrupt, rolled-back tx, etc.) does not leak.
func (s *Store) wrapDBErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrStoreClosed) {
		return ErrStoreClosed
	}
	// Domain errors always win over closed-flag mapping.
	if errors.Is(err, ErrDuplicateSequence) || errors.Is(err, ErrDuplicateEventID) || errors.Is(err, ErrInvalidEvent) {
		return err
	}
	errStr := err.Error()
	// Connection/tx lifecycle failures are store-closed class even if the closed
	// flag race has not flipped yet (common under Close + concurrent Append).
	if errors.Is(err, sql.ErrConnDone) ||
		strings.Contains(errStr, "database is closed") ||
		strings.Contains(errStr, "transaction has already been committed or rolled back") ||
		strings.Contains(errStr, "sql: connection is already closed") ||
		// SQLITE_INTERRUPT during close/cancel races; map even before closed flag flips.
		isSQLiteInterrupt(err) {
		return ErrStoreClosed
	}
	// Closed window: map all remaining operational errors to ErrStoreClosed.
	if s.closed.Load() {
		return ErrStoreClosed
	}
	return err
}

func parseTimestamp(tsStr string) (time.Time, error) {
	if t, err := time.Parse(FixedTimestampLayout, tsStr); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported timestamp format")
}

func mapSQLiteError(err error) error {
	if err == nil {
		return nil
	}
	errStr := err.Error()
	if strings.Contains(errStr, "unq_events_session_seq") ||
		strings.Contains(errStr, "events.session_id, events.sequence_num") ||
		strings.Contains(errStr, "session_id, sequence_num") {
		return fmt.Errorf("%w: %v", ErrDuplicateSequence, err)
	}
	if strings.Contains(errStr, "events.event_id") ||
		strings.Contains(errStr, "PRIMARY KEY") {
		return fmt.Errorf("%w: %v", ErrDuplicateEventID, err)
	}
	if strings.Contains(errStr, "UNIQUE constraint failed") {
		return fmt.Errorf("%w: %v", ErrDuplicateSequence, err)
	}
	return err
}
