package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadSchemas(t *testing.T) {
	if err := LoadSchemas(); err != nil {
		t.Fatalf("LoadSchemas failed: %v", err)
	}

	if len(schemaCache) != 22 {
		t.Errorf("expected 22 schemas cached, got %d", len(schemaCache))
	}

	expectedTypes := []string{
		"agent_session",
		"task_envelope",
		"agent_event",
		"tool_call_event",
		"file_change_event",
		"test_result_event",
		"error_fingerprint",
		"evidence_item",
		"evidence_pack",
		"hypothesis",
		"assumption",
		"tunnel_signal",
		"tunnel_assessment",
		"review_request",
		"review_decision",
		"intervention",
		"budget_state",
		"capability_manifest",
		"checkpoint",
		"rollback_result",
		"provider_usage",
		"audit_record",
	}

	for _, name := range expectedTypes {
		if _, ok := schemaCache[name]; !ok {
			t.Errorf("missing cached schema for %s", name)
		}
	}
}

func TestValidateEvent_ValidPayloads(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	exitCode := 0

	tests := []struct {
		name       string
		schemaType string
		payload    any
	}{
		{
			name:       "AgentSession",
			schemaType: "AgentSession",
			payload: AgentSession{
				SessionID:        "sess-123",
				AgentID:          "claude-code",
				AdapterType:      "cli_process",
				IntegrationLevel: 2,
				WorkspacePath:    "/tmp/workspace",
				Status:           "EXECUTE",
				StartedAt:        time.Now().UTC(),
			},
		},
		{
			name:       "TaskEnvelope",
			schemaType: "task_envelope",
			payload: TaskEnvelope{
				TaskID:         "task-123",
				SessionID:      "sess-123",
				Prompt:         "Fix bug #6",
				MaxDepth:       1,
				TimeoutSeconds: 300,
				CreatedAt:      time.Now().UTC(),
			},
		},
		{
			name:       "AgentEvent",
			schemaType: "AgentEvent",
			payload: AgentEvent{
				EventID:     "evt-123",
				SessionID:   "sess-123",
				SequenceNum: 1,
				EventType:   "tool_call",
				Timestamp:   time.Now().UTC(),
				Payload:     json.RawMessage(`{"tool_name": "Bash"}`),
			},
		},
		{
			name:       "ToolCallEvent",
			schemaType: "tool_call_event",
			payload: ToolCallEvent{
				ToolCallID: "tc-123",
				ToolName:   "Bash",
				Arguments:  map[string]any{"command": "ls -l"},
				ExitCode:   &exitCode,
				DurationMs: 150,
			},
		},
		{
			name:       "FileChangeEvent",
			schemaType: "FileChangeEvent",
			payload: FileChangeEvent{
				FilePath:         "pkg/protocol/schema.go",
				ChangeType:       "MODIFIED",
				LinesAdded:       10,
				LinesRemoved:     2,
				IsScopeViolation: false,
			},
		},
		{
			name:       "TestResultEvent",
			schemaType: "test_result_event",
			payload: TestResultEvent{
				TestRunID:    "tr-123",
				Command:      "go test ./...",
				PassedCount:  5,
				FailedCount:  0,
				SkippedCount: 0,
				PassDelta:    1,
				DurationMs:   1200,
			},
		},
		{
			name:       "ErrorFingerprint",
			schemaType: "ErrorFingerprint",
			payload: ErrorFingerprint{
				FingerprintID:   "fp-123",
				RawError:        "fatal error: runtime panic",
				NormalizedText:  "fatal error: runtime panic",
				OccurrenceCount: 3,
				FirstObservedAt: time.Now().UTC(),
				LastObservedAt:  time.Now().UTC(),
			},
		},
		{
			name:       "EvidenceItem",
			schemaType: "evidence_item",
			payload: EvidenceItem{
				ItemID:   "ei-123",
				Source:   "stderr",
				Category: "ERROR_TRACE",
				Content:  "panic: nil pointer dereference",
			},
		},
		{
			name:       "EvidencePack",
			schemaType: "EvidencePack",
			payload: map[string]any{
				"pack_id":    "pack-123",
				"session_id": "sess-123",
				"fork_turns": "none",
				"items": []any{
					map[string]any{
						"item_id":  "ei-123",
						"source":   "stderr",
						"category": "ERROR_TRACE",
						"content":  "trace log",
					},
				},
				"churn_ratio":          0.15,
				"repeated_error_count": 2,
				"created_at":           now,
			},
		},
		{
			name:       "Hypothesis",
			schemaType: "hypothesis",
			payload: Hypothesis{
				HypothesisID:    "hypo-123",
				Statement:       "Database connection leak causing timeouts",
				Status:          "PROPOSED",
				ConfidenceScore: 0.85,
			},
		},
		{
			name:       "Assumption",
			schemaType: "Assumption",
			payload: Assumption{
				AssumptionID: "asm-123",
				Description:  "SQLite WAL mode is enabled",
				IsVerified:   true,
				AuditVerdict: "VALIDATED",
			},
		},
		{
			name:       "TunnelSignal",
			schemaType: "tunnel_signal",
			payload: TunnelSignal{
				SignalID:     "sig-123",
				SessionID:    "sess-123",
				DetectorName: "RepeatedErrorDetector",
				FailureMode:  "FM-1",
				Weight:       0.75,
				Score:        0.90,
				TriggeredAt:  time.Now().UTC(),
			},
		},
		{
			name:       "TunnelAssessment",
			schemaType: "TunnelAssessment",
			payload: map[string]any{
				"assessment_id":        "asm-123",
				"session_id":           "sess-123",
				"aggregate_score":      0.88,
				"primary_failure_mode": "FM-1",
				"is_tunnel_detected":   true,
				"recommended_action":   "ADVISORY_ZOOM_OUT",
				"signals": []any{
					map[string]any{
						"signal_id":     "sig-123",
						"session_id":    "sess-123",
						"detector_name": "RepeatedErrorDetector",
						"failure_mode":  "FM-1",
						"weight":        0.75,
						"score":         0.90,
						"triggered_at":  now,
					},
				},
				"evaluated_at": now,
			},
		},
		{
			name:       "ReviewRequest",
			schemaType: "review_request",
			payload: ReviewRequest{
				RequestID:      "rr-123",
				ReviewerRole:   "TunnelClassifier",
				EvidencePackID: "pack-123",
				Model:          "gpt-4o",
				Prompt:         "Analyze evidence pack for tunnel vision",
				RequestedAt:    time.Now().UTC(),
			},
		},
		{
			name:       "ReviewDecision",
			schemaType: "ReviewDecision",
			payload: ReviewDecision{
				DecisionID:       "rd-123",
				RequestID:        "rr-123",
				ReviewerRole:     "TunnelClassifier",
				TunnelConfidence: 0.90,
				Classification:   "TUNNEL_VISION",
				Rationale:        "Agent has repeated same failing test 5 times",
				TokensUsed:       350,
				DecidedAt:        time.Now().UTC(),
			},
		},
		{
			name:       "Intervention",
			schemaType: "intervention",
			payload: Intervention{
				InterventionID: "itv-123",
				SessionID:      "sess-123",
				Level:          1,
				ActionType:     "ZOOM_OUT_PROMPT",
				Status:         "EXECUTED",
				ExecutedAt:     time.Now().UTC(),
			},
		},
		{
			name:       "BudgetState",
			schemaType: "BudgetState",
			payload: BudgetState{
				SessionID:         "sess-123",
				MaxTokens:         100000,
				UsedTokens:        25000,
				MaxCostUSD:        5.0,
				CurrentCostUSD:    1.25,
				MaxInterventions:  3,
				InterventionCount: 1,
				IsExhausted:       false,
			},
		},
		{
			name:       "CapabilityManifest",
			schemaType: "capability_manifest",
			payload: CapabilityManifest{
				AgentID:            "claude-code",
				Version:            "1.0.0",
				IntegrationLevel:   2,
				SupportsPause:      true,
				SupportsCancel:     true,
				SupportsResume:     true,
				SupportsCheckpoint: true,
				SupportsRollback:   true,
				SupportsMCP:        true,
			},
		},
		{
			name:       "AgentSession_RESUME",
			schemaType: "agent_session",
			payload: AgentSession{
				SessionID:        "sess-resume",
				AgentID:          "claude-code",
				AdapterType:      "cli_process",
				IntegrationLevel: 2,
				WorkspacePath:    "/tmp",
				Status:           "RESUME",
				StartedAt:        time.Now().UTC(),
			},
		},
		{
			name:       "Checkpoint",
			schemaType: "Checkpoint",
			payload: Checkpoint{
				CheckpointID:     "chk-123",
				SessionID:        "sess-123",
				GitCommitHash:    "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
				BranchName:       "main",
				Description:      "Baseline clean build",
				PassingTestCount: 10,
				CreatedAt:        time.Now().UTC(),
			},
		},
		{
			name:       "RollbackResult",
			schemaType: "rollback_result",
			payload: RollbackResult{
				RollbackID:         "rb-123",
				SessionID:          "sess-123",
				TargetCheckpointID: "chk-123",
				PreviousCommitHash: "1111111111111111111111111111111111111111",
				RestoredCommitHash: "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
				Success:            true,
				CompletedAt:        time.Now().UTC(),
			},
		},
		{
			name:       "ProviderUsage",
			schemaType: "ProviderUsage",
			payload: ProviderUsage{
				UsageID:          "usg-123",
				SessionID:        "sess-123",
				ProviderName:     "anthropic",
				Model:            "claude-3-5-sonnet",
				PromptTokens:     1000,
				CompletionTokens: 200,
				TotalTokens:      1200,
				EstimatedCostUSD: 0.006,
				LatencyMs:        450,
				Timestamp:        time.Now().UTC(),
			},
		},
		{
			name:       "AuditRecord",
			schemaType: "audit_record",
			payload: AuditRecord{
				AuditID:    "aud-123",
				SessionID:  "sess-123",
				Actor:      "SUPERVISOR",
				Category:   "INTERVENTION",
				Summary:    "Executed level 1 zoom out advisory prompt",
				RecordedAt: time.Now().UTC(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payloadBytes []byte
			var err error

			if b, ok := tt.payload.([]byte); ok {
				payloadBytes = b
			} else {
				payloadBytes, err = json.Marshal(tt.payload)
				if err != nil {
					t.Fatalf("failed to marshal payload: %v", err)
				}
			}

			if err := ValidateEvent(payloadBytes, tt.schemaType); err != nil {
				t.Errorf("ValidateEvent failed for valid payload %s (%s): %v", tt.name, tt.schemaType, err)
			}
		})
	}
}

func TestValidateEvent_InvalidPayloads(t *testing.T) {
	tests := []struct {
		name       string
		schemaType string
		payload    string
		wantErrSub string
	}{
		{
			name:       "UnknownSchemaType",
			schemaType: "non_existent_type",
			payload:    `{"foo": "bar"}`,
			wantErrSub: "unknown schema type",
		},
		{
			name:       "MalformedJSON",
			schemaType: "agent_session",
			payload:    `{broken_json`,
			wantErrSub: "malformed JSON payload",
		},
		{
			name:       "MissingRequiredField_AgentSession",
			schemaType: "agent_session",
			payload:    `{"agent_id": "claude"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "MissingRequiredField_TaskEnvelope",
			schemaType: "task_envelope",
			payload:    `{"session_id": "sess-123", "prompt": "test"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "TypeMismatch_ToolCallEvent",
			schemaType: "tool_call_event",
			payload:    `{"tool_call_id": "tc-1", "tool_name": "Bash", "arguments": {}, "duration_ms": "invalid_string"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "TypeMismatch_AgentSessionID",
			schemaType: "agent_session",
			payload:    `{"session_id": 12345, "agent_id": "claude", "adapter_type": "cli_process", "integration_level": 1, "workspace_path": "/tmp", "status": "EXECUTE", "started_at": "2026-08-02T13:00:00Z"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "InvalidEnum_FileChangeEvent",
			schemaType: "file_change_event",
			payload:    `{"file_path": "a.txt", "change_type": "MUTATED", "lines_added": 1, "lines_removed": 0, "is_scope_violation": false}`,
			wantErrSub: "validation error",
		},
		{
			name:       "InvalidEnum_AgentSessionStatus",
			schemaType: "agent_session",
			payload:    `{"session_id": "sess-1", "agent_id": "claude", "adapter_type": "cli_process", "integration_level": 1, "workspace_path": "/tmp", "status": "SUPER_STATUS", "started_at": "2026-08-02T13:00:00Z"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "OutOfBoundScore_TunnelSignal",
			schemaType: "tunnel_signal",
			payload:    `{"signal_id": "sig-1", "session_id": "sess-1", "detector_name": "det", "failure_mode": "FM-1", "weight": 0.5, "score": 1.5, "triggered_at": "2026-08-02T13:00:00Z"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "OutOfBoundScore_TunnelAssessment",
			schemaType: "tunnel_assessment",
			payload:    `{"assessment_id": "asm-1", "session_id": "sess-1", "aggregate_score": -0.5, "primary_failure_mode": "FM-1", "is_tunnel_detected": true, "recommended_action": "NO_ACTION", "signals": [], "evaluated_at": "2026-08-02T13:00:00Z"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "OutOfBoundIntegrationLevel",
			schemaType: "agent_session",
			payload:    `{"session_id": "sess-1", "agent_id": "claude", "adapter_type": "cli_process", "integration_level": 99, "workspace_path": "/tmp", "status": "EXECUTE", "started_at": "2026-08-02T13:00:00Z"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "MaxDepthExceeded_TaskEnvelope",
			schemaType: "task_envelope",
			payload:    `{"task_id":"t1","session_id":"s1","prompt":"p","max_depth":2,"timeout_seconds":10,"created_at":"2026-08-02T13:00:00Z"}`,
			wantErrSub: "validation error",
		},
		{
			name:       "PayloadExceedsMaxSize",
			schemaType: "agent_session",
			payload:    strings.Repeat("x", 1024*1024+10),
			wantErrSub: "exceeds maximum limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEvent([]byte(tt.payload), tt.schemaType)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
			}
			if !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Errorf("expected error containing %q, got %q", tt.wantErrSub, err.Error())
			}
		})
	}
}

func TestStructJSONRoundtrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	endedAt := now.Add(time.Hour)
	exitCode := 0

	tests := []struct {
		name     string
		instance any
		factory  func() any
	}{
		{
			name: "AgentSession",
			instance: AgentSession{
				SessionID:        "sess-123",
				AgentID:          "claude-code",
				AdapterType:      "cli_process",
				IntegrationLevel: 2,
				WorkspacePath:    "/tmp/workspace",
				Status:           "EXECUTE",
				StartedAt:        now,
				EndedAt:          &endedAt,
				Metadata:         map[string]string{"env": "prod"},
			},
			factory: func() any { return &AgentSession{} },
		},
		{
			name: "TaskEnvelope",
			instance: TaskEnvelope{
				TaskID:         "task-123",
				SessionID:      "sess-123",
				Prompt:         "Fix issue #6",
				ScopeWhitelist: []string{"pkg/protocol"},
				MaxDepth:       3,
				TimeoutSeconds: 300,
				CreatedAt:      now,
			},
			factory: func() any { return &TaskEnvelope{} },
		},
		{
			name: "AgentEvent",
			instance: AgentEvent{
				EventID:     "evt-123",
				SessionID:   "sess-123",
				SequenceNum: 42,
				EventType:   "tool_call",
				Timestamp:   now,
				Payload:     json.RawMessage(`{"tool":"Bash"}`),
			},
			factory: func() any { return &AgentEvent{} },
		},
		{
			name: "ToolCallEvent",
			instance: ToolCallEvent{
				ToolCallID: "tc-123",
				ToolName:   "Bash",
				Arguments:  map[string]any{"command": "go test"},
				Output:     "PASS",
				ExitCode:   &exitCode,
				DurationMs: 150,
				Error:      "none",
			},
			factory: func() any { return &ToolCallEvent{} },
		},
		{
			name: "FileChangeEvent",
			instance: FileChangeEvent{
				FilePath:         "pkg/protocol/schema.go",
				ChangeType:       "MODIFIED",
				LinesAdded:       10,
				LinesRemoved:     2,
				NetDiff:          "+added",
				IsScopeViolation: false,
			},
			factory: func() any { return &FileChangeEvent{} },
		},
		{
			name: "TestResultEvent",
			instance: TestResultEvent{
				TestRunID:     "tr-123",
				Command:       "go test ./...",
				PassedCount:   10,
				FailedCount:   0,
				SkippedCount:  1,
				PassDelta:     2,
				FailureOutput: "none",
				DurationMs:    500,
			},
			factory: func() any { return &TestResultEvent{} },
		},
		{
			name: "ErrorFingerprint",
			instance: ErrorFingerprint{
				FingerprintID:   "fp-123",
				RawError:        "fatal panic",
				NormalizedText:  "fatal panic",
				OccurrenceCount: 3,
				FirstObservedAt: now,
				LastObservedAt:  now,
			},
			factory: func() any { return &ErrorFingerprint{} },
		},
		{
			name: "EvidenceItem",
			instance: EvidenceItem{
				ItemID:   "ei-123",
				Source:   "stderr",
				Category: "ERROR_TRACE",
				Content:  "panic trace",
				Metadata: map[string]string{"file": "main.go"},
			},
			factory: func() any { return &EvidenceItem{} },
		},
		{
			name: "EvidencePack",
			instance: EvidencePack{
				PackID:    "pack-123",
				SessionID: "sess-123",
				ForkTurns: "none",
				Items: []EvidenceItem{
					{
						ItemID:   "ei-123",
						Source:   "stderr",
						Category: "ERROR_TRACE",
						Content:  "panic trace",
					},
				},
				ChurnRatio:          0.15,
				RepeatedErrorCount: 2,
				CreatedAt:          now,
			},
			factory: func() any { return &EvidencePack{} },
		},
		{
			name: "Hypothesis",
			instance: Hypothesis{
				HypothesisID:          "hypo-123",
				Statement:             "Leak in handler",
				Status:                "PROPOSED",
				SupportingEvidenceIDs: []string{"ei-123"},
				RefutingEvidenceIDs:   []string{"ei-456"},
				ConfidenceScore:       0.85,
			},
			factory: func() any { return &Hypothesis{} },
		},
		{
			name: "Assumption",
			instance: Assumption{
				AssumptionID:       "asm-123",
				Description:        "SQLite WAL enabled",
				IsVerified:         true,
				VerificationMethod: "ping",
				AuditVerdict:       "VALIDATED",
			},
			factory: func() any { return &Assumption{} },
		},
		{
			name: "TunnelSignal",
			instance: TunnelSignal{
				SignalID:     "sig-123",
				SessionID:    "sess-123",
				DetectorName: "RepeatedErrorDetector",
				FailureMode:  "FM-1",
				Weight:       0.75,
				Score:        0.90,
				Details:      map[string]string{"cnt": "3"},
				TriggeredAt:  now,
			},
			factory: func() any { return &TunnelSignal{} },
		},
		{
			name: "TunnelAssessment",
			instance: TunnelAssessment{
				AssessmentID:       "asm-123",
				SessionID:          "sess-123",
				AggregateScore:     0.88,
				PrimaryFailureMode: "FM-1",
				IsTunnelDetected:   true,
				RecommendedAction:  "ADVISORY_ZOOM_OUT",
				Signals: []TunnelSignal{
					{
						SignalID:     "sig-123",
						SessionID:    "sess-123",
						DetectorName: "RepeatedErrorDetector",
						FailureMode:  "FM-1",
						Weight:       0.75,
						Score:        0.90,
						TriggeredAt:  now,
					},
				},
				EvaluatedAt: now,
			},
			factory: func() any { return &TunnelAssessment{} },
		},
		{
			name: "ReviewRequest",
			instance: ReviewRequest{
				RequestID:      "rr-123",
				ReviewerRole:   "TunnelClassifier",
				EvidencePackID: "pack-123",
				Model:          "gpt-4o",
				Prompt:         "Analyze evidence",
				RequestedAt:    now,
			},
			factory: func() any { return &ReviewRequest{} },
		},
		{
			name: "ReviewDecision",
			instance: ReviewDecision{
				DecisionID:       "rd-123",
				RequestID:        "rr-123",
				ReviewerRole:     "TunnelClassifier",
				TunnelConfidence: 0.90,
				Classification:   "TUNNEL_VISION",
				Rationale:        "Repeated error",
				SuggestedAdvice:  "Zoom out",
				TokensUsed:       350,
				DecidedAt:        now,
			},
			factory: func() any { return &ReviewDecision{} },
		},
		{
			name: "Intervention",
			instance: Intervention{
				InterventionID:     "itv-123",
				SessionID:          "sess-123",
				Level:              1,
				ActionType:         "ZOOM_OUT_PROMPT",
				AdvicePrompt:       "Advice",
				TargetCheckpointID: "chk-123",
				Status:             "EXECUTED",
				ExecutedAt:         now,
			},
			factory: func() any { return &Intervention{} },
		},
		{
			name: "BudgetState",
			instance: BudgetState{
				SessionID:         "sess-123",
				MaxTokens:         100000,
				UsedTokens:        25000,
				MaxCostUSD:        5.0,
				CurrentCostUSD:    1.25,
				MaxInterventions:  3,
				InterventionCount: 1,
				IsExhausted:       false,
			},
			factory: func() any { return &BudgetState{} },
		},
		{
			name: "CapabilityManifest",
			instance: CapabilityManifest{
				AgentID:            "claude-code",
				Version:            "1.0.0",
				IntegrationLevel:   2,
				SupportsPause:      true,
				SupportsCancel:     true,
				SupportsResume:     true,
				SupportsCheckpoint: true,
				SupportsRollback:   true,
				SupportsMCP:        true,
			},
			factory: func() any { return &CapabilityManifest{} },
		},
		{
			name: "Checkpoint",
			instance: Checkpoint{
				CheckpointID:     "chk-123",
				SessionID:        "sess-123",
				GitCommitHash:    "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
				BranchName:       "main",
				Description:      "Clean build",
				PassingTestCount: 10,
				CreatedAt:        now,
			},
			factory: func() any { return &Checkpoint{} },
		},
		{
			name: "RollbackResult",
			instance: RollbackResult{
				RollbackID:         "rb-123",
				SessionID:          "sess-123",
				TargetCheckpointID: "chk-123",
				PreviousCommitHash: "1111111111111111111111111111111111111111",
				RestoredCommitHash: "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
				Success:            true,
				ErrorMessage:       "none",
				CompletedAt:        now,
			},
			factory: func() any { return &RollbackResult{} },
		},
		{
			name: "ProviderUsage",
			instance: ProviderUsage{
				UsageID:          "usg-123",
				SessionID:        "sess-123",
				ProviderName:     "anthropic",
				Model:            "claude-3-5-sonnet",
				PromptTokens:     1000,
				CompletionTokens: 200,
				TotalTokens:      1200,
				EstimatedCostUSD: 0.006,
				LatencyMs:        450,
				Timestamp:        now,
			},
			factory: func() any { return &ProviderUsage{} },
		},
		{
			name: "AuditRecord",
			instance: AuditRecord{
				AuditID:    "aud-123",
				SessionID:  "sess-123",
				Actor:      "SUPERVISOR",
				Category:   "INTERVENTION",
				Summary:    "Zoom out prompt",
				DetailJSON: json.RawMessage(`{"detail":"ok"}`),
				RecordedAt: now,
			},
			factory: func() any { return &AuditRecord{} },
		},
	}

	if len(tests) != 22 {
		t.Fatalf("expected 22 structs for roundtrip test, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b1, err := json.Marshal(tt.instance)
			if err != nil {
				t.Fatalf("failed to marshal %s: %v", tt.name, err)
			}

			target := tt.factory()
			if err := json.Unmarshal(b1, target); err != nil {
				t.Fatalf("failed to unmarshal %s: %v", tt.name, err)
			}

			b2, err := json.Marshal(target)
			if err != nil {
				t.Fatalf("failed to re-marshal %s: %v", tt.name, err)
			}

			if string(b1) != string(b2) {
				t.Errorf("roundtrip JSON mismatch for %s:\noriginal : %s\nroundtrip: %s", tt.name, string(b1), string(b2))
			}
		})
	}
}

func TestRedactionTags(t *testing.T) {
	validTags := map[string]bool{
		"none":      true,
		"path":      true,
		"sensitive": true,
		"sanitize":  true,
	}

	structs := []any{
		AgentSession{},
		TaskEnvelope{},
		AgentEvent{},
		ToolCallEvent{},
		FileChangeEvent{},
		TestResultEvent{},
		ErrorFingerprint{},
		EvidenceItem{},
		EvidencePack{},
		Hypothesis{},
		Assumption{},
		TunnelSignal{},
		TunnelAssessment{},
		ReviewRequest{},
		ReviewDecision{},
		Intervention{},
		BudgetState{},
		CapabilityManifest{},
		Checkpoint{},
		RollbackResult{},
		ProviderUsage{},
		AuditRecord{},
	}

	if len(structs) != 22 {
		t.Fatalf("expected 22 structs for redaction tag audit, got %d", len(structs))
	}

	for _, s := range structs {
		st := reflect.TypeOf(s)
		t.Run(st.Name(), func(t *testing.T) {
			numFields := st.NumField()
			if numFields == 0 {
				t.Fatalf("struct %s has no fields", st.Name())
			}

			for i := 0; i < numFields; i++ {
				field := st.Field(i)
				// Only check exported fields
				if field.PkgPath != "" {
					continue
				}

				tagValue := field.Tag.Get("redact")
				if tagValue == "" {
					t.Errorf("field %s.%s is missing 'redact' tag", st.Name(), field.Name)
					continue
				}

				if !validTags[tagValue] {
					t.Errorf("field %s.%s has invalid 'redact' tag value %q (must be one of: none, path, sensitive, sanitize)", st.Name(), field.Name, tagValue)
				}
			}
		})
	}
}

func BenchmarkValidateEvent(b *testing.B) {
	payload := []byte(`{
		"tool_call_id": "call-123",
		"tool_name": "Bash",
		"arguments": {"command": "go test ./..."},
		"duration_ms": 150
	}`)

	if err := LoadSchemas(); err != nil {
		b.Fatalf("failed to load schemas: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ValidateEvent(payload, "tool_call_event"); err != nil {
			b.Fatalf("validation failed: %v", err)
		}
	}
}

func TestCapability_JSONRoundTrip_Lossless(t *testing.T) {
	manifest := CapabilityManifest{
		AgentID:                "test-agent",
		Version:                "1.0.0",
		IntegrationLevel:       2,
		SupportsEventStream:    true,
		SupportsToolInspection: true,
		SupportsDiffInspection: true,
		SupportsCostTracking:   true,
		SupportsHooks:          true,
		SupportsHeadless:       true,
		SupportsCLIControl:     true,
		SupportsPause:          true,
		SupportsCancel:         true,
		SupportsResume:         true,
		SupportsCheckpoint:     true,
		SupportsRollback:       true,
		SupportsMCP:            true,
		SupportsSubagents:      true,
		SupportsExtensions:     true,
		SupportsSwitchModel:    true,
		SupportsCustomProvider: true,
		SupportsOpenAICompat:   true,
		SupportsLocalModels:    true,
		SupportsSDK:            true,
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("failed to marshal CapabilityManifest: %v", err)
	}

	if err := ValidateEvent(data, "capability_manifest"); err != nil {
		t.Fatalf("ValidateEvent failed for full CapabilityManifest: %v", err)
	}

	var restored CapabilityManifest
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("failed to unmarshal CapabilityManifest: %v", err)
	}

	if restored.SupportsEventStream != manifest.SupportsEventStream ||
		restored.SupportsToolInspection != manifest.SupportsToolInspection ||
		restored.SupportsDiffInspection != manifest.SupportsDiffInspection ||
		restored.SupportsCostTracking != manifest.SupportsCostTracking ||
		restored.SupportsHooks != manifest.SupportsHooks ||
		restored.SupportsHeadless != manifest.SupportsHeadless ||
		restored.SupportsCLIControl != manifest.SupportsCLIControl ||
		restored.SupportsPause != manifest.SupportsPause ||
		restored.SupportsCancel != manifest.SupportsCancel ||
		restored.SupportsResume != manifest.SupportsResume ||
		restored.SupportsCheckpoint != manifest.SupportsCheckpoint ||
		restored.SupportsRollback != manifest.SupportsRollback ||
		restored.SupportsMCP != manifest.SupportsMCP ||
		restored.SupportsSubagents != manifest.SupportsSubagents ||
		restored.SupportsExtensions != manifest.SupportsExtensions ||
		restored.SupportsSwitchModel != manifest.SupportsSwitchModel ||
		restored.SupportsCustomProvider != manifest.SupportsCustomProvider ||
		restored.SupportsOpenAICompat != manifest.SupportsOpenAICompat ||
		restored.SupportsLocalModels != manifest.SupportsLocalModels ||
		restored.SupportsSDK != manifest.SupportsSDK {
		t.Fatalf("JSON roundtrip lost capability boolean flags: got %+v, want %+v", restored, manifest)
	}

	expectedMask := uint64((1 << 20) - 1)
	if mask := restored.ToBitmask(); mask != expectedMask {
		t.Errorf("ToBitmask mismatch after JSON roundtrip: got 0x%x, want 0x%x", mask, expectedMask)
	}
}

func TestValidateEvent_TrailingContent(t *testing.T) {
	baseJSON := `{"session_id":"s1","agent_id":"a1","adapter_type":"cli_process","workspace_path":"/tmp","status":"OBSERVE","started_at":"2024-01-01T00:00:00Z","integration_level":0}`

	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{
			name:    "Valid JSON + second JSON object",
			payload: baseJSON + `{"extra":true}`,
			wantErr: true,
		},
		{
			name:    "Valid JSON + trailing text",
			payload: baseJSON + ` trailing text`,
			wantErr: true,
		},
		{
			name:    "Valid JSON + trailing whitespace only",
			payload: baseJSON + "  \n\t ",
			wantErr: false,
		},
		{
			name:    "Single valid JSON",
			payload: baseJSON,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEvent([]byte(tt.payload), "agent_session")
			if tt.wantErr && err == nil {
				t.Errorf("expected error for payload %q, got nil", tt.payload)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error for payload %q: %v", tt.payload, err)
			}
		})
	}
}

