# Historical #167 v1 evidence (immutable)

The file:

`issue-167-live-grok-1.0.0-3cd0d0cbcebe-stable-darwin-2026-08-08.json`

is **historical**. It records a real darwin/`grok 1.0.0` live run and remains useful.

It must **not** be treated as a closed, mechanically gated **GO** product disposition.

Public status (2026-08-09, issue **#201**):

```text
real live Grok evidence exists
mandatory foundation paths observed (hooks, ACP init/auth/session/prompt/update)
GO requalification pending (#199)
public product disposition = MORE_DATA
explicit ACK = not proven
Level 2 / CapPause = not proven
exactly-once = not proven
```

Reasons the v1 package cannot support unconditional GO (see #199):

- disposition matrix omitted trust/tool-map/static-permission/dedupe/challenge/optional-load gates
- schema allowed `additionalProperties` with only three required fields
- HOOK-DENY was inductive (side-effect absence), not direct deny correlation
- HOOK-FAIL could not distinguish fail-open from untrusted/skipped hook
- session_visible was temporal, not source-correlated to InterventionID/request

A new **v2** evidence artifact will be produced under #199 without rewriting this file.
