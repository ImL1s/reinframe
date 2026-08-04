package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// SourceRecordIdentitySchemaVersion is the closed version for Codex record identity (#118).
const SourceRecordIdentitySchemaVersion = "reinframe.codex_source_record.v1"

// CodexEventIDSource is the stable source namespace for rollout/tail EventIDs.
const CodexEventIDSource = "codex_jsonl"

// SourceRecordIdentity identifies one physical JSONL record across offline replay and tail.
type SourceRecordIdentity struct {
	SchemaVersion string `json:"schema_version"`
	Source        string `json:"source"`
	SessionID     string `json:"session_id"`
	Generation    int    `json:"generation"`
	FileIdentity  string `json:"file_identity"`
	RecordOffset  int64  `json:"record_offset"`
	EventKind     string `json:"event_kind"`
}

// FormatEventID builds a collision-safe EventID.
// Shape: codex_jsonl|{session}|g{gen}|{fileIdentity}|off-{offset}|{kind}
func (id SourceRecordIdentity) FormatEventID() string {
	sess := sanitizeIDPart(id.SessionID)
	if sess == "" {
		sess = "unknown"
	}
	fi := sanitizeIDPart(id.FileIdentity)
	if fi == "" {
		fi = "file"
	}
	kind := sanitizeIDPart(id.EventKind)
	if kind == "" {
		kind = "other"
	}
	src := id.Source
	if src == "" {
		src = CodexEventIDSource
	}
	return fmt.Sprintf("%s|%s|g%d|%s|off-%d|%s", src, sess, id.Generation, fi, id.RecordOffset, kind)
}

// FileIdentityFromPath returns a bounded file identity for cursor/EventID.
func FileIdentityFromPath(path string) string {
	if path == "" {
		return "mem"
	}
	base := filepath.Base(path)
	h := sha256.Sum256([]byte(filepath.Clean(path)))
	return sanitizeIDPart(base) + "-" + hex.EncodeToString(h[:4])
}

// FileFingerprint is a bounded rotation detector beyond size alone.
type FileFingerprint struct {
	Size    int64  `json:"size"`
	ModNano int64  `json:"mod_nano"`
	Inode   uint64 `json:"inode,omitempty"`
}

// RotationDetected reports replacement/rotation when fingerprint diverges from cursor.
func RotationDetected(curOffset int64, prev, now FileFingerprint) bool {
	if now.Size < curOffset {
		return true
	}
	if prev.Inode != 0 && now.Inode != 0 && prev.Inode != now.Inode {
		return true
	}
	if prev.Size > 0 && now.Size < prev.Size && curOffset > now.Size {
		return true
	}
	return false
}

func sanitizeIDPart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}
