package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// uuidInName matches a standard UUID substring in rollout filenames.
var uuidInName = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// CodexSessionCandidate is a discovered rollout file under a sessions root.
type CodexSessionCandidate struct {
	Path      string    `json:"path"`
	ModTime   time.Time `json:"mod_time"`
	Size      int64     `json:"size"`
	SessionID string    `json:"session_id,omitempty"` // best-effort from filename
}

// DiscoverCodexRollouts lists rollout-*.jsonl under root (typically ~/.codex/sessions).
// maxAge zero means no age filter. Does not select a session automatically when multiple match.
func DiscoverCodexRollouts(root string, maxAge time.Duration) ([]CodexSessionCandidate, error) {
	if root == "" {
		return nil, fmt.Errorf("codex discover: root required")
	}
	var out []CodexSessionCandidate
	cutoff := time.Time{}
	if maxAge > 0 {
		cutoff = time.Now().Add(-maxAge)
	}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := d.Name()
		if !strings.HasPrefix(base, "rollout-") || !strings.HasSuffix(base, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if !cutoff.IsZero() && info.ModTime().Before(cutoff) {
			return nil
		}
		c := CodexSessionCandidate{
			Path:    path,
			ModTime: info.ModTime().UTC(),
			Size:    info.Size(),
		}
		// Prefer full UUID from filename (not the last hyphen fragment of a UUID).
		if m := uuidInName.FindString(base); m != "" {
			c.SessionID = strings.ToLower(m)
		}
		out = append(out, c)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out, nil
}

// SelectCodexRollout requires an explicit path or unique candidate; never picks by recency alone when n>1.
func SelectCodexRollout(cands []CodexSessionCandidate, explicitPath string) (CodexSessionCandidate, error) {
	if explicitPath != "" {
		for _, c := range cands {
			if c.Path == explicitPath {
				return c, nil
			}
		}
		// allow explicit path even if not in list
		st, err := os.Stat(explicitPath)
		if err != nil {
			return CodexSessionCandidate{}, err
		}
		return CodexSessionCandidate{Path: explicitPath, ModTime: st.ModTime().UTC(), Size: st.Size()}, nil
	}
	if len(cands) == 0 {
		return CodexSessionCandidate{}, fmt.Errorf("codex: no rollout candidates; pass explicit path")
	}
	if len(cands) > 1 {
		return CodexSessionCandidate{}, fmt.Errorf("codex: %d candidates; pass explicit path (refuse auto-pick by recency)", len(cands))
	}
	return cands[0], nil
}

// CodexCapabilityManifest is capability-honest control surface for a pinned integration.
type CodexCapabilityManifest struct {
	ObserveEvents      bool   `json:"observe_events"`
	InjectMessage      bool   `json:"inject_message"`
	PreToolGate        bool   `json:"pre_tool_gate"`
	PauseCancelResume  bool   `json:"pause_cancel_resume"`
	CheckpointRollback bool   `json:"checkpoint_rollback"`
	ExplicitAck        bool   `json:"explicit_ack"`
	NegotiatedLevel    int    `json:"negotiated_level"` // 0 observe
	HonestyNote        string `json:"honesty_note"`
	IntegrationVersion string `json:"integration_version"`
}

// DefaultCodexCapabilityManifest reflects only proven surfaces on main (JSONL observe/tail).
func DefaultCodexCapabilityManifest() CodexCapabilityManifest {
	return CodexCapabilityManifest{
		ObserveEvents:      true,
		InjectMessage:      false,
		PreToolGate:        false,
		PauseCancelResume:  false,
		CheckpointRollback: false,
		ExplicitAck:        false,
		NegotiatedLevel:    0,
		HonestyNote:        "observe-only via rollout JSONL; not process attach or bidirectional control",
		IntegrationVersion: "codex-jsonl-observe-v1",
	}
}
