# Issue execution queue (2026-08-03)

Durable triage for epic #80 backlog. Source hierarchy: README → `docs/roadmap/CURRENT.md` → specs → epic #80 → this queue.

## Depends-on graph

```text
#109 governance (docs)
#104 classifier research ──► #105 shadow (BLOCKED until #104) ──► #100 benchmarks
#106 Claude productize ──► #108 advice consumer (BLOCKED until #106 injection surface)
#107 Codex productize (parallel; observe-first)
#99 managed worktree rollback (P1; large; after ownership review)
#100 M3 FP benchmarks (P2; after #105 or deferred)
```

## Execution order (this loop)

| Order | Issue | Status | This loop |
|------:|-------|--------|-----------|
| 1 | #109 | ready | implement docs PR |
| 2 | #104 | ready | implement research/spec PR |
| 3 | #106 | ready | implement install+fixture control-loop PR |
| 4 | #107 | ready | implement discovery+durable tail+capability PR |
| — | #105 | blocked | defer until #104 merged; comment only |
| — | #108 | blocked | defer until #106 injection surface exists |
| — | #99 | ready P1 | defer with honesty if capacity (large runtime) |
| — | #100 | ready P2 | defer after classifier research |
| — | #80 | epic | update residual; keep OPEN |

## Process

One issue → one branch/PR → `go test` → multi-OS CI → Claude Opus-class review → merge → close with honesty.
