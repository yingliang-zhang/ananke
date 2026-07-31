Resume exact P5 v3 direct-OMP implementation session `019f9fef-1f36-7000-b898-48eff4d5b69c` in the current worktree. Do not redesign or broaden scope. Do not call tools to re-read the external wrapper or revisit architecture; the evidence and design are complete. Finish the code/test migration already in progress.

Current independently observed blockers:
- `audit_wrapper_compatibility_test.go`: unused `strconv` import.
- `audit_wrapper_compatibility_test.go:930`: obsolete undefined `auditWrapperDependencyPaths` reference.
- `audit_executor_test.go`: unused `bytes` import.
- gofmt drift is 32138 bytes and `git diff --check` reports a blank line at EOF.

Required finish order:
1. Fix the three compile errors without restoring legacy wrapper execution/dependency machinery.
2. Run gofmt on every changed `internal/trustedsupervisor/*.go`; resolve diff-check.
3. Run compile-only package test.
4. Run focused direct/runtime-authority/timeout/credential/process-group tests; fix root causes. Keep production exactly one direct pinned OMP launch path.
5. Run full `go test ./internal/trustedsupervisor -count=1 -timeout 600s`; then focused race, vet, tagged canary compile if time.
6. Do NOT run the real provider canary. Do NOT modify P6/repaircontract files. Do NOT commit. Do NOT print credentials or raw model output.
7. Return concise changed files, exact tests/results, and any remaining failures. If time is short, prioritize compiling code and a truthful failure ledger over more design commentary.
