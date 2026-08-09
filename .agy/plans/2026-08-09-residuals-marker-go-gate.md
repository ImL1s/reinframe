# Plan (ralplan) — residuals marker migrate + GO gate + embed

## Critic verdict: APPROVE
Clear residual list; sequential implement on existing branch; no architecture choice risk.

## Approach
1. Ledger: migrate legacy markers; fail closed on ambiguous pipes; require trailing newline
2. Grok: pre-SessionPrompt cancel → not_sent
3. Report: hard GO demotion on preflight/privacy/capability failures
4. Embed schema under cmd/groklive/embed/; loadCommitted uses embed; write evidence schema from embed; drift test
5. Tests + PR + CI + resolve #212 threads

## Order
adapter recovery → grok boundary → report GO gates → schema embed → verify → review → QA → merge
