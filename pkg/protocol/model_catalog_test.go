package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestModelSupportState_ValidAndMethods(t *testing.T) {
	t.Parallel()

	validStates := []protocol.ModelSupportState{
		protocol.ModelSupportStateDiscovered,
		protocol.ModelSupportStateSelectable,
		protocol.ModelSupportStateCapabilityPinned,
		protocol.ModelSupportStateLiveQualified,
	}

	for _, s := range validStates {
		if !s.Valid() {
			t.Errorf("expected state %q to be valid", s)
		}
		if s.Rank() <= 0 {
			t.Errorf("expected rank for state %q to be > 0", s)
		}
	}

	invalidStates := []protocol.ModelSupportState{
		"",
		"unknown",
		"deprecated",
		"invalid",
	}

	for _, s := range invalidStates {
		if s.Valid() {
			t.Errorf("expected state %q to be invalid", s)
		}
		if s.Rank() != 0 {
			t.Errorf("expected rank for invalid state %q to be 0", s)
		}
		if s.IsSelectable() {
			t.Errorf("expected invalid state %q to not be selectable", s)
		}
	}

	// Test IsSelectable
	if protocol.ModelSupportStateDiscovered.IsSelectable() {
		t.Error("discovered state should not be selectable")
	}
	if !protocol.ModelSupportStateSelectable.IsSelectable() {
		t.Error("selectable state should be selectable")
	}
	if !protocol.ModelSupportStateCapabilityPinned.IsSelectable() {
		t.Error("capability_pinned state should be selectable")
	}
	if !protocol.ModelSupportStateLiveQualified.IsSelectable() {
		t.Error("live_qualified state should be selectable")
	}

	// Test Satisfies
	if !protocol.ModelSupportStateLiveQualified.Satisfies(protocol.ModelSupportStateSelectable) {
		t.Error("live_qualified should satisfy selectable")
	}
	if protocol.ModelSupportStateDiscovered.Satisfies(protocol.ModelSupportStateSelectable) {
		t.Error("discovered should not satisfy selectable")
	}
}

func TestParseModelSupportState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    protocol.ModelSupportState
		wantErr bool
	}{
		{"discovered", protocol.ModelSupportStateDiscovered, false},
		{"DISCOVERED", protocol.ModelSupportStateDiscovered, false},
		{"selectable", protocol.ModelSupportStateSelectable, false},
		{"available", protocol.ModelSupportStateSelectable, false},
		{"capability_pinned", protocol.ModelSupportStateCapabilityPinned, false},
		{"capability-pinned", protocol.ModelSupportStateCapabilityPinned, false},
		{"pinned", protocol.ModelSupportStateCapabilityPinned, false},
		{"live_qualified", protocol.ModelSupportStateLiveQualified, false},
		{"live-qualified", protocol.ModelSupportStateLiveQualified, false},
		{"qualified", protocol.ModelSupportStateLiveQualified, false},
		{"invalid_state", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		got, err := protocol.ParseModelSupportState(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseModelSupportState(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseModelSupportState(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestModelDescriptor_ValidationAndCapabilities(t *testing.T) {
	t.Parallel()

	validDesc := protocol.ModelDescriptor{
		ModelID:                "gpt-5.3-codex-spark",
		DisplayName:            "GPT-5.3 Codex Spark",
		SupportState:           protocol.ModelSupportStateLiveQualified,
		Capabilities:           uint64(protocol.CapEventStream | protocol.CapToolInspection | protocol.CapHooks),
		ContextWindow:          128000,
		InputModalities:        []string{"text"},
		DefaultReasoningEffort: "high",
		IsDefault:              true,
	}

	if err := validDesc.Validate(); err != nil {
		t.Fatalf("expected valid descriptor, got error: %v", err)
	}

	if !validDesc.HasCapability(protocol.CapEventStream) {
		t.Error("expected HasCapability(CapEventStream) to be true")
	}
	if validDesc.HasCapability(protocol.CapPause) {
		t.Error("expected HasCapability(CapPause) to be false")
	}
	if !validDesc.HasAllCapabilities(uint64(protocol.CapEventStream | protocol.CapToolInspection)) {
		t.Error("expected HasAllCapabilities to be true")
	}
	if !validDesc.IsSelectable() {
		t.Error("expected IsSelectable() to be true")
	}
	if !validDesc.IsLiveQualified() {
		t.Error("expected IsLiveQualified() to be true")
	}

	// Invalid cases
	invalidDesc1 := validDesc
	invalidDesc1.ModelID = ""
	if err := invalidDesc1.Validate(); err == nil {
		t.Error("expected error for empty ModelID")
	}

	invalidDesc2 := validDesc
	invalidDesc2.SupportState = "invalid"
	if err := invalidDesc2.Validate(); err == nil {
		t.Error("expected error for invalid SupportState")
	}

	invalidDesc3 := validDesc
	invalidDesc3.ContextWindow = -10
	if err := invalidDesc3.Validate(); err == nil {
		t.Error("expected error for negative ContextWindow")
	}
}

func TestModelCatalogSnapshot_ValidationAndLookups(t *testing.T) {
	t.Parallel()

	authGen := protocol.ComputeAuthGenerationHash("default", "gen-123")
	scope := protocol.ComputeScopeHash([]string{"chatgpt_pro"})

	models := []protocol.ModelDescriptor{
		{
			ModelID:         "gpt-5.3-codex",
			DisplayName:     "GPT-5.3 Codex",
			SupportState:    protocol.ModelSupportStateSelectable,
			Capabilities:    uint64(protocol.CapEventStream | protocol.CapToolInspection | protocol.CapDiffInspection),
			ContextWindow:   200000,
			InputModalities: []string{"text", "image"},
			IsDefault:       true,
		},
		{
			ModelID:                "gpt-5.3-codex-spark",
			DisplayName:            "GPT-5.3 Codex Spark (Research Preview)",
			SupportState:           protocol.ModelSupportStateDiscovered,
			Capabilities:           uint64(protocol.CapEventStream),
			ContextWindow:          128000,
			InputModalities:        []string{"text"},
			DefaultReasoningEffort: "high",
			IsDefault:              false,
		},
		{
			ModelID:         "gpt-5.3-codex-qualified",
			DisplayName:     "GPT-5.3 Codex Qualified",
			SupportState:    protocol.ModelSupportStateLiveQualified,
			Capabilities:    uint64(protocol.CapEventStream | protocol.CapToolInspection | protocol.CapAdviceDelivery),
			ContextWindow:   200000,
			InputModalities: []string{"text"},
		},
	}

	now := time.Now().UTC()
	snap, err := protocol.NewModelCatalogSnapshot(authGen, scope, models, now, now.Add(1*time.Hour))
	if err != nil {
		t.Fatalf("failed to create snapshot: %v", err)
	}

	if err := snap.Validate(); err != nil {
		t.Fatalf("snapshot validation failed: %v", err)
	}

	if len(snap.CatalogHash) != 64 {
		t.Fatalf("expected 64-char hex catalog hash, got %d chars: %s", len(snap.CatalogHash), snap.CatalogHash)
	}

	// Lookup GetModel
	m1, found := snap.GetModel("gpt-5.3-codex")
	if !found || m1.ModelID != "gpt-5.3-codex" {
		t.Errorf("failed to lookup model gpt-5.3-codex")
	}

	// Case-insensitive lookup
	m1Upper, found := snap.GetModel("GPT-5.3-CODEX")
	if !found || m1Upper.ModelID != "gpt-5.3-codex" {
		t.Errorf("case-insensitive lookup failed")
	}

	_, found = snap.GetModel("nonexistent-model")
	if found {
		t.Errorf("expected nonexistent-model to not be found")
	}

	// IsSelectable
	if !snap.IsSelectable("gpt-5.3-codex") {
		t.Error("expected gpt-5.3-codex to be selectable")
	}
	if snap.IsSelectable("gpt-5.3-codex-spark") {
		t.Error("expected gpt-5.3-codex-spark in discovered state to NOT be selectable")
	}
	if snap.IsSelectable("nonexistent") {
		t.Error("expected nonexistent model to NOT be selectable")
	}

	// FindQualified
	reqCap := uint64(protocol.CapEventStream | protocol.CapToolInspection)
	qualified := snap.FindQualified(reqCap)
	if len(qualified) != 2 {
		t.Fatalf("expected 2 qualified selectable models, got %d", len(qualified))
	}

	// DefaultModel
	defModel, found := snap.DefaultModel()
	if !found || defModel.ModelID != "gpt-5.3-codex" {
		t.Errorf("expected default model gpt-5.3-codex, got %v", defModel)
	}

	// Expiration check
	if snap.IsExpired(now.Add(30 * time.Minute)) {
		t.Error("snapshot should not be expired at +30m")
	}
	if !snap.IsExpired(now.Add(2 * time.Hour)) {
		t.Error("snapshot should be expired at +2h")
	}

	// JSON serialization
	rawJSON, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("failed to marshal snapshot: %v", err)
	}
	var deserialized protocol.ModelCatalogSnapshot
	if err := json.Unmarshal(rawJSON, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal snapshot: %v", err)
	}
	if deserialized.CatalogHash != snap.CatalogHash {
		t.Errorf("catalog hash mismatch after JSON roundtrip: %s vs %s", deserialized.CatalogHash, snap.CatalogHash)
	}
}

func TestModelCatalogSnapshot_ValidationFailures(t *testing.T) {
	t.Parallel()

	authGen := protocol.ComputeAuthGenerationHash("default", "gen-123")
	scope := protocol.ComputeScopeHash([]string{"global"})
	now := time.Now().UTC()

	// Invalid schema version
	snap1 := protocol.ModelCatalogSnapshot{
		SchemaVersion:      "invalid_version",
		CatalogHash:        "hash",
		AuthGenerationHash: authGen,
		ScopeHash:          scope,
		DiscoveredAt:       now,
	}
	if err := snap1.Validate(); err == nil || !strings.Contains(err.Error(), "invalid schema version") {
		t.Errorf("expected invalid schema version error, got %v", err)
	}

	// Missing catalog hash
	snap2 := protocol.ModelCatalogSnapshot{
		SchemaVersion:      protocol.ModelCatalogSnapshotSchemaVersion,
		CatalogHash:        "",
		AuthGenerationHash: authGen,
		ScopeHash:          scope,
		DiscoveredAt:       now,
	}
	if err := snap2.Validate(); err == nil {
		t.Error("expected error for missing catalog hash")
	}

	// Duplicate model IDs
	dupModels := []protocol.ModelDescriptor{
		{ModelID: "model-a", SupportState: protocol.ModelSupportStateSelectable},
		{ModelID: "model-a", SupportState: protocol.ModelSupportStateDiscovered},
	}
	_, err := protocol.NewModelCatalogSnapshot(authGen, scope, dupModels, now, time.Time{})
	if err == nil || !strings.Contains(err.Error(), "duplicate model ID") {
		t.Errorf("expected duplicate model ID error, got %v", err)
	}
}

func TestComputeCatalogHash_Deterministic(t *testing.T) {
	t.Parallel()

	authGen := protocol.ComputeAuthGenerationHash("prof", "fp1")
	scope := protocol.ComputeScopeHash([]string{"s1", "s2"})

	models1 := []protocol.ModelDescriptor{
		{ModelID: "model-b", DisplayName: "B", SupportState: protocol.ModelSupportStateSelectable, ContextWindow: 100},
		{ModelID: "model-a", DisplayName: "A", SupportState: protocol.ModelSupportStateDiscovered, ContextWindow: 200},
	}
	models2 := []protocol.ModelDescriptor{
		{ModelID: "model-a", DisplayName: "A", SupportState: protocol.ModelSupportStateDiscovered, ContextWindow: 200},
		{ModelID: "model-b", DisplayName: "B", SupportState: protocol.ModelSupportStateSelectable, ContextWindow: 100},
	}

	h1 := protocol.ComputeCatalogHash(authGen, scope, models1)
	h2 := protocol.ComputeCatalogHash(authGen, scope, models2)

	if h1 != h2 {
		t.Errorf("hash must be deterministic regardless of input order: %s vs %s", h1, h2)
	}

	// Hash should change if authGen or scope or model property changes
	h3 := protocol.ComputeCatalogHash(authGen+"-diff", scope, models1)
	if h1 == h3 {
		t.Errorf("hash must change when authGen changes")
	}

	h4 := protocol.ComputeCatalogHash(authGen, scope+"-diff", models1)
	if h1 == h4 {
		t.Errorf("hash must change when scope changes")
	}

	modelsDiff := []protocol.ModelDescriptor{
		{ModelID: "model-a", DisplayName: "A", SupportState: protocol.ModelSupportStateLiveQualified, ContextWindow: 200},
		{ModelID: "model-b", DisplayName: "B", SupportState: protocol.ModelSupportStateSelectable, ContextWindow: 100},
	}
	h5 := protocol.ComputeCatalogHash(authGen, scope, modelsDiff)
	if h1 == h5 {
		t.Errorf("hash must change when model state changes")
	}
}
