# Level Axes Mapping: Integration vs Intervention

**Status:** Normative planning document (2026-08-02).  
**Problem fixed:** Both research docs used “Level 0–3” for **different axes** without a crosswalk, which caused false hard-gates (e.g. treating Claude Code as protocol L2 because the matrix listed it under “Guarded”).

---

## Two different axes

| Axis | Name | Question it answers | Primary sources |
|---|---|---|---|
| **A. Integration Level** | Capability / handshake level | *What control surface does this Target Agent expose to Reinframe?* | `harness_capability_matrix.md`, `pkg/protocol/capability.go`, #7, #65 |
| **B. Intervention Level** | Escalation ladder after detection | *How aggressively may the supervisor intervene on this session?* | `anti_tunnel_threat_model.md` §4, session SM, #32, #69, #70 |

They **share numeric labels 0–3 by historical coincidence**. They are **not** the same negotiated integer unless a product rule explicitly binds them.

---

## Axis A — Integration Level (handshake)

Negotiated at session start from `CapabilityManifest` flags.

| Level | Intent | Code mask today | Delivery note (#65) |
|---|---|---|---|
| **0 Observe** | Passive observation | `CapEventStream` | No advice required |
| **1 Advisory** | Can advise the agent | + `CapToolInspection` | **Must add `CapAdviceDelivery`** or degrade (cannot call mode “Advisory”) |
| **2 Guarded** | Can pause/cancel/gate tools | + Diff + Pause + Cancel + Resume | Needs real pause **or** explicit CapPause semantics (#72) |
| **3 Full-control** | Checkpoint/rollback/headless/… | L2 + Checkpoint/Rollback/Headless/CLI/MCP/Subagents/SwitchModel | Full actuator surface |

**Research tension (documented, not silently fixed in code):**  
Research L0 text emphasizes log/git/diff tailing; code L0 only requires `CapEventStream`. Treat log/git as **implementation strategies for L0 adapters**, not extra required flags, unless #7 is amended.

---

## Axis B — Intervention Escalation Ladder

Applied **after** detectors/reviewers (or fast-path safety rules). May be **capped** by Axis A.

| Intervention step | Typical action | Min Integration Level usually needed | Notes |
|---|---|---|---|
| **B0 Observe** | Append events to Store / audit | A0 | Always available if events exist |
| **B1 Advisory** | `ZOOM_OUT_PROMPT` / replan advice + ACK path | A1 + CapAdviceDelivery | If A0 only → **human alert** degradation (#68) |
| **B2 Guarded** | Pause / tool defer-deny / require experiment | A2 (CapPause or tool gate) | Fast path may `defer` without full pause (#67) |
| **B3 Full-control** | Git rollback, terminate, switch model, escalate human | A3 for model-switch; A2+workspace for rollback | Schema gaps: switch-model / escalate-human not fully in `action_type` yet (#57) |

---

## Binding rules (product)

1. **Negotiated Integration Level is a ceiling**, not an automatic intervention level.  
   Example: session at A2 may still only run B0–B1 interventions if policy is soft.
2. **Never claim B-step succeeded** if the required Axis A capability is missing.  
   Emit `unsupported_capability` + audit + human path.
3. **Matrix “Target Level” columns are planning hints**, not `NegotiateLevel()` outputs.  
   Handshake uses **flags**, not the marketing row for a harness name.
4. **Claude Code (hooks, no native pause):** plan as **A1** (advisory + hook gate defer) until #72 decides SIGSTOP ≡ CapPause.  
   Do **not** hard-gate “Claude Code sessions are L2”.
5. **OpenHands (native pause):** may plan **A2** for that adapter when pause API is wired.

---

## Crosswalk table (planning)

| If harness matrix suggests… | Default Integration plan | Default max Intervention step without extra product work |
|---|---|---|
| Observe-only IDE | A0 | B0 + human notify |
| Stdio / hooks, no pause | A1 | B1 advisory (+ PreTool defer if CapToolGate) |
| Native pause/cancel | A2 | B2 guarded |
| Graph/SDK full control | A3 | B3 full-control |

---

## Related issues
- #49 research matrix refresh  
- #2 threat model / scoring contracts  
- #7 capability handshake  
- #65 delivery capabilities  
- #57 intervention schema (delivery + L3 actions)  
- #72 CapPause semantics (SIGSTOP vs native)  
- #73 dual-axis docs/ADR acceptance in architecture set  
