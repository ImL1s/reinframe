# Reinframe Backlog Execution DAG & Implementation Sequence

## Overview
This document specifies the complete Directed Acyclic Graph (DAG) for all 47 atomized issues across Milestones M0 to M3.
Every issue has explicit `Blocked by` and `Blocks` constraints to guarantee parallelizability and prevent dependency deadlocks.

---

## Complete Backlog Execution Order

### Phase 1: Architecture & Foundation (M0 & Core Data Models)
1. **Issue #1** `[P0][Research]` Harness Capability Matrix
2. **Issue #2** `[P0][Research]` Anti-Tunnel Threat Model
3. **Issue #3** `[P0][Architecture]` ADR 001 Supervisor Choice *(Blocked by #1)*
4. **Issue #4** `[P0][Architecture]` ADR 002 Language/DB/IPC Stack *(Blocked by #3)*
5. **Issue #5** `[P0][Architecture]` MVP Scope & Non-goals *(Blocked by #3, #4)*
6. **Issue #6** `[P0][Core]` Canonical Event Schema *(Blocked by #4, #5)*
7. **Issue #7** `[P0][Protocol]` Capability Manifest & Handshake *(Blocked by #1, #6)*
8. **Issue #8** `[P0][Policy]` Core Session State Machine *(Blocked by #4, #6, #7)*
9. **Issue #9** `[P0][State]` Event Store & SQLite WAL *(Blocked by #4, #6)*
10. **Issue #10** `[P0][State]` Checkpoint Manager *(Blocked by #9)*

### Phase 2: Agent Adapters & Reviewer Providers (M1 Engine)
11. **Issue #11** `[P0][Agent-Adapter]` Agent Adapter Core SDK *(Blocked by #6, #7)*
12. **Issue #12** `[P0][Agent-Adapter]` Log Observer Adapter *(Blocked by #11)*
13. **Issue #13** `[P1][Agent-Adapter]` CLI Process Adapter *(Blocked by #11)*
14. **Issue #14** `[P1][Agent-Adapter]` Native Subagent Bridge Adapter *(Blocked by #11)*
15. **Issue #15** `[P1][Agent-Adapter]` Event Stream & Hook Adapter *(Blocked by #11)*
16. **Issue #16** `[P1][Agent-Adapter]` MCP Bridge Adapter *(Blocked by #11, #15)*
17. **Issue #17** `[P0][Reviewer-Provider]` Reviewer Provider SDK *(Blocked by #6)*
18. **Issue #18** `[P0][Reviewer-Provider]` OpenAI-Compatible Provider *(Blocked by #17)*
19. **Issue #19** `[P0][Reviewer-Provider]` Local Model Provider & Local-Only Mode *(Blocked by #17, #18)*
20. **Issue #20** `[P1][Reviewer-Provider]` Cloud Provider Adapters *(Blocked by #17)*
21. **Issue #21** `[P1][Reviewer-Provider]` CLI Model Provider *(Blocked by #17)*
22. **Issue #22** `[P1][Reviewer-Provider]` Provider Retry & Fallback Policy *(Blocked by #17, #18, #20)*

### Phase 3: Anti-Tunnel Intelligence & Detection (M2 Engine)
23. **Issue #23** `[P0][Detectors]` Signal Detector Framework *(Blocked by #6, #8)*
24. **Issue #24** `[P0][Detectors]` Repeated Error Detector *(Blocked by #23)*
25. **Issue #25** `[P0][Detectors]` Patch Churn Detector *(Blocked by #23)*
26. **Issue #26** `[P1][Detectors]` Scope Drift Detector *(Blocked by #23)*
27. **Issue #27** `[P0][Reviewers]` Evidence Pack Builder *(Blocked by #6, #14)*
28. **Issue #28** `[P0][Reviewers]` Tunnel Classifier Role *(Blocked by #17, #27)*
29. **Issue #29** `[P0][Reviewers]` Assumption Auditor Role *(Blocked by #27)*
30. **Issue #30** `[P1][Reviewers]` Contrarian & Alternative Generator *(Blocked by #29)*
31. **Issue #31** `[P1][Reviewers]` Evidence Verifier Role *(Blocked by #27)*
32. **Issue #32** `[P1][Reviewers]` Discriminating Experiment Planner *(Blocked by #30)*
33. **Issue #33** `[P0][Policy]` Adjudicator Policy Engine *(Blocked by #7, #8, #22, #23, #28)*
34. **Issue #34** `[P1][Policy]` False Positive Suppression Rules *(Blocked by #33)*
35. **Issue #35** `[P0][State]` Checkpoint Rollback Workflow *(Blocked by #10, #33)*

### Phase 4: Operations, Hardening & Release (M3 Package)
36. **Issue #36** `[P1][Observability]` Budget Manager *(Blocked by #22)*
37. **Issue #37** `[P1][Security]` Sensitive Data Redaction Engine *(Blocked by #19)*
38. **Issue #38** `[P1][Observability]` Structured Logging & Audit Trail *(Blocked by #9, #36, #37)*
39. **Issue #39** `[P1][Core]` Crash Recovery & Cleanup Manager *(Blocked by #13)*
40. **Issue #40** `[P0][CI]` Cross-Platform CI Matrix *(Blocked by #39)*
41. **Issue #41** `[P1][Evaluation]` Synthetic Anti-Tunnel Benchmark *(Blocked by #2, #24, #25)*
42. **Issue #42** `[P1][Evaluation]` False-Positive Benchmark Suite *(Blocked by #34)*
43. **Issue #43** `[P0][Test]` End-to-End Vertical Slice Test *(Blocked by #12, #16, #18, #27, #28, #33, #35, #38, #39, #41, #42)*
44. **Issue #44** `[P1][Docs]` Comprehensive Documentation Suite *(Blocked by #43)*
45. **Issue #45** `[P1][Docs]` Local Reference Configuration *(Blocked by #13, #19, #44)*
46. **Issue #46** `[P1][Docs]` Cloud Reference Configuration *(Blocked by #13, #20, #44)*
47. **Issue #47** `[P0][Release]` Semantic Release Packaging *(Blocked by #40, #43, #45, #46)*

---

## Suggested Next Issue for Implementation
When switching to `MODE: IMPLEMENT_NEXT`, start with:
- **Issue #1** (`[P0][Research] Build verified harness capability & integration surface matrix`) or
- **Issue #6** (`[P0][Core] Define canonical agent event schema and JSON Schema validation`) once M0 research reviews complete.
