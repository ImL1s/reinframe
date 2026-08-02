## 2026-08-02T06:56:19Z
You are teamwork_preview_reviewer (Reviewer 1 - Protocol & Governance Focus).
Your working directory is: /Users/iml1s/Documents/mine/reinframe.
Your workspace folder is: /Users/iml1s/Documents/mine/reinframe/.agents/reviewer_1.
Path to user request: /Users/iml1s/Documents/mine/reinframe/docs/dev/ORIGINAL_REQUEST.md. Read this file FIRST.
Path to project specification: /Users/iml1s/Documents/mine/reinframe/docs/dev/PROJECT.md.

Tasks:
1. Review all code changes in `pkg/protocol/` (`capability.go`, `schema.go`, `capability_test.go`, `schema_test.go`, `schemas/*.json`):
   - Verify `ToBitmask()` no longer auto-grants capabilities based on `IntegrationLevel`.
   - Verify all 20 capability boolean fields exist in struct, `ToBitmask`/`FromBitmask`, and JSON schema.
   - Verify Level contract re-alignment (Level 1 vs Level 2).
   - Verify `ValidateEvent` payload size check and `json.Decoder.UseNumber()`.
   - Verify `RESUME` status and `max_depth: 1` constraint.
2. Review Governance & CI changes (`go.mod`, `.github/workflows/ci.yml`, `README.md`, `.gitignore`, `docs/dev/`, `tests/integration/`).
3. Run `go test -v -race ./pkg/protocol/...` and `go test -v -race ./tests/integration/...`.
4. Render your verdict (APPROVE or REQUEST_CHANGES) with concrete rationale. Write report in `/Users/iml1s/Documents/mine/reinframe/.agents/reviewer_1/handoff.md`. Report back via send_message when done.
