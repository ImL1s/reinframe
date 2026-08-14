package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ModelCatalogSnapshotSchemaVersion is the closed schema version for model catalog snapshots.
const ModelCatalogSnapshotSchemaVersion = "reinframe.model_catalog_snapshot.v1"

// ModelSupportState represents the discrete lifecycle and qualification state of a model.
type ModelSupportState string

const (
	// ModelSupportStateDiscovered indicates the model was discovered in the active catalog/scope,
	// but has not been verified as selectable or qualified.
	ModelSupportStateDiscovered ModelSupportState = "discovered"

	// ModelSupportStateSelectable indicates the model is actively available and selectable for sessions/turns.
	ModelSupportStateSelectable ModelSupportState = "selectable"

	// ModelSupportStateCapabilityPinned indicates the model's capabilities and constraints are strictly bound and verified.
	ModelSupportStateCapabilityPinned ModelSupportState = "capability_pinned"

	// ModelSupportStateLiveQualified indicates the model has passed empirical qualification runs (smoke tests, zero substitution proof).
	ModelSupportStateLiveQualified ModelSupportState = "live_qualified"
)

// Valid reports whether the ModelSupportState is one of the closed enum values.
func (s ModelSupportState) Valid() bool {
	switch s {
	case ModelSupportStateDiscovered,
		ModelSupportStateSelectable,
		ModelSupportStateCapabilityPinned,
		ModelSupportStateLiveQualified:
		return true
	default:
		return false
	}
}

// IsSelectable returns true if the state allows active model selection (selectable, capability_pinned, or live_qualified).
func (s ModelSupportState) IsSelectable() bool {
	switch s {
	case ModelSupportStateSelectable,
		ModelSupportStateCapabilityPinned,
		ModelSupportStateLiveQualified:
		return true
	default:
		return false
	}
}

// Rank returns a numeric progression rank for the support state (1 to 4).
func (s ModelSupportState) Rank() int {
	switch s {
	case ModelSupportStateDiscovered:
		return 1
	case ModelSupportStateSelectable:
		return 2
	case ModelSupportStateCapabilityPinned:
		return 3
	case ModelSupportStateLiveQualified:
		return 4
	default:
		return 0
	}
}

// Satisfies reports whether current state meets or exceeds the required support state rank.
func (s ModelSupportState) Satisfies(required ModelSupportState) bool {
	return s.Rank() >= required.Rank() && s.Rank() > 0
}

// ParseModelSupportState parses and normalizes a string into a valid ModelSupportState.
func ParseModelSupportState(str string) (ModelSupportState, error) {
	norm := strings.ToLower(strings.TrimSpace(str))
	norm = strings.ReplaceAll(norm, "-", "_")
	switch norm {
	case "discovered", "discover", "unverified":
		return ModelSupportStateDiscovered, nil
	case "selectable", "available", "supported", "enabled", "active":
		return ModelSupportStateSelectable, nil
	case "capability_pinned", "pinned", "bound":
		return ModelSupportStateCapabilityPinned, nil
	case "live_qualified", "qualified", "verified", "passed":
		return ModelSupportStateLiveQualified, nil
	default:
		return "", fmt.Errorf("%w: unknown model support state %q", ErrInvalidModelSupportState, str)
	}
}

// Common errors for model catalog validation and lookups.
var (
	ErrInvalidCatalogSchemaVersion = errors.New("model_catalog: invalid schema version")
	ErrInvalidModelSupportState    = errors.New("model_catalog: invalid model support state")
	ErrMissingCatalogHash          = errors.New("model_catalog: catalog hash is required")
	ErrMissingCatalogAuthGenHash   = errors.New("model_catalog: auth generation hash is required")
	ErrMissingCatalogScopeHash     = errors.New("model_catalog: scope hash is required")
	ErrZeroDiscoveredAt            = errors.New("model_catalog: discovered_at timestamp is zero")
	ErrInvalidModelDescriptor      = errors.New("model_catalog: invalid model descriptor")
	ErrEmptyModelID                = errors.New("model_catalog: model ID cannot be empty")
	ErrModelNotFound               = errors.New("model_catalog: model not found in catalog")
	ErrModelNotSelectable          = errors.New("model_catalog: model is not selectable in current scope")
	ErrDuplicateModelID            = errors.New("model_catalog: duplicate model ID detected in catalog")
)

// ModelDescriptor describes a model's identity, support state, and capabilities.
type ModelDescriptor struct {
	ModelID                string            `json:"model_id" redact:"none"`
	DisplayName            string            `json:"display_name" redact:"none"`
	SupportState           ModelSupportState `json:"support_state" redact:"none"`
	Capabilities           uint64            `json:"capabilities" redact:"none"`
	ContextWindow          int64             `json:"context_window" redact:"none"`
	InputModalities        []string          `json:"input_modalities,omitempty" redact:"none"`
	DefaultReasoningEffort string            `json:"default_reasoning_effort,omitempty" redact:"none"`
	IsDefault              bool              `json:"is_default" redact:"none"`
	Metadata               map[string]string `json:"metadata,omitempty" redact:"sanitize"`
}

// Validate verifies structural correctness of a ModelDescriptor.
func (d ModelDescriptor) Validate() error {
	if strings.TrimSpace(d.ModelID) == "" {
		return ErrEmptyModelID
	}
	if !d.SupportState.Valid() {
		return fmt.Errorf("%w: model %q has invalid support state %q", ErrInvalidModelSupportState, d.ModelID, d.SupportState)
	}
	if d.ContextWindow < 0 {
		return fmt.Errorf("%w: model %q has negative context window %d", ErrInvalidModelDescriptor, d.ModelID, d.ContextWindow)
	}
	return nil
}

// HasCapability checks if a specific protocol CapabilityFlag is set in Capabilities bitmask.
func (d ModelDescriptor) HasCapability(flag CapabilityFlag) bool {
	return (d.Capabilities & uint64(flag)) == uint64(flag)
}

// HasAllCapabilities checks if all flags in the given mask are set.
func (d ModelDescriptor) HasAllCapabilities(mask uint64) bool {
	return (d.Capabilities & mask) == mask
}

// IsSelectable reports whether the model is in a selectable state.
func (d ModelDescriptor) IsSelectable() bool {
	return d.SupportState.IsSelectable()
}

// IsLiveQualified reports whether the model is live qualified.
func (d ModelDescriptor) IsLiveQualified() bool {
	return d.SupportState == ModelSupportStateLiveQualified
}

// ModelCatalogSnapshot represents a point-in-time, entitlement-aware snapshot of available models.
type ModelCatalogSnapshot struct {
	SchemaVersion      string            `json:"schema_version" redact:"none"`
	CatalogHash        string            `json:"catalog_hash" redact:"none"`
	AuthGenerationHash string            `json:"auth_generation_hash" redact:"none"`
	ScopeHash          string            `json:"scope_hash" redact:"none"`
	Models             []ModelDescriptor `json:"models" redact:"none"`
	DiscoveredAt       time.Time         `json:"discovered_at" redact:"none"`
	ExpiresAt          time.Time         `json:"expires_at,omitempty" redact:"none"`
}

// Validate verifies structural correctness and integrity of a ModelCatalogSnapshot.
func (s ModelCatalogSnapshot) Validate() error {
	if s.SchemaVersion != ModelCatalogSnapshotSchemaVersion {
		return fmt.Errorf("%w: got %q, want %q", ErrInvalidCatalogSchemaVersion, s.SchemaVersion, ModelCatalogSnapshotSchemaVersion)
	}
	if strings.TrimSpace(s.AuthGenerationHash) == "" {
		return ErrMissingCatalogAuthGenHash
	}
	if strings.TrimSpace(s.ScopeHash) == "" {
		return ErrMissingCatalogScopeHash
	}
	if strings.TrimSpace(s.CatalogHash) == "" {
		return ErrMissingCatalogHash
	}
	if s.DiscoveredAt.IsZero() {
		return ErrZeroDiscoveredAt
	}

	seen := make(map[string]struct{}, len(s.Models))
	for _, m := range s.Models {
		if err := m.Validate(); err != nil {
			return err
		}
		normID := strings.ToLower(strings.TrimSpace(m.ModelID))
		if _, exists := seen[normID]; exists {
			return fmt.Errorf("%w: %q", ErrDuplicateModelID, m.ModelID)
		}
		seen[normID] = struct{}{}
	}
	return nil
}

// GetModel retrieves a model by modelID (case-insensitive trimmed lookup).
func (s ModelCatalogSnapshot) GetModel(modelID string) (ModelDescriptor, bool) {
	norm := strings.ToLower(strings.TrimSpace(modelID))
	if norm == "" {
		return ModelDescriptor{}, false
	}
	for _, m := range s.Models {
		if strings.ToLower(strings.TrimSpace(m.ModelID)) == norm {
			return m, true
		}
	}
	return ModelDescriptor{}, false
}

// IsSelectable reports whether modelID exists in the snapshot and is selectable.
func (s ModelCatalogSnapshot) IsSelectable(modelID string) bool {
	m, found := s.GetModel(modelID)
	if !found {
		return false
	}
	return m.IsSelectable()
}

// FindQualified returns all models in the snapshot that satisfy the required capability mask and are selectable.
func (s ModelCatalogSnapshot) FindQualified(capability uint64) []ModelDescriptor {
	var out []ModelDescriptor
	for _, m := range s.Models {
		if (m.Capabilities&capability) == capability && m.IsSelectable() {
			out = append(out, m)
		}
	}
	return out
}

// DefaultModel returns the default model descriptor if configured, or false if none.
func (s ModelCatalogSnapshot) DefaultModel() (ModelDescriptor, bool) {
	for _, m := range s.Models {
		if m.IsDefault {
			return m, true
		}
	}
	return ModelDescriptor{}, false
}

// IsExpired reports whether the snapshot has passed its expiration time.
func (s ModelCatalogSnapshot) IsExpired(now time.Time) bool {
	if s.ExpiresAt.IsZero() {
		return false
	}
	return now.After(s.ExpiresAt)
}

// ComputeCatalogHash computes the deterministic SHA-256 catalog hash for this snapshot.
func (s ModelCatalogSnapshot) ComputeCatalogHash() string {
	return ComputeCatalogHash(s.AuthGenerationHash, s.ScopeHash, s.Models)
}

// ComputeCatalogHash computes a deterministic, 64-character SHA-256 hex digest of catalog models
// tied to the authenticated scope and auth generation.
func ComputeCatalogHash(authGenHash, scopeHash string, models []ModelDescriptor) string {
	sorted := make([]ModelDescriptor, len(models))
	copy(sorted, models)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ModelID < sorted[j].ModelID
	})

	hasher := sha256.New()
	hasher.Write([]byte("reinframe_model_catalog_v1:\n"))
	hasher.Write([]byte(strings.TrimSpace(authGenHash)))
	hasher.Write([]byte("\n"))
	hasher.Write([]byte(strings.TrimSpace(scopeHash)))
	hasher.Write([]byte("\n"))

	for _, m := range sorted {
		hasher.Write([]byte(strings.TrimSpace(m.ModelID)))
		hasher.Write([]byte(":"))
		hasher.Write([]byte(strings.TrimSpace(m.DisplayName)))
		hasher.Write([]byte(":"))
		hasher.Write([]byte(string(m.SupportState)))
		hasher.Write([]byte(":"))
		_, _ = fmt.Fprintf(hasher, "%d:%d:%t:%s:", m.Capabilities, m.ContextWindow, m.IsDefault, m.DefaultReasoningEffort)
		mods := make([]string, len(m.InputModalities))
		copy(mods, m.InputModalities)
		sort.Strings(mods)
		for _, mod := range mods {
			hasher.Write([]byte(mod))
			hasher.Write([]byte(","))
		}
		hasher.Write([]byte("\n"))
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// NewModelCatalogSnapshot constructs and validates a new ModelCatalogSnapshot.
func NewModelCatalogSnapshot(
	authGenHash string,
	scopeHash string,
	models []ModelDescriptor,
	discoveredAt time.Time,
	expiresAt time.Time,
) (ModelCatalogSnapshot, error) {
	if discoveredAt.IsZero() {
		discoveredAt = time.Now().UTC()
	}
	snap := ModelCatalogSnapshot{
		SchemaVersion:      ModelCatalogSnapshotSchemaVersion,
		CatalogHash:        ComputeCatalogHash(authGenHash, scopeHash, models),
		AuthGenerationHash: authGenHash,
		ScopeHash:          scopeHash,
		Models:             models,
		DiscoveredAt:       discoveredAt.UTC(),
		ExpiresAt:          expiresAt.UTC(),
	}
	if err := snap.Validate(); err != nil {
		return ModelCatalogSnapshot{}, err
	}
	return snap, nil
}

// MarshalJSON ensures deterministic output formatting.
func (s ModelCatalogSnapshot) MarshalJSON() ([]byte, error) {
	type alias ModelCatalogSnapshot
	out := alias(s)
	if out.SchemaVersion == "" {
		out.SchemaVersion = ModelCatalogSnapshotSchemaVersion
	}
	if out.CatalogHash == "" {
		out.CatalogHash = ComputeCatalogHash(out.AuthGenerationHash, out.ScopeHash, out.Models)
	}
	return json.Marshal(out)
}
