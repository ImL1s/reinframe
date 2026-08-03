# Reinframe Backlog Execution DAG & Implementation Sequence

> **Partially historical.** Prefer [`docs/roadmap/CURRENT.md`](../roadmap/CURRENT.md) for the **current executable backlog**.  
> This DAG remains useful for milestone↔phase mapping and completed-cluster history.  
> Do not implement from unchecked historical lists when CURRENT.md or Epic #80 disagrees.

## Overview
Directed Acyclic Graph of atomized issues across milestones **M0–M3**.  
**Normative milestone boundaries:** `docs/specs/mvp_scope_and_non_goals.md` (aligned 2026-08-02; M2.2 experimental bridges noted in CURRENT.md).  
**Dual level axes:** `docs/research/level_axes_mapping.md` (Integration ≠ Intervention).

Issue numbers in GitHub may drift slightly from historical lists; prefer **title + milestone** when conflicting.

---

## Milestone ↔ Phase map (locked)

| Milestone | Phase below | May claim when done |
|---|---|---|
| **M0** | Phase 1 research/ADR | Architecture decisions exist |
| **M1** | Phase 1 data models + Phase 2 **observe adapters** (not full intelligence) | Protocol + Store + L0 observe path |
| **M2** | Phase 3 intelligence + control plane (#65–#71) | Detection + delivery + orchestrated vertical slice |
| **M3** | Phase 4 evaluation/hardening/release | Benchmarks; thresholds may become hard-gates |

**Do not** place production detectors/reviewers/adjudicator acceptance in M1.

---

## Complete Backlog Execution Order

### Phase 1: Architecture & Foundation (M0 + M1 data plane)
1. **Issue #1** `[EPIC]` Reinframe
2. **Issue #2** `[P0][Research]` Anti-Tunnel Threat Model *(refresh contracts: normalization, provisional thresholds)*
3. **Issue #3** `[P0][Architecture]` ADR 001 Supervisor Choice
4. **Issue #4** `[P0][Architecture]` ADR 002 Language/DB/IPC Stack
5. **Issue #5** `[P0][Architecture]` MVP Scope & Non-goals *(normative M1/M2/M3 boundaries)*
6. **Issue #6** `[P0][Protocol]` Canonical Event Schema
7. **Issue #7** `[P0][Protocol]` Capability Manifest & Handshake
8. **Issue #49** `[P0][Research]` Harness Capability Matrix *(continuous revalidation)*
9. **Issue #9** `[P0][State]` Event Store & SQLite WAL
10. **Issue #10** `[P0][State]` Checkpoint Manager
11. **Issue #8** `[P0][Policy]` Core Session State Machine *(skeleton OK in M1; full policy M2)*

### Phase 2: M1 observe adapters & provider stubs
12. **Issue #11** `[P0][Agent-Adapter]` Adapter SDK (bidirectional contracts — interfaces land with #66)
13. **Issue #12** `[P0][Agent-Adapter]` Log Observer Adapter (L0)
14. **Issue #66** `[P0][Agent-Adapter]` EventSource + InterventionActuator interfaces
15. **Issue #17–#19** Reviewer provider SDK + OpenAI-compatible + local *(stubs usable before M2 roles)*
16. **Issue #13–#16** CLI / subagent / inbound hooks / MCP pull *(P1 parallel; MCP/hooks feed M2)*

### Phase 3: M2 Intelligence + bidirectional control plane
17. **Issue #65** Delivery capability flags + L1 Advisory requires CapAdviceDelivery
18. **Issue #57** Intervention schema: delivery/ACK/expiry + L3 action gaps
19. **Issue #72** CapPause semantics (native pause vs SIGSTOP)
20. **Issue #73** Dual-axis level mapping accepted in architecture set
21. **Issue #23–#26** Detectors (framework, repeated error, churn, scope)
22. **Issue #26–#31** Reviewers (evidence pack, classifier, auditors, …)
23. **Issue #69 / #32 / #33** Fast/slow policy + adjudicator + cooldown
24. **Issue #67 / #68** Sync hook gate + safe-turn advisory delivery/ACK
25. **Issue #70 / #56** Orchestrator bidirectional lifecycle wiring
26. **Issue #71 / #42** Real hook→agent vertical slice (not Store fixtures only)
27. **Issue #34 / #35** FP suppression + checkpoint rollback workflow

### Phase 4: M3 Evaluation, hardening, release
28. **Issue #40 / #41** Synthetic + false-positive benchmarks *(unlock calibrated thresholds)*
29. **Issue #74** Threshold calibration from benchmarks → product hard-gates
30. Observability / security / crash recovery / CI packaging / docs / release (#35–#47 class)

---

## Suggested Next Implementation Clusters
1. Finish M1 observe path: #12 LogObserver + #66 interfaces.  
2. Start M2 control plane in order: **#65 → #57 → #66 → #67/#69/#68 → #70 → #71**.  
3. Detectors #23/#24 in parallel once events flow.  
4. Do **not** hard-code threat-model weights as final until #40/#41/#74.
