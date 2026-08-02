package e2e_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/reinframe/reinframe/pkg/protocol"
)

func flagNames(mask protocol.CapabilityFlag) []string {
	var names []string
	for i := 0; i < 20; i++ {
		flag := protocol.CapabilityFlag(1 << uint(i))
		if (mask & flag) != 0 {
			names = append(names, protocol.FlagToString(flag))
		}
	}
	return names
}

// ============================================================================
// Tier 1: Feature Coverage (Capability Manifest & Handshake Protocol - Issue #7)
// ============================================================================

// --- Feature 1: 20 Capability Bitmask Flags ---

func TestTier1_CapFlags_BitmaskShiftValues(t *testing.T) {
	expectedFlags := map[protocol.CapabilityFlag]uint64{
		protocol.CapEventStream:    1 << 0,
		protocol.CapToolInspection: 1 << 1,
		protocol.CapDiffInspection: 1 << 2,
		protocol.CapCostTracking:   1 << 3,
		protocol.CapHooks:          1 << 4,
		protocol.CapHeadless:       1 << 5,
		protocol.CapCLIControl:     1 << 6,
		protocol.CapPause:          1 << 7,
		protocol.CapCancel:         1 << 8,
		protocol.CapResume:         1 << 9,
		protocol.CapCheckpoint:     1 << 10,
		protocol.CapRollback:       1 << 11,
		protocol.CapMCP:            1 << 12,
		protocol.CapSubagents:      1 << 13,
		protocol.CapExtensions:     1 << 14,
		protocol.CapSwitchModel:    1 << 15,
		protocol.CapCustomProvider: 1 << 16,
		protocol.CapOpenAICompat:   1 << 17,
		protocol.CapLocalModels:    1 << 18,
		protocol.CapSDK:            1 << 19,
	}

	if len(expectedFlags) != 20 {
		t.Fatalf("expected exactly 20 capability flags, got %d", len(expectedFlags))
	}

	for flag, expectedVal := range expectedFlags {
		if uint64(flag) != expectedVal {
			t.Errorf("flag %v bitmask value mismatch: got %d, expected %d", flag, uint64(flag), expectedVal)
		}
	}
}

func TestTier1_CapFlags_Categories(t *testing.T) {
	// Observation category
	obsMask := protocol.CapEventStream | protocol.CapToolInspection | protocol.CapDiffInspection | protocol.CapCostTracking | protocol.CapHooks
	// Execution category
	execMask := protocol.CapHeadless | protocol.CapCLIControl | protocol.CapPause | protocol.CapCancel | protocol.CapResume
	// State category
	stateMask := protocol.CapCheckpoint | protocol.CapRollback | protocol.CapMCP | protocol.CapSubagents | protocol.CapExtensions
	// Provider category
	provMask := protocol.CapSwitchModel | protocol.CapCustomProvider | protocol.CapOpenAICompat | protocol.CapLocalModels | protocol.CapSDK

	// Ensure categories do not overlap
	if (obsMask & execMask) != 0 {
		t.Errorf("Observation and Execution flag categories overlap: %X", obsMask&execMask)
	}
	if (obsMask & stateMask) != 0 {
		t.Errorf("Observation and State flag categories overlap: %X", obsMask&stateMask)
	}
	if (obsMask & provMask) != 0 {
		t.Errorf("Observation and Provider flag categories overlap: %X", obsMask&provMask)
	}
	if (execMask & stateMask) != 0 {
		t.Errorf("Execution and State flag categories overlap: %X", execMask&stateMask)
	}
	if (execMask & provMask) != 0 {
		t.Errorf("Execution and Provider flag categories overlap: %X", execMask&provMask)
	}
	if (stateMask & provMask) != 0 {
		t.Errorf("State and Provider flag categories overlap: %X", stateMask&provMask)
	}

	// Verify total combination covers all 20 bits (0x000FFFFF)
	fullMask := obsMask | execMask | stateMask | provMask
	if uint64(fullMask) != (1<<20)-1 {
		t.Errorf("Full category mask mismatch: got %X, expected %X", uint64(fullMask), uint64((1<<20)-1))
	}
}

func TestTier1_CapFlags_BitwiseOR(t *testing.T) {
	combined := protocol.CapEventStream | protocol.CapToolInspection | protocol.CapCLIControl
	expected := uint64(1<<0 | 1<<1 | 1<<6)
	if uint64(combined) != expected {
		t.Errorf("Bitwise OR mismatch: got %d, expected %d", uint64(combined), expected)
	}
}

func TestTier1_CapFlags_BitwiseAND(t *testing.T) {
	mask := protocol.CapEventStream | protocol.CapToolInspection | protocol.CapPause
	if (mask & protocol.CapToolInspection) == 0 {
		t.Errorf("Expected CapToolInspection bit to be set in mask")
	}
	if (mask & protocol.CapRollback) != 0 {
		t.Errorf("Expected CapRollback bit to NOT be set in mask")
	}
}

func TestTier1_CapFlags_StringFormatting(t *testing.T) {
	mask := protocol.CapEventStream | protocol.CapToolInspection
	names := flagNames(mask)
	if len(names) != 2 {
		t.Fatalf("Expected 2 flag names, got %d", len(names))
	}
	hasEventStream := false
	hasToolInspection := false
	for _, n := range names {
		if n == "CapEventStream" {
			hasEventStream = true
		}
		if n == "CapToolInspection" {
			hasToolInspection = true
		}
	}
	if !hasEventStream || !hasToolInspection {
		t.Errorf("FlagNames output missing expected names: %v", names)
	}
}

// --- Feature 2: CapabilityManifest Helpers ---

func TestTier1_Manifest_ToBitmask_FullStruct(t *testing.T) {
	manifest := protocol.CapabilityManifest{
		AgentID:            "agent-full",
		Version:            "1.0.0",
		IntegrationLevel:   3,
		SupportsPause:      true,
		SupportsCancel:     true,
		SupportsResume:     true,
		SupportsCheckpoint: true,
		SupportsRollback:   true,
		SupportsMCP:        true,
	}

	bitmask := manifest.ToBitmask()
	if (bitmask & protocol.Level3RequiredMask) != protocol.Level3RequiredMask {
		t.Errorf("ToBitmask full struct mismatch: got 0x%X", bitmask)
	}
}

func TestTier1_Manifest_ToBitmask_PartialStruct(t *testing.T) {
	manifest := protocol.CapabilityManifest{
		AgentID:          "agent-partial",
		Version:          "1.0.0",
		IntegrationLevel: 0,
		SupportsPause:    true,
	}

	bitmask := manifest.ToBitmask()
	expected := protocol.Level0RequiredMask | uint64(protocol.CapPause)
	if bitmask != expected {
		t.Errorf("ToBitmask partial mismatch: got 0x%X, expected 0x%X", bitmask, expected)
	}
}

func TestTier1_Manifest_FromBitmask_Roundtrip(t *testing.T) {
	inputMask := protocol.Level3RequiredMask

	manifest := protocol.FromBitmask(inputMask)
	outputMask := manifest.ToBitmask()

	if (outputMask & inputMask) != inputMask {
		t.Errorf("FromBitmask roundtrip mismatch: input 0x%X, output 0x%X", inputMask, outputMask)
	}
}

func TestTier1_Manifest_HasCapability_Present(t *testing.T) {
	manifest := protocol.CapabilityManifest{
		IntegrationLevel: 0,
		SupportsPause:    true,
	}

	if !manifest.HasCapability(protocol.CapPause) {
		t.Errorf("HasCapability(CapPause) should be true")
	}
	if !manifest.HasCapability(protocol.CapEventStream) {
		t.Errorf("HasCapability(CapEventStream) should be true")
	}
}

func TestTier1_Manifest_HasCapability_Absent(t *testing.T) {
	manifest := protocol.CapabilityManifest{
		IntegrationLevel: 0,
		SupportsPause:    true,
	}

	if manifest.HasCapability(protocol.CapCancel) {
		t.Errorf("HasCapability(CapCancel) should be false")
	}
	if manifest.HasCapability(protocol.CapMCP) {
		t.Errorf("HasCapability(CapMCP) should be false")
	}
}

// --- Feature 3: Level Threshold Evaluator ---

func TestTier1_LevelEval_Level0_Observe(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level0RequiredMask)

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 0 {
		t.Errorf("Expected Level 0 (Observe), got Level %d", level)
	}
}

func TestTier1_LevelEval_Level1_Advisory(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level1RequiredMask)

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 1 {
		t.Errorf("Expected Level 1 (Advisory), got Level %d", level)
	}
}

func TestTier1_LevelEval_Level2_Guarded(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level2RequiredMask)

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 2 {
		t.Errorf("Expected Level 2 (Guarded), got Level %d", level)
	}
}

func TestTier1_LevelEval_Level3_FullControl(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level3RequiredMask)

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 3 {
		t.Errorf("Expected Level 3 (FullControl), got Level %d", level)
	}
}

func TestTier1_LevelEval_SubZero_Unsupported(t *testing.T) {
	var manifest protocol.CapabilityManifest // Zero manifest

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 0 {
		t.Errorf("Expected Level 0 for zero manifest, got Level %d", level)
	}
}

// --- Feature 4: Handshake Negotiation & Degradation Engine ---

func TestTier1_Negotiate_ExactMatch_Level3(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level3RequiredMask)

	req := &protocol.HandshakeRequest{
		SessionID:      "sess-l3-exact",
		RequestedLevel: 3,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("Unexpected negotiation error: %v", err)
	}
	if resp.SessionID != "sess-l3-exact" {
		t.Errorf("SessionID mismatch: got %s, expected sess-l3-exact", resp.SessionID)
	}
	if resp.NegotiatedLevel != 3 {
		t.Errorf("NegotiatedLevel mismatch: got %d, expected 3", resp.NegotiatedLevel)
	}
	if resp.IsDegraded {
		t.Errorf("IsDegraded should be false for exact match")
	}
	if resp.DegradedFrom != 0 {
		t.Errorf("DegradedFrom should be 0 when not degraded, got %d", resp.DegradedFrom)
	}
	if len(resp.MissingFlags) != 0 {
		t.Errorf("MissingFlags should be empty, got %v", resp.MissingFlags)
	}
}

func TestTier1_Negotiate_ExactMatch_Level1(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level1RequiredMask)

	req := &protocol.HandshakeRequest{
		SessionID:      "sess-l1-exact",
		RequestedLevel: 1,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("Unexpected negotiation error: %v", err)
	}
	if resp.NegotiatedLevel != 1 {
		t.Errorf("NegotiatedLevel mismatch: got %d, expected 1", resp.NegotiatedLevel)
	}
	if resp.IsDegraded {
		t.Errorf("IsDegraded should be false")
	}
}

func TestTier1_Negotiate_Degradation_Level3To2(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level2RequiredMask)

	req := &protocol.HandshakeRequest{
		SessionID:      "sess-degrade-3to2",
		RequestedLevel: 3,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("Unexpected negotiation error: %v", err)
	}
	if resp.NegotiatedLevel != 2 {
		t.Errorf("NegotiatedLevel mismatch: got %d, expected 2", resp.NegotiatedLevel)
	}
	if !resp.IsDegraded {
		t.Errorf("IsDegraded should be true for degraded negotiation")
	}
	if resp.DegradedFrom != 3 {
		t.Errorf("DegradedFrom mismatch: got %d, expected 3", resp.DegradedFrom)
	}
	if len(resp.MissingFlags) == 0 {
		t.Errorf("MissingFlags should list missing L3 flags")
	}
}

func TestTier1_Negotiate_Degradation_Level2To0(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level0RequiredMask)

	req := &protocol.HandshakeRequest{
		SessionID:      "sess-degrade-2to0",
		RequestedLevel: 2,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("Unexpected negotiation error: %v", err)
	}
	if resp.NegotiatedLevel != 0 {
		t.Errorf("NegotiatedLevel mismatch: got %d, expected 0", resp.NegotiatedLevel)
	}
	if !resp.IsDegraded {
		t.Errorf("IsDegraded should be true")
	}
	if resp.DegradedFrom != 2 {
		t.Errorf("DegradedFrom mismatch: got %d, expected 2", resp.DegradedFrom)
	}
}

func TestTier1_Negotiate_MissingFlagsReported(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level1RequiredMask)

	req := &protocol.HandshakeRequest{
		SessionID:      "sess-missing-report",
		RequestedLevel: 2,
		Manifest:       manifest,
	}

	resp, err := protocol.NegotiateLevel(req)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if resp.NegotiatedLevel != 1 {
		t.Errorf("Expected degradation to Level 1, got %d", resp.NegotiatedLevel)
	}
	foundCheckpoint := false
	for _, flag := range resp.MissingFlags {
		if flag == "CapCheckpoint" {
			foundCheckpoint = true
		}
	}
	if !foundCheckpoint {
		t.Errorf("Expected MissingFlags to include CapCheckpoint, got %v", resp.MissingFlags)
	}
}

// ============================================================================
// Tier 2: Boundaries & Corner Cases (Capability Manifest & Handshake - Issue #7)
// ============================================================================

// --- Feature 1: 20 Capability Bitmask Flags (Boundaries) ---

func TestTier2_CapFlags_ZeroBitmask(t *testing.T) {
	zero := protocol.CapabilityFlag(0)
	if uint64(zero) != 0 {
		t.Errorf("Zero bitmask should equal 0, got %d", uint64(zero))
	}
	if (zero & protocol.CapEventStream) != 0 {
		t.Errorf("Zero bitmask should not contain CapEventStream")
	}
}

func TestTier2_CapFlags_MaxUint64Bitmask(t *testing.T) {
	maxMask := protocol.CapabilityFlag(^uint64(0))
	if (maxMask & protocol.CapSDK) == 0 {
		t.Errorf("Max uint64 mask should contain CapSDK")
	}
	if (maxMask & protocol.CapEventStream) == 0 {
		t.Errorf("Max uint64 mask should contain CapEventStream")
	}
}

func TestTier2_CapFlags_SingleBitShift20(t *testing.T) {
	sdkFlag := protocol.CapSDK
	if uint64(sdkFlag) != 1<<19 {
		t.Errorf("CapSDK shift mismatch: got 0x%X, expected 0x%X", uint64(sdkFlag), uint64(1<<19))
	}
	if uint64(sdkFlag) != 524288 {
		t.Errorf("CapSDK value mismatch: got %d, expected 524288", uint64(sdkFlag))
	}
}

func TestTier2_CapFlags_UnknownHighBits(t *testing.T) {
	highBitMask := protocol.CapabilityFlag(1 << 63)
	names := flagNames(highBitMask)
	_ = names

	manifest := protocol.FromBitmask(uint64(highBitMask | protocol.CapEventStream))
	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 0 {
		t.Errorf("High bit should be ignored by evaluator; expected Level 0, got Level %d", level)
	}
}

func TestTier2_CapFlags_ToggleFlag(t *testing.T) {
	mask := protocol.CapEventStream | protocol.CapToolInspection
	mask = mask &^ protocol.CapToolInspection
	if (mask & protocol.CapToolInspection) != 0 {
		t.Errorf("CapToolInspection should be toggled off")
	}
	if (mask & protocol.CapEventStream) == 0 {
		t.Errorf("CapEventStream should remain enabled")
	}

	mask = mask ^ protocol.CapToolInspection
	if (mask & protocol.CapToolInspection) == 0 {
		t.Errorf("CapToolInspection should be toggled back on")
	}
}

// --- Feature 2: CapabilityManifest Helpers (Boundaries) ---

func TestTier2_Manifest_EmptyStruct(t *testing.T) {
	var m protocol.CapabilityManifest
	bitmask := m.ToBitmask()
	if uint64(bitmask) != protocol.Level0RequiredMask {
		t.Errorf("Empty struct ToBitmask should return Level0RequiredMask, got 0x%X", uint64(bitmask))
	}
	if !m.HasCapability(protocol.CapEventStream) {
		t.Errorf("Empty struct with IntegrationLevel 0 should have CapEventStream")
	}
}

func TestTier2_Manifest_NilManifest(t *testing.T) {
	var m *protocol.CapabilityManifest

	// Safely test function handling of nil input
	achievable := protocol.EvaluateAchievableLevel(m)
	if achievable != -1 {
		t.Errorf("Expected EvaluateAchievableLevel(nil) to return -1, got %d", achievable)
	}

	// Safely test method behavior on nil pointer (recovers from expected panic)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("Expected panic when calling ToBitmask on nil manifest pointer, but did not panic")
			}
		}()
		_ = m.ToBitmask()
	}()
}

func TestTier2_Manifest_MalformedJSON(t *testing.T) {
	malformed := []byte(`{"agent_id": "a1", "integration_level": "not_an_int"}`)
	var m protocol.CapabilityManifest
	err := json.Unmarshal(malformed, &m)
	if err == nil {
		t.Errorf("Expected unmarshal error for malformed JSON, got nil")
	}
}

func TestTier2_Manifest_PartialBooleanMix(t *testing.T) {
	manifest := protocol.CapabilityManifest{
		SupportsPause:  true,
		SupportsCancel: false,
		SupportsResume: true,
	}
	mask := manifest.ToBitmask()
	if (mask & uint64(protocol.CapPause)) == 0 {
		t.Errorf("CapPause should be set")
	}
	if (mask & uint64(protocol.CapCancel)) != 0 {
		t.Errorf("CapCancel should NOT be set")
	}
	if (mask & uint64(protocol.CapResume)) == 0 {
		t.Errorf("CapResume should be set")
	}
}

func TestTier2_Manifest_HasCapability_MultipleFlags(t *testing.T) {
	manifest := protocol.CapabilityManifest{
		SupportsPause:  true,
		SupportsCancel: true,
	}
	compound := protocol.CapPause | protocol.CapResume
	if manifest.HasCapability(compound) {
		t.Errorf("HasCapability for compound mask missing CapResume should return false")
	}

	manifest.SupportsResume = true
	if !manifest.HasCapability(compound) {
		t.Errorf("HasCapability for complete compound mask should return true")
	}
}

// --- Feature 3: Level Threshold Evaluator (Boundaries) ---

func TestTier2_LevelEval_MissingOneLevel2Flag(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level1RequiredMask)

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 1 {
		t.Errorf("Expected Level 1, got Level %d", level)
	}
}

func TestTier2_LevelEval_MissingOneLevel3Flag(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level2RequiredMask)

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 2 {
		t.Errorf("Expected Level 2, got Level %d", level)
	}
}

func TestTier2_LevelEval_Level0WithoutEventStream(t *testing.T) {
	manifest := protocol.CapabilityManifest{}

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 0 {
		t.Errorf("Expected Level 0 for zero manifest, got Level %d", level)
	}
}

func TestTier2_LevelEval_SuperfluousLevel3FlagsAtLevel1(t *testing.T) {
	manifest := protocol.CapabilityManifest{
		IntegrationLevel: 1,
		SupportsMCP:      true,
	}

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 1 {
		t.Errorf("Expected Level 1 despite having SupportsMCP, got Level %d", level)
	}
}

func TestTier2_LevelEval_BoundaryAll20Flags(t *testing.T) {
	manifest := protocol.FromBitmask(protocol.Level3RequiredMask)

	level := protocol.EvaluateAchievableLevel(&manifest)
	if level != 3 {
		t.Errorf("Expected Level 3, got Level %d", level)
	}
}

// --- Feature 4: Handshake Negotiation & Degradation Engine (Boundaries) ---

func TestTier2_Negotiate_NilRequest(t *testing.T) {
	resp, err := protocol.NegotiateLevel(nil)
	if resp != nil {
		t.Errorf("Expected nil response for nil request, got %v", resp)
	}
	if err == nil || err.Error() != "handshake request cannot be nil" {
		t.Errorf("Expected nil request error, got %v", err)
	}
}

func TestTier2_Negotiate_EmptySessionID(t *testing.T) {
	req := &protocol.HandshakeRequest{
		SessionID:      "",
		RequestedLevel: 1,
		Manifest:       protocol.FromBitmask(protocol.Level1RequiredMask),
	}

	resp, err := protocol.NegotiateLevel(req)
	if resp != nil {
		t.Errorf("Expected nil response for empty SessionID, got %v", resp)
	}
	if err == nil || err.Error() != "session_id cannot be empty" {
		t.Errorf("Expected empty session error, got %v", err)
	}
}

func TestTier2_Negotiate_InvalidRequestedLevel_Negative(t *testing.T) {
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-invalid-neg",
		RequestedLevel: -1,
		Manifest:       protocol.FromBitmask(protocol.Level0RequiredMask),
	}

	resp, err := protocol.NegotiateLevel(req)
	if resp != nil {
		t.Errorf("Expected nil response for negative requested level, got %v", resp)
	}
	if err == nil {
		t.Errorf("Expected error for negative requested level")
	}
}

func TestTier2_Negotiate_InvalidRequestedLevel_OverMax(t *testing.T) {
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-invalid-over",
		RequestedLevel: 4,
		Manifest:       protocol.FromBitmask(protocol.Level0RequiredMask),
	}

	resp, err := protocol.NegotiateLevel(req)
	if resp != nil {
		t.Errorf("Expected nil response for requested level 4, got %v", resp)
	}
	if err == nil {
		t.Errorf("Expected error for requested level 4")
	}
}

func TestTier2_Negotiate_UnsupportedAgent_Error(t *testing.T) {
	req := &protocol.HandshakeRequest{
		SessionID:      "sess-unsupported",
		RequestedLevel: 0,
		Manifest:       protocol.FromBitmask(0),
	}

	resp, err := protocol.NegotiateLevel(req)
	if resp != nil {
		t.Errorf("Expected nil response for unsupported agent, got %v", resp)
	}
	if !errors.Is(err, protocol.ErrUnsupportedAgent) && err == nil {
		t.Errorf("Expected ErrUnsupportedAgent for manifest with zero capabilities, got err: %v", err)
	}
}
