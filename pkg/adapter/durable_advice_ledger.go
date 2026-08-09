package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// DurableAdviceLedger is an append-only, user-private JSONL ledger of advice
// delivery transitions (#108). Source advice channel history is never rewritten.
//
// Schema: reinframe.advice_delivery.v1
type DurableAdviceLedger struct {
	Path string

	mu     sync.Mutex
	seen   map[string]struct{} // bound dedupe keys that suppress redelivery
	cursor int64               // bytes written (restart: file size)
}

// DeliveryTransition is one closed, versioned append-only record.
type DeliveryTransition struct {
	Schema         string    `json:"schema"` // reinframe.advice_delivery.v1
	At             time.Time `json:"at"`
	InterventionID string    `json:"intervention_id"`
	SessionID      string    `json:"session_id"`
	FromState      string    `json:"from_state,omitempty"`
	ToState        string    `json:"to_state"`
	AckLayer       string    `json:"ack_layer,omitempty"`
	AckStatus      string    `json:"ack_status,omitempty"`
	HostFamily     string    `json:"host_family,omitempty"`
	HostVersion    string    `json:"host_version,omitempty"`
	Profile        string    `json:"profile,omitempty"`
	SafeBoundary   string    `json:"safe_boundary,omitempty"`
	TargetSession  string    `json:"target_session_id,omitempty"`
	CapsDigest     string    `json:"caps_digest,omitempty"`
	Message        string    `json:"message,omitempty"`
	SourceKind     string    `json:"source_kind,omitempty"`
	SourceEventID  string    `json:"source_event_id,omitempty"`
	CorrelationID  string    `json:"correlation_id,omitempty"`
	Fingerprint    string    `json:"fingerprint,omitempty"`
}

// Recovery / integrity errors (#208).
var (
	ErrLedgerRecoveryIncomplete = errors.New("durable advice ledger: suppress recovery incomplete")
	ErrLedgerCorrupt            = errors.New("durable advice ledger: corrupt transition record")
	ErrLedgerMarkerInvalid      = errors.New("durable advice ledger: invalid suppress marker")
	ErrLedgerPathUnsafe         = errors.New("durable advice ledger: unsafe path")
)

const (
	adviceLedgerSchemaV1     = "reinframe.advice_delivery.v1"
	maxSuppressMarkerBytes   = 4096
	suppressMarkerKeySuffix  = ".key"
	suppressMarkerTempSuffix = ".key.tmp"
)

// OpenDurableAdviceLedger loads existing IDs from path (if present) for restart dedupe.
// Rejects symlink/reparse paths (#200). Fail-closed on incomplete suppress recovery (#208).
func OpenDurableAdviceLedger(path string) (*DurableAdviceLedger, error) {
	if path == "" {
		return nil, fmt.Errorf("durable advice ledger: path required")
	}
	if err := rejectLedgerSymlink(path); err != nil {
		return nil, err
	}
	l := &DurableAdviceLedger{
		Path: path,
		seen: make(map[string]struct{}),
	}
	b, err := os.ReadFile(path)
	if err != nil {
		// Missing JSONL is fine (first run). Path-as-directory (poisoned write path)
		// still loads sidecar suppress markers so restart does not silently redeliver.
		if !os.IsNotExist(err) {
			if fi, se := os.Lstat(path); se != nil || !fi.IsDir() {
				return nil, err
			}
		}
	} else {
		l.cursor = int64(len(b))
		if err := l.ingestJSONL(b); err != nil {
			return nil, err
		}
	}
	// Fail-closed: incomplete suppress recovery must not yield a usable ledger.
	if err := l.loadAmbiguousMarkersUnlocked(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *DurableAdviceLedger) ingestJSONL(b []byte) error {
	// Append protocol always writes JSON + '\n'. A non-empty file without a
	// trailing newline is not safe to reopen for O_APPEND (next write glues lines).
	// Fail closed even when the last record would otherwise parse.
	if len(b) > 0 && b[len(b)-1] != '\n' {
		return fmt.Errorf("%w: missing trailing newline (torn or incomplete append protocol)", ErrLedgerCorrupt)
	}
	for _, line := range splitLines(b) {
		if len(line) == 0 {
			continue
		}
		var tr DeliveryTransition
		if err := json.Unmarshal(line, &tr); err != nil {
			return fmt.Errorf("%w: malformed transition line", ErrLedgerCorrupt)
		}
		if err := validateTransitionSemantic(tr); err != nil {
			return err
		}
		// Only states that prove host accepted transport (or terminal no-retry) suppress redelivery.
		if isSuppressState(DeliveryState(tr.ToState)) {
			l.seen[dedupeKey(tr.InterventionID, tr.SessionID, tr.HostFamily, tr.Fingerprint)] = struct{}{}
		}
	}
	return nil
}

// validateTransitionSemantic rejects parseable-but-invalid records that would
// silently erase suppress knowledge (missing/unknown schema/state, empty identity).
// Schema must be exactly reinframe.advice_delivery.v1 — empty/omitted is corrupt on ingest.
func validateTransitionSemantic(tr DeliveryTransition) error {
	if tr.Schema != adviceLedgerSchemaV1 {
		return fmt.Errorf("%w: missing or unknown schema %q", ErrLedgerCorrupt, tr.Schema)
	}
	if tr.ToState == "" {
		return fmt.Errorf("%w: empty to_state", ErrLedgerCorrupt)
	}
	if !isKnownDeliveryState(DeliveryState(tr.ToState)) {
		return fmt.Errorf("%w: unknown to_state %q", ErrLedgerCorrupt, tr.ToState)
	}
	if tr.FromState != "" && !isKnownDeliveryState(DeliveryState(tr.FromState)) {
		return fmt.Errorf("%w: unknown from_state %q", ErrLedgerCorrupt, tr.FromState)
	}
	if tr.InterventionID == "" {
		return fmt.Errorf("%w: empty intervention_id", ErrLedgerCorrupt)
	}
	// Suppress transitions must carry session-bound identity so restart keys match writes.
	if isSuppressState(DeliveryState(tr.ToState)) && tr.SessionID == "" {
		return fmt.Errorf("%w: suppress transition missing session_id", ErrLedgerCorrupt)
	}
	return nil
}

func isKnownDeliveryState(st DeliveryState) bool {
	switch st {
	case StatePending, StateDelivering, StateTransportAccepted, StateSessionVisible,
		StateExplicitACK, StateBehavioralACK, StateAcked, StateRejected, StateTimedOut,
		StateExpired, StateSuppressed, StateFailed, StateUnsupported, StateAmbiguous:
		return true
	default:
		return false
	}
}

// suppressSidecarDir is a sibling of the JSONL path used when Append cannot commit
// after host acceptance.
func (l *DurableAdviceLedger) suppressSidecarDir() string {
	return l.Path + ".suppress"
}

func suppressMarkerFilename(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:16]) + suppressMarkerKeySuffix
}

func suppressMarkerTempName(key string) string {
	return suppressMarkerFilename(key) + ".tmp"
}

// parseAndValidateMarkerBody validates marker bytes and returns the bound key
// in current JSON-array encoding. Does not migrate legacy pipe markers.
func parseAndValidateMarkerBody(name, body string) (string, error) {
	if len(body) == 0 || len(body) > maxSuppressMarkerBytes {
		return "", fmt.Errorf("%w: marker size", ErrLedgerMarkerInvalid)
	}
	if !utf8.ValidString(body) {
		return "", fmt.Errorf("%w: marker encoding", ErrLedgerMarkerInvalid)
	}
	key := strings.TrimSpace(body)
	if key == "" || strings.ContainsRune(key, 0) {
		return "", fmt.Errorf("%w: empty or null key", ErrLedgerMarkerInvalid)
	}
	// Canonical name: hash.key or hash.key.tmp
	base := name
	if strings.HasSuffix(name, ".tmp") {
		base = strings.TrimSuffix(name, ".tmp")
	}
	if base != suppressMarkerFilename(key) {
		return "", fmt.Errorf("%w: marker name not canonical for key", ErrLedgerMarkerInvalid)
	}
	if err := validateDedupeKeyEncoding(key); err != nil {
		return "", err
	}
	return key, nil
}

// tryParseLegacyPipeKey accepts only the pre-#212 body form intervention|session|host|fingerprint
// when there are exactly three unescaped pipe separators (four components). Fingerprints that
// themselves contain '|' yield Count != 3 and must fail closed for operator reconciliation.
func tryParseLegacyPipeKey(raw string) (parts [4]string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Count(raw, "|") != 3 {
		return parts, false
	}
	p := strings.SplitN(raw, "|", 4)
	if len(p) != 4 || p[0] == "" {
		return parts, false
	}
	copy(parts[:], p)
	return parts, true
}

// migrateLegacyPipeMarker rewrites a validated old-format marker into JSON-array form.
// Caller must have verified name == hash(legacy body). Returns the new canonical key.
func migrateLegacyPipeMarker(dir, legacyFull, legacyBody string, parts [4]string) (string, error) {
	newKey := dedupeKey(parts[0], parts[1], parts[2], parts[3])
	final := filepath.Join(dir, suppressMarkerFilename(newKey))
	tmp := filepath.Join(dir, suppressMarkerTempName(newKey))
	if err := os.WriteFile(tmp, []byte(newKey+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("%w: write migrated marker: %v", ErrLedgerRecoveryIncomplete, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("%w: promote migrated marker: %v", ErrLedgerRecoveryIncomplete, err)
	}
	// Remove legacy only after new marker is durable.
	if err := os.Remove(legacyFull); err != nil && !os.IsNotExist(err) {
		// New marker exists; legacy left behind is redundant but open can proceed.
		// Prefer fail-closed only if remove fails and paths collide — rare.
		_ = err
	}
	return newKey, nil
}

// loadMarkerFile validates a .key (or promotes/migrates) and returns the bound key.
func loadMarkerFile(dir, name, full string, body []byte) (string, error) {
	// Prefer current JSON-array encoding.
	if key, err := parseAndValidateMarkerBody(name, string(body)); err == nil {
		return key, nil
	}
	// Legacy pipe-delimited body from #207–#211 (exactly three separators).
	raw := strings.TrimSpace(string(body))
	base := name
	if strings.HasSuffix(name, ".tmp") {
		base = strings.TrimSuffix(name, ".tmp")
	}
	// Legacy filename is hash of the raw pipe body.
	if base != suppressMarkerFilename(raw) {
		return "", fmt.Errorf("%w: marker not current encoding and not migratable legacy", ErrLedgerMarkerInvalid)
	}
	parts, ok := tryParseLegacyPipeKey(raw)
	if !ok {
		// Ambiguous pipe count (e.g. fingerprint contained '|') — operator must reconcile.
		return "", fmt.Errorf("%w: legacy marker not uniquely pipe-delimited (operator reconcile)", ErrLedgerMarkerInvalid)
	}
	return migrateLegacyPipeMarker(dir, full, raw, parts)
}

// loadAmbiguousMarkersUnlocked reads sidecar suppress keys into seen.
// Crash-window .tmp markers are validated and promoted to .key, or open fails closed.
func (l *DurableAdviceLedger) loadAmbiguousMarkersUnlocked() error {
	dir := l.suppressSidecarDir()
	if err := rejectLedgerSymlink(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrLedgerPathUnsafe, err)
	}
	if fi, err := os.Lstat(dir); err == nil {
		if !fi.IsDir() {
			return fmt.Errorf("%w: suppress path is not a directory", ErrLedgerRecoveryIncomplete)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("%w: suppress path: %v", ErrLedgerRecoveryIncomplete, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("%w: suppress dir: %v", ErrLedgerRecoveryIncomplete, err)
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)

		// Crash-window temp markers: never ignore — promote or fail closed.
		if strings.HasSuffix(name, ".tmp") {
			if e.IsDir() {
				return fmt.Errorf("%w: temp marker %s is a directory", ErrLedgerRecoveryIncomplete, name)
			}
			if !strings.HasSuffix(name, suppressMarkerTempSuffix) {
				return fmt.Errorf("%w: unexpected temp name %s", ErrLedgerRecoveryIncomplete, name)
			}
			if err := rejectLedgerSymlink(full); err != nil {
				return fmt.Errorf("%w: %v", ErrLedgerPathUnsafe, err)
			}
			b, rerr := os.ReadFile(full)
			if rerr != nil {
				return fmt.Errorf("%w: temp marker %s: %v", ErrLedgerRecoveryIncomplete, name, rerr)
			}
			// Temp may be current JSON or (rare) legacy — loadMarkerFile handles both.
			// If already current encoding, promote via rename to final name.
			if key, err := parseAndValidateMarkerBody(name, string(b)); err == nil {
				final := filepath.Join(dir, suppressMarkerFilename(key))
				if err := os.Rename(full, final); err != nil {
					return fmt.Errorf("%w: promote temp marker: %v", ErrLedgerRecoveryIncomplete, err)
				}
				l.seen[key] = struct{}{}
				continue
			}
			key, perr := loadMarkerFile(dir, name, full, b)
			if perr != nil {
				return fmt.Errorf("%w: temp marker %s: %v", ErrLedgerRecoveryIncomplete, name, perr)
			}
			// loadMarkerFile may have written final .key; ensure temp is gone.
			_ = os.Remove(full)
			l.seen[key] = struct{}{}
			continue
		}

		if e.IsDir() {
			if strings.HasSuffix(name, suppressMarkerKeySuffix) {
				return fmt.Errorf("%w: marker %s is a directory", ErrLedgerRecoveryIncomplete, name)
			}
			continue
		}
		if !strings.HasSuffix(name, suppressMarkerKeySuffix) {
			continue
		}
		if err := rejectLedgerSymlink(full); err != nil {
			return fmt.Errorf("%w: %v", ErrLedgerPathUnsafe, err)
		}
		b, rerr := os.ReadFile(full)
		if rerr != nil {
			return fmt.Errorf("%w: marker %s: %v", ErrLedgerRecoveryIncomplete, name, rerr)
		}
		key, perr := loadMarkerFile(dir, name, full, b)
		if perr != nil {
			return perr
		}
		l.seen[key] = struct{}{}
	}
	return nil
}

// MarkAmbiguousSuppress records a bound suppress key when the JSONL path cannot be
// written after the host may have accepted delivery. Survives process restart via sidecar.
func (l *DurableAdviceLedger) MarkAmbiguousSuppress(interventionID, sessionID, hostFamily, fingerprint string) error {
	if l == nil {
		return fmt.Errorf("nil ledger")
	}
	if interventionID == "" {
		return fmt.Errorf("intervention id required")
	}
	key := dedupeKey(interventionID, sessionID, hostFamily, fingerprint)
	l.mu.Lock()
	defer l.mu.Unlock()
	dir := l.suppressSidecarDir()
	if err := rejectLedgerSymlink(dir); err != nil {
		return fmt.Errorf("%w: %v", ErrLedgerPathUnsafe, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	final := filepath.Join(dir, suppressMarkerFilename(key))
	tmp := filepath.Join(dir, suppressMarkerTempName(key))
	if err := os.WriteFile(tmp, []byte(key+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// Only mark in-memory after durable rename succeeds.
	l.seen[key] = struct{}{}
	return nil
}

// AlreadyDelivered reports whether InterventionID has a durable delivery transition.
// Prefer AlreadyDeliveredKey for session/host/action-bound dedupe (#200).
func (l *DurableAdviceLedger) AlreadyDelivered(interventionID string) bool {
	return l.AlreadyDeliveredKey(interventionID, "", "", "")
}

// AlreadyDeliveredKey checks a bound dedupe key (intervention|session|host|fingerprint).
func (l *DurableAdviceLedger) AlreadyDeliveredKey(interventionID, sessionID, hostFamily, fingerprint string) bool {
	if l == nil || interventionID == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.seen[dedupeKey(interventionID, sessionID, hostFamily, fingerprint)]
	return ok
}

// dedupeKey encodes bound identity without ambiguous delimiters (JSON array).
// Components may contain '|' without breaking recovery validation.
func dedupeKey(interventionID, sessionID, hostFamily, fingerprint string) string {
	b, err := json.Marshal([4]string{interventionID, sessionID, hostFamily, fingerprint})
	if err != nil {
		// Should never fail for strings; fall back to escaped form.
		return fmt.Sprintf("%q|%q|%q|%q", interventionID, sessionID, hostFamily, fingerprint)
	}
	return string(b)
}

func validateDedupeKeyEncoding(key string) error {
	var parts [4]string
	if err := json.Unmarshal([]byte(key), &parts); err != nil {
		return fmt.Errorf("%w: key encoding", ErrLedgerMarkerInvalid)
	}
	if parts[0] == "" {
		return fmt.Errorf("%w: empty intervention in key", ErrLedgerMarkerInvalid)
	}
	return nil
}

func rejectLedgerSymlink(path string) error {
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("durable advice ledger: path is a symlink")
		}
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("durable advice ledger: parent path is a symlink")
		}
	}
	return nil
}

// Append records a transition and marks the InterventionID as seen for dedupe.
func (l *DurableAdviceLedger) Append(tr DeliveryTransition) error {
	if l == nil {
		return fmt.Errorf("nil ledger")
	}
	if tr.Schema == "" {
		tr.Schema = adviceLedgerSchemaV1
	}
	if tr.At.IsZero() {
		tr.At = time.Now().UTC()
	}
	if err := validateTransitionSemantic(tr); err != nil {
		return err
	}
	line, err := json.Marshal(tr)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil && !os.IsExist(err) {
		if filepath.Dir(l.Path) != "." {
			return err
		}
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	n, err := f.Write(line)
	if err != nil {
		return err
	}
	l.cursor += int64(n)
	if isSuppressState(DeliveryState(tr.ToState)) {
		key := dedupeKey(tr.InterventionID, tr.SessionID, tr.HostFamily, tr.Fingerprint)
		l.seen[key] = struct{}{}
	}
	return nil
}

func isSuppressState(st DeliveryState) bool {
	switch st {
	case StateTransportAccepted, StateSessionVisible, StateExplicitACK, StateBehavioralACK,
		StateAcked, StateRejected, StateTimedOut, StateExpired, StateSuppressed,
		StateAmbiguous:
		return true
	default:
		return false
	}
}

// Cursor returns the durable byte offset (for tests / recovery diagnostics).
func (l *DurableAdviceLedger) Cursor() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cursor
}

// RecordResult appends a transition from a delivery result snapshot.
func (l *DurableAdviceLedger) RecordResult(from DeliveryState, sessionID string, res InterventionResult, to DeliveryState) error {
	return l.RecordResultWithSource(from, sessionID, res, to, "", "", "", "")
}

// RecordResultWithSource appends with source-bound ACK identity (#200).
func (l *DurableAdviceLedger) RecordResultWithSource(from DeliveryState, sessionID string, res InterventionResult, to DeliveryState, srcKind, srcEvent, corr, fingerprint string) error {
	return l.Append(DeliveryTransition{
		InterventionID: res.InterventionID,
		SessionID:      sessionID,
		FromState:      string(from),
		ToState:        string(to),
		AckLayer:       res.AckLayer,
		AckStatus:      res.AckStatus,
		HostFamily:     res.HostFamily,
		HostVersion:    res.HostVersion,
		Profile:        res.Profile,
		SafeBoundary:   res.SafeBoundary,
		TargetSession:  res.TargetSessionID,
		CapsDigest:     res.CapsDigest,
		Message:        boundRunes(res.Message, 240),
		SourceKind:     srcKind,
		SourceEventID:  srcEvent,
		CorrelationID:  corr,
		Fingerprint:    fingerprint,
	})
}

func splitLines(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
