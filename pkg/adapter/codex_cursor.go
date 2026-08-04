package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const codexCursorSchemaVersion = "reinframe.codex_tail_cursor.v1"

// CodexTailCursor is a durable byte offset for near-live tail restart (#107 / #118).
type CodexTailCursor struct {
	Path            string           `json:"path"`
	Offset          int64            `json:"offset"`
	Generation      int              `json:"generation"`
	SessionID       string           `json:"session_id,omitempty"`
	FileIdentity    string           `json:"file_identity,omitempty"`
	LastFingerprint *FileFingerprint `json:"last_fingerprint,omitempty"`
	SchemaVersion   string           `json:"schema_version,omitempty"`
}

// LoadCodexTailCursor reads cursor JSON; missing → zero cursor. Parse errors returned.
func LoadCodexTailCursor(cursorPath string) (CodexTailCursor, error) {
	b, err := os.ReadFile(cursorPath)
	if os.IsNotExist(err) {
		return CodexTailCursor{}, nil
	}
	if err != nil {
		return CodexTailCursor{}, err
	}
	var c CodexTailCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return CodexTailCursor{}, fmt.Errorf("codex cursor: parse error: %w", err)
	}
	return c, nil
}

// SaveCodexTailCursor writes cursor atomically (temp + rename), mode 0o600.
func SaveCodexTailCursor(cursorPath string, c CodexTailCursor) error {
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0o755); err != nil {
		return err
	}
	if c.SchemaVersion == "" {
		c.SchemaVersion = codexCursorSchemaVersion
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := cursorPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, cursorPath)
}

// ReconcileCursorAgainstFile adjusts cursor when truncated or rotated.
func ReconcileCursorAgainstFile(c CodexTailCursor, fileSize int64, fp *FileFingerprint) CodexTailCursor {
	prev := FileFingerprint{}
	if c.LastFingerprint != nil {
		prev = *c.LastFingerprint
	}
	now := FileFingerprint{Size: fileSize}
	if fp != nil {
		now = *fp
	}
	if RotationDetected(c.Offset, prev, now) {
		c.Offset = 0
		c.Generation++
	}
	c.LastFingerprint = &now
	return c
}
