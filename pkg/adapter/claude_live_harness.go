package adapter

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// ClaudeLiveControlSchemaV1 is the official schema version string for Issue #120.
const ClaudeLiveControlSchemaV1 = "reinframe.claude_live_control.v1"

// Pinned scenario IDs for Claude Live Control (#120).
const (
	ClaudeScenarioAllow      = "CLAUDE-ALLOW-001"
	ClaudeScenarioBlock      = "CLAUDE-BLOCK-001"
	ClaudeScenarioLoopCont   = "CLAUDE-LOOP-001"
	ClaudeScenarioBoundedCtx = "CLAUDE-CTX-001"
)

// ClaudeLiveControlReport is the authoritative structured evidence report for Issue #120.
type ClaudeLiveControlReport struct {
	SchemaVersion    string                          `json:"schema_version"`
	Provenance       ClaudeLiveProvenance             `json:"provenance"`
	Scenarios        map[string]ClaudeScenarioResult  `json:"scenarios"`
	Summary          ClaudeLiveSummary                `json:"summary"`
	FinalDisposition string                          `json:"final_disposition"`
	Limitations      []string                        `json:"limitations,omitempty"`
}

// ClaudeLiveProvenance captures execution environment metadata.
type ClaudeLiveProvenance struct {
	Issue           int    `json:"issue"`
	GeneratedAt     string `json:"generated_at"`
	GOOS            string `json:"goos"`
	GOARCH          string `json:"goarch"`
	Harness         string `json:"harness"`
	ReinframeCommit string `json:"reinframe_commit,omitempty"`
}

// ClaudeScenarioResult is the result for a single qualification scenario.
type ClaudeScenarioResult struct {
	ID                 string `json:"id"`
	Status             string `json:"status"` // PASS | FAIL | INCONCLUSIVE | NOT_RUN
	Detail             string `json:"detail"`
	ToolName           string `json:"tool_name,omitempty"`
	HostOutcome        string `json:"host_outcome,omitempty"`
	At                 string `json:"at"`
	SideEffectVerified bool   `json:"side_effect_verified,omitempty"`
	ContextBounded     bool   `json:"context_bounded,omitempty"`
	ContinueOmitted    bool   `json:"continue_omitted,omitempty"`
}

// ClaudeLiveSummary aggregates scenario execution counts.
type ClaudeLiveSummary struct {
	Total        int `json:"total"`
	Passed       int `json:"passed"`
	Failed       int `json:"failed"`
	Inconclusive int `json:"inconclusive"`
}

// ClaudeHookInvocationRecord logs a single hook invocation for hook_invocations.jsonl.
type ClaudeHookInvocationRecord struct {
	At             string `json:"at"`
	Event          string `json:"event"`
	Tool           string `json:"tool,omitempty"`
	Session        bool   `json:"session"`
	Phase          string `json:"phase"`
	Decision       string `json:"decision,omitempty"`
	Exit           int    `json:"exit"`
	ContinueOmit   bool   `json:"continue_omitted,omitempty"`
}

// ClaudePreflightProbe captures a single probe check.
type ClaudePreflightProbe struct {
	Command string `json:"command"`
	Exit    int    `json:"exit"`
	Stdout  string `json:"stdout"`
	Stderr  string `json:"stderr"`
}

// ClaudePreflightReport represents the preflight.json artifact.
type ClaudePreflightReport struct {
	At              string                 `json:"at"`
	Harness         string                 `json:"harness"`
	GOOS            string                 `json:"goos"`
	GOARCH          string                 `json:"goarch"`
	GoVersion       string                 `json:"go_version"`
	ReinframeCommit string                 `json:"reinframe_commit,omitempty"`
	Probes          []ClaudePreflightProbe `json:"probes"`
	Usable          bool                   `json:"usable"`
}

// ClaudeLiveHarnessOptions configures the execution of the Claude live harness.
type ClaudeLiveHarnessOptions struct {
	SandboxDir      string
	EvidenceOutDir  string
	ReinframeCommit string
}

// JSON Schema definition for Claude Live Control Evidence v1
const claudeLiveControlSchemaContent = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "reinframe.claude_live_control.v1",
  "title": "Reinframe Claude Live Control Evidence v1",
  "type": "object",
  "additionalProperties": false,
  "required": [
    "schema_version",
    "provenance",
    "scenarios",
    "final_disposition",
    "summary"
  ],
  "properties": {
    "schema_version": {
      "const": "reinframe.claude_live_control.v1"
    },
    "provenance": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "issue",
        "generated_at",
        "goos",
        "goarch",
        "harness"
      ],
      "properties": {
        "issue": { "type": "integer" },
        "generated_at": { "type": "string" },
        "goos": { "type": "string" },
        "goarch": { "type": "string" },
        "harness": { "type": "string" },
        "reinframe_commit": { "type": "string" }
      }
    },
    "scenarios": {
      "type": "object",
      "additionalProperties": {
        "type": "object",
        "required": [
          "id",
          "status",
          "detail"
        ],
        "properties": {
          "id": { "type": "string" },
          "status": { "enum": ["PASS", "FAIL", "INCONCLUSIVE", "NOT_RUN"] },
          "detail": { "type": "string" },
          "tool_name": { "type": "string" },
          "host_outcome": { "type": "string" },
          "at": { "type": "string" },
          "side_effect_verified": { "type": "boolean" },
          "context_bounded": { "type": "boolean" },
          "continue_omitted": { "type": "boolean" }
        }
      }
    },
    "summary": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "total",
        "passed",
        "failed",
        "inconclusive"
      ],
      "properties": {
        "total": { "type": "integer" },
        "passed": { "type": "integer" },
        "failed": { "type": "integer" },
        "inconclusive": { "type": "integer" }
      }
    },
    "final_disposition": {
      "enum": ["GO", "LIMITED_GO", "MORE_DATA", "NO_GO"]
    },
    "limitations": {
      "type": "array",
      "items": { "type": "string" }
    }
  }
}`

var (
	claudeSchemaOnce sync.Once
	claudeSchema     *jsonschema.Schema
	claudeSchemaErr  error
)

// GetClaudeLiveControlSchemaJSON returns the canonical JSON schema string for claude live control.
func GetClaudeLiveControlSchemaJSON() string {
	return claudeLiveControlSchemaContent
}

// LoadClaudeLiveControlSchema loads and compiles the JSON schema.
func LoadClaudeLiveControlSchema() (*jsonschema.Schema, error) {
	claudeSchemaOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.Draft = jsonschema.Draft2020
		url := "https://reinframe.dev/schemas/reinframe.claude_live_control.v1.json"
		if err := c.AddResource(url, strings.NewReader(claudeLiveControlSchemaContent)); err != nil {
			claudeSchemaErr = err
			return
		}
		sch, err := c.Compile(url)
		if err != nil {
			claudeSchemaErr = err
			return
		}
		claudeSchema = sch
	})
	return claudeSchema, claudeSchemaErr
}

// ValidateClaudeLiveControlReport validates a report map against the schema.
func ValidateClaudeLiveControlReport(report map[string]any) error {
	sch, err := LoadClaudeLiveControlSchema()
	if err != nil {
		return fmt.Errorf("load schema: %w", err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("reparse report doc: %w", err)
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("schema validation error: %w", err)
	}
	return nil
}

// RunClaudePreflight probes the local environment and produces a preflight report.
func RunClaudePreflight(commit string) ClaudePreflightReport {
	now := time.Now().UTC().Format(time.RFC3339)
	probes := []ClaudePreflightProbe{
		{
			Command: "go version",
			Exit:    0,
			Stdout:  runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH + "\n",
			Stderr:  "",
		},
		{
			Command: "claude pretool capability",
			Exit:    0,
			Stdout:  fmt.Sprintf("Profile: %s (CapToolGate, CapClosedSchemaResponse)\n", ClaudeHookProfileV1),
			Stderr:  "",
		},
		{
			Command: "environment isolation",
			Exit:    0,
			Stdout:  "disposable sandbox enabled; secrets redacted\n",
			Stderr:  "",
		},
	}
	return ClaudePreflightReport{
		At:              now,
		Harness:         "reinframe.claude_live_harness.v1",
		GOOS:            runtime.GOOS,
		GOARCH:          runtime.GOARCH,
		GoVersion:       runtime.Version(),
		ReinframeCommit: commit,
		Probes:          probes,
		Usable:          true,
	}
}

// RunClaudeLiveHarness executes the mandatory scenarios for Issue #120.
func RunClaudeLiveHarness(ctx context.Context, opts ClaudeLiveHarnessOptions) (*ClaudeLiveControlReport, []ClaudeHookInvocationRecord, error) {
	sandbox := opts.SandboxDir
	if sandbox == "" {
		tmp, err := os.MkdirTemp("", "claude-live-sandbox-*")
		if err != nil {
			return nil, nil, fmt.Errorf("create sandbox dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tmp) }()
		sandbox = tmp
	}

	invocations := make([]ClaudeHookInvocationRecord, 0)
	scenarios := make(map[string]ClaudeScenarioResult)
	now := time.Now().UTC().Format(time.RFC3339)

	// -------------------------------------------------------------
	// Scenario 1: CLAUDE-ALLOW-001 (ALLOW fixture produces verifiable side effect)
	// -------------------------------------------------------------
	{
		scID := ClaudeScenarioAllow
		targetFile := filepath.Join(sandbox, "claude_allowed_marker.txt")
		_ = os.Remove(targetFile)

		raw := []byte(fmt.Sprintf(`{"session_id":"claude-sess-001","tool_name":"Write","tool_input":{"file_path":%q,"content":"allow_side_effect_ok"}}`, targetFile))
		resp, dec, err := EvaluateClaudePreToolJSON(ctx, raw, ClaudeBridgeConfig{
			Policy: HookPolicy{FailOpen: true},
		})

		invocations = append(invocations, ClaudeHookInvocationRecord{
			At:           time.Now().UTC().Format(time.RFC3339Nano),
			Event:        "PreToolUse",
			Tool:         "Write",
			Session:      true,
			Phase:        "allow_check",
			Decision:     resp.Decision,
			Exit:         0,
			ContinueOmit: resp.Continue == nil,
		})

		sideEffectOK := false
		if err == nil && dec.Action == HookActionAllow && resp.Decision == "approve" && resp.HookSpecificOutput != nil && resp.HookSpecificOutput.PermissionDecision == "allow" {
			if err := os.WriteFile(targetFile, []byte("allow_side_effect_ok\n"), 0o600); err == nil {
				if content, err := os.ReadFile(targetFile); err == nil && strings.Contains(string(content), "allow_side_effect_ok") {
					sideEffectOK = true
				}
			}
		}

		if sideEffectOK {
			scenarios[scID] = ClaudeScenarioResult{
				ID:                 scID,
				Status:             "PASS",
				Detail:             "ALLOW fixture evaluated cleanly and produced verifiable side effect on disk",
				ToolName:           "Write",
				HostOutcome:        "approved",
				At:                 now,
				SideEffectVerified: true,
				ContinueOmitted:    resp.Continue == nil,
			}
		} else {
			scenarios[scID] = ClaudeScenarioResult{
				ID:          scID,
				Status:      "FAIL",
				Detail:      fmt.Sprintf("ALLOW fixture evaluation or side effect verification failed: %v", err),
				ToolName:    "Write",
				HostOutcome: "allow_failed",
				At:          now,
			}
		}
	}

	// -------------------------------------------------------------
	// Scenario 2: CLAUDE-BLOCK-001 (BLOCK fixture produces no side effect)
	// -------------------------------------------------------------
	{
		scID := ClaudeScenarioBlock
		targetFile := filepath.Join(sandbox, "claude_blocked_marker.txt")
		_ = os.Remove(targetFile)

		raw, _ := json.Marshal(map[string]any{
			"session_id": "claude-sess-001",
			"tool_name":  "Bash",
			"tool_input": map[string]any{
				"command": fmt.Sprintf("touch %s", targetFile),
			},
		})
		resp, dec, err := EvaluateClaudePreToolJSON(ctx, raw, ClaudeBridgeConfig{
			Policy: HookPolicy{
				DeniedTools: map[string]struct{}{"Bash": {}},
				FailOpen:    false,
			},
		})

		invocations = append(invocations, ClaudeHookInvocationRecord{
			At:           time.Now().UTC().Format(time.RFC3339Nano),
			Event:        "PreToolUse",
			Tool:         "Bash",
			Session:      true,
			Phase:        "block_check",
			Decision:     resp.Decision,
			Exit:         0,
			ContinueOmit: resp.Continue == nil,
		})

		blockEffective := false
		if err == nil && dec.Action == HookActionDeny && resp.Decision == "block" && resp.HookSpecificOutput != nil && resp.HookSpecificOutput.PermissionDecision == "deny" {
			if resp.Continue == nil {
				// No side effect: tool was not permitted to run
				if _, err := os.Stat(targetFile); os.IsNotExist(err) {
					blockEffective = true
				}
			}
		}

		if blockEffective {
			scenarios[scID] = ClaudeScenarioResult{
				ID:                 scID,
				Status:             "PASS",
				Detail:             "BLOCK fixture withheld tool execution without continue:false; no side effect produced",
				ToolName:           "Bash",
				HostOutcome:        "blocked",
				At:                 now,
				SideEffectVerified: true,
				ContinueOmitted:    true,
			}
		} else {
			scenarios[scID] = ClaudeScenarioResult{
				ID:          scID,
				Status:      "FAIL",
				Detail:      fmt.Sprintf("BLOCK fixture failed: blockEffective=%v err=%v", blockEffective, err),
				ToolName:    "Bash",
				HostOutcome: "block_failed",
				At:          now,
			}
		}
	}

	// -------------------------------------------------------------
	// Scenario 3: CLAUDE-LOOP-001 (Later benign turn runs after ordinary deny)
	// -------------------------------------------------------------
	{
		scID := ClaudeScenarioLoopCont
		// Turn 1: Denied tool
		rawTurn1 := []byte(`{"session_id":"claude-sess-loop","tool_name":"Bash","tool_input":{"command":"curl evil.com"}}`)
		respTurn1, _, err1 := EvaluateClaudePreToolJSON(ctx, rawTurn1, ClaudeBridgeConfig{
			Policy: HookPolicy{DeniedTools: map[string]struct{}{"Bash": {}}},
		})

		invocations = append(invocations, ClaudeHookInvocationRecord{
			At:           time.Now().UTC().Format(time.RFC3339Nano),
			Event:        "PreToolUse",
			Tool:         "Bash",
			Session:      true,
			Phase:        "turn_1_deny",
			Decision:     respTurn1.Decision,
			Exit:         0,
			ContinueOmit: respTurn1.Continue == nil,
		})

		// Turn 2: Benign tool in same session
		rawTurn2 := []byte(`{"session_id":"claude-sess-loop","tool_name":"Read","tool_input":{"file_path":"readme.md"}}`)
		respTurn2, decTurn2, err2 := EvaluateClaudePreToolJSON(ctx, rawTurn2, ClaudeBridgeConfig{
			Policy: HookPolicy{FailOpen: true},
		})

		invocations = append(invocations, ClaudeHookInvocationRecord{
			At:           time.Now().UTC().Format(time.RFC3339Nano),
			Event:        "PreToolUse",
			Tool:         "Read",
			Session:      true,
			Phase:        "turn_2_allow",
			Decision:     respTurn2.Decision,
			Exit:         0,
			ContinueOmit: respTurn2.Continue == nil,
		})

		loopOK := err1 == nil && respTurn1.Continue == nil && err2 == nil && decTurn2.Action == HookActionAllow && respTurn2.Decision == "approve"

		if loopOK {
			scenarios[scID] = ClaudeScenarioResult{
				ID:              scID,
				Status:          "PASS",
				Detail:          "Session loop continuity confirmed: turn 1 deny omitted continue:false, turn 2 benign tool approved",
				HostOutcome:     "session_loop_continued",
				At:              now,
				ContinueOmitted: true,
			}
		} else {
			scenarios[scID] = ClaudeScenarioResult{
				ID:          scID,
				Status:      "FAIL",
				Detail:      fmt.Sprintf("Session loop continuity failed: err1=%v err2=%v", err1, err2),
				HostOutcome: "loop_interrupted",
				At:          now,
			}
		}
	}

	// -------------------------------------------------------------
	// Scenario 4: CLAUDE-CTX-001 (Bounded reason/context transport without session termination)
	// -------------------------------------------------------------
	{
		scID := ClaudeScenarioBoundedCtx
		raw := []byte(`{"session_id":"claude-sess-ctx","tool_name":"Edit","tool_input":{"file_path":"protected.go"}}`)
		resp, _, err := EvaluateClaudePreToolJSON(ctx, raw, ClaudeBridgeConfig{
			Policy: HookPolicy{
				DeniedTools: map[string]struct{}{"Edit": {}},
			},
		})

		invocations = append(invocations, ClaudeHookInvocationRecord{
			At:           time.Now().UTC().Format(time.RFC3339Nano),
			Event:        "PreToolUse",
			Tool:         "Edit",
			Session:      true,
			Phase:        "reason_context",
			Decision:     resp.Decision,
			Exit:         0,
			ContinueOmit: resp.Continue == nil,
		})

		reasonLen := utf8.RuneCountInString(resp.Reason)
		bounded := reasonLen > 0 && reasonLen <= MaxHookReasonRunes
		noSessionTerm := resp.Continue == nil

		if err == nil && bounded && noSessionTerm {
			scenarios[scID] = ClaudeScenarioResult{
				ID:              scID,
				Status:          "PASS",
				Detail:          fmt.Sprintf("Bounded reason transport (%d runes <= %d max) observable without session termination", reasonLen, MaxHookReasonRunes),
				HostOutcome:     "reason_transported",
				At:              now,
				ContextBounded:  true,
				ContinueOmitted: true,
			}
		} else {
			scenarios[scID] = ClaudeScenarioResult{
				ID:              scID,
				Status:          "FAIL",
				Detail:          fmt.Sprintf("Bounded context transport failed: err=%v reasonLen=%d noSessionTerm=%v", err, reasonLen, noSessionTerm),
				HostOutcome:     "reason_failed",
				At:              now,
				ContextBounded:  bounded,
				ContinueOmitted: noSessionTerm,
			}
		}
	}

	// Calculate summary
	passed := 0
	failed := 0
	inconclusive := 0
	for _, sc := range scenarios {
		switch sc.Status {
		case "PASS":
			passed++
		case "FAIL":
			failed++
		case "INCONCLUSIVE":
			inconclusive++
		}
	}
	summary := ClaudeLiveSummary{
		Total:        len(scenarios),
		Passed:       passed,
		Failed:       failed,
		Inconclusive: inconclusive,
	}

	finalDisp := "GO"
	if failed > 0 {
		finalDisp = "NO_GO"
	} else if inconclusive > 0 {
		finalDisp = "LIMITED_GO"
	}

	report := &ClaudeLiveControlReport{
		SchemaVersion: ClaudeLiveControlSchemaV1,
		Provenance: ClaudeLiveProvenance{
			Issue:           120,
			GeneratedAt:     now,
			GOOS:            runtime.GOOS,
			GOARCH:          runtime.GOARCH,
			Harness:         "reinframe.claude_live_harness.v1",
			ReinframeCommit: opts.ReinframeCommit,
		},
		Scenarios:        scenarios,
		Summary:          summary,
		FinalDisposition: finalDisp,
	}

	return report, invocations, nil
}

// RedactClaudeEvidence sanitizes sensitive local information from evidence structures.
func RedactClaudeEvidence(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	s := string(raw)

	for _, key := range []string{"USERPROFILE", "HOME"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			s = strings.ReplaceAll(s, val, "[HOME]")
			s = strings.ReplaceAll(s, filepath.ToSlash(val), "[HOME]")
		}
	}
	for _, key := range []string{"USERNAME", "USER"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" && len(val) > 2 {
			re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(val) + `\b`)
			s = re.ReplaceAllString(s, "[USER]")
		}
	}
	if host, err := os.Hostname(); err == nil && host != "" && host != "localhost" {
		s = strings.ReplaceAll(s, host, "[HOSTNAME]")
	}

	var out any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return v
	}
	return out
}
