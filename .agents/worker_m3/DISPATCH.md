## 2026-08-02T06:48:30Z
You are teamwork_preview_worker for Milestone M3 (Governance, CI & Directory Refactoring).
Your working directory is: /Users/iml1s/Documents/mine/reinframe.
Your workspace folder is: /Users/iml1s/Documents/mine/reinframe/.agents/worker_m3.
Path to user request: /Users/iml1s/Documents/mine/reinframe/ORIGINAL_REQUEST.md. Read this file FIRST.
Path to project specification: /Users/iml1s/Documents/mine/reinframe/PROJECT.md.
Path to Explorer 3 survey analysis: /Users/iml1s/Documents/mine/reinframe/.agents/explorer_survey_3/analysis.md. Read this file for exact fix specifications.

Write Ownership Boundary: You exclusively own `go.mod`, `.github/workflows/ci.yml`, root `README.md`, `.gitignore`, `docs/dev/` migration, and `tests/` directory rename & cleanup.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Tasks for M3:
1. Align Go version in `.github/workflows/ci.yml` using `go-version-file: 'go.mod'` in `actions/setup-go@v5`.
2. Create root `README.md` containing project objective, architecture overview, and Quickstart guide.
3. Create root `.gitignore` excluding `*.db`, `.DS_Store`, `.agents/`, `bin/`, `tmp/`.
4. Create `docs/dev/` directory and move `ORIGINAL_REQUEST.md`, `PROJECT.md`, `TEST_INFRA.md`, `TEST_READY.md` into `docs/dev/` (keep a copy or symlink of PROJECT.md at root if needed for tools, or copy to docs/dev/PROJECT.md).
5. Add `golangci-lint` step (`golangci/golangci-lint-action@v6`) to `.github/workflows/ci.yml`.
6. Rename directory `tests/e2e/` to `tests/integration/`. Update package declaration in all files inside `tests/integration/` to `package integration_test`. Update `ci.yml` test path if needed.
7. Increase `BusyTimeout` in `tests/integration/store_e2e_test.go` (line 1167) from `50 * time.Millisecond` to `500 * time.Millisecond`.
8. Replace all occurrences of `os.MkdirTemp` + `defer os.RemoveAll` in `tests/integration/` with standard `t.TempDir()`.
9. Verify all integration tests pass with `go test -v -race ./tests/integration/...`.

Write a complete report of your changes and test outputs in `/Users/iml1s/Documents/mine/reinframe/.agents/worker_m3/changes.md` and handoff report in `/Users/iml1s/Documents/mine/reinframe/.agents/worker_m3/handoff.md`. Update progress.md in your agent folder regularly. Report back via send_message when done.
