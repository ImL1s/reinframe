package adapter

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

// Hook action decisions for the synchronous PreTool / PreCommand fast path.
const (
	HookActionAllow = "allow"
	HookActionDeny  = "deny"
	HookActionDefer = "defer"
)

// Common reason codes (audit-friendly, no secrets).
const (
	ReasonAllow                 = "allow"
	ReasonDeniedTool            = "denied_tool"
	ReasonDeniedPathScope       = "denied_path_scope"
	ReasonDeniedBudget          = "denied_budget_exhausted"
	ReasonDeniedHardLatch       = "denied_hard_latch"
	ReasonDeferPendingAdvisory  = "defer_pending_advisory"
	ReasonTimeoutFailOpen       = "timeout_fail_open"
	ReasonTimeoutFailClosed     = "timeout_fail_closed"
	ReasonContextCanceled       = "context_canceled"
	ReasonRedundantValidation   = "redundant_validation"
	ReasonDisproportionateScope = "disproportionate_scope"
)

// DefaultHookTimeout is the default wall-clock budget for pure-Go deterministic checks.
const DefaultHookTimeout = 50 * time.Millisecond

// HookDecision is the structured outcome of EvaluateHook.
type HookDecision struct {
	Action         string
	ReasonCode     string
	InterventionID string
	Deadline       *time.Time
}

// HookRequest is the adapter-facing PreTool / PreCommand interception surface.
// Fields are intentionally narrow: only data needed for deterministic rules.
// Prefer Proposed (#115) when available so shell Command is not stuffed into ToolName.
type HookRequest struct {
	SessionID string
	// Phase is "PreTool" or "PreCommand" (informational; not validated strictly).
	Phase string
	// ToolName is the host tool identifier (e.g. "Bash"), not a full shell command.
	ToolName string
	// FilePath is an optional workspace path associated with the call (scope checks).
	FilePath string
	// Proposed is the optional versioned action projection (#115).
	Proposed *ProposedAction
	// Challenge binding (#139).
	ChallengeID    string
	ChallengeNonce string
	Justification  *ClaudeJustificationInput
}

// HookPolicy is the deterministic policy table consulted by EvaluateHook.
// No Reviewer, LLM, or network interface appears in this struct or in EvaluateHook.
type HookPolicy struct {
	// Timeout is the per-call wall budget. Zero uses DefaultHookTimeout.
	Timeout time.Duration
	// FailOpen controls timeout / cancel behavior:
	//   true  → allow (fail-open)
	//   false → deny  (fail-closed)
	FailOpen bool

	// DeniedTools is a set of tool/command names that must be denied.
	DeniedTools map[string]struct{}
	// ToolDenyReasons maps tool/command name → reason code (e.g. redundant_validation).
	// When the tool matches, deny with that reason (takes precedence over DeniedTools
	// for the same tool when both set).
	ToolDenyReasons map[string]string
	// ScopeWhitelist, when non-empty, requires FilePath to match a prefix (or
	// path.Match pattern). Empty FilePath with a non-empty whitelist is denied.
	ScopeWhitelist []string
	// BudgetExhausted forces deny.
	BudgetExhausted bool
	// HardDenyInterventionID, when set, forces deny and populates InterventionID.
	HardDenyInterventionID string
	// PendingAdvisoryInterventionID, when set, forces defer (coordinate with #68 queue).
	PendingAdvisoryInterventionID string
	// DeferDeadline is optional deadline attached to defer decisions.
	DeferDeadline *time.Time

	// Wait, when non-nil, blocks rule evaluation until the channel is closed or
	// the timeout fires. Intended for timeout unit tests; production leaves it nil.
	Wait <-chan struct{}
}

// EvaluateHook runs the synchronous hook gate fast path.
//
// It is deterministic only: no Reviewer/LLM interface is accepted or invoked.
// On timeout or context cancellation, the decision follows policy.FailOpen.
func EvaluateHook(ctx context.Context, req HookRequest, policy HookPolicy) HookDecision {
	if ctx == nil {
		ctx = context.Background()
	}

	timeout := policy.Timeout
	if timeout <= 0 {
		timeout = DefaultHookTimeout
	}

	// Fast path: already past deadline / canceled before work starts.
	if err := ctx.Err(); err != nil {
		return timeoutDecision(policy, err)
	}

	// Bound evaluation so Wait barriers and slow pure rules cannot leak goroutines.
	evalCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	type result struct {
		dec HookDecision
	}
	done := make(chan result, 1)

	go func() {
		if policy.Wait != nil {
			select {
			case <-policy.Wait:
			case <-evalCtx.Done():
				done <- result{dec: timeoutDecision(policy, evalCtx.Err())}
				return
			}
		}
		// Re-check after wait barrier.
		if err := evalCtx.Err(); err != nil {
			done <- result{dec: timeoutDecision(policy, err)}
			return
		}
		done <- result{dec: evaluateHookRules(req, policy)}
	}()

	select {
	case r := <-done:
		return r.dec
	case <-evalCtx.Done():
		// Prefer completed result if it raced in with the deadline.
		select {
		case r := <-done:
			return r.dec
		default:
			return timeoutDecision(policy, evalCtx.Err())
		}
	}
}

func timeoutDecision(policy HookPolicy, err error) HookDecision {
	if policy.FailOpen {
		return HookDecision{
			Action:     HookActionAllow,
			ReasonCode: ReasonTimeoutFailOpen,
		}
	}
	reason := ReasonTimeoutFailClosed
	if err != nil && err != context.DeadlineExceeded {
		// Canceled contexts still follow fail-open/closed policy but use a distinct code.
		if err == context.Canceled {
			// Keep fail-closed/open action; use dedicated reason when not deadline.
			// Still map action via FailOpen below; reason remains timeout-class for audit.
			_ = ReasonContextCanceled
		}
	}
	return HookDecision{
		Action:     HookActionDeny,
		ReasonCode: reason,
	}
}

func evaluateHookRules(req HookRequest, policy HookPolicy) HookDecision {
	// Priority: hard deny latch → budget → deny-list tools → path scope → pending advisory defer → allow.
	if id := policy.HardDenyInterventionID; id != "" {
		return HookDecision{
			Action:         HookActionDeny,
			ReasonCode:     ReasonDeniedHardLatch,
			InterventionID: id,
		}
	}
	if policy.BudgetExhausted {
		return HookDecision{
			Action:     HookActionDeny,
			ReasonCode: ReasonDeniedBudget,
		}
	}
	if req.ToolName != "" {
		if reason, ok := policy.ToolDenyReasons[req.ToolName]; ok && reason != "" {
			return HookDecision{
				Action:     HookActionDeny,
				ReasonCode: reason,
			}
		}
	}
	if len(policy.DeniedTools) > 0 && req.ToolName != "" {
		if _, denied := policy.DeniedTools[req.ToolName]; denied {
			return HookDecision{
				Action:     HookActionDeny,
				ReasonCode: ReasonDeniedTool,
			}
		}
	}
	if len(policy.ScopeWhitelist) > 0 {
		if !pathInScope(req.FilePath, policy.ScopeWhitelist) {
			return HookDecision{
				Action:     HookActionDeny,
				ReasonCode: ReasonDeniedPathScope,
			}
		}
	}
	if id := policy.PendingAdvisoryInterventionID; id != "" {
		return HookDecision{
			Action:         HookActionDefer,
			ReasonCode:     ReasonDeferPendingAdvisory,
			InterventionID: id,
			Deadline:       policy.DeferDeadline,
		}
	}
	return HookDecision{
		Action:     HookActionAllow,
		ReasonCode: ReasonAllow,
	}
}

func pathInScope(filePath string, whitelist []string) bool {
	if filePath == "" {
		return false
	}
	// Normalize for prefix comparison (slash-agnostic via filepath).
	clean := filepath.Clean(filePath)
	for _, entry := range whitelist {
		if entry == "" {
			continue
		}
		e := filepath.Clean(entry)
		if clean == e {
			return true
		}
		// Prefix match: "/workspace" allows "/workspace/foo".
		sep := string(filepath.Separator)
		prefix := strings.TrimRight(e, sep) + sep
		if strings.HasPrefix(clean, prefix) {
			return true
		}
		// filepath.Match patterns (e.g. "/tmp/*").
		if ok, _ := filepath.Match(entry, clean); ok {
			return true
		}
	}
	return false
}
