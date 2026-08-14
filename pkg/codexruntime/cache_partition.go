package codexruntime

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

var (
	ErrUnauthenticatedPartition = errors.New("cannot create active cache partition for unauthenticated snapshot")
)

// InvalidationReason represents the reason why cache was partitioned or cleared.
type InvalidationReason string

const (
	ReasonInitialPartition     InvalidationReason = "initial_partition"
	ReasonAuthGenChanged       InvalidationReason = "auth_generation_changed"
	ReasonScopeChanged         InvalidationReason = "scope_changed"
	ReasonModeChanged          InvalidationReason = "mode_changed"
	ReasonProfileChanged       InvalidationReason = "profile_changed"
	ReasonUnauthenticatedState InvalidationReason = "auth_state_not_authenticated"
	ReasonManualInvalidation   InvalidationReason = "manual_invalidation"
)

// CachePartitionManager manages cache partition keys and invalidation triggers.
type CachePartitionManager struct {
	mu                 sync.RWMutex
	currentSnapshot    protocol.RuntimeAuthSnapshot
	currentKey         string
	invalidationCount  int
	lastReason         InvalidationReason
}

// NewCachePartitionManager creates a new CachePartitionManager.
func NewCachePartitionManager() *CachePartitionManager {
	return &CachePartitionManager{}
}

// CurrentPartitionKey returns the current cache partition key, or empty if invalid / unauthenticated.
func (m *CachePartitionManager) CurrentPartitionKey() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentKey
}

// CurrentSnapshot returns a copy of the currently tracked snapshot.
func (m *CachePartitionManager) CurrentSnapshot() (protocol.RuntimeAuthSnapshot, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.currentKey == "" {
		return protocol.RuntimeAuthSnapshot{}, false
	}
	return m.currentSnapshot, true
}

// InvalidationCount returns how many times cache was invalidated.
func (m *CachePartitionManager) InvalidationCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.invalidationCount
}

// LastInvalidationReason returns the last invalidation reason.
func (m *CachePartitionManager) LastInvalidationReason() InvalidationReason {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastReason
}

// UpdateSnapshot checks whether the new snapshot requires cache invalidation and updates partition key.
// Returns (invalidated bool, reason InvalidationReason, err error).
func (m *CachePartitionManager) UpdateSnapshot(newSnap protocol.RuntimeAuthSnapshot) (bool, InvalidationReason, error) {
	if err := newSnap.Validate(); err != nil {
		return false, "", fmt.Errorf("invalid snapshot for cache partition: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// If new snapshot is not authenticated, clear partition
	if !newSnap.IsAuthenticated() {
		if m.currentKey != "" {
			m.currentKey = ""
			m.currentSnapshot = newSnap
			m.invalidationCount++
			m.lastReason = ReasonUnauthenticatedState
			return true, ReasonUnauthenticatedState, nil
		}
		m.currentSnapshot = newSnap
		return false, "", nil
	}

	newKey := newSnap.CacheKeyPartition()

	// If no prior partition existed
	if m.currentKey == "" {
		m.currentKey = newKey
		m.currentSnapshot = newSnap
		m.invalidationCount++
		m.lastReason = ReasonInitialPartition
		return true, ReasonInitialPartition, nil
	}

	// Check components for specific change reason
	var reason InvalidationReason
	if newSnap.AuthGenerationHash != m.currentSnapshot.AuthGenerationHash {
		reason = ReasonAuthGenChanged
	} else if newSnap.ScopeHash != m.currentSnapshot.ScopeHash {
		reason = ReasonScopeChanged
	} else if newSnap.Mode != m.currentSnapshot.Mode {
		reason = ReasonModeChanged
	} else if newSnap.RuntimeProfile != m.currentSnapshot.RuntimeProfile {
		reason = ReasonProfileChanged
	} else if newKey != m.currentKey {
		reason = ReasonAuthGenChanged
	}

	if reason != "" {
		m.currentKey = newKey
		m.currentSnapshot = newSnap
		m.invalidationCount++
		m.lastReason = reason
		return true, reason, nil
	}

	// Snapshot unchanged
	m.currentSnapshot = newSnap
	return false, "", nil
}

// Invalidate explicitly forces cache invalidation.
func (m *CachePartitionManager) Invalidate(reason InvalidationReason) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentKey = ""
	m.invalidationCount++
	if reason == "" {
		reason = ReasonManualInvalidation
	}
	m.lastReason = reason
}
