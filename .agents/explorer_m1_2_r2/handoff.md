# Handoff Report — Explorer 2 (Milestone 1 / Issue #7: Capability Manifest & Handshake Protocol — Iteration 2)

## 1. Observation

- **Review Target Files**:
  - `pkg/protocol/schema.go`: Line 196 (`CapabilityManifest` struct definition)
  - `pkg/protocol/capability.go`: Lines 101-155 (`ToBitmask`, `FromBitmask`, `HasCapability`, `EvaluateAchievableLevel`)
  - `pkg/protocol/schemas/capability_manifest.json`: Line 50 (`"additionalProperties": false`)
  - `pkg/protocol/capability_test.go`: `TestCapabilityManifest_BitmaskHelpers`
  - `.agents/reviewer_m1_2/handoff.md`: Section 1 (8 failing subtests in `TestChallenger_BoundaryBitmasks`)

- **Exact Test Failure Log Observed in Reviewer 2 Report**:
  ```
  --- FAIL: TestChallenger_BoundaryBitmasks (0.00s)
      --- FAIL: TestChallenger_BoundaryBitmasks/Zero_bitmask (0.00s)
          challenger_stress_test.go:199: HasCapability(shift 0) = true, want false
      --- FAIL: TestChallenger_BoundaryBitmasks/Full_uint64_bitmask_(all_bits_set) (0.00s)
          challenger_stress_test.go:199: HasCapability(shift 19) = false, want true
          challenger_stress_test.go:199: HasCapability(shift 63) = false, want true
      --- FAIL: TestChallenger_BoundaryBitmasks/Bit_19_only_(CapSDK) (0.00s)
          challenger_stress_test.go:199: HasCapability(shift 19) = false, want true
      --- FAIL: TestChallenger_BoundaryBitmasks/Bit_20_(undefined_flag) (0.00s)
          challenger_stress_test.go:199: HasCapability(shift 20) = false, want true
      --- FAIL: TestChallenger_BoundaryBitmasks/Bit_63_(highest_uint64_bit) (0.00s)
          challenger_stress_test.go:199: HasCapability(shift 63) = false, want true
      --- FAIL: TestChallenger_BoundaryBitmasks/Level_1_required_mask_minus_CapPause_(off-by-one_flag) (0.00s)
          challenger_stress_test.go:199: HasCapability(shift 1) = false, want true
      --- FAIL: TestChallenger_BoundaryBitmasks/Level_2_required_mask_minus_CapCheckpoint (0.00s)
          challenger_stress_test.go:199: HasCapability(shift 2) = false, want true
      --- FAIL: TestChallenger_BoundaryBitmasks/Level_3_required_mask_minus_CapSwitchModel (0.00s)
          challenger_stress_test.go:199: HasCapability(shift 5) = false, want true
          challenger_stress_test.go:199: HasCapability(shift 6) = false, want true
          challenger_stress_test.go:199: HasCapability(shift 13) = false, want true
  ```

- **Current Implementation Behavior Analysis**:
  1. `CapabilityManifest` (in `pkg/protocol/schema.go:196-206`) contains `IntegrationLevel int` and 6 boolean fields (`SupportsPause`, `SupportsCancel`, `SupportsResume`, `SupportsCheckpoint`, `SupportsRollback`, `SupportsMCP`).
  2. `FromBitmask(mask uint64)` (in `pkg/protocol/capability.go:144-155`) extracts those 6 booleans and sets `IntegrationLevel = EvaluateAchievableLevelFromMask(mask)`.
  3. `ToBitmask()` (in `pkg/protocol/capability.go:102-141`) calculates a bitmask from the 6 booleans and ORs in `LevelXRequiredMask` according to `IntegrationLevel`.
  4. For `mask = 0`, `IntegrationLevel` is `0`. `ToBitmask()` ORs `Level0RequiredMask` (`CapEventStream`, 0x1), returning `0x1`. Thus `HasCapability(CapEventStream)` returns `true` when `false` was expected.
  5. For isolated flags (e.g. `CapSDK` bit 19, undefined bit 20, bit 63), or flags belonging to higher levels where required mask threshold is not fully satisfied (bits 1, 2, 5, 6, 13), `FromBitmask` discards those bits because `CapabilityManifest` lacks boolean fields for them.
  6. `pkg/protocol/schemas/capability_manifest.json` specifies `"additionalProperties": false`. Adding exported fields with JSON tags to `CapabilityManifest` would break JSON schema validation.

---

## 2. Logic Chain

1. **Root Cause Identification**:
   - `CapabilityManifest` is a lossy representation when converting from raw uint64 bitmasks because it only holds 6 explicit boolean capability flags plus an `IntegrationLevel`.
   - When `FromBitmask` converts a uint64 mask to `CapabilityManifest`, any set bits that do not belong to the 6 booleans AND do not form a complete required mask for an integration level are discarded.
   - In addition, for `mask = 0`, `EvaluateAchievableLevelFromMask(0)` returns `0`. `ToBitmask()` treats `IntegrationLevel = 0` as requiring `Level0RequiredMask` (`CapEventStream`, bit 0), falsely inflating a zero bitmask to `0x1`.

2. **Field Storage & Schema Constraints Evaluation**:
   - `pkg/protocol/schemas/capability_manifest.json` requires strict JSON compliance with `"additionalProperties": false`.
   - Exported Go struct fields with `json:"..."` tags would be recognized as additional properties by `ValidateEvent` and rejected.
   - However, in Go, unexported struct fields (or fields tagged `json:"-"`) are completely ignored during JSON marshaling/unmarshaling and are invisible to `jsonschema` JSON payloads.
   - Adding unexported `rawBitmask uint64` and `hasRawBitmask bool` fields to `CapabilityManifest` allows `FromBitmask` to preserve the original `mask` bit-for-bit without affecting JSON schema validation.

3. **Remediation Plan Formulation**:
   - **Step 1**: In `pkg/protocol/schema.go`, add unexported fields `rawBitmask uint64` and `hasRawBitmask bool` to `CapabilityManifest`.
   - **Step 2**: In `pkg/protocol/capability.go`, update `FromBitmask(mask uint64)` to set `rawBitmask: mask` and `hasRawBitmask: true`.
   - **Step 3**: In `pkg/protocol/capability.go`, update `ToBitmask()` to check `if m.hasRawBitmask { return m.rawBitmask }` before performing boolean/level fallback computation.
   - **Step 4**: In `pkg/protocol/capability_test.go`, add `TestChallenger_BoundaryBitmasks` to permanently assert boundary bitmask lossless behavior.

---

## 3. Caveats

- **Read-Only Mode**: Explorer 2 performed a read-only investigation. No implementation code in `pkg/protocol/` was modified during this turn.
- **Unmarshaled Manifests**: Manually unmarshaled JSON payloads or struct literals created without `FromBitmask` will have `hasRawBitmask == false` and will rely on `ToBitmask()` fallback calculations, which is expected behavior for standard JSON manifests.

---

## 4. Conclusion

The 8 subtest failures in `TestChallenger_BoundaryBitmasks` are caused by loss of raw bitmask bits in `FromBitmask` and default `Level0RequiredMask` ORing in `ToBitmask`.

Preserving raw bitmask storage via unexported fields `rawBitmask uint64` and `hasRawBitmask bool` on `CapabilityManifest` cleanly solves the issue while remaining 100% compliant with `capability_manifest.json` (`additionalProperties: false`).

### Concrete Remediation Plan for Worker 2:

#### 1. Modify `pkg/protocol/schema.go`:
Update `CapabilityManifest` struct (around line 196):
```go
type CapabilityManifest struct {
	AgentID            string `json:"agent_id" redact:"none"`
	Version            string `json:"version" redact:"none"`
	IntegrationLevel   int    `json:"integration_level" redact:"none"`
	SupportsPause      bool   `json:"supports_pause" redact:"none"`
	SupportsCancel     bool   `json:"supports_cancel" redact:"none"`
	SupportsResume     bool   `json:"supports_resume" redact:"none"`
	SupportsCheckpoint bool   `json:"supports_checkpoint" redact:"none"`
	SupportsRollback   bool   `json:"supports_rollback" redact:"none"`
	SupportsMCP        bool   `json:"supports_mcp" redact:"none"`

	rawBitmask    uint64 `json:"-"`
	hasRawBitmask bool   `json:"-"`
}
```

#### 2. Modify `pkg/protocol/capability.go`:
Update `ToBitmask` and `FromBitmask`:
```go
// ToBitmask combines boolean capability flags and IntegrationLevel defaults into a uint64 bitmask.
func (m CapabilityManifest) ToBitmask() uint64 {
	if m.hasRawBitmask {
		return m.rawBitmask
	}

	var mask uint64

	if m.SupportsPause {
		mask |= uint64(CapPause)
	}
	if m.SupportsCancel {
		mask |= uint64(CapCancel)
	}
	if m.SupportsResume {
		mask |= uint64(CapResume)
	}
	if m.SupportsCheckpoint {
		mask |= uint64(CapCheckpoint)
	}
	if m.SupportsRollback {
		mask |= uint64(CapRollback)
	}
	if m.SupportsMCP {
		mask |= uint64(CapMCP)
	}

	switch m.IntegrationLevel {
	case 3:
		mask |= Level3RequiredMask
	case 2:
		mask |= Level2RequiredMask
	case 1:
		mask |= Level1RequiredMask
	case 0:
		mask |= Level0RequiredMask
	default:
		if m.IntegrationLevel > 3 {
			mask |= Level3RequiredMask
		} else if m.IntegrationLevel >= 0 {
			mask |= Level0RequiredMask
		}
	}

	return mask
}

// FromBitmask populates a CapabilityManifest struct from a bitmask.
func FromBitmask(mask uint64) CapabilityManifest {
	manifest := CapabilityManifest{
		SupportsPause:      (mask & uint64(CapPause)) != 0,
		SupportsCancel:     (mask & uint64(CapCancel)) != 0,
		SupportsResume:     (mask & uint64(CapResume)) != 0,
		SupportsCheckpoint: (mask & uint64(CapCheckpoint)) != 0,
		SupportsRollback:   (mask & uint64(CapRollback)) != 0,
		SupportsMCP:        (mask & uint64(CapMCP)) != 0,
		rawBitmask:         mask,
		hasRawBitmask:      true,
	}
	manifest.IntegrationLevel = EvaluateAchievableLevelFromMask(mask)
	return manifest
}
```

#### 3. Add Boundary Bitmasks Test to `pkg/protocol/capability_test.go`:
Add `TestChallenger_BoundaryBitmasks`:
```go
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
			wantLevel: 0,
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
			wantLevel: 0,
		},
		{
			name:      "Bit_20_(undefined_flag)",
			mask:      1 << 20,
			checkBit:  20,
			wantHas:   true,
			wantLevel: 0,
		},
		{
			name:      "Bit_63_(highest_uint64_bit)",
			mask:      1 << 63,
			checkBit:  63,
			wantHas:   true,
			wantLevel: 0,
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
```

---

## 5. Verification Method

To independently verify Worker 2's fix:

1. Execute full package tests with race detector:
   ```bash
   go test -v -count=1 -race ./pkg/protocol/...
   ```
   Confirm all tests (including `TestChallenger_BoundaryBitmasks`) pass cleanly with exit code 0.

2. Verify JSON schema validation remains untouched and passing:
   ```bash
   go test -v -count=1 -run TestValidateEvent ./pkg/protocol/...
   ```
