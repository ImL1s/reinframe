# Handoff Report — Explorer 1 (Milestone 1 / Issue #7: Capability Manifest & Handshake Protocol — Iteration 2)

## 1. Observation

- **檢視與測試執行**:
  - 執行測試命令: `go test -v -count=1 -race ./pkg/protocol/...`
  - 目前程式碼輸出: PASS (`github.com/reinframe/reinframe/pkg/protocol 2.725s`)。
  - Reviewer 2 報告之 `TestChallenger_BoundaryBitmasks` 失敗細節 (`reviewer_m1_2/handoff.md`):
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

- **程式碼結構觀察**:
  - `pkg/protocol/schema.go`:
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
    }
    ```
  - `pkg/protocol/schemas/capability_manifest.json`:
    包含 `"additionalProperties": false` 並且要求 `agent_id`, `version`, `integration_level`, `supports_pause`, `supports_cancel`, `supports_resume`, `supports_checkpoint`, `supports_rollback`, `supports_mcp` 等欄位。
  - `pkg/protocol/capability.go`:
    - `FromBitmask(mask uint64) CapabilityManifest`:
      只提取 6 個布林值 (`SupportsPause`, `SupportsCancel`, `SupportsResume`, `SupportsCheckpoint`, `SupportsRollback`, `SupportsMCP`)，並呼叫 `EvaluateAchievableLevelFromMask(mask)` 計算 `IntegrationLevel` (0-3)。
    - `ToBitmask() uint64`:
      結合上述 6 個布林值與 `IntegrationLevel` 的 RequiredMask (例如 Level 0 為 `CapEventStream` 0x1)。
    - `HasCapability(flag CapabilityFlag) bool`:
      判斷 `(m.ToBitmask() & uint64(flag)) == uint64(flag)`。

---

## 2. Logic Chain

1. **Test Failure 原因剖析**:
   - **Zero_bitmask (`mask = 0`) 失敗機制**:
     - 傳入 `FromBitmask(0)` 時，`EvaluateAchievableLevelFromMask(0)` 返回 `0`。
     - `CapabilityManifest` 的 `IntegrationLevel` 被設定為 `0`。
     - 呼叫 `manifest.ToBitmask()` 時，`switch m.IntegrationLevel` 匹配到 `case 0`，強行加入了 `Level0RequiredMask` (`CapEventStream`, 1<<0 = 0x1)。
     - 導致 `FromBitmask(0).ToBitmask()` 返回 `0x1` 而非 `0x0`。
     - 因此 `manifest.HasCapability(CapEventStream)` 返回 `true`，但預期應為 `false`。

   - **Isolated/Unassigned 旗標與降級時未覆蓋欄位丟失機制**:
     - 20 個 `CapabilityFlag` 中，只有 6 個代表明確的 `CapabilityManifest` 布林欄位。其餘欄位（如 `CapSDK` bit 19、`CapToolInspection` bit 1、`CapDiffInspection` bit 2、`CapHeadless` bit 5、`CapCLIControl` bit 6、`CapSubagents` bit 13、`CapSwitchModel` bit 15、`CapCostTracking` bit 3、`CapHooks` bit 4、`CapExtensions` bit 14、`CapCustomProvider` bit 16、`CapOpenAICompat` bit 17、`CapLocalModels` bit 18 以及未定義位元 20..63）在 `CapabilityManifest` 結構中皆無獨立的布林欄位可供存儲。
     - 當原始 bitmask 無法滿足高階 Supervision Level 的完整集合（例如 Level 3 扣除 `CapSwitchModel`）時，`EvaluateAchievableLevelFromMask` 將位階降級至 Level 2。
     - 降級後，`FromBitmask` 丟失了 bit 5 (`CapHeadless`)、bit 6 (`CapCLIControl`)、bit 13 (`CapSubagents`) 等位元，因為這些位元既未包含於 Level 2 的 RequiredMask 中，亦無獨立布林欄位可存。
     - 當對該 `CapabilityManifest` 呼叫 `ToBitmask()` 時，這些位元無法被重建，導致 `HasCapability` 斷言失敗。

2. **解決方案設計考量**:
   - 若在 `CapabilityManifest` 加上導出欄位（Exported field）如 `RawBitmask uint64` 並加上 JSON tag，則在 JSON 反序列化時會改變 JSON 結構，且會被 `capability_manifest.json` 的 `"additionalProperties": false` 拒絕，導致 `ValidateEvent` 失敗。
   - 若在 `CapabilityManifest` 結構（`pkg/protocol/schema.go`）中加入**未導出欄位（Unexported fields）** `rawBitmask uint64` 與 `hasRawBitmask bool`:
     - **JSON Marshaling/Unmarshaling 影響**: Go 的 `encoding/json` 庫會自動忽略未導出欄位。
     - **JSON Schema 驗證影響**: `ValidateEvent` 驗證的是傳入的 JSON Byte 陣列，未導出欄位不會存在於 JSON 陣列中，完美符合 `"additionalProperties": false`。
     - **Redaction Audit 影響**: `TestRedactionTags` 中的反射檢查（`schema_test.go:861`）會自動跳過未導出欄位 (`if field.PkgPath != "" { continue }`)。
     - **Bitmask Conversion 忠實度**:
       - 當 `FromBitmask(mask)` 被呼叫時，將原始 `mask` 記錄於 `rawBitmask` 並標記 `hasRawBitmask = true`。
       - 當 `ToBitmask()` 被呼叫時，若 `hasRawBitmask` 為 `true`，直接返回 `rawBitmask`。
       - 若 `hasRawBitmask` 為 `false`（例如直接自 JSON 反序列化或直接實例化 `CapabilityManifest{IntegrationLevel: 0}` 的結構體），則維持原有的計算邏輯（依據 `IntegrationLevel` 與 6 個布林欄位計算 bitmask）。

3. **擬議修改擬合性驗證**:
   - `FromBitmask(0).ToBitmask()` 將精確返回 `0x0`。
   - `FromBitmask(0xFFFFFFFFFFFFFFFF).ToBitmask()` 將精確返回 `0xFFFFFFFFFFFFFFFF`。
   - 包含任意 Isolated 旗標（如 bit 19 `CapSDK` 或 bit 20..63）的 raw bitmask 在過渡為 `CapabilityManifest` 後呼叫 `HasCapability` 均能 100% 精確匹配。

---

## 3. Caveats

- **唯讀調查限制**: 本 Agent 為 Explorer (唯讀調查角色)，並未直接修改 `pkg/protocol/schema.go` 或 `pkg/protocol/capability.go` 原始碼。實際代碼修正應由 Worker 完成。
- **作用域限制**: 調查範圍聚焦於 Issue #7 (`pkg/protocol/...`) 的能力宣告與 Handshake 協議機制，不影響 Issue #9 (`pkg/state/...`) 的 SQLite WAL Event Store。

---

## 4. Conclusion

1. **失敗根因確證**:
   Reviewer 2 報告的 `TestChallenger_BoundaryBitmasks` 8 個子測試失敗，係因 `FromBitmask` 轉換為 `CapabilityManifest` 時屬於有損轉換（Lossy conversion），且 `ToBitmask()` 對 `IntegrationLevel 0` 強制疊加 `Level0RequiredMask` (0x1) 導致 `0x0` bitmask 變成 `0x1`。

2. **具體修復建議**:
   - **`pkg/protocol/schema.go`**:
     在 `CapabilityManifest` 結構體增加兩個未導出欄位：
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
         rawBitmask         uint64
         hasRawBitmask      bool
     }
     ```
   - **`pkg/protocol/capability.go`**:
     - `FromBitmask(mask uint64)` 設定 `rawBitmask: mask` 與 `hasRawBitmask: true`：
       ```go
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
     - `ToBitmask()` 優先檢查 `hasRawBitmask`：
       ```go
       func (m CapabilityManifest) ToBitmask() uint64 {
           if m.hasRawBitmask {
               return m.rawBitmask
           }
           // ... 既有計算邏輯 ...
       }
       ```
   - **`pkg/protocol/challenger_stress_test.go`**:
     確保包含完整的 `TestChallenger_BoundaryBitmasks` 測試案例，覆蓋 `Zero_bitmask` (0x0)、`Full_uint64_bitmask` (0xFFFFFFFFFFFFFFFF)、`CapSDK` (bit 19)、未定義位元 (bit 20)、最高位元 (bit 63) 與偏離 RequiredMask 之邊界條件。

---

## 5. Verification Method

Worker 實作修改後，依循下列步驟進行獨立驗證：

1. **單元測試與 Race Detector 驗證**:
   ```bash
   go test -v -count=1 -race ./pkg/protocol/...
   ```
   確認 exit code 為 0，無任何失敗子測試，亦無 Race 警告。

2. **特定測試案例精確驗證**:
   ```bash
   go test -v -count=1 -race -run "TestChallenger_BoundaryBitmasks" ./pkg/protocol/...
   ```
   確認 `TestChallenger_BoundaryBitmasks` 下包含 `Zero_bitmask` 等 8 個子測試全數 PASS。

3. **Schema 驗證與 Redaction Audit 雙重檢查**:
   ```bash
   go test -v -count=1 -race -run "TestValidateEvent|TestRedactionTags|TestStructJSONRoundtrip" ./pkg/protocol/...
   ```
   確認新增之未導出欄位未對 JSON Schema 與 Redaction Tag 稽核造成破壞。
