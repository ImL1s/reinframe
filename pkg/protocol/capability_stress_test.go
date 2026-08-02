package protocol

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestChallengerR2_HighConcurrencyNegotiation stress tests NegotiateLevel under heavy parallel load.
func TestChallengerR2_HighConcurrencyNegotiation(t *testing.T) {
	const goroutines = 500
	const iterations = 200

	var wg sync.WaitGroup
	var successCount int64
	var errCount int64

	masks := []uint64{
		0,
		Level0RequiredMask,
		Level1RequiredMask,
		Level2RequiredMask,
		Level3RequiredMask,
		0xFFFFFFFFFFFFFFFF,
		Level3RequiredMask &^ uint64(CapSwitchModel),
		Level2RequiredMask &^ uint64(CapPause),
		Level1RequiredMask &^ uint64(CapPause),
	}

	start := time.Now()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(gid)))

			for i := 0; i < iterations; i++ {
				mask := masks[rng.Intn(len(masks))]
				reqLevel := rng.Intn(6) - 1 // -1 to 4

				manifest := FromBitmask(mask)
				req := &HandshakeRequest{
					SessionID:      fmt.Sprintf("sess-concurrent-%d-%d", gid, i),
					RequestedLevel: reqLevel,
					Manifest:       manifest,
				}

				resp, err := NegotiateLevel(req)
				if err != nil {
					atomic.AddInt64(&errCount, 1)
					// Expect errors for invalid requested levels (-1 or 4) or unachievable level (-1)
					achievable := EvaluateAchievableLevelFromMask(mask)
					if reqLevel >= 0 && reqLevel <= 3 && achievable >= 0 {
						t.Errorf("Unexpected error for valid req: gid=%d, iter=%d, err=%v", gid, i, err)
					}
				} else {
					atomic.AddInt64(&successCount, 1)
					if resp.SessionID != req.SessionID {
						t.Errorf("SessionID mismatch: got %s, want %s", resp.SessionID, req.SessionID)
					}
				}

				// Check thread-safety of ToBitmask and HasCapability
				_ = manifest.ToBitmask()
				_ = manifest.HasCapability(CapEventStream)
			}
		}(g)
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("High concurrency negotiation completed: %d workers, %d ops, %d success, %d expected/handshake errors in %v",
		goroutines, goroutines*iterations, successCount, errCount, elapsed)
}

// TestChallengerR2_All64BitsBitmaskMatrix asserts lossless roundtrip bitmask conversion across all 64 uint64 bits.
func TestChallengerR2_All64BitsBitmaskMatrix(t *testing.T) {
	for bit := 0; bit < 64; bit++ {
		mask := uint64(1) << uint(bit)
		t.Run(fmt.Sprintf("Bit_%d", bit), func(t *testing.T) {
			manifest := FromBitmask(mask)
			reconstructed := manifest.ToBitmask()

			expected := mask
			if bit >= CapabilityFlagCount {
				expected = 0
			}
			if reconstructed != expected {
				t.Fatalf("Bit %d mismatch: got 0x%x, want 0x%x", bit, reconstructed, expected)
			}

			flag := CapabilityFlag(mask)
			if bit < CapabilityFlagCount {
				if !manifest.HasCapability(flag) {
					t.Errorf("Bit %d: HasCapability(flag) returned false, expected true", bit)
				}
			} else {
				if manifest.HasCapability(flag) {
					t.Errorf("Bit %d: HasCapability(flag) returned true, expected false for unassigned bit", bit)
				}
			}

			// Verify other bit positions return false
			otherBit := (bit + 1) % 64
			otherFlag := CapabilityFlag(uint64(1) << uint(otherBit))
			if manifest.HasCapability(otherFlag) {
				t.Errorf("Bit %d: HasCapability(otherBit %d) returned true, expected false", bit, otherBit)
			}
		})
	}
}

// TestChallengerR2_DegradationAndMissingFlagsExactness checks exact missing flags during degradation.
func TestChallengerR2_DegradationAndMissingFlagsExactness(t *testing.T) {
	tests := []struct {
		name             string
		mask             uint64
		requestedLevel   int
		wantDegradedFrom int
		wantNegotiated   int
		wantMissing      []string
	}{
		{
			name:             "Degrade_Level3_to_Level2",
			mask:             Level2RequiredMask,
			requestedLevel:   3,
			wantDegradedFrom: 3,
			wantNegotiated:   2,
			wantMissing: []string{
				"CapHeadless",
				"CapCLIControl",
				"CapCheckpoint",
				"CapRollback",
				"CapMCP",
				"CapSubagents",
				"CapSwitchModel",
			},
		},
		{
			name:             "Degrade_Level3_to_Level1",
			mask:             Level1RequiredMask,
			requestedLevel:   3,
			wantDegradedFrom: 3,
			wantNegotiated:   1,
			wantMissing: []string{
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
		{
			name:             "Degrade_Level2_to_Level1",
			mask:             Level1RequiredMask,
			requestedLevel:   2,
			wantDegradedFrom: 2,
			wantNegotiated:   1,
			wantMissing: []string{
				"CapDiffInspection",
				"CapPause",
				"CapCancel",
				"CapResume",
			},
		},
		{
			name:             "Degrade_Level1_to_Level0",
			mask:             Level0RequiredMask,
			requestedLevel:   1,
			wantDegradedFrom: 1,
			wantNegotiated:   0,
			wantMissing: []string{
				"CapToolInspection",
				"CapAdviceDelivery",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &HandshakeRequest{
				SessionID:      "sess-degrade-test",
				RequestedLevel: tt.requestedLevel,
				Manifest:       FromBitmask(tt.mask),
			}

			resp, err := NegotiateLevel(req)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !resp.IsDegraded {
				t.Errorf("Expected IsDegraded true, got false")
			}
			if resp.DegradedFrom != tt.wantDegradedFrom {
				t.Errorf("DegradedFrom = %d, want %d", resp.DegradedFrom, tt.wantDegradedFrom)
			}
			if resp.NegotiatedLevel != tt.wantNegotiated {
				t.Errorf("NegotiatedLevel = %d, want %d", resp.NegotiatedLevel, tt.wantNegotiated)
			}
			if !reflect.DeepEqual(resp.MissingFlags, tt.wantMissing) {
				t.Errorf("MissingFlags mismatch:\nGot:  %+v\nWant: %+v", resp.MissingFlags, tt.wantMissing)
			}
		})
	}
}

// TestChallengerR2_HighBitsInterferenceCheck verifies unassigned high bits (25-63) do not alter supervision level logic.
func TestChallengerR2_HighBitsInterferenceCheck(t *testing.T) {
	highBits := uint64(0xFFFFFFFFFE000000) // bits 25..63 set to 1 (bits 0-24 are defined flags)

	baseCases := []struct {
		name      string
		baseMask  uint64
		wantLevel int
	}{
		{"Zero_with_high_bits", 0, -1},
		{"Level0_with_high_bits", Level0RequiredMask, 0},
		{"Level1_with_high_bits", Level1RequiredMask, 1},
		{"Level2_with_high_bits", Level2RequiredMask, 2},
		{"Level3_with_high_bits", Level3RequiredMask, 3},
	}

	for _, bc := range baseCases {
		t.Run(bc.name, func(t *testing.T) {
			combinedMask := bc.baseMask | highBits
			manifest := FromBitmask(combinedMask)

			achievable := EvaluateAchievableLevel(&manifest)
			if achievable != bc.wantLevel {
				t.Errorf("Level mismatch for %s: got %d, want %d", bc.name, achievable, bc.wantLevel)
			}

			if manifest.ToBitmask() != bc.baseMask {
				t.Errorf("ToBitmask mismatch for %s: got 0x%x, want 0x%x", bc.name, manifest.ToBitmask(), bc.baseMask)
			}
		})
	}
}

// TestChallengerR2_ZeroBitmaskJSONSchemaViolation demonstrates that FromBitmask(0) produces integration_level=-1, violating JSON schema minimum 0 constraint.
func TestChallengerR2_ZeroBitmaskJSONSchemaViolation(t *testing.T) {
	if err := LoadSchemas(); err != nil {
		t.Fatalf("LoadSchemas failed: %v", err)
	}

	manifest := FromBitmask(0)
	manifest.AgentID = "agent-zero"
	manifest.Version = "1.0.0"

	jsonBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal failed for mask 0: %v", err)
	}

	// FromBitmask(0) sets IntegrationLevel = -1.
	// JSON schema requires minimum: 0 for integration_level.
	// Therefore, ValidateEvent fails on integration_level=-1.
	err = ValidateEvent(jsonBytes, "capability_manifest")
	if err == nil {
		t.Errorf("Expected JSON schema validation error for FromBitmask(0) due to integration_level=-1, got nil")
	} else {
		t.Logf("Observed expected JSON schema violation for FromBitmask(0): %v", err)
	}
}

// TestChallengerR2_ValidBitmaskJSONSchemaCompliance verifies valid capability bitmasks pass JSON schema validation cleanly.
func TestChallengerR2_ValidBitmaskJSONSchemaCompliance(t *testing.T) {
	if err := LoadSchemas(); err != nil {
		t.Fatalf("LoadSchemas failed: %v", err)
	}

	validMasks := []uint64{
		Level0RequiredMask,
		Level1RequiredMask,
		Level2RequiredMask,
		Level3RequiredMask,
		0xFFFFFFFFFFFFFFFF,
	}

	for i, mask := range validMasks {
		manifest := FromBitmask(mask)
		manifest.AgentID = fmt.Sprintf("agent-valid-%d", i)
		manifest.Version = "1.0.0"

		jsonBytes, err := json.Marshal(manifest)
		if err != nil {
			t.Fatalf("json.Marshal failed for mask 0x%x: %v", mask, err)
		}

		if err := ValidateEvent(jsonBytes, "capability_manifest"); err != nil {
			t.Errorf("ValidateEvent failed for valid mask 0x%x (JSON: %s): %v", mask, string(jsonBytes), err)
		}
	}
}

// TestChallengerR2_JSONRoundtripLevelEvaluation verifies JSON serialized & unmarshaled manifests evaluate levels consistently.
func TestChallengerR2_JSONRoundtripLevelEvaluation(t *testing.T) {
	masks := []uint64{
		Level0RequiredMask,
		Level1RequiredMask,
		Level2RequiredMask,
		Level3RequiredMask,
	}

	for _, mask := range masks {
		m1 := FromBitmask(mask)
		m1.AgentID = "test-agent"
		m1.Version = "2.0"

		jsonBytes, err := json.Marshal(m1)
		if err != nil {
			t.Fatalf("Marshal failed for mask 0x%x: %v", mask, err)
		}

		var m2 CapabilityManifest
		if err := json.Unmarshal(jsonBytes, &m2); err != nil {
			t.Fatalf("Unmarshal failed for mask 0x%x: %v", mask, err)
		}

		level1 := EvaluateAchievableLevel(&m1)
		level2 := EvaluateAchievableLevel(&m2)

		if level1 != level2 {
			t.Errorf("Level mismatch post JSON roundtrip for mask 0x%x: before=%d, after=%d", mask, level1, level2)
		}
	}
}
