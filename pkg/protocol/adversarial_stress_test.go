package protocol

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestAdversarial_EmptyPayloads tests empty strings, empty JSON structures, and whitespace payloads.
func TestAdversarial_EmptyPayloads(t *testing.T) {
	emptyPayloads := []struct {
		name    string
		payload []byte
	}{
		{"EmptyByteSlice", []byte("")},
		{"SingleSpace", []byte(" ")},
		{"WhitespaceTabsNewlines", []byte("\t\n\r  ")},
		{"JSONNull", []byte("null")},
		{"EmptyStringJSON", []byte(`""`)},
		{"EmptyArrayJSON", []byte(`[]`)},
		{"EmptyObjectJSON", []byte(`{}`)},
	}

	types := []string{
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

	for _, st := range types {
		for _, ep := range emptyPayloads {
			t.Run(fmt.Sprintf("%s/%s", st, ep.name), func(t *testing.T) {
				err := ValidateEvent(ep.payload, st)
				if err == nil {
					t.Errorf("expected error for empty payload %s on schema %s, got nil", ep.name, st)
				}
			})
		}
	}
}

// TestAdversarial_CorruptBytes tests non-UTF8 bytes, truncated JSON, and corrupt structure.
func TestAdversarial_CorruptBytes(t *testing.T) {
	corruptPayloads := []struct {
		name    string
		payload []byte
	}{
		{"InvalidUTF8_HighBytes", []byte{0xff, 0xfe, 0xfd, 0x00}},
		{"TruncatedObject", []byte(`{"session_id": "sess-123"`)},
		{"TruncatedString", []byte(`{"session_id": "sess-123`)},
		{"TrailingComma", []byte(`{"session_id": "sess-123",}`)},
		{"UnquotedKeys", []byte(`{session_id: "sess-123"}`)},
		{"SingleQuotes", []byte(`{'session_id': 'sess-123'}`)},
		{"EmbeddedNUL", []byte("{\"session_id\x00\": \"sess-123\"}")},
		{"RawBinaryBlob", []byte("\x7fELF\x02\x01\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00")},
	}

	for _, cp := range corruptPayloads {
		t.Run(cp.name, func(t *testing.T) {
			err := ValidateEvent(cp.payload, "agent_session")
			if err == nil {
				t.Fatalf("expected error for corrupt payload %s, got nil", cp.name)
			}
			if !strings.Contains(err.Error(), "malformed JSON payload") {
				t.Errorf("expected 'malformed JSON payload' in error, got: %v", err)
			}
		})
	}
}

// TestAdversarial_UnexpectedProperties tests payloads containing arbitrary injected fields.
func TestAdversarial_UnexpectedProperties(t *testing.T) {
	validAgentSession := `{
		"session_id": "sess-123",
		"agent_id": "claude-code",
		"adapter_type": "cli_process",
		"integration_level": 2,
		"workspace_path": "/tmp/workspace",
		"status": "EXECUTE",
		"started_at": "2026-08-02T13:00:00Z"
	}`

	injectedAgentSession := `{
		"session_id": "sess-123",
		"agent_id": "claude-code",
		"adapter_type": "cli_process",
		"integration_level": 2,
		"workspace_path": "/tmp/workspace",
		"status": "EXECUTE",
		"started_at": "2026-08-02T13:00:00Z",
		"unauthorized_field": "hacked",
		"admin_override": true
	}`

	t.Run("ValidSessionPasses", func(t *testing.T) {
		if err := ValidateEvent([]byte(validAgentSession), "agent_session"); err != nil {
			t.Fatalf("valid session failed: %v", err)
		}
	})

	t.Run("UnexpectedPropertyRejected", func(t *testing.T) {
		err := ValidateEvent([]byte(injectedAgentSession), "agent_session")
		if err == nil {
			t.Fatalf("expected rejection for unexpected property, got nil")
		}
		if !strings.Contains(err.Error(), "validation error") {
			t.Errorf("expected validation error, got: %v", err)
		}
	})
}

// TestAdversarial_NullFields tests behavior when required or optional fields are explicitly set to JSON null.
func TestAdversarial_NullFields(t *testing.T) {
	nullFieldCases := []struct {
		name       string
		schemaType string
		payload    string
	}{
		{
			name:       "NullRequiredString_AgentSession",
			schemaType: "agent_session",
			payload: `{
				"session_id": null,
				"agent_id": "claude",
				"adapter_type": "cli_process",
				"integration_level": 1,
				"workspace_path": "/tmp",
				"status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "NullRequiredInt_AgentSession",
			schemaType: "agent_session",
			payload: `{
				"session_id": "sess-1",
				"agent_id": "claude",
				"adapter_type": "cli_process",
				"integration_level": null,
				"workspace_path": "/tmp",
				"status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "NullRequiredDateTime_AgentSession",
			schemaType: "agent_session",
			payload: `{
				"session_id": "sess-1",
				"agent_id": "claude",
				"adapter_type": "cli_process",
				"integration_level": 1,
				"workspace_path": "/tmp",
				"status": "EXECUTE",
				"started_at": null
			}`,
		},
		{
			name:       "NullOptionalArray_TaskEnvelope",
			schemaType: "task_envelope",
			payload: `{
				"task_id": "task-1",
				"session_id": "sess-1",
				"prompt": "hello",
				"scope_whitelist": null,
				"max_depth": 1,
				"timeout_seconds": 10,
				"created_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "NullOptionalMap_AgentSession",
			schemaType: "agent_session",
			payload: `{
				"session_id": "sess-1",
				"agent_id": "claude",
				"adapter_type": "cli_process",
				"integration_level": 1,
				"workspace_path": "/tmp",
				"status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z",
				"metadata": null
			}`,
		},
	}

	for _, nfc := range nullFieldCases {
		t.Run(nfc.name, func(t *testing.T) {
			err := ValidateEvent([]byte(nfc.payload), nfc.schemaType)
			if err == nil {
				t.Fatalf("expected error for null field in %s, got nil", nfc.name)
			}
		})
	}
}

// TestAdversarial_OutOfRangeNumbers tests boundary violations for numeric fields across schemas.
func TestAdversarial_OutOfRangeNumbers(t *testing.T) {
	outOfRangeCases := []struct {
		name       string
		schemaType string
		payload    string
	}{
		{
			name:       "NegativeIntegrationLevel",
			schemaType: "agent_session",
			payload: `{
				"session_id": "sess-1",
				"agent_id": "claude",
				"adapter_type": "cli_process",
				"integration_level": -1,
				"workspace_path": "/tmp",
				"status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "IntegrationLevelTooHigh",
			schemaType: "agent_session",
			payload: `{
				"session_id": "sess-1",
				"agent_id": "claude",
				"adapter_type": "cli_process",
				"integration_level": 4,
				"workspace_path": "/tmp",
				"status": "EXECUTE",
				"started_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "ZeroMaxDepth",
			schemaType: "task_envelope",
			payload: `{
				"task_id": "task-1",
				"session_id": "sess-1",
				"prompt": "test",
				"max_depth": 0,
				"timeout_seconds": 10,
				"created_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "NegativeTimeoutSeconds",
			schemaType: "task_envelope",
			payload: `{
				"task_id": "task-1",
				"session_id": "sess-1",
				"prompt": "test",
				"max_depth": 1,
				"timeout_seconds": -30,
				"created_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "NegativeSequenceNum",
			schemaType: "agent_event",
			payload: `{
				"event_id": "evt-1",
				"session_id": "sess-1",
				"sequence_num": 0,
				"event_type": "tool_call",
				"timestamp": "2026-08-02T13:00:00Z",
				"payload": {}
			}`,
		},
		{
			name:       "NegativeDurationMs",
			schemaType: "tool_call_event",
			payload: `{
				"tool_call_id": "tc-1",
				"tool_name": "Bash",
				"arguments": {},
				"duration_ms": -10
			}`,
		},
		{
			name:       "TunnelSignalWeightExceedsOne",
			schemaType: "tunnel_signal",
			payload: `{
				"signal_id": "sig-1",
				"session_id": "sess-1",
				"detector_name": "det",
				"failure_mode": "FM-1",
				"weight": 1.05,
				"score": 0.5,
				"triggered_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "TunnelSignalScoreNegative",
			schemaType: "tunnel_signal",
			payload: `{
				"signal_id": "sig-1",
				"session_id": "sess-1",
				"detector_name": "det",
				"failure_mode": "FM-1",
				"weight": 0.5,
				"score": -0.01,
				"triggered_at": "2026-08-02T13:00:00Z"
			}`,
		},
		{
			name:       "BudgetStateNegativeMaxTokens",
			schemaType: "budget_state",
			payload: `{
				"session_id": "sess-1",
				"max_tokens": -100,
				"used_tokens": 0,
				"max_cost_usd": 1.0,
				"current_cost_usd": 0.0,
				"max_interventions": 1,
				"intervention_count": 0,
				"is_exhausted": false
			}`,
		},
		{
			name:       "BudgetStateNegativeCostUSD",
			schemaType: "budget_state",
			payload: `{
				"session_id": "sess-1",
				"max_tokens": 100,
				"used_tokens": 0,
				"max_cost_usd": -5.0,
				"current_cost_usd": 0.0,
				"max_interventions": 1,
				"intervention_count": 0,
				"is_exhausted": false
			}`,
		},
	}

	for _, oor := range outOfRangeCases {
		t.Run(oor.name, func(t *testing.T) {
			err := ValidateEvent([]byte(oor.payload), oor.schemaType)
			if err == nil {
				t.Fatalf("expected out-of-range rejection for %s, got nil", oor.name)
			}
		})
	}
}

// TestAdversarial_SchemaTypeSecurity tests malicious schemaType inputs (path traversal, special chars, long strings).
func TestAdversarial_SchemaTypeSecurity(t *testing.T) {
	validPayload := []byte(`{
		"session_id": "sess-1",
		"agent_id": "claude",
		"adapter_type": "cli_process",
		"integration_level": 1,
		"workspace_path": "/tmp",
		"status": "EXECUTE",
		"started_at": "2026-08-02T13:00:00Z"
	}`)

	maliciousTypes := []string{
		"../schemas/agent_session",
		"../../../../etc/passwd",
		"agent_session.json",
		"agent_session\x00",
		"agent_session; DROP TABLE users; --",
		"<script>alert(1)</script>",
		"   ",
		"\t\n",
		strings.Repeat("A", 10000),
	}

	for _, mt := range maliciousTypes {
		t.Run(fmt.Sprintf("Type_%q", mt), func(t *testing.T) {
			err := ValidateEvent(validPayload, mt)
			if err == nil {
				t.Fatalf("expected error for malicious schemaType %q, got nil", mt)
			}
			if !strings.Contains(err.Error(), "unknown schema type") {
				t.Errorf("expected 'unknown schema type' error, got: %v", err)
			}
		})
	}
}

// TestAdversarial_DeepRecursionPayload tests stack exhaustion resilience with deep nesting.
func TestAdversarial_DeepRecursionPayload(t *testing.T) {
	var buf bytes.Buffer
	depth := 500
	for i := 0; i < depth; i++ {
		buf.WriteString(`{"a":`)
	}
	buf.WriteString(`1`)
	for i := 0; i < depth; i++ {
		buf.WriteString(`}`)
	}

	// Should safely return error without crashing/panicking
	err := ValidateEvent(buf.Bytes(), "agent_session")
	if err == nil {
		t.Fatalf("expected error for deeply nested payload, got nil")
	}
}

// TestAdversarial_ConcurrentStress performs high-concurrency validation checks across goroutines.
func TestAdversarial_ConcurrentStress(t *testing.T) {
	validPayload := []byte(`{
		"session_id": "sess-123",
		"agent_id": "claude-code",
		"adapter_type": "cli_process",
		"integration_level": 2,
		"workspace_path": "/tmp/workspace",
		"status": "EXECUTE",
		"started_at": "2026-08-02T13:00:00Z"
	}`)

	invalidPayload := []byte(`{
		"session_id": "sess-123",
		"status": "INVALID_STATUS"
	}`)

	var wg sync.WaitGroup
	workers := 20
	iterations := 500

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if j%2 == 0 {
					if err := ValidateEvent(validPayload, "agent_session"); err != nil {
						t.Errorf("worker %d iteration %d: expected valid, got error: %v", workerID, j, err)
						return
					}
				} else {
					if err := ValidateEvent(invalidPayload, "agent_session"); err == nil {
						t.Errorf("worker %d iteration %d: expected error, got nil", workerID, j)
						return
					}
				}
			}
		}(i)
	}

	wg.Wait()
}
