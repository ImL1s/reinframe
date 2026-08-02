package protocol

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestChallenger_ConcurrentStress tests thread safety and performance of ValidateEvent under heavy concurrency.
func TestChallenger_ConcurrentStress(t *testing.T) {
	if err := LoadSchemas(); err != nil {
		t.Fatalf("LoadSchemas failed: %v", err)
	}

	payloads := map[string][]byte{
		"agent_session": []byte(`{
			"session_id": "sess-stress-1",
			"agent_id": "claude-code",
			"adapter_type": "cli_process",
			"integration_level": 2,
			"workspace_path": "/tmp/workspace",
			"status": "EXECUTE",
			"started_at": "2026-08-02T13:00:00Z"
		}`),
		"task_envelope": []byte(`{
			"task_id": "task-stress-1",
			"session_id": "sess-stress-1",
			"prompt": "Stress test prompt",
			"max_depth": 2,
			"timeout_seconds": 60,
			"created_at": "2026-08-02T13:00:00Z"
		}`),
		"tool_call_event": []byte(`{
			"tool_call_id": "tc-stress-1",
			"tool_name": "Bash",
			"arguments": {"cmd": "go test"},
			"duration_ms": 50
		}`),
	}

	const goroutines = 200
	const iterations = 500

	var wg sync.WaitGroup
	var errorCount int64

	t.Logf("Launching %d goroutines with %d iterations each (%d total validations)", goroutines, iterations, goroutines*iterations)

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			types := []string{"AgentSession", "task_envelope", "tool_call_event"}
			for j := 0; j < iterations; j++ {
				st := types[(id+j)%len(types)]
				p := payloads[toSnakeCase(st)]
				if err := ValidateEvent(p, st); err != nil {
					atomic.AddInt64(&errorCount, 1)
					t.Errorf("Goroutine %d iteration %d failed for %s: %v", id, j, st, err)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	if errorCount > 0 {
		t.Fatalf("Encountered %d validation errors during concurrent stress test", errorCount)
	}

	t.Logf("Concurrent stress test passed! %d validations completed in %v (avg %.2f us/op)",
		goroutines*iterations, elapsed, float64(elapsed.Microseconds())/float64(goroutines*iterations))
}

// TestChallenger_SchemaTypeNormalization tests edge cases in schemaType string resolution.
func TestChallenger_SchemaTypeNormalization(t *testing.T) {
	validPayload := []byte(`{
		"session_id": "sess-norm-1",
		"agent_id": "claude-code",
		"adapter_type": "cli_process",
		"integration_level": 2,
		"workspace_path": "/tmp/workspace",
		"status": "EXECUTE",
		"started_at": "2026-08-02T13:00:00Z"
	}`)

	cases := []struct {
		schemaType string
		shouldPass bool
	}{
		{"AgentSession", true},
		{"agent_session", true},
		{"AGENT_SESSION", true},
		{"agentSession", true},
		{"Agent_Session", true},
		{"", false},
		{"UnknownType", false},
		{"agent_session_extra", false},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("Type_%s", c.schemaType), func(t *testing.T) {
			err := ValidateEvent(validPayload, c.schemaType)
			if c.shouldPass && err != nil {
				t.Errorf("expected pass for %q, got error: %v", c.schemaType, err)
			}
			if !c.shouldPass && err == nil {
				t.Errorf("expected error for %q, got nil", c.schemaType)
			}
		})
	}
}

// TestChallenger_BoundaryAndEdgeCases tests invalid payloads, null values, out-of-bounds numbers, and extra properties across schemas.
func TestChallenger_BoundaryAndEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		schemaType string
		payload    string
		expectErr  bool
	}{
		{
			name:       "EmptyPayload",
			schemaType: "agent_session",
			payload:    "",
			expectErr:  true,
		},
		{
			name:       "NullPayload",
			schemaType: "agent_session",
			payload:    "null",
			expectErr:  true,
		},
		{
			name:       "EmptyJSONObject",
			schemaType: "agent_session",
			payload:    "{}",
			expectErr:  true,
		},
		{
			name:       "ExtraPropertiesViolation",
			schemaType: "agent_session",
			payload: `{
				"session_id": "s1", "agent_id": "a1", "adapter_type": "cli_process",
				"integration_level": 1, "workspace_path": "/w", "status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z", "unsupported_extra_field": 123
			}`,
			expectErr: true,
		},
		{
			name:       "MinLengthViolation_EmptySessionID",
			schemaType: "agent_session",
			payload: `{
				"session_id": "", "agent_id": "a1", "adapter_type": "cli_process",
				"integration_level": 1, "workspace_path": "/w", "status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z"
			}`,
			expectErr: true,
		},
		{
			name:       "DateTimeFormatInvalid",
			schemaType: "agent_session",
			payload: `{
				"session_id": "s1", "agent_id": "a1", "adapter_type": "cli_process",
				"integration_level": 1, "workspace_path": "/w", "status": "EXECUTE",
				"started_at": "invalid-date-format"
			}`,
			expectErr: true,
		},
		{
			name:       "IntegrationLevelNegative",
			schemaType: "agent_session",
			payload: `{
				"session_id": "s1", "agent_id": "a1", "adapter_type": "cli_process",
				"integration_level": -1, "workspace_path": "/w", "status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z"
			}`,
			expectErr: true,
		},
		{
			name:       "IntegrationLevelAboveMax",
			schemaType: "agent_session",
			payload: `{
				"session_id": "s1", "agent_id": "a1", "adapter_type": "cli_process",
				"integration_level": 4, "workspace_path": "/w", "status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z"
			}`,
			expectErr: true,
		},
		{
			name:       "OptionalEndedAtValid",
			schemaType: "agent_session",
			payload: `{
				"session_id": "s1", "agent_id": "a1", "adapter_type": "cli_process",
				"integration_level": 1, "workspace_path": "/w", "status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z", "ended_at": "2026-08-02T14:00:00Z"
			}`,
			expectErr: false,
		},
		{
			name:       "EndedAtNullHandling",
			schemaType: "agent_session",
			payload: `{
				"session_id": "s1", "agent_id": "a1", "adapter_type": "cli_process",
				"integration_level": 1, "workspace_path": "/w", "status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z", "ended_at": null
			}`,
			expectErr: true, // string format date-time does not allow null JSON value
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEvent([]byte(tt.payload), tt.schemaType)
			if tt.expectErr && err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
			if !tt.expectErr && err != nil {
				t.Errorf("expected no error for %s, got: %v", tt.name, err)
			}
		})
	}
}

// TestChallenger_All22SchemasValidation validates minimal compliant payloads for all 22 schemas.
func TestChallenger_All22SchemasValidation(t *testing.T) {
	now := "2026-08-02T13:00:00Z"
	payloads := map[string]string{
		"agent_session": fmt.Sprintf(`{
			"session_id": "s1", "agent_id": "a1", "adapter_type": "cli_process",
			"integration_level": 1, "workspace_path": "/tmp", "status": "EXECUTE",
			"started_at": %q
		}`, now),
		"task_envelope": fmt.Sprintf(`{
			"task_id": "t1", "session_id": "s1", "prompt": "do task",
			"max_depth": 1, "timeout_seconds": 10, "created_at": %q
		}`, now),
		"agent_event": fmt.Sprintf(`{
			"event_id": "e1", "session_id": "s1", "sequence_num": 1,
			"event_type": "tool_call", "timestamp": %q, "payload": {}
		}`, now),
		"tool_call_event": `{
			"tool_call_id": "tc1", "tool_name": "Bash", "arguments": {}, "duration_ms": 10
		}`,
		"file_change_event": `{
			"file_path": "a.txt", "change_type": "MODIFIED", "lines_added": 1,
			"lines_removed": 0, "is_scope_violation": false
		}`,
		"test_result_event": `{
			"test_run_id": "tr1", "command": "go test", "passed_count": 1,
			"failed_count": 0, "skipped_count": 0, "pass_delta": 0, "duration_ms": 10
		}`,
		"error_fingerprint": fmt.Sprintf(`{
			"fingerprint_id": "fp1", "raw_error": "err", "normalized_text": "err",
			"occurrence_count": 1, "first_observed_at": %q, "last_observed_at": %q
		}`, now, now),
		"evidence_item": `{
			"item_id": "ei1", "source": "stderr", "category": "ERROR_TRACE", "content": "trace"
		}`,
		"evidence_pack": fmt.Sprintf(`{
			"pack_id": "ep1", "session_id": "s1", "fork_turns": "0", "items": [],
			"churn_ratio": 0.0, "repeated_error_count": 0, "created_at": %q
		}`, now),
		"hypothesis": `{
			"hypothesis_id": "h1", "statement": "stmt", "status": "PROPOSED", "confidence_score": 0.5
		}`,
		"assumption": `{
			"assumption_id": "a1", "description": "desc", "is_verified": true
		}`,
		"tunnel_signal": fmt.Sprintf(`{
			"signal_id": "ts1", "session_id": "s1", "detector_name": "det",
			"failure_mode": "FM-1", "weight": 0.5, "score": 0.5, "triggered_at": %q
		}`, now),
		"tunnel_assessment": fmt.Sprintf(`{
			"assessment_id": "ta1", "session_id": "s1", "aggregate_score": 0.5,
			"primary_failure_mode": "FM-1", "is_tunnel_detected": false,
			"recommended_action": "NONE", "signals": [], "evaluated_at": %q
		}`, now),
		"review_request": fmt.Sprintf(`{
			"request_id": "rr1", "reviewer_role": "TunnelClassifier", "evidence_pack_id": "ep1",
			"model": "m1", "prompt": "p1", "requested_at": %q
		}`, now),
		"review_decision": fmt.Sprintf(`{
			"decision_id": "rd1", "request_id": "rr1", "reviewer_role": "TunnelClassifier",
			"tunnel_confidence": 0.5, "classification": "NORMAL_PROGRESS", "rationale": "ok",
			"tokens_used": 10, "decided_at": %q
		}`, now),
		"intervention": fmt.Sprintf(`{
			"intervention_id": "i1", "session_id": "s1", "level": 1,
			"action_type": "ZOOM_OUT_PROMPT", "status": "PENDING", "executed_at": %q
		}`, now),
		"budget_state": `{
			"session_id": "s1", "max_tokens": 100, "used_tokens": 10,
			"max_cost_usd": 1.0, "current_cost_usd": 0.1, "max_interventions": 1,
			"intervention_count": 0, "is_exhausted": false
		}`,
		"capability_manifest": `{
			"agent_id": "a1", "version": "1.0", "integration_level": 1,
			"supports_pause": true, "supports_cancel": true, "supports_resume": true,
			"supports_checkpoint": true, "supports_rollback": true, "supports_mcp": true
		}`,
		"checkpoint": fmt.Sprintf(`{
			"checkpoint_id": "c1", "session_id": "s1", "git_commit_hash": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0",
			"branch_name": "main", "description": "d", "passing_test_count": 5, "created_at": %q
		}`, now),
		"rollback_result": fmt.Sprintf(`{
			"rollback_id": "rb1", "session_id": "s1", "target_checkpoint_id": "c1",
			"previous_commit_hash": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "restored_commit_hash": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0", "success": true,
			"completed_at": %q
		}`, now),
		"provider_usage": fmt.Sprintf(`{
			"usage_id": "pu1", "session_id": "s1", "provider_name": "anthropic", "model": "m",
			"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15,
			"estimated_cost_usd": 0.01, "latency_ms": 100, "timestamp": %q
		}`, now),
		"audit_record": fmt.Sprintf(`{
			"audit_id": "ar1", "session_id": "s1", "actor": "SUPERVISOR",
			"category": "INTERVENTION", "summary": "s", "recorded_at": %q
		}`, now),
	}

	if len(payloads) != 22 {
		t.Fatalf("expected 22 schema payloads, got %d", len(payloads))
	}

	for st, payload := range payloads {
		t.Run(st, func(t *testing.T) {
			if err := ValidateEvent([]byte(payload), st); err != nil {
				t.Errorf("ValidateEvent failed for schema type %s: %v", st, err)
			}
		})
	}
}
