# Milestone Scope, Non-Goals & Compatibility Matrix

## 1. Overview
This document is the **normative milestone boundary** for Reinframe.  
It **supersedes** earlier drafts that placed full detectors/reviewers/adjudicator inside “M1 vertical slice” while the execution DAG placed them in Phase 3 (M2). Those two sources conflicted; **this file + `docs/architecture/dag_and_execution_plan.md` must stay aligned**.

**Last aligned:** 2026-08-02 (deep-research validation).

---

## 2. Milestone definitions (locked)

### M0 — Architecture & research (mostly complete)
- ADRs, threat model, capability matrix research, MVP boundaries.
- **Note:** Matrix versions require ongoing refresh (#49); not a one-time “done forever”.

### M1 — Protocol & Store Foundation (+ observe-only adapter path)
**Goal:** Ship a trustworthy **library foundation** and **observe-only** integration path.  
**Not** a claim of production Anti-Tunnel E2E.

#### In-scope for M1
1. **Protocol**
   - Canonical event schemas + `ValidateEvent` (#6 — largely done).
   - Capability manifest & handshake (#7 — largely done; #65 delivery caps follow-on).
2. **State**
   - SQLite WAL append-only event store (#9 — largely done).
3. **Agent adapter (observe)**
   - `LogObserverAdapter` / EventSource for L0 (#12).
   - Mock agent for deterministic tests.
4. **Honesty & CI**
   - Docs match shipped packages; cross-platform CI.
5. **Optional wiring demos only**
   - Fake/mock detector or hand-scripted signals **may** appear in tests to exercise Store + protocol, but **must not** be marketed as calibrated tunnel detection.

#### Explicitly **out of scope for M1** (moved to M2+)
- Production `RepeatedErrorDetector` / churn / scope detectors as product gates.
- Live Reviewer roles (TunnelClassifier, Assumption Auditor, …) as product gates.
- Adjudicator policy hard-gates with fixed confidence thresholds.
- Bidirectional advisory delivery lifecycle (pending → deliver → ACK) — see #65–#71 (**M2 control plane**).
- Real Git rollback orchestration, CLI product surface completeness, benchmarks.

### M2 — Anti-Tunnel intelligence & bidirectional control plane
**Goal:** Detect tunnel-like failure modes, review with evidence, intervene with **delivery + ACK**, orchestrate the loop.

#### In-scope for M2
1. **Detectors** (#23–#26, …): framework + repeated error + churn (+ scope as P1).
2. **Reviewers** (#26–#31): evidence pack, tunnel classifier, assumption auditor, …
3. **Policy** (#8/#32/#69/#33): session SM, adjudicator, fast vs slow path, cooldown.
4. **Control plane** (#65–#70): delivery caps, EventSource/Actuator, hook gate, advisory ACK, orchestrator wiring.
5. **Vertical slice test** (#71/#42): real hook→agent path, not Store-only fixtures.
6. **Thresholds:** numeric weights/thresholds from threat model are **provisional knobs only** until evaluation issues produce evidence (#40/#41).

### M3 — Evaluation, hardening, release packaging
- Synthetic anti-tunnel + false-positive benchmarks (#40/#41).
- Crash recovery, observability, redaction, release (#38–#47 class).
- **Only after benchmarks** may thresholds become product hard-gates.

---

## 3. Dual Level axes (must not mix)
See [`docs/research/level_axes_mapping.md`](../research/level_axes_mapping.md).

| Axis | Meaning |
|---|---|
| **Integration Level (A)** | Handshake capability surface |
| **Intervention Level (B)** | Escalation after detection |

M1 ships **A0** path solidly; A1+ delivery and B1+ intervention are **M2**.

---

## 4. Explicit Non-Goals (global for M1; still non-goals until later milestones)
- Proprietary IDE extension binaries as first-class ship (standalone external supervisor only — ADR 001).
- Subagent recursive nesting beyond `max_depth = 1`.
- Custom GUI / Web dashboard.
- Remote DB/cloud state rollback (workspace Git only when rollback ships).
- Treating threat-model weights as scientifically calibrated without #40/#41 evidence.

---

## 5. Target OS Compatibility Matrix

| OS Platform | Architecture | Supported Mode | Test Runner | Status |
|---|---|---|---|---|
| **macOS 14+** | arm64 & x64 | Full Supervisor & Stdio | Native | Primary |
| **Linux (Ubuntu 22.04+)** | x64 & arm64 | Full Supervisor & Stdio | Docker / Native | Primary |
| **Windows 11 / Server 2022** | x64 | Full Supervisor (Job Objects) | PowerShell / Cmd | Primary |

---

## 6. Acceptance language (what “done” may claim)
| Claim | Allowed when |
|---|---|
| “Protocol & store foundation complete” | M1 packages + CI green |
| “Observe-only supervision path” | LogObserver + Store + audit events |
| “Anti-Tunnel E2E” / “production-ready harness” | **Forbidden** until M2 control plane + #71 + honest docs |
| “Calibrated detector thresholds” | **Forbidden** until M3 evaluation evidence |
