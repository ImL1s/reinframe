# ADR 005: ClassifierProvider separate from ReviewerProvider

**Status:** Accepted (2026-08-04)  
**Issue:** #119  

## Context

#104 described Stage 1 raw severity scoring. Optional LLM Reviewer (PR #103) already powers uncertain-path ZOOM_OUT advice. #105 needs a closed `RawAssessment` contract.

## Decision

1. Add **`ClassifierProvider`** in `pkg/classifier` with:

```go
type ClassifierProvider interface {
    Assess(ctx context.Context, in ClassifierInput) (RawAssessment, error)
}
```

2. Do **not** make `ReviewerProvider` the primary #105 dependency.  
3. Optional adapter may map Reviewer → RawAssessment in a later issue; not required for #105.  
4. Default tests use `FakeClassifierProvider` (deterministic).

## Consequences

- Clear failure semantics and closed parse for severity/reason_code  
- Avoids conflating advice interventions with ALLOW|BLOCK scoring  
- #105 implements shadow wiring against this interface only  
