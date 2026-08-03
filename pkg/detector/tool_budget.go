package detector

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// FailureModeToolBudgetChurn is emitted when tool usage exceeds budget without progress.
const FailureModeToolBudgetChurn = "tool_budget_churn"

// DetectorNameToolBudgetChurn is the DetectorName on TunnelSignal.
const DetectorNameToolBudgetChurn = "ToolBudgetChurnDetector"

// DefaultMaxToolCalls is the provisional default when contract budget is unset.
// Not a calibrated hard-gate (#100); suitable for unit demos only.
const DefaultMaxToolCalls = 30

// ToolBudgetConfig configures ToolBudgetChurnDetector.
type ToolBudgetConfig struct {
	// MaxToolCalls fires when session tool count reaches this without progress.
	// Zero uses DefaultMaxToolCalls unless overridden per Observe via budget.
	MaxToolCalls int
	// Now overrides the clock (tests).
	Now func() time.Time
}

// ToolBudgetChurnDetector counts tool_call events per session and fires when
// the count meets/exceeds the budget while no progress has been recorded since
// the last progress marker (or ever). Progress is an explicit evidence gain
// (file change, met criterion, new evidence ID) — not a successful shell exit.
//
// This is the library start for #98. It does not claim live host intervention.
type ToolBudgetChurnDetector struct {
	maxDefault int
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]*toolBudgetSession
	seq      uint64
}

type toolBudgetSession struct {
	toolCalls    int
	progressHits int
	// toolsSinceProgress increments on each tool; resets on MarkProgress.
	toolsSinceProgress int
	// fired once at threshold so policy can react; re-fire every additional tool
	// after threshold (same pattern as repeated_failure) for re-evaluation.
}

// NewToolBudgetChurnDetector builds a detector with defaults applied.
func NewToolBudgetChurnDetector(cfg ToolBudgetConfig) *ToolBudgetChurnDetector {
	max := cfg.MaxToolCalls
	if max <= 0 {
		max = DefaultMaxToolCalls
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ToolBudgetChurnDetector{
		maxDefault: max,
		now:        now,
		sessions:   make(map[string]*toolBudgetSession),
	}
}

// MaxToolCalls returns the configured default budget.
func (d *ToolBudgetChurnDetector) MaxToolCalls() int {
	return d.maxDefault
}

// Observe processes one AgentEvent. tool_call increments usage; file_change
// counts as progress. Returns a TunnelSignal when budget is exceeded without
// progress in the window (toolsSinceProgress >= max).
func (d *ToolBudgetChurnDetector) Observe(event protocol.AgentEvent) (*protocol.TunnelSignal, bool) {
	return d.ObserveWithBudget(event, 0)
}

// ObserveWithBudget is like Observe but uses maxToolCalls when > 0 instead of
// the detector default (from TaskContract.ToolBudget.MaxToolCalls).
func (d *ToolBudgetChurnDetector) ObserveWithBudget(event protocol.AgentEvent, maxToolCalls int) (*protocol.TunnelSignal, bool) {
	if event.SessionID == "" {
		return nil, false
	}
	switch event.EventType {
	case "tool_call", "ToolCall":
		name := extractToolName(event)
		return d.recordTool(event.SessionID, name, event.EventID, maxToolCalls)
	case "file_change", "FileChange", "evidence", "criterion_met":
		d.MarkProgress(event.SessionID)
		return nil, false
	default:
		return nil, false
	}
}

// ObserveTool records a single tool invocation (adapters / tests).
func (d *ToolBudgetChurnDetector) ObserveTool(sessionID, toolName string) (*protocol.TunnelSignal, bool) {
	return d.recordTool(sessionID, toolName, "", 0)
}

// ObserveToolWithBudget records a tool call with an explicit budget override.
func (d *ToolBudgetChurnDetector) ObserveToolWithBudget(sessionID, toolName string, maxToolCalls int) (*protocol.TunnelSignal, bool) {
	return d.recordTool(sessionID, toolName, "", maxToolCalls)
}

// MarkProgress records evidence gain for the session (resets tools-since-progress).
func (d *ToolBudgetChurnDetector) MarkProgress(sessionID string) {
	if sessionID == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		ss = &toolBudgetSession{}
		d.sessions[sessionID] = ss
	}
	ss.progressHits++
	ss.toolsSinceProgress = 0
}

// ToolCount returns total tool calls observed for the session.
func (d *ToolBudgetChurnDetector) ToolCount(sessionID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		return 0
	}
	return ss.toolCalls
}

// ToolsSinceProgress returns tool calls since the last progress marker.
func (d *ToolBudgetChurnDetector) ToolsSinceProgress(sessionID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		return 0
	}
	return ss.toolsSinceProgress
}

// ResetSession clears session counters.
func (d *ToolBudgetChurnDetector) ResetSession(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.sessions, sessionID)
}

func (d *ToolBudgetChurnDetector) recordTool(sessionID, toolName, sourceID string, maxOverride int) (*protocol.TunnelSignal, bool) {
	if sessionID == "" {
		return nil, false
	}
	max := d.maxDefault
	if maxOverride > 0 {
		max = maxOverride
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	ss := d.sessions[sessionID]
	if ss == nil {
		ss = &toolBudgetSession{}
		d.sessions[sessionID] = ss
	}
	ss.toolCalls++
	ss.toolsSinceProgress++
	count := ss.toolsSinceProgress
	total := ss.toolCalls

	if count < max {
		return nil, false
	}

	d.seq++
	sigID := fmt.Sprintf("sig-tb-%d", d.seq)
	if sourceID != "" {
		sigID = fmt.Sprintf("sig-tb-%s-%d", sourceID, d.seq)
	}
	sig := &protocol.TunnelSignal{
		SignalID:     sigID,
		SessionID:    sessionID,
		DetectorName: DetectorNameToolBudgetChurn,
		FailureMode:  FailureModeToolBudgetChurn,
		Weight:       0.32,
		Score:        1.0,
		Details: map[string]string{
			"tool_name":            toolName,
			"tools_since_progress": fmt.Sprintf("%d", count),
			"total_tool_calls":     fmt.Sprintf("%d", total),
			"max_tool_calls":       fmt.Sprintf("%d", max),
			"progress_hits":        fmt.Sprintf("%d", ss.progressHits),
			"source_id":            sourceID,
		},
		TriggeredAt: d.now(),
	}
	return sig, true
}

func extractToolName(event protocol.AgentEvent) string {
	if len(event.Payload) == 0 {
		return ""
	}
	var tc protocol.ToolCallEvent
	if err := json.Unmarshal(event.Payload, &tc); err == nil && tc.ToolName != "" {
		return tc.ToolName
	}
	var m map[string]any
	if err := json.Unmarshal(event.Payload, &m); err == nil {
		if n, ok := m["tool_name"].(string); ok {
			return n
		}
		if n, ok := m["name"].(string); ok {
			return n
		}
	}
	return ""
}
