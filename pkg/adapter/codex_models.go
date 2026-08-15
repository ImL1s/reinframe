package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/codexruntime"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// Re-export protocol types and constants for model discovery and catalog snapshots (#185).
type (
	ModelSupportState    = protocol.ModelSupportState
	ModelDescriptor      = protocol.ModelDescriptor
	ModelCatalogSnapshot = protocol.ModelCatalogSnapshot
)

const (
	ModelCatalogSnapshotSchemaVersion = protocol.ModelCatalogSnapshotSchemaVersion
	ModelSupportStateDiscovered       = protocol.ModelSupportStateDiscovered
	ModelSupportStateSelectable       = protocol.ModelSupportStateSelectable
	ModelSupportStateCapabilityPinned = protocol.ModelSupportStateCapabilityPinned
	ModelSupportStateLiveQualified    = protocol.ModelSupportStateLiveQualified
)

// Default TTL for cached model catalog discovery.
const DefaultModelCatalogTTL = 5 * time.Minute

// ModelCatalogService provides entitlement-aware dynamic model discovery and capability snapshots (#185).
// It queries the active Codex App Server session via the JSON-RPC "model/list" method and maintains
// cache partitions aligned with authentication generation and scope hashes without hardcoded static lists.
type ModelCatalogService struct {
	client       AppServerClient
	partitionMgr *codexruntime.CachePartitionManager
	ttl          time.Duration

	mu              sync.RWMutex
	currentSnapshot ModelCatalogSnapshot
	currentKey      string
	modelIndex      map[string]ModelDescriptor // lower-case normalized model ID -> descriptor
	selectableSet   map[string]bool            // lower-case normalized model ID -> isSelectable
	lastDiscovered  time.Time
}

// ModelCatalogConfig configures the ModelCatalogService.
type ModelCatalogConfig struct {
	TTL              time.Duration
	PartitionManager *codexruntime.CachePartitionManager
}

// NewModelCatalogService creates a new ModelCatalogService.
func NewModelCatalogService(client AppServerClient, cfg ...ModelCatalogConfig) *ModelCatalogService {
	var c ModelCatalogConfig
	if len(cfg) > 0 {
		c = cfg[0]
	}
	if c.TTL <= 0 {
		c.TTL = DefaultModelCatalogTTL
	}
	pm := c.PartitionManager
	if pm == nil {
		pm = codexruntime.NewCachePartitionManager()
	}

	return &ModelCatalogService{
		client:        client,
		partitionMgr:  pm,
		ttl:           c.TTL,
		modelIndex:    make(map[string]ModelDescriptor),
		selectableSet: make(map[string]bool),
	}
}

// SetTTL updates the cache TTL for discovery snapshots.
func (s *ModelCatalogService) SetTTL(ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ttl > 0 {
		s.ttl = ttl
	}
}

// CurrentSnapshot returns the currently active ModelCatalogSnapshot if present and not expired.
func (s *ModelCatalogService) CurrentSnapshot() (ModelCatalogSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentSnapshot.CatalogHash == "" {
		return ModelCatalogSnapshot{}, false
	}
	if s.currentSnapshot.IsExpired(time.Now().UTC()) {
		return ModelCatalogSnapshot{}, false
	}
	return s.currentSnapshot, true
}

// GetModel retrieves a model descriptor by model ID from the active catalog snapshot.
func (s *ModelCatalogService) GetModel(modelID string) (ModelDescriptor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentSnapshot.CatalogHash == "" || s.currentSnapshot.IsExpired(time.Now().UTC()) {
		return ModelDescriptor{}, false
	}
	norm := strings.ToLower(strings.TrimSpace(modelID))
	if norm == "" {
		return ModelDescriptor{}, false
	}
	m, ok := s.modelIndex[norm]
	return m, ok
}

// IsSelectable reports whether the model ID is present in the active catalog and currently selectable.
func (s *ModelCatalogService) IsSelectable(modelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentSnapshot.CatalogHash == "" || s.currentSnapshot.IsExpired(time.Now().UTC()) {
		return false
	}
	norm := strings.ToLower(strings.TrimSpace(modelID))
	if norm == "" {
		return false
	}
	return s.selectableSet[norm]
}

// FindQualified returns all models in the active catalog that satisfy the capability bitmask and are selectable.
func (s *ModelCatalogService) FindQualified(capability uint64) []ModelDescriptor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentSnapshot.CatalogHash == "" || s.currentSnapshot.IsExpired(time.Now().UTC()) {
		return nil
	}
	var out []ModelDescriptor
	for _, m := range s.currentSnapshot.Models {
		if (m.Capabilities&capability) == capability && m.IsSelectable() {
			out = append(out, m)
		}
	}
	return out
}

// DefaultModel returns the default model descriptor from the active snapshot, if one exists.
func (s *ModelCatalogService) DefaultModel() (ModelDescriptor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentSnapshot.CatalogHash == "" || s.currentSnapshot.IsExpired(time.Now().UTC()) {
		return ModelDescriptor{}, false
	}
	return s.currentSnapshot.DefaultModel()
}

// AllModels returns a copy of all model descriptors in the current active snapshot.
func (s *ModelCatalogService) AllModels() []ModelDescriptor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentSnapshot.CatalogHash == "" || s.currentSnapshot.IsExpired(time.Now().UTC()) {
		return nil
	}
	out := make([]ModelDescriptor, len(s.currentSnapshot.Models))
	copy(out, s.currentSnapshot.Models)
	return out
}

// Invalidate explicitly clears the active model catalog cache.
func (s *ModelCatalogService) Invalidate(reason ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := "manual_invalidation"
	if len(reason) > 0 && reason[0] != "" {
		r = reason[0]
	}
	s.currentSnapshot = ModelCatalogSnapshot{}
	s.currentKey = ""
	s.modelIndex = make(map[string]ModelDescriptor)
	s.selectableSet = make(map[string]bool)
	if s.partitionMgr != nil {
		s.partitionMgr.Invalidate(codexruntime.InvalidationReason(r))
	}
}

// Discover performs dynamic model discovery for the authenticated snapshot.
// It utilizes cache partitioning based on AuthGenerationHash and ScopeHash.
// If a valid, non-expired snapshot exists for the current partition key, it is returned immediately.
func (s *ModelCatalogService) Discover(ctx context.Context, authSnap protocol.RuntimeAuthSnapshot) (ModelCatalogSnapshot, error) {
	if err := authSnap.Validate(); err != nil {
		return ModelCatalogSnapshot{}, fmt.Errorf("model discovery requires valid auth snapshot: %w", err)
	}
	if !authSnap.IsAuthenticated() {
		s.Invalidate(string(codexruntime.ReasonUnauthenticatedState))
		return ModelCatalogSnapshot{}, codexruntime.ErrUnauthenticatedPartition
	}

	partitionKey := authSnap.CacheKeyPartition()

	// Check if cached snapshot is valid for this partition
	s.mu.RLock()
	if s.currentKey == partitionKey && s.currentSnapshot.CatalogHash != "" && !s.currentSnapshot.IsExpired(time.Now().UTC()) {
		snap := s.currentSnapshot
		s.mu.RUnlock()
		return snap, nil
	}
	s.mu.RUnlock()

	// Fetch fresh model catalog
	return s.refreshWithPartition(ctx, authSnap, partitionKey)
}

// Refresh forces a re-fetch of the model catalog via App Server regardless of cache age.
func (s *ModelCatalogService) Refresh(ctx context.Context, authSnap protocol.RuntimeAuthSnapshot) (ModelCatalogSnapshot, error) {
	if err := authSnap.Validate(); err != nil {
		return ModelCatalogSnapshot{}, fmt.Errorf("model refresh requires valid auth snapshot: %w", err)
	}
	if !authSnap.IsAuthenticated() {
		s.Invalidate(string(codexruntime.ReasonUnauthenticatedState))
		return ModelCatalogSnapshot{}, codexruntime.ErrUnauthenticatedPartition
	}

	partitionKey := authSnap.CacheKeyPartition()
	return s.refreshWithPartition(ctx, authSnap, partitionKey)
}

func (s *ModelCatalogService) refreshWithPartition(ctx context.Context, authSnap protocol.RuntimeAuthSnapshot, partitionKey string) (ModelCatalogSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update partition manager
	if s.partitionMgr != nil {
		_, _, _ = s.partitionMgr.UpdateSnapshot(authSnap)
	}

	if s.client == nil {
		return ModelCatalogSnapshot{}, NewAppServerError(ErrCodeRuntimeCrashed, "app server client is nil")
	}

	raw, err := s.client.ListModels(ctx)
	if err != nil {
		return ModelCatalogSnapshot{}, fmt.Errorf("failed to query model/list: %w", err)
	}

	models, err := ParseModelListResponse(raw)
	if err != nil {
		return ModelCatalogSnapshot{}, err
	}

	now := time.Now().UTC()
	var expiresAt time.Time
	if s.ttl > 0 {
		expiresAt = now.Add(s.ttl)
	}

	snapshot, err := protocol.NewModelCatalogSnapshot(
		authSnap.AuthGenerationHash,
		authSnap.ScopeHash,
		models,
		now,
		expiresAt,
	)
	if err != nil {
		return ModelCatalogSnapshot{}, fmt.Errorf("failed to create model catalog snapshot: %w", err)
	}

	// Update index & state
	newIndex := make(map[string]ModelDescriptor, len(models))
	newSelectable := make(map[string]bool, len(models))
	for _, m := range models {
		norm := strings.ToLower(strings.TrimSpace(m.ModelID))
		newIndex[norm] = m
		if m.IsSelectable() {
			newSelectable[norm] = true
		}
	}

	s.currentSnapshot = snapshot
	s.currentKey = partitionKey
	s.modelIndex = newIndex
	s.selectableSet = newSelectable
	s.lastDiscovered = now

	return snapshot, nil
}

// ParseModelListResponse parses and validates raw JSON-RPC model/list response into a slice of ModelDescriptors (#185).
// It supports multiple response envelopes (models list, data list, bare list, string IDs) and normalizes camelCase/snake_case.
func ParseModelListResponse(raw json.RawMessage) ([]ModelDescriptor, error) {
	if len(raw) == 0 {
		return nil, NewAppServerError(ErrCodeProtocolMalformed, "empty model/list response")
	}

	var rawVal any
	if err := json.Unmarshal(raw, &rawVal); err != nil {
		return nil, NewAppServerError(ErrCodeProtocolMalformed, "failed to unmarshal JSON-RPC model/list payload", err)
	}

	var items []any
	switch v := rawVal.(type) {
	case []any:
		items = v
	case map[string]any:
		if mList, ok := v["models"].([]any); ok {
			items = mList
		} else if dList, ok := v["data"].([]any); ok {
			items = dList
		} else if itemsList, ok := v["items"].([]any); ok {
			items = itemsList
		} else {
			// Single model object or empty object
			if _, hasID := v["id"]; hasID {
				items = []any{v}
			} else if _, hasModelID := v["model_id"]; hasModelID {
				items = []any{v}
			} else if _, hasModelId := v["modelId"]; hasModelId {
				items = []any{v}
			} else if len(v) == 0 {
				items = []any{}
			} else {
				return nil, NewAppServerError(ErrCodeProtocolMalformed, "model/list payload does not contain models, data, or items array")
			}
		}
	default:
		return nil, NewAppServerError(ErrCodeProtocolMalformed, fmt.Sprintf("unsupported model/list root payload type: %T", rawVal))
	}

	seen := make(map[string]struct{}, len(items))
	var descriptors []ModelDescriptor

	for idx, item := range items {
		switch m := item.(type) {
		case string:
			// Bare model name string
			modelID := strings.TrimSpace(m)
			if modelID == "" {
				continue
			}
			normID := strings.ToLower(modelID)
			if _, exists := seen[normID]; exists {
				continue
			}
			seen[normID] = struct{}{}
			descriptors = append(descriptors, ModelDescriptor{
				ModelID:         modelID,
				DisplayName:     modelID,
				SupportState:    ModelSupportStateSelectable,
				Capabilities:    uint64(protocol.CapEventStream | protocol.CapToolInspection),
				ContextWindow:   128000,
				InputModalities: []string{"text"},
			})

		case map[string]any:
			desc, err := parseModelMap(m)
			if err != nil {
				// If an item has invalid fields, reject or skip
				return nil, NewAppServerError(ErrCodeProtocolMalformed, fmt.Sprintf("model[%d] invalid: %v", idx, err), err)
			}
			if desc.ModelID == "" {
				continue
			}
			normID := strings.ToLower(desc.ModelID)
			if _, exists := seen[normID]; exists {
				continue
			}
			seen[normID] = struct{}{}
			descriptors = append(descriptors, desc)

		default:
			return nil, NewAppServerError(ErrCodeProtocolMalformed, fmt.Sprintf("model item at index %d has unsupported type %T", idx, item))
		}
	}

	return descriptors, nil
}

func parseModelMap(m map[string]any) (ModelDescriptor, error) {
	// Model ID
	modelID := getStringField(m, "id", "model_id", "modelId", "model", "name")
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ModelDescriptor{}, errors.New("missing model id")
	}

	// Display Name
	displayName := getStringField(m, "displayName", "display_name", "title", "label")
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = modelID
	}

	// Support State
	stateStr := getStringField(m, "supportState", "support_state", "state", "status", "qualificationState", "qualification_state")
	var state ModelSupportState
	if stateStr != "" {
		parsedState, err := protocol.ParseModelSupportState(stateStr)
		if err != nil {
			state = ModelSupportStateDiscovered
		} else {
			state = parsedState
		}
	} else {
		// Default to selectable if not explicitly marked
		state = ModelSupportStateSelectable
	}

	// Capabilities
	capabilities := parseCapabilities(m["capabilities"])

	// Context Window
	contextWindow := getInt64Field(m, "contextWindow", "context_window", "contextLength", "context_length", "maxTokens", "max_tokens")
	if contextWindow < 0 {
		contextWindow = 0
	}

	// Input Modalities
	inputModalities := parseStringSlice(m, "inputModalities", "input_modalities", "modalities")
	if len(inputModalities) == 0 {
		inputModalities = []string{"text"}
	}

	// Default Reasoning Effort
	reasoningEffort := getStringField(m, "defaultReasoningEffort", "default_reasoning_effort", "reasoningEffort", "reasoning_effort")
	reasoningEffort = strings.TrimSpace(reasoningEffort)

	// Is Default
	isDefault := getBoolField(m, "isDefault", "is_default", "default")

	// Metadata
	var metadata map[string]string
	if metaRaw, ok := m["metadata"].(map[string]any); ok {
		metadata = make(map[string]string, len(metaRaw))
		for k, v := range metaRaw {
			metadata[k] = fmt.Sprintf("%v", v)
		}
	}

	desc := ModelDescriptor{
		ModelID:                modelID,
		DisplayName:            displayName,
		SupportState:           state,
		Capabilities:           capabilities,
		ContextWindow:          contextWindow,
		InputModalities:        inputModalities,
		DefaultReasoningEffort: reasoningEffort,
		IsDefault:              isDefault,
		Metadata:               metadata,
	}

	if err := desc.Validate(); err != nil {
		return ModelDescriptor{}, err
	}

	return desc, nil
}

func parseCapabilities(v any) uint64 {
	if v == nil {
		return 0
	}
	switch c := v.(type) {
	case float64:
		return uint64(c)
	case int64:
		return uint64(c)
	case int:
		return uint64(c)
	case uint64:
		return c
	case []any:
		var mask uint64
		for _, item := range c {
			if s, ok := item.(string); ok {
				mask |= parseCapabilityFlagString(s)
			}
		}
		return mask
	case map[string]any:
		var mask uint64
		for k, val := range c {
			if b, ok := val.(bool); ok && b {
				mask |= parseCapabilityFlagString(k)
			}
		}
		return mask
	default:
		return 0
	}
}

func parseCapabilityFlagString(s string) uint64 {
	norm := strings.ToLower(strings.TrimSpace(s))
	norm = strings.ReplaceAll(norm, "_", "")
	norm = strings.ReplaceAll(norm, "-", "")

	switch norm {
	case "eventstream", "streaming", "events":
		return uint64(protocol.CapEventStream)
	case "toolinspection", "tool", "tools":
		return uint64(protocol.CapToolInspection)
	case "diffinspection", "diff", "diffs":
		return uint64(protocol.CapDiffInspection)
	case "costtracking", "cost", "tokens":
		return uint64(protocol.CapCostTracking)
	case "hooks", "hook":
		return uint64(protocol.CapHooks)
	case "headless":
		return uint64(protocol.CapHeadless)
	case "clicontrol", "cli":
		return uint64(protocol.CapCLIControl)
	case "pause":
		return uint64(protocol.CapPause)
	case "cancel":
		return uint64(protocol.CapCancel)
	case "resume":
		return uint64(protocol.CapResume)
	case "checkpoint":
		return uint64(protocol.CapCheckpoint)
	case "rollback":
		return uint64(protocol.CapRollback)
	case "mcp":
		return uint64(protocol.CapMCP)
	case "subagents", "subagent":
		return uint64(protocol.CapSubagents)
	case "extensions", "extension":
		return uint64(protocol.CapExtensions)
	case "switchmodel", "switch":
		return uint64(protocol.CapSwitchModel)
	case "customprovider":
		return uint64(protocol.CapCustomProvider)
	case "openaicompat", "openai":
		return uint64(protocol.CapOpenAICompat)
	case "localmodels", "local":
		return uint64(protocol.CapLocalModels)
	case "sdk":
		return uint64(protocol.CapSDK)
	case "advicedelivery", "advice":
		return uint64(protocol.CapAdviceDelivery)
	case "contextinjection", "context":
		return uint64(protocol.CapContextInjection)
	case "toolgate", "approval", "gate":
		return uint64(protocol.CapToolGate)
	case "turnboundary", "turns":
		return uint64(protocol.CapTurnBoundary)
	case "interventionack", "ack":
		return uint64(protocol.CapInterventionAck)
	default:
		return 0
	}
}

func getStringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func getInt64Field(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch num := v.(type) {
			case float64:
				return int64(num)
			case int64:
				return num
			case int:
				return int64(num)
			}
		}
	}
	return 0
}

func getBoolField(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if v, ok := m[k].(bool); ok {
			return v
		}
	}
	return false
}

func parseStringSlice(m map[string]any, keys ...string) []string {
	for _, k := range keys {
		if slice, ok := m[k].([]any); ok {
			var res []string
			for _, item := range slice {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					res = append(res, strings.TrimSpace(s))
				}
			}
			return res
		}
		if slice, ok := m[k].([]string); ok {
			return slice
		}
	}
	return nil
}
