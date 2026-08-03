package policy

import (
	"context"
	"strings"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// BeforeToolInput is the M2.1 before_tool adjudication surface (#86).
// Combines fast-path HookPolicy with optional contract/ledger/churn signal.
type BeforeToolInput struct {
	Request    adapter.HookRequest
	BasePolicy adapter.HookPolicy
	Contract   *protocol.TaskContract
	Ledger     *protocol.EvidenceLedger
	// ChurnSignal when non-nil and verification_churn forces deny of this tool.
	ChurnSignal *protocol.TunnelSignal
}

// EvaluateBeforeTool decides allow|deny|defer for PreTool / before_tool.
//
// Order: apply churn / disproportionate denies into a copy of BasePolicy, then
// EvaluateFast (HookGate). Never calls Reviewer.
func (e *Engine) EvaluateBeforeTool(ctx context.Context, in BeforeToolInput) adapter.HookDecision {
	pol := in.BasePolicy
	if pol.ToolDenyReasons == nil {
		pol.ToolDenyReasons = map[string]string{}
	} else {
		// shallow copy map so we do not mutate caller's policy
		cp := make(map[string]string, len(pol.ToolDenyReasons)+2)
		for k, v := range pol.ToolDenyReasons {
			cp[k] = v
		}
		pol.ToolDenyReasons = cp
	}

	tool := in.Request.ToolName
	if tool != "" {
		if in.ChurnSignal != nil && in.ChurnSignal.FailureMode == detector.FailureModeVerificationChurn {
			pol.ToolDenyReasons[tool] = adapter.ReasonRedundantValidation
		} else if shouldDenyDisproportionate(in.Contract, in.Ledger, tool) {
			pol.ToolDenyReasons[tool] = adapter.ReasonDisproportionateScope
		}
	}

	return e.EvaluateFast(ctx, FastInput{Request: in.Request, Policy: pol})
}

// shouldDenyDisproportionate is true for trivial/simple low-risk contracts when
// success criteria are met and the tool is a full-suite validation.
func shouldDenyDisproportionate(c *protocol.TaskContract, led *protocol.EvidenceLedger, toolName string) bool {
	if c == nil || !isFullSuiteTool(toolName) {
		return false
	}
	if c.Complexity != protocol.ComplexityTrivial && c.Complexity != protocol.ComplexitySimple {
		return false
	}
	if c.Risk != protocol.RiskLow {
		return false
	}
	if !criteriaAllMet(c, led) {
		return false
	}
	return true
}

func criteriaAllMet(c *protocol.TaskContract, led *protocol.EvidenceLedger) bool {
	if led == nil || len(c.SuccessCriteria) == 0 {
		return false
	}
	for _, cr := range c.SuccessCriteria {
		st, ok := led.CriteriaStatus[cr.ID]
		if !ok || st.Status != "met" {
			return false
		}
	}
	return true
}

// isFullSuiteTool detects full-repo test commands (provisional heuristic).
func isFullSuiteTool(toolName string) bool {
	n := strings.ToLower(strings.TrimSpace(toolName))
	if n == "" {
		return false
	}
	if strings.Contains(n, "./...") {
		return true
	}
	if strings.Contains(n, "go test") && strings.Contains(n, "-race") && strings.Contains(n, "./") {
		return true
	}
	return false
}
