# Independent hard review — Ananke Slice-001 candidate v11

Work read-only in `/Users/yingliangzhang/Projects/ananke`. Do not edit files, regenerate reports, run mutation-tag builds, kill processes, commit, or push. Ordinary focused tests and source inspection are allowed. Your task is to independently audit the frozen candidate before Slice-001 can be committed.

## Frozen evidence

- Manifest: `artifacts/omp/slice-001-final-review/candidate-manifest-v11.json`
- Base: `b8e21eac0b808a9ad35b90e59e1eacd925753d28`
- Candidate aggregate: `93aef9afc7771cb79c35d3c7df0fa6bca6f50e8071619d0fa36473198b82dd7f` over 68 source files.
- Bound reports: verification `b9b091de…08d75`; mutation `ce5beb9a…e797a`; stress `5f10cde5…9c834`; blackbox `2751d191…8d6b0`.
- Canonical v11 command evidence: verify/mutation/stress/blackbox all PASS; Python 26 tests PASS; gofmt/diff PASS; process scans PASS at 0/1/4 seconds.

## Required review order

1. Independently recompute source aggregate/file count and report SHA-256 values. Confirm every report declares the same candidate aggregate/file count. If not exact, stop with `CHANGES REQUESTED`.
2. Audit `internal/lifecycle/engine.go` transcript tail logic. Attempt to falsify: unsealed valid JSON at EOF must not publish or advance; newline records publish; sealed final no-newline record publishes exactly once; concatenated later data after withheld prefix cannot duplicate/prematurely publish.
3. Audit the process topology across `internal/lifecycle/backend.go`, `internal/supervisor/supervisor.go`, identity and ADR-0002. Establish or refute:
   - real worker cannot exec before paused trampoline authority (identity/store/socket/running) publication;
   - supervisor is outside worker group; trampoline/worker is group leader;
   - cleanup only uses atomic negative-PGID signals while unreaped leader pins identity;
   - post-fork publication failure has a durable nonterminal cleanup obligation;
   - no production SignalProcess/BecomeGroupLeader backdoors remain.
4. Audit test hygiene changes in `internal/lifecycle/engine_test.go`, `internal/supervisor/mutation_test.go`, and `internal/supervisor/supervisor_test.go`. Verify they clean only known test fixture processes after intentionally detected mutations; they must not alter production cleanup semantics or turn mutant failures into passes.
5. Review P2 hardening: entropy error propagation, watcher terminal-error readiness close, file and directory fsync, and no token stderr logging.
6. Run only compact independent probes needed to validate claims. Do not run tagged mutations; they intentionally leave negative-test artifacts under some failure modes.

## Verdict format

Start exactly with one line: `VERDICT: ACCEPT` or `VERDICT: CHANGES REQUESTED`.

Then write compact sections: Candidate binding; Confirmed findings (P1/P2/P3 with file:line and repro); Evidence; Residual risks; Required changes. Findings must be real, actionable, and attributable; do not invent concerns. ACCEPT is permitted only if no P1 remains and frozen binding is exact.
