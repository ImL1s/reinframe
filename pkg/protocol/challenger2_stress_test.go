package protocol

import (
	"fmt"
	"math"
	"reflect"
	"sync"
	"testing"
)

// TestChallenger2_BitFlips systematically flips single bits across all required masks and unassigned bits.
func TestChallenger2_BitFlips(t *testing.T) {
	t.Run("Level3_SingleBitFlips", func(t *testing.T) {
		level3Flags := []CapabilityFlag{
			CapEventStream, CapToolInspection, CapDiffInspection, CapHeadless,
			CapCLIControl, CapPause, CapCancel, CapResume, CapCheckpoint,
			CapRollback, CapMCP, CapSubagents, CapSwitchModel,
		}

		for _, flag := range level3Flags {
			flippedMask := Level3RequiredMask &^ uint64(flag)
			m := FromBitmask(flippedMask)
			level := EvaluateAchievableLevel(&m)
			if level >= 3 {
				t.Errorf("expected level < 3 after clearing flag %s, got level %d", flag, level)
			}
		}
	})

	t.Run("Level2_SingleBitFlips", func(t *testing.T) {
		level2Flags := []CapabilityFlag{
			CapEventStream, CapToolInspection, CapDiffInspection,
			CapPause, CapCancel, CapResume, CapCheckpoint, CapRollback,
		}

		for _, flag := range level2Flags {
			flippedMask := Level2RequiredMask &^ uint64(flag)
			m := FromBitmask(flippedMask)
			level := EvaluateAchievableLevel(&m)
			if level >= 2 {
				t.Errorf("expected level < 2 after clearing flag %s, got level %d", flag, level)
			}
		}
	})

	t.Run("Level1_SingleBitFlips", func(t *testing.T) {
		level1Flags := []CapabilityFlag{
			CapEventStream, CapToolInspection, CapPause, CapCancel, CapResume,
		}

		for _, flag := range level1Flags {
			flippedMask := Level1RequiredMask &^ uint64(flag)
			m := FromBitmask(flippedMask)
			level := EvaluateAchievableLevel(&m)
			if level >= 1 {
				t.Errorf("expected level < 1 after clearing flag %s, got level %d", flag, level)
			}
		}
	})

	t.Run("CapEventStream_Cleared_Yields_Negative1", func(t *testing.T) {
		masksToTest := []uint64{
			Level3RequiredMask &^ uint64(CapEventStream),
			Level2RequiredMask &^ uint64(CapEventStream),
			Level1RequiredMask &^ uint64(CapEventStream),
			Level0RequiredMask &^ uint64(CapEventStream),
		}

		for i, mask := range masksToTest {
			m := FromBitmask(mask)
			level := EvaluateAchievableLevel(&m)
			if level != -1 {
				t.Errorf("case %d: expected level -1 when CapEventStream cleared, got %d", i, level)
			}
			req := &HandshakeRequest{
				SessionID:      fmt.Sprintf("sess-no-eventstream-%d", i),
				RequestedLevel: 0,
				Manifest:       m,
			}
			_, err := NegotiateLevel(req)
			if err != ErrUnsupportedAgent {
				t.Errorf("case %d: expected ErrUnsupportedAgent, got %v", i, err)
			}
		}
	})

	t.Run("UnassignedBits_DoNotAffectLevel", func(t *testing.T) {
		unassignedMask := uint64(0xFFFFFFFF00000000) // bits 32-63 set
		m3 := FromBitmask(Level3RequiredMask | unassignedMask)
		if EvaluateAchievableLevel(&m3) != 3 {
			t.Errorf("expected level 3 with unassigned high bits, got %d", EvaluateAchievableLevel(&m3))
		}
		m0 := FromBitmask(Level0RequiredMask | unassignedMask)
		if EvaluateAchievableLevel(&m0) != 0 {
			t.Errorf("expected level 0 with unassigned high bits, got %d", EvaluateAchievableLevel(&m0))
		}
	})
}

// TestChallenger2_ZeroMasks tests zero masks, zero struct instances, and negative integration levels.
func TestChallenger2_ZeroMasks(t *testing.T) {
	t.Run("RawZeroMask_Returns_Minus1_And_ErrUnsupportedAgent", func(t *testing.T) {
		m := FromBitmask(0)
		if m.ToBitmask() != 0 {
			t.Errorf("expected ToBitmask() to return 0, got 0x%x", m.ToBitmask())
		}
		if level := EvaluateAchievableLevel(&m); level != -1 {
			t.Errorf("expected EvaluateAchievableLevel to return -1 for zero bitmask, got %d", level)
		}

		req := &HandshakeRequest{
			SessionID:      "sess-zero-mask",
			RequestedLevel: 0,
			Manifest:       m,
		}
		resp, err := NegotiateLevel(req)
		if err != ErrUnsupportedAgent {
			t.Fatalf("expected ErrUnsupportedAgent for raw zero mask, got err=%v, resp=%+v", err, resp)
		}
	})

	t.Run("ZeroValueStruct_Returns_Level0", func(t *testing.T) {
		m := CapabilityManifest{} // IntegrationLevel: 0, hasRawBitmask: false
		if m.ToBitmask() != Level0RequiredMask {
			t.Errorf("expected ToBitmask() to return Level0RequiredMask (0x1), got 0x%x", m.ToBitmask())
		}
		if level := EvaluateAchievableLevel(&m); level != 0 {
			t.Errorf("expected EvaluateAchievableLevel to return 0 for struct zero value, got %d", level)
		}

		req := &HandshakeRequest{
			SessionID:      "sess-zero-struct",
			RequestedLevel: 0,
			Manifest:       m,
		}
		resp, err := NegotiateLevel(req)
		if err != nil {
			t.Fatalf("unexpected error for zero struct manifest: %v", err)
		}
		if resp.NegotiatedLevel != 0 || resp.IsDegraded {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("NegativeIntegrationLevel_WithoutRawBitmask", func(t *testing.T) {
		m := CapabilityManifest{IntegrationLevel: -1}
		level := EvaluateAchievableLevel(&m)
		if level != -1 {
			t.Errorf("expected level -1 for negative IntegrationLevel without raw bitmask, got %d", level)
		}
		req := &HandshakeRequest{
			SessionID:      "sess-neg-int-level",
			RequestedLevel: 0,
			Manifest:       m,
		}
		_, err := NegotiateLevel(req)
		if err != ErrUnsupportedAgent {
			t.Errorf("expected ErrUnsupportedAgent, got %v", err)
		}
	})
}

// TestChallenger2_WeirdRequestedLevels tests out-of-bounds, negative, and extreme requested levels.
func TestChallenger2_WeirdRequestedLevels(t *testing.T) {
	invalidLevels := []int{
		-1, -2, -100, math.MinInt32,
		4, 5, 10, 100, math.MaxInt32,
	}

	validManifest := CapabilityManifest{IntegrationLevel: 3}

	for _, lvl := range invalidLevels {
		t.Run(fmt.Sprintf("RequestedLevel_%d", lvl), func(t *testing.T) {
			req := &HandshakeRequest{
				SessionID:      "sess-weird-level",
				RequestedLevel: lvl,
				Manifest:       validManifest,
			}
			resp, err := NegotiateLevel(req)
			if err == nil {
				t.Fatalf("expected error for invalid requested level %d, got response: %+v", lvl, resp)
			}
			expectedSubstr := fmt.Sprintf("invalid requested level: %d (must be 0-3)", lvl)
			if err.Error() != expectedSubstr {
				t.Errorf("unexpected error text: %q, want %q", err.Error(), expectedSubstr)
			}
		})
	}

	t.Run("OverCapableAgent_Requests_Level0", func(t *testing.T) {
		req := &HandshakeRequest{
			SessionID:      "sess-overcapable-0",
			RequestedLevel: 0,
			Manifest:       CapabilityManifest{IntegrationLevel: 3},
		}
		resp, err := NegotiateLevel(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.NegotiatedLevel != 0 || resp.IsDegraded || len(resp.MissingFlags) > 0 {
			t.Errorf("unexpected response for over-capable requesting level 0: %+v", resp)
		}
	})

	t.Run("UnderCapableAgent_Requests_Level3_Degrades_To_Level0", func(t *testing.T) {
		req := &HandshakeRequest{
			SessionID:      "sess-undercapable-3-to-0",
			RequestedLevel: 3,
			Manifest:       CapabilityManifest{IntegrationLevel: 0},
		}
		resp, err := NegotiateLevel(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.NegotiatedLevel != 0 || !resp.IsDegraded || resp.DegradedFrom != 3 {
			t.Errorf("unexpected response: %+v", resp)
		}
		expectedMissingCount := 12 // Level 3 required mask has 13 flags; Level 0 provides 1 flag (CapEventStream) -> 12 missing
		if len(resp.MissingFlags) != expectedMissingCount {
			t.Errorf("expected %d missing flags, got %d (%v)", expectedMissingCount, len(resp.MissingFlags), resp.MissingFlags)
		}
	})
}

// TestChallenger2_MissingFlagSortingAndDeterminism asserts missing flag slice sorting, completeness, and concurrency determinism.
func TestChallenger2_MissingFlagSortingAndDeterminism(t *testing.T) {
	req := &HandshakeRequest{
		SessionID:      "sess-missing-flags-sort",
		RequestedLevel: 3,
		Manifest:       CapabilityManifest{IntegrationLevel: 1},
	}

	expectedMissing := []string{
		"CapDiffInspection",
		"CapHeadless",
		"CapCLIControl",
		"CapCheckpoint",
		"CapRollback",
		"CapMCP",
		"CapSubagents",
		"CapSwitchModel",
	}

	resp, err := NegotiateLevel(req)
	if err != nil {
		t.Fatalf("NegotiateLevel failed: %v", err)
	}

	if !reflect.DeepEqual(resp.MissingFlags, expectedMissing) {
		t.Errorf("MissingFlags mismatch:\nGot:  %v\nWant: %v", resp.MissingFlags, expectedMissing)
	}

	// Test concurrency determinism
	const workers = 50
	var wg sync.WaitGroup
	results := make([][]string, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r, err := NegotiateLevel(req)
			if err == nil {
				results[idx] = r.MissingFlags
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		if !reflect.DeepEqual(results[i], expectedMissing) {
			t.Fatalf("worker %d returned non-deterministic missing flags: %v", i, results[i])
		}
	}
}
