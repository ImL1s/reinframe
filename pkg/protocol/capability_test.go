package protocol

import (
	"reflect"
	"sync"
	"testing"
)

func TestCapabilityFlag_ConstantsAndStringer(t *testing.T) {
	expectedFlags := map[CapabilityFlag]struct {
		shift int
		name  string
	}{
		CapEventStream:    {0, "CapEventStream"},
		CapToolInspection: {1, "CapToolInspection"},
		CapDiffInspection: {2, "CapDiffInspection"},
		CapCostTracking:   {3, "CapCostTracking"},
		CapHooks:          {4, "CapHooks"},
		CapHeadless:       {5, "CapHeadless"},
		CapCLIControl:     {6, "CapCLIControl"},
		CapPause:          {7, "CapPause"},
		CapCancel:         {8, "CapCancel"},
		CapResume:         {9, "CapResume"},
		CapCheckpoint:     {10, "CapCheckpoint"},
		CapRollback:       {11, "CapRollback"},
		CapMCP:            {12, "CapMCP"},
		CapSubagents:      {13, "CapSubagents"},
		CapExtensions:     {14, "CapExtensions"},
		CapSwitchModel:    {15, "CapSwitchModel"},
		CapCustomProvider: {16, "CapCustomProvider"},
		CapOpenAICompat:   {17, "CapOpenAICompat"},
		CapLocalModels:    {18, "CapLocalModels"},
		CapSDK:            {19, "CapSDK"},
	}

	if len(expectedFlags) != 20 {
		t.Fatalf("expected 20 capability flags, got %d", len(expectedFlags))
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

func TestCapabilityManifest_BitmaskHelpers(t *testing.T) {
	manifest := CapabilityManifest{
		IntegrationLevel:   1,
		SupportsPause:      true,
		SupportsCancel:     true,
		SupportsResume:     true,
		SupportsCheckpoint: false,
		SupportsRollback:   false,
		SupportsMCP:        false,
	}

	mask := manifest.ToBitmask()
	expectedMask := Level1RequiredMask
	if mask != expectedMask {
		t.Fatalf("ToBitmask() mismatch: got 0x%x, want 0x%x", mask, expectedMask)
	}

	if !manifest.HasCapability(CapPause) {
		t.Errorf("expected HasCapability(CapPause) to be true")
	}
	if !manifest.HasCapability(CapEventStream) {
		t.Errorf("expected HasCapability(CapEventStream) to be true")
	}
	if manifest.HasCapability(CapCheckpoint) {
		t.Errorf("expected HasCapability(CapCheckpoint) to be false")
	}

	restored := FromBitmask(Level2RequiredMask)
	if restored.IntegrationLevel != 2 {
		t.Errorf("FromBitmask IntegrationLevel mismatch: got %d, want 2", restored.IntegrationLevel)
	}
	if !restored.SupportsCheckpoint || !restored.SupportsRollback {
		t.Errorf("FromBitmask boolean fields missing for Level 2 mask")
	}
	if restored.ToBitmask() != Level2RequiredMask {
		t.Errorf("round-trip ToBitmask mismatch: got 0x%x, want 0x%x", restored.ToBitmask(), Level2RequiredMask)
	}
}

func TestEvaluateAchievableLevel(t *testing.T) {
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
			name: "zero manifest",
			manifest: &CapabilityManifest{
				IntegrationLevel: 0,
			},
			want: 0,
		},
		{
			name: "level 1 manifest",
			manifest: &CapabilityManifest{
				IntegrationLevel: 1,
			},
			want: 1,
		},
		{
			name: "level 2 manifest",
			manifest: &CapabilityManifest{
				IntegrationLevel: 2,
			},
			want: 2,
		},
		{
			name: "level 3 manifest",
			manifest: &CapabilityManifest{
				IntegrationLevel: 3,
			},
			want: 3,
		},
		{
			name: "booleans without ToolInspection yields level 0",
			manifest: &CapabilityManifest{
				SupportsPause:  true,
				SupportsCancel: true,
				SupportsResume: true,
			},
			want: 0,
		},
		{
			name: "booleans with IntegrationLevel 1 yields level 1",
			manifest: &CapabilityManifest{
				IntegrationLevel: 1,
				SupportsPause:    true,
				SupportsCancel:   true,
				SupportsResume:   true,
			},
			want: 1,
		},
		{
			name: "booleans with IntegrationLevel 2 yields level 2",
			manifest: &CapabilityManifest{
				IntegrationLevel:   2,
				SupportsPause:      true,
				SupportsCancel:     true,
				SupportsResume:     true,
				SupportsCheckpoint: true,
				SupportsRollback:   true,
			},
			want: 2,
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
	tests := []struct {
		name         string
		req          *HandshakeRequest
		wantResp     *HandshakeResponse
		wantErr      bool
		errSubstring string
	}{
		{
			name: "level 0 exact match",
			req: &HandshakeRequest{
				SessionID:      "session-00",
				RequestedLevel: 0,
				Manifest: CapabilityManifest{
					IntegrationLevel: 0,
				},
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
				Manifest: CapabilityManifest{
					IntegrationLevel: 3,
				},
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
				Manifest: CapabilityManifest{
					IntegrationLevel: 3,
				},
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
			name: "degradation from 3 to 1",
			req: &HandshakeRequest{
				SessionID:      "session-degrade-3-to-1",
				RequestedLevel: 3,
				Manifest: CapabilityManifest{
					IntegrationLevel: 1,
				},
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
			name: "total degradation from 3 to 0",
			req: &HandshakeRequest{
				SessionID:      "session-degrade-3-to-0",
				RequestedLevel: 3,
				Manifest: CapabilityManifest{
					IntegrationLevel: 0,
				},
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
				},
			},
			wantErr: false,
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
			Manifest: CapabilityManifest{
				IntegrationLevel: 1,
			},
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
			Manifest: CapabilityManifest{
				IntegrationLevel: 1,
			},
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
			Manifest: CapabilityManifest{
				IntegrationLevel: 1,
			},
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
				Manifest: CapabilityManifest{
					IntegrationLevel: (id + 1) % 4,
				},
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

func TestChallenger_BoundaryBitmasks(t *testing.T) {
	tests := []struct {
		name      string
		mask      uint64
		checkBit  int
		wantHas   bool
		wantLevel int
	}{
		{
			name:      "Zero_bitmask",
			mask:      0,
			checkBit:  0,
			wantHas:   false,
			wantLevel: -1,
		},
		{
			name:      "Full_uint64_bitmask_(all_bits_set)_bit19",
			mask:      0xFFFFFFFFFFFFFFFF,
			checkBit:  19,
			wantHas:   true,
			wantLevel: 3,
		},
		{
			name:      "Full_uint64_bitmask_(all_bits_set)_bit63",
			mask:      0xFFFFFFFFFFFFFFFF,
			checkBit:  63,
			wantHas:   true,
			wantLevel: 3,
		},
		{
			name:      "Bit_19_only_(CapSDK)",
			mask:      1 << 19,
			checkBit:  19,
			wantHas:   true,
			wantLevel: -1,
		},
		{
			name:      "Bit_20_(undefined_flag)",
			mask:      1 << 20,
			checkBit:  20,
			wantHas:   true,
			wantLevel: -1,
		},
		{
			name:      "Bit_63_(highest_uint64_bit)",
			mask:      1 << 63,
			checkBit:  63,
			wantHas:   true,
			wantLevel: -1,
		},
		{
			name:      "Level_1_required_mask_minus_CapPause_(off-by-one_flag)",
			mask:      Level1RequiredMask &^ uint64(CapPause),
			checkBit:  1,
			wantHas:   true,
			wantLevel: 0,
		},
		{
			name:      "Level_2_required_mask_minus_CapCheckpoint",
			mask:      Level2RequiredMask &^ uint64(CapCheckpoint),
			checkBit:  2,
			wantHas:   true,
			wantLevel: 1,
		},
		{
			name:      "Level_3_required_mask_minus_CapSwitchModel_bit5",
			mask:      Level3RequiredMask &^ uint64(CapSwitchModel),
			checkBit:  5,
			wantHas:   true,
			wantLevel: 2,
		},
		{
			name:      "Level_3_required_mask_minus_CapSwitchModel_bit6",
			mask:      Level3RequiredMask &^ uint64(CapSwitchModel),
			checkBit:  6,
			wantHas:   true,
			wantLevel: 2,
		},
		{
			name:      "Level_3_required_mask_minus_CapSwitchModel_bit13",
			mask:      Level3RequiredMask &^ uint64(CapSwitchModel),
			checkBit:  13,
			wantHas:   true,
			wantLevel: 2,
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
			if m.ToBitmask() != tt.mask {
				t.Errorf("ToBitmask() = 0x%x, want 0x%x", m.ToBitmask(), tt.mask)
			}
		})
	}
}
