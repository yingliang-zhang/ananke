Resume the exact P5 session for a narrow installed-runtime drift repair. Preserve all dirty work. Do not run the real-provider canary, edit the ledger/external wrapper, commit/push, reset/clean/restore, or modify historical OMP prompt/review artifacts.

Verified environment evidence:
- `/opt/homebrew/Cellar/omp/17.1.3/bin/omp` no longer exists.
- current `omp --version` is `omp/17.1.4`.
- `/opt/homebrew/Cellar/omp/17.1.4/bin/omp` is a regular 0555 binary.
- `/Users/yingliangzhang/.omp/natives/17.1.4/pi_natives.darwin-arm64.node` exists.
- the canary now fails safely at setup with closed class `OMP fixture`; no child/provider launch occurred.

Narrow TDD task:
1. Update the frozen supported OMP/runtime contract from 17.1.3 to 17.1.4 in production source and test fixture paths. Do not accept multiple/ambient versions; 17.1.4 becomes the sole closed supported version and 17.1.3 must reject.
2. Add/adjust focused tests proving a 17.1.4 native layout is accepted and 17.1.3/stale/other versions reject; preserve executable/native identity and runtime-authority fail-closed behavior.
3. Update `docs/local-trusted-supervisor-transport-runbook.md` accurately: preserve the historical statement that the earlier provider-free preflight used v17.1.3, but state that the current pinned runtime/canary contract is v17.1.4. Update its enforcing test accordingly.
4. Do not edit historical files under `artifacts/omp/`. Do not change policy/evidence schema versions merely for this frozen runtime value migration unless an existing machine contract demonstrably requires it; explain the decision.
5. Run strict RED, focused tests, relevant `internal/trustedsupervisor` full package, vet, scoped gofmt-diff, and git diff check. Do not run canary.

Return exact commands/results, changed files, and residual risk. Keep this a bounded runtime-version migration only.