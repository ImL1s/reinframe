package codexruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/config"
)

var (
	ErrBinaryNotFound       = errors.New("codex binary not found")
	ErrBinaryHashMismatch   = errors.New("codex binary sha256 hash mismatch")
	ErrBinaryExecution      = errors.New("codex binary version check failed")
	ErrExecutableEmpty      = errors.New("codex executable path is empty")
	ErrIllegalShellChars    = errors.New("codex executable contains illegal shell characters")
)

// ResolvedBinary holds verified metadata for the resolved Codex executable.
type ResolvedBinary struct {
	Path       string    `json:"path"`
	SHA256     string    `json:"sha256"`
	Version    string    `json:"version"`
	ResolvedAt time.Time `json:"resolved_at"`
}

// ResolveBinary verifies and resolves the Codex executable from configuration.
// It checks file existence, evaluates symlinks, verifies optional SHA-256 integrity,
// and invokes `codex --version` safely (no shell interpolation).
func ResolveBinary(ctx context.Context, cfg config.CodexRuntimeConfig) (*ResolvedBinary, error) {
	execName := cfg.NormalizeExecutable()
	if execName == "" {
		return nil, ErrExecutableEmpty
	}
	if strings.ContainsAny(execName, "&|;$`\n\r><()\"'") {
		return nil, ErrIllegalShellChars
	}

	// 1. Resolve path
	resolvedPath, err := exec.LookPath(execName)
	if err != nil {
		return nil, fmt.Errorf("%w: %q (%v)", ErrBinaryNotFound, execName, err)
	}

	absPath, err := filepath.Abs(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for %q: %w", resolvedPath, err)
	}

	canonicalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		canonicalPath = absPath
	}

	// 2. Compute SHA-256
	fileHash, err := computeFileSHA256(canonicalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to hash binary %q: %w", canonicalPath, err)
	}

	// 3. Verify SHA-256 if pinned in config
	if cfg.BinarySHA256 != "" {
		expectedHash := strings.ToLower(strings.TrimSpace(cfg.BinarySHA256))
		if strings.ToLower(fileHash) != expectedHash {
			return nil, fmt.Errorf("%w: expected %s, got %s", ErrBinaryHashMismatch, expectedHash, fileHash)
		}
	}

	// 4. Probe version safely (argv only, no shell execution)
	timeout := 3 * time.Second
	if cfg.StatusCheckTimeoutMS > 0 {
		timeout = time.Duration(cfg.StatusCheckTimeoutMS) * time.Millisecond
	}

	vCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(vCtx, canonicalPath, "--version")
	outBytes, err := cmd.Output()
	if err != nil {
		// Even if version command fails (e.g. older mock binary), we report execution error safely
		return nil, fmt.Errorf("%w: %v", ErrBinaryExecution, err)
	}

	versionStr := sanitizeVersionString(string(outBytes))

	return &ResolvedBinary{
		Path:       canonicalPath,
		SHA256:     fileHash,
		Version:    versionStr,
		ResolvedAt: time.Now().UTC(),
	}, nil
}

func computeFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func sanitizeVersionString(raw string) string {
	lines := strings.Split(raw, "\n")
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			var b strings.Builder
			for _, r := range trimmed {
				if r >= 32 && r < 127 {
					b.WriteRune(r)
				}
			}
			out := b.String()
			if len(out) > 64 {
				out = out[:64]
			}
			return out
		}
	}
	return "unknown"
}
