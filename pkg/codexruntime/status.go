package codexruntime

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/config"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

var (
	ErrProbeTimeout        = errors.New("auth status probe timed out")
	ErrProbeExecution      = errors.New("auth status probe execution failed")
	ErrOperatorRequired    = errors.New("operator login required: codex is unauthenticated")
	ErrSessionExpired      = errors.New("codex authentication has expired: turn halted")
	ErrRuntimeUnavailable  = errors.New("codex runtime is unavailable")
	ErrAuthModeMismatchCfg = errors.New("codex runtime auth mode mismatch against required_auth configuration")
)

// StatusProber abstracts auth status projection from the child runtime.
type StatusProber interface {
	ProbeAuthStatus(ctx context.Context, cfg config.CodexRuntimeConfig, bin *ResolvedBinary, scope []string) (protocol.RuntimeAuthSnapshot, error)
}

// CLIStatusProber queries the Codex CLI runtime for non-secret auth status.
// SECURITY GUARANTEE: This prober NEVER reads ~/.codex/auth.json or any credential store files.
type CLIStatusProber struct{}

// NewCLIStatusProber creates a new CLIStatusProber.
func NewCLIStatusProber() *CLIStatusProber {
	return &CLIStatusProber{}
}

// ProbeAuthStatus executes a safe, read-only status command and projects bounded hashes.
func (p *CLIStatusProber) ProbeAuthStatus(
	ctx context.Context,
	cfg config.CodexRuntimeConfig,
	bin *ResolvedBinary,
	scope []string,
) (protocol.RuntimeAuthSnapshot, error) {
	if bin == nil || bin.Path == "" {
		return protocol.RuntimeAuthSnapshot{}, ErrRuntimeUnavailable
	}

	timeout := 3 * time.Second
	if cfg.StatusCheckTimeoutMS > 0 {
		timeout = time.Duration(cfg.StatusCheckTimeoutMS) * time.Millisecond
	}

	pCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Safe argv execution: call status subcommands (no shell interpolation)
	cmd := exec.CommandContext(pCtx, bin.Path, "login", "status")
	outBytes, err := cmd.CombinedOutput()
	rawOutput := string(outBytes)

	var (
		mode           protocol.RuntimeAuthMode
		state          protocol.RuntimeAuthState
		generationSeed string
	)

	if err != nil {
		lowerOut := strings.ToLower(rawOutput)
		if strings.Contains(lowerOut, "not logged in") || strings.Contains(lowerOut, "unauthenticated") || strings.Contains(lowerOut, "no active session") {
			state = protocol.RuntimeAuthStateUnauthenticated
			mode = protocol.RuntimeAuthModeChatGPTSubscription
			generationSeed = "unauth"
		} else if strings.Contains(lowerOut, "expired") || strings.Contains(lowerOut, "token expired") {
			state = protocol.RuntimeAuthStateExpired
			mode = protocol.RuntimeAuthModeChatGPTSubscription
			generationSeed = "expired"
		} else {
			state = protocol.RuntimeAuthStateUnavailable
			generationSeed = "unavailable"
		}
	} else {
		lowerOut := strings.ToLower(rawOutput)
		if strings.Contains(lowerOut, "logged in") || strings.Contains(lowerOut, "authenticated") {
			state = protocol.RuntimeAuthStateAuthenticated
			if strings.Contains(lowerOut, "chatgpt") || strings.Contains(lowerOut, "subscription") || strings.Contains(lowerOut, "oauth") {
				mode = protocol.RuntimeAuthModeChatGPTSubscription
			} else if strings.Contains(lowerOut, "api_key") || strings.Contains(lowerOut, "key") {
				mode = protocol.RuntimeAuthModeAPIKey
			} else {
				mode = protocol.RuntimeAuthModeChatGPTSubscription
			}
			shaPrefix := bin.SHA256
			if len(shaPrefix) > 16 {
				shaPrefix = shaPrefix[:16]
			}
			// Bounded generation seed derived from profile & binary hash, never secret tokens
			generationSeed = fmt.Sprintf("auth:%s:%s", shaPrefix, bin.Version)
		} else {
			state = protocol.RuntimeAuthStateUnauthenticated
			mode = protocol.RuntimeAuthModeChatGPTSubscription
			generationSeed = "unauth"
		}
	}

	scopeHash := protocol.ComputeScopeHash(scope)
	authGenHash := protocol.ComputeAuthGenerationHash(cfg.NormalizeProfile(), generationSeed)

	return protocol.NewRuntimeAuthSnapshot(
		protocol.CredentialOwner(cfg.NormalizeCredentialOwner()),
		mode,
		state,
		cfg.NormalizeProfile(),
		bin.Version,
		scopeHash,
		authGenHash,
		time.Now().UTC(),
		nil,
	)
}

// FakeStatusProber is an in-memory mock prober for testing and shadow environments.
type FakeStatusProber struct {
	mu           sync.Mutex
	Snapshot     protocol.RuntimeAuthSnapshot
	ProbeErr     error
	CallCount    int
	CustomProber func(ctx context.Context, cfg config.CodexRuntimeConfig, bin *ResolvedBinary, scope []string) (protocol.RuntimeAuthSnapshot, error)
}

// NewFakeStatusProber creates a mock status prober with initial snapshot.
func NewFakeStatusProber(initial protocol.RuntimeAuthSnapshot) *FakeStatusProber {
	return &FakeStatusProber{
		Snapshot: initial,
	}
}

// SetSnapshot updates the mock snapshot returned by the fake prober.
func (f *FakeStatusProber) SetSnapshot(snap protocol.RuntimeAuthSnapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Snapshot = snap
}

// SetError sets the error to return on next probe calls.
func (f *FakeStatusProber) SetError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ProbeErr = err
}

// Calls returns the invocation count.
func (f *FakeStatusProber) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.CallCount
}

// ProbeAuthStatus returns configured mock snapshot or executes custom callback.
func (f *FakeStatusProber) ProbeAuthStatus(
	ctx context.Context,
	cfg config.CodexRuntimeConfig,
	bin *ResolvedBinary,
	scope []string,
) (protocol.RuntimeAuthSnapshot, error) {
	f.mu.Lock()
	f.CallCount++
	probeErr := f.ProbeErr
	snap := f.Snapshot
	custom := f.CustomProber
	f.mu.Unlock()

	if probeErr != nil {
		return protocol.RuntimeAuthSnapshot{}, probeErr
	}

	if custom != nil {
		return custom(ctx, cfg, bin, scope)
	}

	// Ensure scope and auth hashes are populated if not explicitly set
	if snap.ScopeHash == "" {
		snap.ScopeHash = protocol.ComputeScopeHash(scope)
	}
	if snap.AuthGenerationHash == "" {
		snap.AuthGenerationHash = protocol.ComputeAuthGenerationHash(cfg.NormalizeProfile(), "fake_gen_seed")
	}
	if snap.ObservedAt.IsZero() {
		snap.ObservedAt = time.Now().UTC()
	}
	if snap.SchemaVersion == "" {
		snap.SchemaVersion = protocol.RuntimeAuthSnapshotSchemaVersion
	}
	if snap.CredentialOwner == "" {
		snap.CredentialOwner = protocol.CredentialOwner(cfg.NormalizeCredentialOwner())
	}
	if snap.RuntimeProfile == "" {
		snap.RuntimeProfile = cfg.NormalizeProfile()
	}

	return snap, nil
}
