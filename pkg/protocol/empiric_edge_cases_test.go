package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestEdgeCase_OversizedPayloads tests payloads around the 1MB boundary.
func TestEdgeCase_OversizedPayloads(t *testing.T) {
	t.Run("Payload_Exactly_MaxPayloadSize", func(t *testing.T) {
		// Construct valid JSON of exact length MaxPayloadSize (1MB = 1048576 bytes)
		// We use a large prompt in TaskEnvelope
		prefix := `{"task_id":"t1","session_id":"s1","prompt":"`
		suffix := `","max_depth":1,"timeout_seconds":10,"created_at":"2026-08-02T13:00:00Z"}`
		neededPadding := MaxPayloadSize - len(prefix) - len(suffix)

		if neededPadding > 0 {
			padding := strings.Repeat("a", neededPadding)
			payload := []byte(prefix + padding + suffix)
			if len(payload) != MaxPayloadSize {
				t.Fatalf("constructed payload length %d != %d", len(payload), MaxPayloadSize)
			}
			// Size check must pass (error if any should come from validation, not size limit)
			err := ValidateEvent(payload, "task_envelope")
			if err != nil {
				t.Fatalf("expected payload of exact size 1MB to pass size limit check, got err: %v", err)
			}
		}
	})

	t.Run("Payload_Exceeds_MaxPayloadSize_By_1_Byte", func(t *testing.T) {
		prefix := `{"task_id":"t1","session_id":"s1","prompt":"`
		suffix := `","max_depth":1,"timeout_seconds":10,"created_at":"2026-08-02T13:00:00Z"}`
		neededPadding := MaxPayloadSize - len(prefix) - len(suffix) + 1

		padding := strings.Repeat("a", neededPadding)
		payload := []byte(prefix + padding + suffix)
		if len(payload) != MaxPayloadSize+1 {
			t.Fatalf("constructed payload length %d != %d", len(payload), MaxPayloadSize+1)
		}

		err := ValidateEvent(payload, "task_envelope")
		if err == nil {
			t.Fatalf("expected size limit error for payload of size 1MB+1, got nil")
		}
		expectedSubstr := fmt.Sprintf("payload size %d exceeds maximum limit of %d bytes", MaxPayloadSize+1, MaxPayloadSize)
		if err.Error() != expectedSubstr {
			t.Errorf("unexpected error message: %q, want %q", err.Error(), expectedSubstr)
		}
	})
}

// TestEdgeCase_FloatingPointInIntegerFields tests float values in integer-typed schema fields.
func TestEdgeCase_FloatingPointInIntegerFields(t *testing.T) {
	testCases := []struct {
		name       string
		schemaType string
		payload    string
	}{
		{
			name:       "Float_In_AgentSession_IntegrationLevel",
			schemaType: "agent_session",
			payload: `{
				"session_id": "sess-1",
				"agent_id": "claude",
				"adapter_type": "cli_process",
				"integration_level": 1.5,
				"workspace_path": "/tmp",
				"status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "Float_In_TaskEnvelope_MaxDepth",
			schemaType: "task_envelope",
			payload: `{
				"task_id": "t1",
				"session_id": "s1",
				"prompt": "prompt",
				"max_depth": 1.5,
				"timeout_seconds": 10,
				"created_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "Float_In_FileChangeEvent_LinesAdded",
			schemaType: "file_change_event",
			payload: `{
				"file_path": "a.go",
				"change_type": "MODIFIED",
				"lines_added": 10.7,
				"lines_removed": 2,
				"is_scope_violation": false
			}`,
		},
		{
			name:       "Float_In_ToolCallEvent_DurationMs",
			schemaType: "tool_call_event",
			payload: `{
				"tool_call_id": "tc-1",
				"tool_name": "Bash",
				"arguments": {},
				"duration_ms": 150.99
			}`,
		},
		{
			name:       "Float_In_TestResultEvent_PassedCount",
			schemaType: "test_result_event",
			payload: `{
				"test_run_id": "tr-1",
				"command": "go test",
				"passed_count": 5.5,
				"failed_count": 0,
				"skipped_count": 0,
				"pass_delta": 0,
				"duration_ms": 100
			}`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEvent([]byte(tc.payload), tc.schemaType)
			if err == nil {
				t.Fatalf("expected validation error for floating-point number in integer field (%s), got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "validation error") {
				t.Errorf("expected 'validation error' in error string, got: %v", err)
			}
		})
	}
}

// TestEdgeCase_MissingBooleanFieldsCapabilityNegotiation tests capability negotiation when boolean fields are missing.
func TestEdgeCase_MissingBooleanFieldsCapabilityNegotiation(t *testing.T) {
	t.Run("JSON_Schema_Validation_Fails_When_Boolean_Field_Missing", func(t *testing.T) {
		// Missing supports_sdk
		incompleteJSON := `{
			"agent_id": "claude",
			"version": "1.0.0",
			"integration_level": 2,
			"supports_event_stream": true,
			"supports_tool_inspection": true,
			"supports_diff_inspection": true,
			"supports_cost_tracking": true,
			"supports_hooks": true,
			"supports_headless": true,
			"supports_cli_control": true,
			"supports_pause": true,
			"supports_cancel": true,
			"supports_resume": true,
			"supports_checkpoint": true,
			"supports_rollback": true,
			"supports_mcp": true,
			"supports_subagents": true,
			"supports_extensions": true,
			"supports_switch_model": true,
			"supports_custom_provider": true,
			"supports_openai_compat": true,
			"supports_local_models": true
		}`

		err := ValidateEvent([]byte(incompleteJSON), "capability_manifest")
		if err == nil {
			t.Fatalf("expected schema validation error for missing boolean field in capability_manifest, got nil")
		}
	})

	t.Run("Go_Struct_Unmarshaling_Missing_Booleans_Default_To_False_And_Degrade", func(t *testing.T) {
		// JSON missing process control flags (supports_pause, supports_cancel, supports_resume)
		jsonPayload := `{
			"agent_id": "claude",
			"version": "1.0.0",
			"integration_level": 2,
			"supports_event_stream": true,
			"supports_tool_inspection": true,
			"supports_diff_inspection": true,
			"supports_checkpoint": true,
			"supports_rollback": true
		}`

		var manifest CapabilityManifest
		if err := json.Unmarshal([]byte(jsonPayload), &manifest); err != nil {
			t.Fatalf("json.Unmarshal failed: %v", err)
		}

		if manifest.SupportsPause || manifest.SupportsCancel || manifest.SupportsResume {
			t.Errorf("missing boolean fields in JSON should default to false")
		}

		// Negotiate Level 2 request
		req := &HandshakeRequest{
			SessionID:      "sess-degrade-missing-booleans",
			RequestedLevel: 2,
			Manifest:       manifest,
		}

		resp, err := NegotiateLevel(req)
		if err != nil {
			t.Fatalf("unexpected error during negotiation: %v", err)
		}

		if resp.NegotiatedLevel != 1 {
			t.Errorf("expected negotiated level to degrade to 1 due to missing process control booleans, got %d", resp.NegotiatedLevel)
		}
		if !resp.IsDegraded || resp.DegradedFrom != 2 {
			t.Errorf("expected IsDegraded=true, DegradedFrom=2, got %+v", resp)
		}
		expectedMissing := []string{"CapPause", "CapCancel", "CapResume"}
		if len(resp.MissingFlags) != 3 {
			t.Errorf("expected 3 missing flags %v, got %v", expectedMissing, resp.MissingFlags)
		}
	})
}

// TestEdgeCase_RESUME_SessionStateTransitions tests valid and invalid status enum values for AgentSession.
func TestEdgeCase_RESUME_SessionStateTransitions(t *testing.T) {
	validStatuses := []string{
		"OBSERVE",
		"EXECUTE",
		"AUDIT",
		"SUSPECT",
		"ZOOM_OUT",
		"PAUSED",
		"RESUME",
		"TERMINATED",
		"COMPLETED",
	}

	for _, status := range validStatuses {
		t.Run(fmt.Sprintf("ValidStatus_%s", status), func(t *testing.T) {
			sess := AgentSession{
				SessionID:        "sess-status-" + status,
				AgentID:          "claude",
				AdapterType:      "cli_process",
				IntegrationLevel: 2,
				WorkspacePath:    "/tmp",
				Status:           status,
				StartedAt:        time.Now().UTC(),
			}

			payload, err := json.Marshal(sess)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			if err := ValidateEvent(payload, "agent_session"); err != nil {
				t.Errorf("expected status %q to pass validation, got err: %v", status, err)
			}
		})
	}

	invalidStatuses := []string{
		"resume",
		"RESUMING",
		"RESTART",
		"PAUSE",
		"CANCELLED",
		"INVALID",
	}

	for _, status := range invalidStatuses {
		t.Run(fmt.Sprintf("InvalidStatus_%s", status), func(t *testing.T) {
			sess := AgentSession{
				SessionID:        "sess-invalid-status",
				AgentID:          "claude",
				AdapterType:      "cli_process",
				IntegrationLevel: 2,
				WorkspacePath:    "/tmp",
				Status:           status,
				StartedAt:        time.Now().UTC(),
			}

			payload, err := json.Marshal(sess)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			if err := ValidateEvent(payload, "agent_session"); err == nil {
				t.Errorf("expected invalid status %q to fail validation, got nil", status)
			}
		})
	}
}

// TestEdgeCase_InvalidMaxDepth tests bounds on task_envelope max_depth (strictly maximum 1).
func TestEdgeCase_InvalidMaxDepth(t *testing.T) {
	invalidMaxDepths := []int{
		0, -1, 2, 3, 5, 10, 100,
	}

	for _, md := range invalidMaxDepths {
		t.Run(fmt.Sprintf("MaxDepth_%d", md), func(t *testing.T) {
			env := TaskEnvelope{
				TaskID:         "task-md",
				SessionID:      "sess-md",
				Prompt:         "Subagent prompt",
				MaxDepth:       md,
				TimeoutSeconds: 60,
				CreatedAt:      time.Now().UTC(),
			}

			payload, err := json.Marshal(env)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			err = ValidateEvent(payload, "task_envelope")
			if err == nil {
				t.Fatalf("expected max_depth %d to fail validation (max allowed is 1), got nil", md)
			}
			if !strings.Contains(err.Error(), "validation error") {
				t.Errorf("expected 'validation error', got: %v", err)
			}
		})
	}

	t.Run("MaxDepth_1_Passes", func(t *testing.T) {
		env := TaskEnvelope{
			TaskID:         "task-md-1",
			SessionID:      "sess-md-1",
			Prompt:         "Allowed prompt",
			MaxDepth:       1,
			TimeoutSeconds: 60,
			CreatedAt:      time.Now().UTC(),
		}

		payload, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}

		if err := ValidateEvent(payload, "task_envelope"); err != nil {
			t.Fatalf("expected max_depth 1 to pass validation, got: %v", err)
		}
	})
}
