package protocol

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
)

func TestCapabilityFlag_ConstantsAndStringer(t *testing.T) {
	expectedFlags := map[CapabilityFlag]struct {
		shift int
		name  string
	}{
		CapEventStream:      {0, "CapEventStream"},
		CapToolInspection:   {1, "CapToolInspection"},
		CapDiffInspection:   {2, "CapDiffInspection"},
		CapCostTracking:     {3, "CapCostTracking"},
		CapHooks:            {4, "CapHooks"},
		CapHeadless:         {5, "CapHeadless"},
		CapCLIControl:       {6, "CapCLIControl"},
		CapPause:            {7, "CapPause"},
		CapCancel:           {8, "CapCancel"},
		CapResume:           {9, "CapResume"},
		CapCheckpoint:       {10, "CapCheckpoint"},
		CapRollback:         {11, "CapRollback"},
		CapMCP:              {12, "CapMCP"},
		CapSubagents:        {13, "CapSubagents"},
		CapExtensions:       {14, "CapExtensions"},
		CapSwitchModel:      {15, "CapSwitchModel"},
		CapCustomProvider:   {16, "CapCustomProvider"},
		CapOpenAICompat:     {17, "CapOpenAICompat"},
		CapLocalModels:      {18, "CapLocalModels"},
		CapSDK:              {19, "CapSDK"},
		CapAdviceDelivery:   {20, "CapAdviceDelivery"},
		CapContextInjection: {21, "CapContextInjection"},
		CapToolGate:         {22, "CapToolGate"},
		CapTurnBoundary:     {23, "CapTurnBoundary"},
		CapInterventionAck:  {24, "CapInterventionAck"},
	}

	if len(expectedFlags) != CapabilityFlagCount {
		t.Fatalf("expected %d capability flags, got %d", CapabilityFlagCount, len(expectedFlags))
	}

	for flag, exp := range expectedFlags {
		expectedValue := uint64(1 << exp.shift)
		if uint64(flag) != expectedValue {
			t.Errorf("flag %s bit value mismatch: got 0x%x, want 0x%x", exp.name, uint64(flag), expectedValue)
		}
		if flag.String() != exp.name {
			t.Errorf("flag String() mismatch: got %q, want %q", flag.String(), exp.name)
		}
		if FlagToString(flag) != exp.name {
			t.Errorf("FlagToString mismatch: got %q, want %q", FlagToString(flag), exp.name)
		}
	}

	unknownFlag := CapabilityFlag(1 << 30)
	if unknownFlag.String() != "CapabilityFlag(0x40000000)" {
		t.Errorf("unknown flag string mismatch: got %q", unknownFlag.String())
	}
}

func TestCapabilityManifest_ToBitmask_StrictExplicitBooleans(t *testing.T) {
	t.Run("IntegrationLevel_does_not_auto_grant_capabilities", func(t *testing.T) {
		manifest := CapabilityManifest{
			IntegrationLevel: 3, // Level 3 set, but ALL booleans false
		}
		mask := manifest.ToBitmask()
		if mask != 0 {
			t.Fatalf("expected ToBitmask() to return 0 for struct without booleans, got 0x%x (auto-grant violation)", mask)
		}
		if manifest.HasCapability(CapEventStream) {
			t.Errorf("HasCapability(CapEventStream) should be false when SupportsEventStream is false")
		}
		if manifest.HasCapability(CapPause) {
			t.Errorf("HasCapability(CapPause) should be false when SupportsPause is false")
		}
	})

	t.Run("Constructs_bitmask_strictly_from_explicit_booleans", func(t *testing.T) {
		manifest := CapabilityManifest{
			SupportsEventStream:    true,
			SupportsToolInspection: true,
			SupportsPause:          true,
		}
		mask := manifest.ToBitmask()
		expectedMask := uint64(CapEventStream) | uint64(CapToolInspection) | uint64(CapPause)
		if mask != expectedMask {
			t.Fatalf("ToBitmask() mismatch: got 0x%x, want 0x%x", mask, expectedMask)
		}
		if !manifest.HasCapability(CapEventStream) {
			t.Errorf("expected HasCapability(CapEventStream) to be true")
		}
		if !manifest.HasCapability(CapToolInspection) {
			t.Errorf("expected HasCapability(CapToolInspection) to be true")
		}
		if !manifest.HasCapability(CapPause) {
			t.Errorf("expected HasCapability(CapPause) to be true")
		}
		if manifest.HasCapability(CapCancel) {
			t.Errorf("expected HasCapability(CapCancel) to be false")
		}
	})
}

func TestCapabilityManifest_FromBitmask_RoundTrip(t *testing.T) {
	restored := FromBitmask(Level2RequiredMask)
	if restored.IntegrationLevel != 2 {
		t.Errorf("FromBitmask IntegrationLevel mismatch: got %d, want 2", restored.IntegrationLevel)
	}

	// Level 2 required booleans (includes CapAdviceDelivery via Level1)
	if !restored.SupportsEventStream || !restored.SupportsToolInspection || !restored.SupportsAdviceDelivery ||
		!restored.SupportsDiffInspection || !restored.SupportsPause || !restored.SupportsCancel || !restored.SupportsResume {
		t.Errorf("FromBitmask boolean fields missing for Level 2 mask: %+v", restored)
	}

	if restored.ToBitmask() != Level2RequiredMask {
		t.Errorf("round-trip ToBitmask mismatch: got 0x%x, want 0x%x", restored.ToBitmask(), Level2RequiredMask)
	}
}

func TestLevel1_RequiresCapAdviceDelivery(t *testing.T) {
	// EventStream + ToolInspection without CapAdviceDelivery must not achieve Level 1 (#65).
	withoutAdvice := CapabilityManifest{
		SupportsEventStream:    true,
		SupportsToolInspection: true,
		SupportsAdviceDelivery: false,
	}
	if got := EvaluateAchievableLevel(&withoutAdvice); got >= 1 {
		t.Fatalf("L1 without CapAdviceDelivery: EvaluateAchievableLevel = %d, want < 1", got)
	}

	req := &HandshakeRequest{
		SessionID:      "sess-no-advice",
		RequestedLevel: 1,
		Manifest:       withoutAdvice,
	}
	resp, err := NegotiateLevel(req)
	if err != nil {
		t.Fatalf("NegotiateLevel unexpected error: %v", err)
	}
	if resp.NegotiatedLevel >= 1 {
		t.Errorf("L1 request without CapAdviceDelivery should degrade to level < 1, got %d", resp.NegotiatedLevel)
	}
	if !resp.IsDegraded || resp.DegradedFrom != 1 {
		t.Errorf("expected degraded from 1, got %+v", resp)
	}
	found := false
	for _, f := range resp.MissingFlags {
		if f == "CapAdviceDelivery" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("MissingFlags should include CapAdviceDelivery, got %v", resp.MissingFlags)
	}

	// Full Level1 mask roundtrip
	m := FromBitmask(Level1RequiredMask)
	if m.ToBitmask() != Level1RequiredMask {
		t.Errorf("Level1RequiredMask roundtrip: got 0x%x, want 0x%x", m.ToBitmask(), Level1RequiredMask)
	}
	if !m.SupportsAdviceDelivery || !m.HasCapability(CapAdviceDelivery) {
		t.Errorf("FromBitmask(Level1RequiredMask) must set CapAdviceDelivery")
	}
	if EvaluateAchievableLevel(&m) != 1 {
		t.Errorf("Level1RequiredMask achievable level = %d, want 1", EvaluateAchievableLevel(&m))
	}
}

func TestDeliveryCaps_BitmaskRoundTrip(t *testing.T) {
	flags := []CapabilityFlag{
		CapAdviceDelivery, CapContextInjection, CapToolGate, CapTurnBoundary, CapInterventionAck,
	}
	var mask uint64
	for _, f := range flags {
		mask |= uint64(f)
	}
	m := FromBitmask(mask)
	if m.ToBitmask() != mask {
		t.Fatalf("delivery caps roundtrip: got 0x%x, want 0x%x", m.ToBitmask(), mask)
	}
	if !m.SupportsAdviceDelivery || !m.SupportsContextInjection || !m.SupportsToolGate ||
		!m.SupportsTurnBoundary || !m.SupportsInterventionAck {
		t.Fatalf("delivery bools incomplete after FromBitmask: %+v", m)
	}
	for _, f := range flags {
		if !m.HasCapability(f) {
			t.Errorf("HasCapability(%s) = false", f.String())
		}
	}
}

func TestEvaluateAchievableLevel_Contracts(t *testing.T) {
	tests := []struct {
		name     string
		manifest *CapabilityManifest
		want     int
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			want:     -1,
		},
		{
			name: "zero manifest (no booleans set)",
			manifest: &CapabilityManifest{
				IntegrationLevel: 0,
			},
			want: -1, // Without SupportsEventStream, achievable level is -1
		},
		{
			name: "IntegrationLevel 3 with no booleans set",
			manifest: &CapabilityManifest{
				IntegrationLevel: 3,
			},
			want: -1, // Zero auto-granting
		},
		{
			name: "Level 0 Observe (SupportsEventStream only)",
			manifest: &CapabilityManifest{
				SupportsEventStream: true,
			},
			want: 0,
		},
		{
			name: "Level 1 Advisory Contract (EventStream + ToolInspection + AdviceDelivery WITHOUT process control)",
			manifest: &CapabilityManifest{
				SupportsEventStream:    true,
				SupportsToolInspection: true,
				SupportsAdviceDelivery: true,
				SupportsPause:          false,
				SupportsCancel:         false,
				SupportsResume:         false,
			},
			want: 1, // Advisory mode achieves Level 1 without requiring Pause/Cancel/Resume
		},
		{
			name: "Level 1 without CapAdviceDelivery degrades below Advisory",
			manifest: &CapabilityManifest{
				SupportsEventStream:    true,
				SupportsToolInspection: true,
				SupportsAdviceDelivery: false,
			},
			want: 0,
		},
		{
			name: "Level 2 Guarded Contract (EventStream + Tool + Advice + Diff + Pause + Cancel + Resume + Checkpoint + Rollback)",
			manifest: &CapabilityManifest{
				SupportsEventStream:    true,
				SupportsToolInspection: true,
				SupportsAdviceDelivery: true,
				SupportsDiffInspection: true,
				SupportsPause:          true,
				SupportsCancel:         true,
				SupportsResume:         true,
				SupportsCheckpoint:     true,
				SupportsRollback:       true,
			},
			want: 2,
		},
		{
			name: "Missing CapPause degrades Guarded candidate to Level 1",
			manifest: &CapabilityManifest{
				SupportsEventStream:    true,
				SupportsToolInspection: true,
				SupportsAdviceDelivery: true,
				SupportsDiffInspection: true,
				SupportsPause:          false, // missing required Level 2 process control flag
				SupportsCancel:         true,
				SupportsResume:         true,
				SupportsCheckpoint:     true,
				SupportsRollback:       true,
			},
			want: 1,
		},
		{
			name: "Level 3 Full Control Contract",
			manifest: &CapabilityManifest{
				SupportsEventStream:    true,
				SupportsToolInspection: true,
				SupportsAdviceDelivery: true,
				SupportsDiffInspection: true,
				SupportsPause:          true,
				SupportsCancel:         true,
				SupportsResume:         true,
				SupportsCheckpoint:     true,
				SupportsRollback:       true,
				SupportsHeadless:       true,
				SupportsCLIControl:     true,
				SupportsMCP:            true,
				SupportsSubagents:      true,
				SupportsSwitchModel:    true,
			},
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateAchievableLevel(tt.manifest)
			if got != tt.want {
				t.Errorf("EvaluateAchievableLevel() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNegotiateLevel_Matrix(t *testing.T) {
	level0Manifest := CapabilityManifest{
		SupportsEventStream: true,
	}

	level1Manifest := CapabilityManifest{
		SupportsEventStream:    true,
		SupportsToolInspection: true,
		SupportsAdviceDelivery: true,
	}

	level3Manifest := FromBitmask(Level3RequiredMask)

	tests := []struct {
		name     string
		req      *HandshakeRequest
		wantResp *HandshakeResponse
		wantErr  bool
	}{
		{
			name: "level 0 exact match",
			req: &HandshakeRequest{
				SessionID:      "session-00",
				RequestedLevel: 0,
				Manifest:       level0Manifest,
			},
			wantResp: &HandshakeResponse{
				SessionID:       "session-00",
				NegotiatedLevel: 0,
				IsDegraded:      false,
				DegradedFrom:    0,
				MissingFlags:    nil,
			},
			wantErr: false,
		},
		{
			name: "level 3 exact match",
			req: &HandshakeRequest{
				SessionID:      "session-03",
				RequestedLevel: 3,
				Manifest:       level3Manifest,
			},
			wantResp: &HandshakeResponse{
				SessionID:       "session-03",
				NegotiatedLevel: 3,
				IsDegraded:      false,
				DegradedFrom:    0,
				MissingFlags:    nil,
			},
			wantErr: false,
		},
		{
			name: "over-capable agent requesting level 1 with level 3 manifest",
			req: &HandshakeRequest{
				SessionID:      "session-overcapable",
				RequestedLevel: 1,
				Manifest:       level3Manifest,
			},
			wantResp: &HandshakeResponse{
				SessionID:       "session-overcapable",
				NegotiatedLevel: 1,
				IsDegraded:      false,
				DegradedFrom:    0,
				MissingFlags:    nil,
			},
			wantErr: false,
		},
		{
			name: "degradation from level 3 request to level 1 achievable",
			req: &HandshakeRequest{
				SessionID:      "session-degrade-3-to-1",
				RequestedLevel: 3,
				Manifest:       level1Manifest,
			},
			wantResp: &HandshakeResponse{
				SessionID:       "session-degrade-3-to-1",
				NegotiatedLevel: 1,
				IsDegraded:      true,
				DegradedFrom:    3,
				MissingFlags: []string{
					"CapDiffInspection",
					"CapHeadless",
					"CapCLIControl",
					"CapPause",
					"CapCancel",
					"CapResume",
					"CapCheckpoint",
					"CapRollback",
					"CapMCP",
					"CapSubagents",
					"CapSwitchModel",
				},
			},
			wantErr: false,
		},
		{
			name: "total degradation from level 3 request to level 0 achievable",
			req: &HandshakeRequest{
				SessionID:      "session-degrade-3-to-0",
				RequestedLevel: 3,
				Manifest:       level0Manifest,
			},
			wantResp: &HandshakeResponse{
				SessionID:       "session-degrade-3-to-0",
				NegotiatedLevel: 0,
				IsDegraded:      true,
				DegradedFrom:    3,
				MissingFlags: []string{
					"CapToolInspection",
					"CapDiffInspection",
					"CapHeadless",
					"CapCLIControl",
					"CapPause",
					"CapCancel",
					"CapResume",
					"CapCheckpoint",
					"CapRollback",
					"CapMCP",
					"CapSubagents",
					"CapSwitchModel",
					"CapAdviceDelivery",
				},
			},
			wantErr: false,
		},
		{
			name: "unsupported agent due to missing required boolean fields",
			req: &HandshakeRequest{
				SessionID:      "session-missing-all",
				RequestedLevel: 0,
				Manifest:       CapabilityManifest{IntegrationLevel: 1}, // no booleans set
			},
			wantResp: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := NegotiateLevel(tt.req)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NegotiateLevel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if !reflect.DeepEqual(resp, tt.wantResp) {
					t.Errorf("NegotiateLevel() response mismatch:\nGot:  %+v\nWant: %+v", resp, tt.wantResp)
				}
			} else {
				if err != ErrUnsupportedAgent {
					t.Errorf("expected ErrUnsupportedAgent, got %v", err)
				}
			}
		})
	}
}

func TestNegotiateLevel_EdgeCases(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		_, err := NegotiateLevel(nil)
		if err == nil {
			t.Fatal("expected error for nil request, got nil")
		}
		if err.Error() != "handshake request cannot be nil" {
			t.Errorf("unexpected error string: %v", err)
		}
	})

	t.Run("empty session ID", func(t *testing.T) {
		req := &HandshakeRequest{
			SessionID:      "",
			RequestedLevel: 1,
			Manifest:       FromBitmask(Level1RequiredMask),
		}
		_, err := NegotiateLevel(req)
		if err == nil {
			t.Fatal("expected error for empty session_id, got nil")
		}
		if err.Error() != "session_id cannot be empty" {
			t.Errorf("unexpected error string: %v", err)
		}
	})

	t.Run("invalid requested level negative", func(t *testing.T) {
		req := &HandshakeRequest{
			SessionID:      "sess-invalid",
			RequestedLevel: -1,
			Manifest:       FromBitmask(Level1RequiredMask),
		}
		_, err := NegotiateLevel(req)
		if err == nil {
			t.Fatal("expected error for negative requested level, got nil")
		}
	})

	t.Run("invalid requested level overflow", func(t *testing.T) {
		req := &HandshakeRequest{
			SessionID:      "sess-invalid",
			RequestedLevel: 4,
			Manifest:       FromBitmask(Level1RequiredMask),
		}
		_, err := NegotiateLevel(req)
		if err == nil {
			t.Fatal("expected error for requested level 4, got nil")
		}
	})
}

func TestNegotiateLevel_ConcurrentRace(t *testing.T) {
	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()

			level := id % 4
			req := &HandshakeRequest{
				SessionID:      "concurrent-session",
				RequestedLevel: level,
				Manifest:       FromBitmask(Level3RequiredMask),
			}

			resp, err := NegotiateLevel(req)
			if err != nil {
				t.Errorf("goroutine %d NegotiateLevel error: %v", id, err)
				return
			}

			if resp.SessionID != "concurrent-session" {
				t.Errorf("goroutine %d session ID mismatch: %s", id, resp.SessionID)
			}
		}(i)
	}

	wg.Wait()
}

func TestCapability_JSONRoundTrip_Lossless_Explicit(t *testing.T) {
	t.Run("All boolean capability flags set to true", func(t *testing.T) {
		manifest := CapabilityManifest{
			AgentID:                  "agent-full-25",
			Version:                  "1.0.0",
			IntegrationLevel:         3,
			SupportsEventStream:      true,
			SupportsToolInspection:   true,
			SupportsDiffInspection:   true,
			SupportsCostTracking:     true,
			SupportsHooks:            true,
			SupportsHeadless:         true,
			SupportsCLIControl:       true,
			SupportsPause:            true,
			SupportsCancel:           true,
			SupportsResume:           true,
			SupportsCheckpoint:       true,
			SupportsRollback:         true,
			SupportsMCP:              true,
			SupportsSubagents:        true,
			SupportsExtensions:       true,
			SupportsSwitchModel:      true,
			SupportsCustomProvider:   true,
			SupportsOpenAICompat:     true,
			SupportsLocalModels:      true,
			SupportsSDK:              true,
			SupportsAdviceDelivery:   true,
			SupportsContextInjection: true,
			SupportsToolGate:         true,
			SupportsTurnBoundary:     true,
			SupportsInterventionAck:  true,
		}

		jsonBytes, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var restored CapabilityManifest
		if err := json.Unmarshal(jsonBytes, &restored); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		if restored.AgentID != manifest.AgentID || restored.Version != manifest.Version || restored.IntegrationLevel != manifest.IntegrationLevel {
			t.Errorf("Header fields mismatch: got %+v, want %+v", restored, manifest)
		}

		bools := map[string]bool{
			"SupportsEventStream":      restored.SupportsEventStream,
			"SupportsToolInspection":   restored.SupportsToolInspection,
			"SupportsDiffInspection":   restored.SupportsDiffInspection,
			"SupportsCostTracking":     restored.SupportsCostTracking,
			"SupportsHooks":            restored.SupportsHooks,
			"SupportsHeadless":         restored.SupportsHeadless,
			"SupportsCLIControl":       restored.SupportsCLIControl,
			"SupportsPause":            restored.SupportsPause,
			"SupportsCancel":           restored.SupportsCancel,
			"SupportsResume":           restored.SupportsResume,
			"SupportsCheckpoint":       restored.SupportsCheckpoint,
			"SupportsRollback":         restored.SupportsRollback,
			"SupportsMCP":              restored.SupportsMCP,
			"SupportsSubagents":        restored.SupportsSubagents,
			"SupportsExtensions":       restored.SupportsExtensions,
			"SupportsSwitchModel":      restored.SupportsSwitchModel,
			"SupportsCustomProvider":   restored.SupportsCustomProvider,
			"SupportsOpenAICompat":     restored.SupportsOpenAICompat,
			"SupportsLocalModels":      restored.SupportsLocalModels,
			"SupportsSDK":              restored.SupportsSDK,
			"SupportsAdviceDelivery":   restored.SupportsAdviceDelivery,
			"SupportsContextInjection": restored.SupportsContextInjection,
			"SupportsToolGate":         restored.SupportsToolGate,
			"SupportsTurnBoundary":     restored.SupportsTurnBoundary,
			"SupportsInterventionAck":  restored.SupportsInterventionAck,
		}

		for name, val := range bools {
			if !val {
				t.Errorf("Field %s lost during JSON round-trip: got false, want true", name)
			}
		}

		expectedMask := CapabilityKnownMask
		if restored.ToBitmask() != expectedMask {
			t.Errorf("Bitmask mismatch after round-trip: got 0x%x, want 0x%x", restored.ToBitmask(), expectedMask)
		}
	})

	t.Run("Alternating boolean flags pattern", func(t *testing.T) {
		manifest := CapabilityManifest{
			AgentID:                  "agent-alt-25",
			Version:                  "2.0.0",
			IntegrationLevel:         1,
			SupportsEventStream:      true,  // Bit 0
			SupportsToolInspection:   false, // Bit 1
			SupportsDiffInspection:   true,  // Bit 2
			SupportsCostTracking:     false, // Bit 3
			SupportsHooks:            true,  // Bit 4
			SupportsHeadless:         false, // Bit 5
			SupportsCLIControl:       true,  // Bit 6
			SupportsPause:            false, // Bit 7
			SupportsCancel:           true,  // Bit 8
			SupportsResume:           false, // Bit 9
			SupportsCheckpoint:       true,  // Bit 10
			SupportsRollback:         false, // Bit 11
			SupportsMCP:              true,  // Bit 12
			SupportsSubagents:        false, // Bit 13
			SupportsExtensions:       true,  // Bit 14
			SupportsSwitchModel:      false, // Bit 15
			SupportsCustomProvider:   true,  // Bit 16
			SupportsOpenAICompat:     false, // Bit 17
			SupportsLocalModels:      true,  // Bit 18
			SupportsSDK:              false, // Bit 19
			SupportsAdviceDelivery:   true,  // Bit 20
			SupportsContextInjection: false, // Bit 21
			SupportsToolGate:         true,  // Bit 22
			SupportsTurnBoundary:     false, // Bit 23
			SupportsInterventionAck:  true,  // Bit 24
		}

		jsonBytes, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		var restored CapabilityManifest
		if err := json.Unmarshal(jsonBytes, &restored); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		if restored.ToBitmask() != manifest.ToBitmask() {
			t.Errorf("Bitmask mismatch for alternating pattern: got 0x%x, want 0x%x", restored.ToBitmask(), manifest.ToBitmask())
		}
	})
}

func TestChallenger_BoundaryBitmasks(t *testing.T) {
	tests := []struct {
		name      string
		mask      uint64
		checkBit  int
		wantHas   bool
		wantLevel int
		wantMask  uint64
	}{
		{
			name:      "Zero_bitmask",
			mask:      0,
			checkBit:  0,
			wantHas:   false,
			wantLevel: -1,
			wantMask:  0,
		},
		{
			name:      "Full_uint64_bitmask_(all_bits_set)_bit19",
			mask:      0xFFFFFFFFFFFFFFFF,
			checkBit:  19,
			wantHas:   true,
			wantLevel: 3,
			wantMask:  CapabilityKnownMask,
		},
		{
			name:      "Full_uint64_bitmask_(all_bits_set)_bit24",
			mask:      0xFFFFFFFFFFFFFFFF,
			checkBit:  24,
			wantHas:   true,
			wantLevel: 3,
			wantMask:  CapabilityKnownMask,
		},
		{
			name:      "Full_uint64_bitmask_(all_bits_set)_bit63",
			mask:      0xFFFFFFFFFFFFFFFF,
			checkBit:  63,
			wantHas:   false,
			wantLevel: 3,
			wantMask:  CapabilityKnownMask,
		},
		{
			name:      "Bit_19_only_(CapSDK)",
			mask:      1 << 19,
			checkBit:  19,
			wantHas:   true,
			wantLevel: -1,
			wantMask:  1 << 19,
		},
		{
			name:      "Bit_20_only_(CapAdviceDelivery)",
			mask:      1 << 20,
			checkBit:  20,
			wantHas:   true,
			wantLevel: -1,
			wantMask:  1 << 20,
		},
		{
			name:      "Bit_25_(undefined_flag)",
			mask:      1 << 25,
			checkBit:  25,
			wantHas:   false,
			wantLevel: -1,
			wantMask:  0,
		},
		{
			name:      "Bit_63_(highest_uint64_bit)",
			mask:      1 << 63,
			checkBit:  63,
			wantHas:   false,
			wantLevel: -1,
			wantMask:  0,
		},
		{
			name:      "Level_1_required_mask_minus_CapToolInspection",
			mask:      Level1RequiredMask &^ uint64(CapToolInspection),
			checkBit:  0, // CapEventStream remains set
			wantHas:   true,
			wantLevel: 0,
			wantMask:  Level1RequiredMask &^ uint64(CapToolInspection),
		},
		{
			name:      "Level_1_required_mask_minus_CapAdviceDelivery",
			mask:      Level1RequiredMask &^ uint64(CapAdviceDelivery),
			checkBit:  0,
			wantHas:   true,
			wantLevel: 0,
			wantMask:  Level1RequiredMask &^ uint64(CapAdviceDelivery),
		},
		{
			name:      "Level_2_required_mask_minus_CapPause",
			mask:      Level2RequiredMask &^ uint64(CapPause),
			checkBit:  2,
			wantHas:   true,
			wantLevel: 1,
			wantMask:  Level2RequiredMask &^ uint64(CapPause),
		},
		{
			name:      "Level_3_required_mask_minus_CapSwitchModel",
			mask:      Level3RequiredMask &^ uint64(CapSwitchModel),
			checkBit:  5,
			wantHas:   true,
			wantLevel: 2,
			wantMask:  Level3RequiredMask &^ uint64(CapSwitchModel),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := FromBitmask(tt.mask)
			gotLevel := EvaluateAchievableLevel(&m)
			if gotLevel != tt.wantLevel {
				t.Errorf("EvaluateAchievableLevel = %d, want %d", gotLevel, tt.wantLevel)
			}
			flag := CapabilityFlag(1 << uint(tt.checkBit))
			gotHas := m.HasCapability(flag)
			if gotHas != tt.wantHas {
				t.Errorf("HasCapability(shift %d) = %v, want %v", tt.checkBit, gotHas, tt.wantHas)
			}
			if m.ToBitmask() != tt.wantMask {
				t.Errorf("ToBitmask() = 0x%x, want 0x%x", m.ToBitmask(), tt.wantMask)
			}
		})
	}
}

func TestFromBitmask_PublicMutationRevokesCapability(t *testing.T) {
	manifest := FromBitmask(Level3RequiredMask)

	// Verify rollback is initially present
	if !manifest.HasCapability(CapRollback) {
		t.Fatal("expected CapRollback to be set from Level3RequiredMask")
	}

	// Revoke rollback via public field
	manifest.SupportsRollback = false

	// ToBitmask and HasCapability MUST reflect the revocation
	if manifest.HasCapability(CapRollback) {
		t.Fatal("CapRollback should be revoked after setting SupportsRollback=false")
	}

	mask := manifest.ToBitmask()
	if (mask & uint64(CapRollback)) != 0 {
		t.Fatal("ToBitmask should not include CapRollback after revocation")
	}
}
