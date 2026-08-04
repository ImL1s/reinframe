# Shadow-mode Action Alignment classifier (#105)

**Status:** library shadow runtime  
**Enforced:** always `false`  
**Depends on:** #115 ProposedAction, #119 wire contract

## Behavior

1. Existing HookGate / EvaluateBeforeTool decision is **authoritative**.
2. `ShadowClassifier.EvaluateShadow` predicts `ALLOW|BLOCK`, records severity, threshold, disagreement.
3. `ResolvedDecision.Enforced` is always `false` — never changes tool allow/deny.
4. Default threshold **50** is provisional (not calibrated; see #100).

## Entry

```go
s := &classifier.ShadowClassifier{Provider: classifier.FakeClassifierProvider{}}
res, err := s.EvaluateShadow(ctx, classifier.ShadowInput{
    SessionID: sess,
    Proposed: pa, // from #115
    HookGateAction: dec.Action,
    RulesetID: "fixture:clear_allow", // tests
})
// res.Resolved.Enforced == false always
_ = res.AuditJSON()
```

## Non-claims

- Not hard-gate
- Not live Claude smoke
- Not advice injection
- Not calibrated thresholds
