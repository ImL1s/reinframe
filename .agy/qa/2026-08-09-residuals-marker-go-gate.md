# UltraQA — residuals marker / GO gate

**Verdict: PASS**

## Scenarios
1. Legacy marker migrate → Open loads suppress key (PASS)
2. Ambiguous pipes → Open fails (PASS)
3. Missing JSONL newline → Open fails (PASS)
4. Grok pre-cancel → not_sent, no suppress (PASS)
5. GO + secret_pattern_hits → NO_GO (PASS)
6. GO + missing capability_manifests → NO_GO (PASS)
7. Embedded schema == docs artifact (PASS)
8. Regression: SessionPrompt-fail still send_attempted_unknown path (covered by prior tests, suite green)

## Commands
```
go test ./pkg/adapter/ ./cmd/groklive/ ./pkg/supervisor/ -count=1
```
All ok.
