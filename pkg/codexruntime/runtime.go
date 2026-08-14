package codexruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ImL1s/reinframe/pkg/config"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

var (
	ErrRuntimeDisabled = errors.New("codex runtime is disabled in configuration")
)

// RuntimeService coordinates binary resolution, status projection, cache partitioning, and runtime lifecycle.
type RuntimeService struct {
	mu               sync.RWMutex
	cfg              config.CodexRuntimeConfig
	prober           StatusProber
	binary           *ResolvedBinary
	partitionManager *CachePartitionManager
}

// NewRuntimeService creates a new RuntimeService.
func NewRuntimeService(cfg config.CodexRuntimeConfig, prober StatusProber) *RuntimeService {
	if prober == nil {
		prober = NewCLIStatusProber()
	}
	return &RuntimeService{
		cfg:              cfg,
		prober:           prober,
		partitionManager: NewCachePartitionManager(),
	}
}

// Config returns a copy of the active configuration.
func (s *RuntimeService) Config() config.CodexRuntimeConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// PartitionManager returns the underlying CachePartitionManager.
func (s *RuntimeService) PartitionManager() *CachePartitionManager {
	return s.partitionManager
}

// SetBinary sets or overrides the resolved binary (useful for tests).
func (s *RuntimeService) SetBinary(bin *ResolvedBinary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binary = bin
}

// EnsureReady performs preflight binary resolution and auth status probing before any turn or model selection.
// It strictly enforces fail-closed semantics:
//   - RequiredAuth mismatch (e.g. configured ChatGPT subscription vs API key) -> fails closed with error.
//   - Unauthenticated -> fails closed with ErrOperatorRequired (no silent API fallback).
//   - Expired -> fails closed with ErrSessionExpired (stops turn execution).
func (s *RuntimeService) EnsureReady(ctx context.Context, scope []string) (protocol.RuntimeAuthSnapshot, error) {
	s.mu.Lock()
	cfg := s.cfg
	prober := s.prober
	bin := s.binary
	s.mu.Unlock()

	if !cfg.Enabled {
		return protocol.RuntimeAuthSnapshot{}, ErrRuntimeDisabled
	}

	// 1. Resolve binary if not already cached
	if bin == nil {
		resolved, err := ResolveBinary(ctx, cfg)
		if err != nil {
			return protocol.RuntimeAuthSnapshot{}, fmt.Errorf("preflight binary check failed: %w", err)
		}
		s.mu.Lock()
		s.binary = resolved
		bin = resolved
		s.mu.Unlock()
	}

	// 2. Probe auth status (never reads credential files directly)
	snap, err := prober.ProbeAuthStatus(ctx, cfg, bin, scope)
	if err != nil {
		return protocol.RuntimeAuthSnapshot{}, fmt.Errorf("preflight auth probe failed: %w", err)
	}

	// 3. Update cache partition tracking
	_, _, _ = s.partitionManager.UpdateSnapshot(snap)

	// 4. Validate auth mode against configuration requirement
	reqAuth := cfg.NormalizeRequiredAuth()
	if string(snap.Mode) != reqAuth {
		return snap, fmt.Errorf("%w: config requires %q, but codex runtime reports %q",
			ErrAuthModeMismatchCfg, reqAuth, snap.Mode)
	}

	// 5. Enforce state lifecycle
	switch snap.State {
	case protocol.RuntimeAuthStateAuthenticated:
		return snap, nil
	case protocol.RuntimeAuthStateUnauthenticated:
		return snap, ErrOperatorRequired
	case protocol.RuntimeAuthStateExpired:
		return snap, ErrSessionExpired
	case protocol.RuntimeAuthStateUnavailable:
		return snap, ErrRuntimeUnavailable
	default:
		return snap, fmt.Errorf("unexpected runtime auth state: %s", snap.State)
	}
}

// CurrentPartitionKey returns active cache key partition.
func (s *RuntimeService) CurrentPartitionKey() string {
	return s.partitionManager.CurrentPartitionKey()
}

// InvalidateSession explicitly invalidates the cache partition (e.g. after logout, expiry, or user switch).
func (s *RuntimeService) InvalidateSession(reason InvalidationReason) {
	s.partitionManager.Invalidate(reason)
}
