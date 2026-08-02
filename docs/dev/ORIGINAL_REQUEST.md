# Original User Request

## Initial Request — 2026-08-02T13:25:04Z

Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go, powered by SQLite WAL state persistence and JSON-RPC 2.0 / NDJSON protocol interfaces.

Working directory: /Users/iml1s/Documents/mine/reinframe
Integrity mode: development

## Requirements

### R1. Implement Issue #6: Canonical Agent Event Schema & JSON Validation
Define all 22 canonical event and data structures (AgentSession, TaskEnvelope, AgentEvent, ToolCallEvent, FileChangeEvent, TestResultEvent, ErrorFingerprint, EvidenceItem, EvidencePack, Hypothesis, Assumption, TunnelSignal, TunnelAssessment, ReviewRequest, ReviewDecision, Intervention, BudgetState, CapabilityManifest, Checkpoint, RollbackResult, ProviderUsage, AuditRecord) in pkg/protocol/schema.go, JSON schemas in pkg/protocol/schemas/, and JSON schema validation engine.

### R2. Strict Issue-Driven Development Workflow
- Create Git branch: issue-6-canonical-agent-event-schema
- Implement scope of Issue #6 only.
- Write unit tests in pkg/protocol/schema_test.go.
- Validate with go test ./pkg/protocol/...
- Create clean commit, update Issue #6 comment on GitHub, and open Pull Request.

## Acceptance Criteria

### Schema Completeness & Validation
- [ ] All 22 canonical types defined in Go struct models with JSON tags and redaction metadata tags.
- [ ] JSON Schema files created under pkg/protocol/schemas/*.json for canonical payload validation.
- [ ] ValidateEvent(payload []byte, schemaType string) error function passes unit tests.
- [ ] Unit tests pass with go test ./pkg/protocol/...
- [ ] Git commit and Pull Request created for Issue #6.

## Follow-up — 2026-08-02T13:38:58Z

Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go, powered by SQLite WAL state persistence and JSON-RPC 2.0 / NDJSON protocol interfaces.

Working directory: /Users/iml1s/Documents/mine/reinframe
Integrity mode: development

## Requirements

### R1. Implement Issue #7: Capability Manifest & Handshake Protocol
Build CapabilityManifest struct, 20 capability flags, and negotiation engine (pkg/protocol/capability.go) supporting Level 0 (Observe), Level 1 (Advisory), Level 2 (Guarded), Level 3 (Full-control) with automatic degradation. Write unit tests in pkg/protocol/capability_test.go.

### R2. Implement Issue #9: Append-Only Event Store & SQLite WAL Engine
Build SQLite WAL-backed event store (pkg/state/store.go), schema migration engine (pkg/state/migrations/001_initial_events.sql), AppendEvent and QueryEvents methods with multi-goroutine safety. Write unit tests in pkg/state/store_test.go.

### R3. Strict Issue-Driven Development Workflow
- Create Git branches: issue-7-capability-manifest-negotiation and issue-9-sqlite-wal-event-store.
- Implement unit tests and run go test -race ./pkg/...
- Run Victory Audit and create GitHub comments & Pull Requests for Issue #7 and Issue #9.

## Acceptance Criteria

### Issue #7 Criteria
- [ ] CapabilityManifest and NegotiateLevel correctly map capability flags to Levels 0-3.
- [ ] Degradation policy handles partial capabilities safely.
- [ ] Unit tests pass with go test -race ./pkg/protocol/...
- [ ] Pull Request opened for Issue #7.

### Issue #9 Criteria
- [ ] SQLite connection configured with journal_mode=WAL and busy timeout.
- [ ] 001_initial_events.sql migration runs cleanly on fresh DB.
- [ ] AppendEvent and QueryEvents pass concurrent race tests (go test -race ./pkg/state/...).
- [ ] Pull Request opened for Issue #9.

## Follow-up — 2026-08-02T14:44:25Z

修復 reinframe 專案中由兩份獨立審計（4-Reviewer 內部審查 + GPT 5.6 Pro 外部審計）發現的全部 P0 Blocker 與 P1 問題。目標是將專案從「初期 foundation」提升至「架構正確、可驗證、可繼續實作後續 Issue」的狀態。

Working directory: /Users/iml1s/Documents/mine/reinframe
Integrity mode: development

## Reference Material

- 內部審計報告: 4 個獨立 Code Reviewer 的發現 (Protocol, State, E2E, Governance)
- 外部審計報告: GPT 5.6 Pro 的獨立驗證
- 合併去重清單: 13 個 P0 + 8 個 P1

## Requirements

### R1. 修復 pkg/state SQLite 併發架構（P0 Batch 1）

pkg/state/store.go 存在 4 個互相關聯的 SQLite 併發缺陷，必須一起修復：

1. **DSN Pragma 配置**：PRAGMA busy_timeout / journal_mode / foreign_keys 目前透過 db.Exec() 執行，只套用到連線池中隨機一條連線。必須改為透過 DSN 連線字串配置，確保連線池中每條連線都套用。
2. **移除全域 Mutex**：目前 AppendEvents 使用 s.mu.Lock()、QueryEvents 使用 s.mu.RLock() 包住整個 DB 操作，完全抵銷 WAL 併發讀寫優勢。移除 sync.RWMutex，改用 atomic.Bool 管理 s.closed 狀態。
3. **改用 db.BeginTx**：目前手動取得 conn 並執行 "BEGIN IMMEDIATE"，若 ROLLBACK 失敗會導致連線池狀態洩漏。改用標準的 db.BeginTx(ctx, nil) 搭配 DSN 的 _txlock=immediate。
4. **修復預設 :memory: pooling**：SQLite `:memory:` 是 per-connection，多連線池中各連線是獨立的空 DB。改為 `file:reinframe-memory?mode=memory&cache=shared` 或限制 maxOpen=1。

### R2. 修復 pkg/protocol Capability 與 Schema 缺陷（P0 Batch 2）

1. **ToBitmask 權限繞過**：ToBitmask() 根據 IntegrationLevel 自動注入 capability flags，覆蓋顯式 boolean。修復為 ToBitmask() 僅從顯式 boolean 建構 bitmask。
2. **Level contract 對齊**：目前 Level 1 Required Mask 包含 Pause/Cancel/Resume，但原始設計 Level 1 是 Advisory（不可強制控制）。必須重新對齊 Level 定義。
3. **補齊 14 個 Capability 欄位**：CapabilityManifest 結構體和 JSON schema 只有 6 個 boolean，但有 20 個 CapabilityFlag。補齊缺失的 14 個，確保 JSON 序列化不遺失。
4. **ValidateEvent 安全修復**：(a) 加入 payload 大小上限檢查防止 DoS；(b) 改用 json.Decoder.UseNumber() 防止 float64 精度問題。
5. **AgentSession.status 加入 RESUME**：目前 enum 缺少 RESUME，導致合法流程 ZOOM_OUT → RESUME 無法通過 Schema validation。
6. **max_depth schema 加上 maximum:1**：防止子代理多層巢狀。

### R3. 修復 Governance、CI 與測試問題（P0 Batch 3）

1. **Go 版本對齊**：go.mod 寫 go 1.25.0，CI 用 1.22.x（實際被 GOTOOLCHAIN=auto 靜默升級）。統一為 go.mod 實際版本，CI 改用 go-version-file: go.mod。
2. **建立 README.md 和 .gitignore**：README 包含專案目標、架構概覽、Quickstart。.gitignore 排除 *.db, .DS_Store, .agents/ 執行日誌。
3. **根目錄清理**：將 ORIGINAL_REQUEST.md, PROJECT.md, TEST_INFRA.md, TEST_READY.md 移到 docs/dev/。
4. **CI 加入 golangci-lint**。
5. **修復 Issue DAG**：(a) 已完成的 Issue #6, #7, #9, #39 必須標為 closed 並更新 Epic checklist。(b) 修復雙向依賴不一致。(c) 正確計數 Issue 數量。
6. **Migration 競爭條件**：SELECT EXISTS 移到 Transaction 內部。
7. **sync.Once 改為 init() fail-fast**。
8. **測試目錄重新命名**：tests/e2e/ → tests/integration/，更新 CI workflow。
9. **50ms BusyTimeout 測試**：調高到 500ms 防止 flaky。
10. **測試使用 t.TempDir()**：取代手動 os.MkdirTemp + defer cleanup。

### R4. 重寫 Capability 測試套件

目前的 capability 測試鞏固了有缺陷的 auto-granting 邏輯。在修復 R2 的 ToBitmask 和 Level contract 後，必須重寫所有相關測試案例，確保：
- 缺少必要 boolean 欄位時正確降級或失敗
- JSON 序列化→反序列化 round-trip 不遺失任何 capability
- Level contract 與原始 ADR 設計一致

### R5. 壓力測試重新驗證

在修復 R1（移除全域 Mutex、DSN Pragma、db.BeginTx）後，必須重新執行所有壓力測試（含 500 goroutine 與 100 writer/50 reader），確認 WAL 模式下的真實高併發能力，而非被 Go Mutex 掩蓋的假綠燈。

## Acceptance Criteria

### SQLite 併發修復 (R1)
- [ ] store.go 不包含任何 sync.RWMutex 或 sync.Mutex（grep 驗證）
- [ ] store.go 使用 atomic.Bool 管理 closed 狀態
- [ ] SQLite 連線字串包含 `_pragma=busy_timeout` 和 `_pragma=journal_mode(WAL)` 和 `_pragma=foreign_keys(1)`
- [ ] 不使用 conn.ExecContext("BEGIN IMMEDIATE")，改用 db.BeginTx
- [ ] 預設 `:memory:` 路徑使用 `cache=shared` 或 maxOpen=1
- [ ] `go test -race -count=5 ./pkg/state/...` 全部通過
- [ ] challenger_stress_test.go (100 writers / 50 readers) 通過
- [ ] 500 goroutine 高併發壓力測試通過

### Capability 與 Schema 修復 (R2)
- [ ] ToBitmask() 不再根據 IntegrationLevel 自動注入 flags
- [ ] CapabilityManifest 結構體有 20 個 boolean 欄位（不是 6 個）
- [ ] capability_manifest.json schema 有 20 個 boolean properties
- [ ] `json.Marshal → json.Unmarshal` round-trip 不遺失任何 capability（單元測試驗證）
- [ ] ValidateEvent 在 Unmarshal 前檢查 len(payload) > 限制
- [ ] ValidateEvent 使用 json.Decoder.UseNumber()
- [ ] AgentSession status enum 包含 RESUME
- [ ] max_depth schema 包含 maximum: 1
- [ ] `go test -race ./pkg/protocol/...` 全部通過

### Governance 與 CI 修復 (R3)
- [ ] go.mod 版本與 CI go-version 一致（不依賴 GOTOOLCHAIN=auto 靜默升級）
- [ ] README.md 存在且包含專案描述和 Quickstart
- [ ] .gitignore 存在且排除 *.db, .DS_Store, .agents/
- [ ] ORIGINAL_REQUEST.md, PROJECT.md, TEST_INFRA.md, TEST_READY.md 移到 docs/dev/
- [ ] CI workflow 包含 golangci-lint 步驟
- [ ] Issue #6, #7, #9, #39 在 GitHub 上為 closed 狀態
- [ ] Epic #1 checklist 中這 4 個 Issue 已勾選
- [ ] tests/e2e/ 已重新命名為 tests/integration/
- [ ] `gh run list` 最新一次 main 分支 CI 三平台全綠

### 測試品質 (R4 + R5)
- [ ] capability_test.go 中不存在驗證 IntegrationLevel 自動授予的測試案例
- [ ] 新增 TestCapability_JSONRoundTrip_Lossless 測試
- [ ] 壓力測試在移除 Mutex 後仍 100% 通過 (go test -race -count=5)

