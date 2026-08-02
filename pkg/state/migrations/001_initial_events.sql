CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    event_id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL,
    sequence_num INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    timestamp TEXT NOT NULL,
    payload TEXT NOT NULL,
    CONSTRAINT unq_events_session_seq UNIQUE (session_id, sequence_num)
);

CREATE TABLE IF NOT EXISTS audit_records (
    audit_id TEXT PRIMARY KEY NOT NULL,
    session_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    category TEXT NOT NULL,
    summary TEXT NOT NULL,
    detail_json TEXT,
    recorded_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_event_type ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_events_sequence_num ON events(sequence_num);
CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
CREATE INDEX IF NOT EXISTS idx_events_session_type_seq ON events(session_id, event_type, sequence_num);

CREATE INDEX IF NOT EXISTS idx_audit_records_session_id ON audit_records(session_id);
CREATE INDEX IF NOT EXISTS idx_audit_records_recorded_at ON audit_records(recorded_at);
