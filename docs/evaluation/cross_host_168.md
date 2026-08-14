# Cross-Host Evaluation Framework (#168)

## 1. Overview & Lanes

The cross-host evaluation framework provides an objective, empirical basis for comparing agent host integration, tool gating, intervention transport, and session control across major coding agent runtimes without subjective or anecdotal rankings.

| Lane | Description |
|------|-------------|
| `cross_host_eval_fake` | Deterministic fake host adapters only (CI; no network/credentials/transcripts). |
| `cross_host_eval_partial_live` | Fake fixtures **plus** a single live host pin (#164, #120, or #167). |
| `cross_host_eval_tri_host_live` | Unified 3-way live matrix synthesizing **Codex (#164)**, **Claude Code (#120)**, and **Grok Build (#167)**. |

---

## 2. Tri-Host Comparative Synthesis (Codex vs Claude vs Grok Build)

| Dimension | OpenAI Codex (#164) | Claude Code (#120) | Grok Build (#167) |
|---|---|---|---|
| **Host Integration Level** | Level 1 (Hooks) & Level 2/3 (App Server JSON-RPC) | Level 1 (Experimental PreTool bridge) | Level 1 (Hooks) & Level 2 (ACP stdio) |
| **Tool Gating & Interception** | **Fail-Closed** (`deny` JSON-RPC / exit code; `continue: false` omitted for session continuity) | **Fail-Closed** (`approve` / `block` / custom reason string; `continue` omitted) | **Fail-Open** on hooks (host runs tool on hook timeout/crash); ACP prompt transport |
| **Intervention Transport** | Bounded `additionalContext` in PreTool response (<= 2000 runes) | Bounded `reason` in PreTool response (<= 2000 runes) | ACP `session/prompt` prompt injection with watermark |
| **Strongest Proven ACK** | `transport` | `transport` | `transport` (clean quota) / `session_visible` (legacy temporal) |
| **Model Selection Policy** | Exact match enforced; zero silent substitution fail-closed | Exact match enforced | Host-managed |
| **Live Control Evidence** | `docs/evidence/codex/runs/20260815T020000Z/` (**GO**) | `docs/evidence/claude/runs/20260815T020000Z/` (**GO**) | `docs/evidence/grok_build/runs/20260811T130935Z/` (**NO_GO**) |
| **Live Qualification Model** | `gpt-5.3-codex`, `gpt-5.3-codex-spark` (#187) | Claude 3.5 Sonnet / Opus | `grok 1.0.0 (3cd0d0cbcebe)` |

---

## 3. Comparative Disposition: MORE-DATA

**Authoritative Disposition: `MORE-DATA`**

1. **Zero Anecdotal Ranking:**
   Even though all three live lane pins are now attached under `cross_host_eval_tri_host_live`, Reinframe **strictly withholds** comparative ranking scores (tunneling scores remain fixture-zero). A valid ranking requires identical, matched benchmark tasks executed concurrently under identical network, workspace, and quota conditions.

2. **ACK Layer Invariance:**
   Reinframe maintains strict separation across ACK layers:
   - `transport`: Wire delivery confirmed to host process stdio/hooks.
   - `session_visible`: Proven appearance in model conversation context.
   - `explicit`: Explicit cognitive acknowledgment by the agent model.
   - `behavioral`: Observable change in model downstream plan.
   Explicit ACK is never claimed for any host in synthetic or partial live runs.

3. **Intervention & Gating Mechanics:**
   - **Codex (#164 / #187):** Demonstrates robust, deterministic tool blocking via JSON-RPC `.codex/hooks.json` and App Server stdio. Bounded context injection successfully steers subsequent model turns without session abortion.
   - **Claude (#120):** Demonstrates clean PreTool allow/block interception with custom reason payloads, preserving session loop continuity.
   - **Grok Build (#167):** Hooks fail open on crash/timeout (documented and expected behavior). ACP stdio allows session/prompt intervention, but quota exhaustion and platform binding gaps require further clean-quota campaigns for a `GO` disposition.

---

## 4. Live Lane Pins

### A. Codex Live Pin (#164)
- **Profile:** `reinframe.codex_hooks.2026-08-06.v1`
- **Evidence:** `docs/evidence/codex/runs/20260815T020000Z/issue-164-live-codex-control.json`
- **Disposition:** `GO` (5/5 scenarios PASS)
- **Strongest ACK:** `transport`

### B. Claude Live Pin (#120)
- **Profile:** `reinframe.claude_hook_response.v1`
- **Evidence:** `docs/evidence/claude/runs/20260815T020000Z/issue-120-live-claude-control.json`
- **Disposition:** `GO` (4/4 scenarios PASS)
- **Strongest ACK:** `transport`

### C. Grok Build Live Pin (#167)
- **Profile:** `reinframe.grok_build_hooks.2026-08-06.v1 + reinframe.grok_build_acp.v1`
- **Evidence:** `docs/evidence/grok_build/runs/20260811T130935Z/` (`issue-167-live-v2-grok-1.0.0-3cd0d0cbcebe-unknown-2026-08-12.json`)
- **Disposition:** `NO_GO` (executable-binding & scan context gates)
- **Strongest ACK:** `transport`

---

## 5. Verification & Testing

```bash
# Test deterministic fake CI lane
go test -v ./pkg/evaluation/ -run 'TestCrossHostEvalFake'

# Test single live lane attachments
go test -v ./pkg/evaluation/ -run 'TestAttachLive'

# Test 3-way live matrix synthesis
go test -v ./pkg/evaluation/ -run 'TestSynthesizeCrossHost168Report'
```
