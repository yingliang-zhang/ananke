Resume the exact same session and complete the interrupted implementation. Do not rediscover or redesign.

Current concrete blocker from independent compile:
- `internal/trustedsupervisor/audit_executor_test.go:601` contains leaked edit syntax `INS.PRE 655:` and line 602 contains a misplaced `spec.PathRefs = append(spec.PathRefs, path)` inside the artifact-count loop. Remove the leaked/misplaced lines and restore the intended loop. Then run gofmt and the two fresh-session tests immediately.
- Production helpers now exist at `audit_executor.go:1567+` and `audit_evidence.go:839+`. Review those exact ranges only for compile/correctness.

After scanner GREEN, complete only the missing runtime proof:
1. Extend fake OMP minimally to create one authenticated fresh OMP session JSONL for a nonzero-exit scenario.
2. Prove `runAuditInvocation` returns `runErr == nil` with the requested nonzero ExitCode, so the existing runtime classification is `direct_omp_exit_nonzero` rather than the generic capture failure.
3. Add one malformed/leaking fake-session case proving `runErr != nil` and fail-closed classification.
4. Run focused once, `-count=10`, focused `-race -count=3`, full `internal/trustedsupervisor`, vet, gofmt-diff, and git diff check. No real-provider canary, ledger edit, commit, or push.

Do not expand the test matrix further. Report exact commands/results.