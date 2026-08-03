package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CodexTailCursor is a durable byte offset for near-live tail restart (#107).
type CodexTailCursor struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset"`
	// Generation bumps when truncation/rotation is detected.
	Generation int `json:"generation"`
}

// LoadCodexTailCursor reads cursor JSON from path; missing file → zero cursor.
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
		return CodexTailCursor{}, fmt.Errorf("codex cursor: %w", err)
	}
	return c, nil
}

// SaveCodexTailCursor writes cursor atomically (temp + rename).
func SaveCodexTailCursor(cursorPath string, c CodexTailCursor) error {
	if err := os.MkdirAll(filepath.Dir(cursorPath), 0o755); err != nil {
		return err
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

// ReconcileCursorAgainstFile adjusts cursor when file truncated (size < offset).
func ReconcileCursorAgainstFile(c CodexTailCursor, fileSize int64) CodexTailCursor {
	if fileSize < c.Offset {
		c.Offset = 0
		c.Generation++
	}
	return c
}
