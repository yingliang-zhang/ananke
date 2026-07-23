# Ananke Experiment Ledger

This ledger records executable reliability experiments and independent review gates for Ananke. Results must come from checked-in reports, test output, or review artifacts; blank cells mean unverified.

## Summary

| Date | Experiment / run | Hypothesis | Branch / commit | Variable | Verification result | Stress result | Mutation result | Review result | Decision | Evidence |
|---|---|---|---|---|---|---|---|---|---|---|
| 2026-07-18 | `slice-001-hard-review-b8e21ea` | Passing functional gates is insufficient unless source-level ADR invariants survive an independent hard review. | `main` @ `b8e21ea` | Independent hard review of frozen Slice-001 candidate | Prior gates were green, but review found source-contract violations | Prior stress did not cover live transcript truncation | Review found M1/M4 gates were proxy/no-op detections | 2 BLOCKER, 7 MAJOR, 6 MINOR | `iterate` — repair blockers/majors, then rerun all gates and independent re-review | [`artifacts/omp/slice-001-review/full-review.md`](../artifacts/omp/slice-001-review/full-review.md) |
| 2026-07-18 | `slice-001-baseline-8bc3e21` | The initial Darwin supervisor proof can pass build, race, no-CGO, mutation, repeated lifecycle, and black-box gates. | `main` @ `8bc3e21` | Initial Vertical Slice 1 implementation | 6/6 verification gates PASS: gofmt, vet, test, race, no-CGO test, no-CGO build | Crash/restart 20/20; cancellation 20/20 | 6/6 reported detected | Not yet reviewed at this commit | `iterate` — later hard review showed the passing suite gave false confidence | [`reports/verification.json`](../reports/verification.json), [`reports/stress-lifecycle.json`](../reports/stress-lifecycle.json), [`reports/mutation-proof.json`](../reports/mutation-proof.json), [`reports/blackbox-lifecycle.json`](../reports/blackbox-lifecycle.json) |
| 2026-07-21 | `p0a-schema-codegen` | A generated Go/Rust/TypeScript contract toolchain can reproduce the frozen public/private JSON boundary without hand-edited generated code or renderer secret leakage. | `spike/p0a-schema-codegen` @ `eedba6d` | Binary spike: JSON Schema + Quicktype versus Proto3 + Buf ecosystem | Arm A revision-4 verifier PASS: 15.889s, 6 files / 677 LOC; Arm B partial scripts failed runtime/drift gates | Arm A preserves all frozen payload forms; Arm B Rust drops `null` payload | Arm A staged content drift PASS; Arm B drift probe failed to prove content difference | Arm A final focused hard re-review ACCEPT; Arm B BOUNDED | `select Arm A` — JSON Schema + Quicktype, P0a only | [`P0a contract`](experiments/p0a-schema-codegen-contract.md), [`ADR-0004`](adr/0004-select-json-schema-quicktype-for-p0a-codegen.md), [`A final review`](../artifacts/omp/p0a/arm-a-final-focused-rereview-output.md) |

## Decisions Log

### 2026-07-21 — P0a selects JSON Schema + Quicktype for the next experiment stage

- Frozen contract revision 4 separated private daemon requests, internal daemon
  responses, and public renderer DTOs. The public nested `RunDto.diagnostics`
  fixture, `ok:false` presence, payload variants, generated-public privacy,
  drift, clean regeneration, optional evolution, and production-scope guard
  were all explicit must-haves.
- Arm A (`JSON Schema 2020-12 + Quicktype`) independently ran
  `node scripts/verify.mjs` successfully in `15.889s`: six generated files / 677
  LOC, Go 1.26.5, Git 2.54.0, Quicktype 26.0.0, TypeScript 5.9.3, Cargo 1.97.1.
  The final focused hard review returned `ACCEPT`.
- Arm B (`Proto3 + Buf`) generated code but is **BOUNDED — CHANGES REQUESTED**:
  its Rust fixture run dropped `payload:null`, several probes target missing
  Rust tests, and drift rejection was not a content-difference proof. It also
  lacked complete verifier, evolution/breaking, and README evidence.
- ADR-0004 selects Arm A for further experiment work only. No daemon transport,
  production DTO, or semantic adapter migration is authorized by this decision.

### 2026-07-18 — Slice-001 baseline not accepted despite all executable gates passing

The checked-in reports at commit `8bc3e21` verify:

- verification gates: 6/6 PASS;
- crash/restart stress: 20/20;
- cancellation stress: 20/20;
- mutation proof: 6/6 reported detected;
- black-box scenarios: success and cancellation both PASS with zero name-matched survivors.

The independent hard review at commit `b8e21ea` found 2 BLOCKER, 7 MAJOR, and 6 MINOR findings. Therefore these earlier results are retained as reproducible evidence but are **not** evidence that ADR-0002/0003 were satisfied. The candidate remains `iterate` until blocker/major repair, complete gate rerun, and independent re-review.

## Current Repair Campaign

| Batch | Scope | Status | Exit criteria |
|---|---|---|---|
| P0 | B1 live transcript truncation; B2 `cleanup_required` progression; M6 real M4 mutation | focused PASS | Independent focused runs passed: B1 3×, B2 3×, M6 baseline 2× plus tagged behavioral rejection; full gate remains pending |
| Lifecycle identity | M1 worker deferred reap; M7 supervisor PID pinning | focused PASS | M1 uses Darwin `kqueue`/`EVFILT_PROC`/`NOTE_EXIT` with worker `Wait4` isolated to `ReapWorker`; M7 locally observes supervisor exit without reaping, pins unresolved crashes, reaps only terminal/finalized children, and leaves live supervisors untouched on `Engine.Close`; independent focused/race/mutation/process checks passed |
| Recovery | M5 identity verification; M4 recovery exit; M3 outbox reconciliation | focused PASS | M5 authenticates durable identity/socket adoption; M4 safely exits `recovery_unknown`; M3 replaces PID-only outbox handling with one identity-authenticated startup/tick reconciler, durable abandonment diagnostics, exact local-child pinning, and idempotent resolution |
| Cancellation | M2 immediate asynchronous acceptance | focused PASS | Nonterminal API calls return accepted before supervisor I/O; exact-token background cancel is bounded and deduplicated, failed/non-OK requests clear their marker for safe retry, and missing/terminal runs reject without scheduling |
| Repair C | Durable cancellation intent; joined Engine shutdown | focused PASS | Schema v5 persists idempotent cancel intent before acceptance; created/recovery states retry only with exact local ownership or authenticated recovery; `Engine.Close` cancels and joins owned work before closing tails/store; independent package/race/format/leak gates passed |
| Evidence harness | Candidate binding; terminal stress/black-box proof | focused PASS | `ananke-source-manifest-v1` binds all reports to 64 source files; one-iteration crash/restart and cancellation stress plus both black-box scenarios prove terminal state, event/offset durability, acknowledged outbox, exact PGID quiescence, and zero survivors |
| Final gate | verify + mutation + stress + black-box + independent re-review | iterate | Candidate-bound 20× stress, 6/6 mutations, and black-box passed, but canonical no-CGO exposed test cleanup races and mutation cleanup leaked a resistant helper; repair and full rerun required |

### 2026-07-18 — P0 focused repair evidence

- B1: `go test ./internal/lifecycle/ -run '^TestEngineTranscript(Corruption|TruncationAfterTailing)$' -count=3` → PASS (6 test executions).
- B2: focused store, supervisor, and lifecycle commands with `-count=3` → PASS; includes live corruption cleanup, nonblocking one-shot cancel, safe dead-supervisor probes, and terminal/outbox atomic rollback.
- M6: baseline dedicated mutation test `-count=2` → PASS; tagged mutation exited 1 with `terminal state "failed" visible while worker/supervisor alive` and both PID liveness values true.
- These are focused repair results, not final Slice-001 acceptance. Full verify/race/mutation/stress/black-box gates and independent re-review remain required.

### 2026-07-19 — M1 deferred worker reap focused evidence

- Exact-resume OMP session `019f7578-a8ff-7000-a925-b6c43474043c` completed with Darwin `kqueue` + `EVFILT_PROC` + `NOTE_EXIT` observation; a production-source scan found the sole `Wait4` at `internal/lifecycle/backend.go:184` inside `ReapWorker`.
- `go test ./internal/lifecycle/ -run '^Test(BecomeGroupLeader|LaunchWorker|WorkerExited|ReapWorker|ProcessAlive)' -count=3 -timeout 120s` → PASS (`ok`, 2.755s).
- `go test ./internal/supervisor/ -run '^TestMutationReapBeforeCleanupOrder$' -count=3 -timeout 120s` → PASS (`ok`, 17.937s).
- Tagged `mutation_reap_before_cleanup` run exited 1 with the behavioral rejection `worker PID ... was reaped before group cleanup`; it was not a compile failure or timeout.
- Focused race runs for lifecycle exit/reap tests and the M1 supervisor baseline both passed (`ok`, 4.760s and 8.456s).
- Two stale orphaned `supervisor.test` processes from 2026-07-18 were identified and removed; the final process scan reported no matching Ananke worker, supervisor, lifecycle-test, or supervisor-test processes.
- This is focused M1 acceptance only. M7 and all final Slice-001 executable/re-review gates remain pending.

### 2026-07-19 — M7 daemon supervisor PID pinning focused evidence

- The M7 exact-resume implementation run timed out at 600 seconds after completing code and initial focused/race checks; worktree and exact JSONL inspection showed only final verification/prose remained, so acceptance was performed independently without another resume.
- Production-source scan found no eager/background supervisor wait; the sole supervisor reap is `h.cmd.Wait()` inside the `runHandle.reap` once-guard after exact exit observation plus terminal and acknowledged/abandoned outbox checks.
- `go test ./internal/lifecycle/ -run '^TestEngine(SupervisorSIGKILL|TerminalSupervisorReaped|CloseLeavesLiveSupervisor|DaemonRestartRecover)$' -count=3 -timeout 240s` → PASS (`ok`, 47.753s).
- The same M7 focused set under `go test -race` → PASS (`ok`, 17.951s).
- Post-refactor M1 backend tests `-count=3` → PASS (`ok`, 1.812s); M1 supervisor mutation baseline `-count=3` → PASS (`ok`, 17.574s); focused M1 race → PASS (`ok`, 2.740s).
- Tagged `mutation_reap_before_cleanup` still exited 1 with `worker PID ... was reaped before group cleanup`, confirming behavioral rejection after the shared watcher refactor.
- `gofmt -d` and `git diff --check` passed; the final process scan found no matching Ananke worker, supervisor, lifecycle-test, or supervisor-test processes.
- This is focused Lifecycle identity acceptance only. Recovery M5/M4/M3, M2, final executable gates, and independent re-review remain pending.

### 2026-07-19 — M5 identity-authenticated restart recovery focused evidence

- Fresh OMP session `019f7a6e-1fea-7000-a467-e29b0d5e3766` timed out after implementing M5 and running initial green checks; exact JSONL showed only final verification/reporting remained, so acceptance was completed independently without another resume.
- Recovery now reads the durable identity before trust, validates run ID plus supervisor/worker PID, PGID, socket, token, and transcript fields, then authenticates both `status` and `adopt`; rejected identity/authentication paths durably enter `recovery_unknown` without group enumeration, signals, cleanup requests, tailing, or false local child ownership.
- `go test ./internal/lifecycle -run '^(TestEngineRecover(ValidAuthenticatedSupervisor|RejectsIdentity|RejectsSupervisorAuthentication)|TestIdentityFileRoundTrip)$' -count=3 -timeout 180s` → PASS (`ok`, 0.361s).
- `go test ./internal/lifecycle -run '^TestEngineDaemonRestartRecover$' -count=3 -timeout 180s` → PASS (`ok`, 23.009s).
- `TestSupervisorAdoptCommand` and store transition focused runs with `-count=3` → PASS (`ok`, 0.386s and 0.376s).
- M5/restart lifecycle `-race` → PASS (`ok`, 10.460s); supervisor adopt `-race` → PASS (`ok`, 1.615s).
- Accepted M7 focused set rerun after shared engine/socket changes → PASS (`ok`, 16.300s).
- `gofmt -d`, `git diff --check`, and final process-leak scan passed.
- This is focused M5 acceptance only. M4 recovery progression, M3 outbox reconciliation, M2, final executable gates, and independent re-review remain pending.

### 2026-07-19 — M4 `recovery_unknown` progression focused evidence

- The M4 exact-resume implementation timed out after its focused GREEN runs. Independent review found that a correctly pinned local supervisor zombie remained visible in its PGID and could prevent real quiescence forever; an exact-UUID resume added the real-process regression and fixed production to ignore only that exact locally tracked, NOTE_EXIT-confirmed supervisor PID. Unowned/stored supervisor PIDs remain group occupancy.
- Authenticated `status`/`adopt` responses must agree on state and identity before `recovery_unknown` transitions to `running`, `cancelling`, or `cleanup_required`. Ambiguous/live-unauthenticated cases remain nonterminal with zero signals.
- Confirmed supervisor loss commits real terminal `failed` plus a pending outbox only after the validated worker PID is absent and group enumeration has no member other than an exact pinned local supervisor zombie. A fresh-engine regression proves the pending obligation survives without the in-memory marker and the terminal transition remains exactly once.
- Independent `go test ./internal/lifecycle -run '^TestEngineRecoveryUnknown' -count=3 -timeout 240s` → PASS (`ok`, 8.412s); focused `-race` → PASS (`ok`, 6.106s).
- Independent terminal/outbox store tests with `-count=3` → PASS (`ok`, 0.674s).
- Accepted M5 recovery and M7 crash/pinning focused regressions after the M4 production changes → PASS (`ok`, 9.063s and 11.268s).
- One independent `-count=3` run exposed a test setup race where `running` became visible before durable supervisor IDs; the regression now deterministically waits for positive durable IDs. A temporary `hermes-verify-*` script reran that exact test 3× (`ok`, 8.490s), checked formatting, and was removed. This was ad-hoc verification, not a full-suite claim.
- A separate temporary `hermes-verify-*` script exercised the local-zombie and fresh-engine durability regressions (`ok`, 4.614s), checked M4 file formatting/whitespace, and was removed; also ad-hoc only.
- Final `gofmt -d`, `git diff --check`, and process-leak scan passed.
- This is focused M4 acceptance only. M3 outbox reconciliation, M2, final executable gates, and independent re-review remain pending.

### 2026-07-19 — M3 identity-safe outbox reconciliation focused evidence

- Fresh OMP session `019f7a98-bca2-7000-940d-686c42a682f6` timed out after implementing the shared reconciler, v3 schema migration, authenticated `finalize` command, and focused GREEN tests. An exact resume fixed two M4 integration expectations before also timing out; final acceptance was completed independently.
- Startup `Recover` and periodic `tick` now use one idempotent pending-outbox reconciler. It requires matching durable run/identity/outbox authority, never trusts a live PID alone, acknowledges only an authenticated terminal `finalize` response, and abandons only after validated supervisor loss plus worker/group quiescence.
- The temporary M4 `recoveryOutbox` map/reason guard was removed. Migration v3 adds a durable `diagnostic` column; abandonment now rejects an empty reason and persists the nonempty diagnostic. Exact local NOTE_EXIT-confirmed zombies may be ignored as group occupancy and remain pinned until durable resolution, after which they are reaped once; unowned numeric PIDs remain occupancy.
- Independent `go test ./internal/lifecycle -run '^TestReconcilePendingOutbox' -count=3 -timeout 240s` → PASS (`ok`, 2.752s); focused `-race` → PASS (`ok`, 6.064s).
- Independent M3 store/supervisor tests with `-count=3` → PASS (`store` 1.041s, `supervisor` 0.575s).
- Updated full M4 recovery set → PASS (`ok`, 4.745s); M4 `-race` → PASS (`ok`, 8.603s).
- Accepted M5 and M7 focused regressions after the shared recovery/finalization changes → PASS (`ok`, 10.660s and 12.683s). Migration-v1/outbox and adopt/finalize protocol regressions → PASS (`store` 1.178s, `supervisor` 1.557s).
- A temporary `hermes-verify-*` script exercised the two corrected exact-child integration paths (`ok`, 4.508s), checked formatting/whitespace, and was removed. This was ad-hoc verification, not a full-suite claim.
- Final scoped `gofmt -d`, `git diff --check`, and process-leak scan passed; all temporary scripts created by this session were removed. Unrelated GW verification files in the shared OS temp directory were left untouched.
- This is focused M3 acceptance only. M2, final executable gates, and independent re-review remain pending.

### 2026-07-19 — M2 immediate asynchronous cancellation focused evidence

- The first fresh M2 OMP run timed out after planning without M2 edits. Exact resume `019f7aad-57ff-7000-b56a-fa5d81e30067` implemented and verified the minimal asynchronous handler. A second exact resume exposed and fixed one cleanup-retry safety issue before timing out; final test coverage and acceptance were completed independently.
- RED evidence against the synchronous handler: delayed-response and unreachable-socket cases failed (`2.356s`), proving the API waited for supervisor I/O and rejected unreachable cancellation.
- `handleCancelRun` now synchronously validates only run existence/terminal state, schedules the existing deduplicated background `cancel`, and immediately returns accepted. The background marker remains after authenticated success but is cleared after transport/decode/non-OK failure so a later explicit request or `cleanup_required` tick can retry.
- Independent `go test ./internal/lifecycle -run '^TestEngineCancelRun.*$' -count=3 -timeout 300s` → PASS (`ok`, 39.919s).
- Independent M2 plus shared cleanup `-race` → PASS (`ok`, 14.888s); shared cleanup-required regressions with `-count=3` → PASS (`ok`, 1.221s).
- Deterministic coverage proves a blocked supervisor response does not delay acceptance beyond 500ms, exact `cmd=cancel`/token delivery, unreachable acceptance, 16-way duplicate deduplication, terminal/missing rejection with zero requests, failed/non-OK marker clearing followed by exactly one successful retry, and real cancellation reaching `cancelled`.
- A fresh temporary `hermes-verify-*` script reran the failed-request retry 3× (`ok`, 0.349s), checked M2 formatting/whitespace, and was removed. This was ad-hoc verification, not a full-suite claim.
- Final `gofmt -d`, repository `git diff --check`, and process-leak scan passed.
- This is focused M2 acceptance only. Final executable gates and independent re-review remain pending.

### 2026-07-19 — Final executable gates and frozen review candidate

- The first canonical `python3 scripts/verify.py` run exposed two stale/racy integration fixtures: immediate identity consumption after `running`, and a pre-M3 terminal outbox with missing authority. No production change was needed. Tests now wait for published durable identity, and the crash-before-finalize fixture supplies matching run/identity/outbox authority plus deterministic dead/quiescent backend evidence.
- Independent focused identity/finalization regressions → PASS (`ok`, 24.521s). OMP focused 3× and race evidence also passed (`56.613s` and `28.092s`).
- Canonical `python3 scripts/verify.py` rerun → **ALL GATES PASS**: gofmt 0.4s, vet 2.0s, test 105.1s, race 116.4s, no-CGO test 80.1s, no-CGO build 1.2s. Report timestamp `2026-07-19T14:50:04Z`.
- `python3 scripts/mutation_proof.py` → **6/6 DETECTED**, every mutation classified `behavioral_test_rejection`; no compile-failure or timeout classifications. Report timestamp `2026-07-19T14:51:34Z`.
- `python3 scripts/stress_lifecycle.py` → crash/restart **20/20**, cancellation **20/20** with every request accepted and terminal state `cancelled`. Report timestamp `2026-07-19T14:56:44Z`.
- The first black-box invocation exposed a harness readiness race (`daemon.sock` absent after fixed one-second sleep). `scripts/blackbox_lifecycle.py` now probes authenticated `ping` with a bounded readiness deadline and fails with captured daemon stderr. Rerun → success `completed`, cancellation `cancelled`, **0 survivors** in both scenarios. Report timestamp `2026-07-19T14:59:55Z`.
- Final Python script compilation, repository `git diff --check`, and daemon/worker/supervisor/test process-leak scan passed.
- Frozen code/test candidate: base `b8e21eac0b808a9ad35b90e59e1eacd925753d28`, 19 modified/untracked files under `internal/` and `scripts/`, manifest `artifacts/omp/slice-001-final-review/candidate-manifest.json`, SHA256 `19a3629aef125034538626ec0aa9391dc948c4869b5f175d4eb2179fe8d68d9e`.
- Executable gates are complete. Slice-001 remains pending until an independent hard review returns explicit ACCEPT for this exact candidate hash.

### 2026-07-20 — Repair C durable cancellation and shutdown evidence

- Exact OMP session `019f7b4f-5b6d-7000-9141-8fccf0085bf4` implemented schema v5 durable cancellation and joined Engine shutdown. Its first three-package race run exposed a real startup cancel race: `TestSupervisorCancelCommand` observed `completed` instead of `cancelled`; buffering the one-shot cancel channel fixed the lost pre-receiver signal, and the subsequent focused plus three-package race reruns passed.
- The OMP synthesis incorrectly reported that its process scan found no leaks. Independent inspection instead found one live supervisor intentionally preserved by `TestEngineCloseLeavesLiveSupervisor`; the test now proves `Engine.Close` leaves it alive and then removes the exact worker/supervisor during `t.Cleanup`, including reaping the owned supervisor handle.
- Independent `go test ./internal/lifecycle -run '^TestEngineCloseLeavesLiveSupervisor$' -count=1 -timeout 60s` → PASS (`ok`, 1.669s), followed by an empty Ananke process scan.
- Independent `go test ./internal/store ./internal/supervisor ./internal/lifecycle -count=1 -timeout 240s` → PASS (`store` 0.462s, `supervisor` 6.951s, `lifecycle` 15.235s).
- Independent `go test -race ./internal/store ./internal/supervisor ./internal/lifecycle -count=1 -timeout 300s` → PASS (`store` 2.301s, `supervisor` 17.492s, `lifecycle` 25.799s).
- Scoped `gofmt -d`, repository `git diff --check`, and final process-leak scan → PASS. Repair C is focused-accepted; canonical verify/mutation/stress/black-box gates and a newly frozen independent hard review remain required.
- Repair C changed the source candidate, so the 2026-07-19 executable reports and candidate SHA256 above are retained as historical evidence but are superseded for acceptance.

### 2026-07-20 — Candidate-bound harness focused acceptance

- Added deterministic `ananke-source-manifest-v1` over `go.mod`, `go.sum`, `cmd/**/*.go`, `internal/**/*.go`, and direct `scripts/*.py`; reports refuse to overwrite when the source manifest drifts during execution.
- Independent `python3 -m unittest scripts/test_candidate_manifest.py scripts/test_harness_support.py scripts/test_stress_lifecycle.py` → 18/18 PASS.
- Independent `python3 scripts/blackbox_lifecycle.py` → success PASS and cancellation PASS; both terminal outboxes acknowledged and exact persisted PGIDs had zero survivors.
- Independent `python3 scripts/stress_lifecycle.py --iterations 1` → crash/restart 1/1 PASS and cancellation 1/1 PASS. Crash evidence: `completed`, one event at sequence 1, positive committed offset, acknowledged outbox, zero survivors. Cancellation evidence: accepted, `cancelled`, acknowledged outbox, zero survivors.
- Both smoke reports independently recomputed and matched candidate SHA256 `d5cbc341d1c46875d5d901afd8636686652889c4a1caf9f8f93a322305c4d6bf` across 64 files. Python compilation, `git diff --check`, and final process-leak scan passed.
- This is focused harness acceptance only; canonical verify, six mutation gates, default 20× stress, final black-box, and independent hard review remain required.

### 2026-07-20 — Promotion gate attempt rejected by test-process leaks

- Candidate SHA256 `d5cbc341d1c46875d5d901afd8636686652889c4a1caf9f8f93a322305c4d6bf` was exercised through all four candidate-bound reports.
- Canonical verify: gofmt PASS (0.1s), vet PASS (0.4s), normal tests PASS (16.0s), race tests PASS (27.4s), no-CGO test FAIL (15.3s), no-CGO build PASS (0.7s). The report lacked stdout, so the original Go failure text was not retained.
- Independent no-CGO reproduction passed once, then `CGO_ENABLED=0 go test ./... -count=3 -timeout 300s` exposed `TestSupervisorAdoptCommand` racing `TempDir RemoveAll` (`directory not empty`) and left two fake workers. Root cause: per-test supervisor-only Kill/Wait defers run before the central fork cleanup and can abandon the worker process group.
- Mutation proof behaviorally rejected 6/6 unsafe mutations, but `mutation_cancel_parent_only` intentionally left resistant helper PID 20672 alive because failed-test cleanup did not remove the exact owned group.
- Hardened stress reached terminal evidence for crash/restart 20/20 and cancellation 20/20; black-box success and cancellation both passed. These results do not override the canonical/leak failures.
- The attempt is rejected. Repair is limited to exact test-process ownership cleanup and bounded stdout diagnostic capture; all source-bound reports must be regenerated afterward.

### 2026-07-20 — Final-gate test ownership repair

- Centralized `forkSupervisor` cleanup now signals the exact still-owned supervisor process group before killing/waiting the supervisor child; redundant per-test supervisor-only Kill/Wait defers were removed. A new regression owns a supervisor, worker, and resistant child in one PGID and proves all three plus the socket are gone after subtest cleanup.
- `scripts/verify.py` now prints and stores bounded stdout as well as stderr, so Go test failures remain diagnosable in the candidate-bound report.
- RED proof: the new cleanup regression initially reported its worker and resistant child surviving. GREEN proof: the same regression passed after central exact-group cleanup.
- Independent `CGO_ENABLED=0 go test ./internal/supervisor -run '^(TestForkSupervisorCleanupKillsOwnedGroup|TestSupervisorAdoptCommand|TestSupervisorResistantChildEscalation)$' -count=5 -timeout 120s` → PASS (4.257s).
- Independent reproduction of the original failure, `CGO_ENABLED=0 go test ./... -count=3 -timeout 300s`, → PASS (`lifecycle` 42.672s, `store` 1.246s, `supervisor` 19.003s).
- Tagged `mutation_cancel_parent_only` still behaviorally rejected with exit 1 and the expected resistant-child assertion; the exact executable scan immediately afterward and the corrected final `ps ... comm=` basename scan both found zero Ananke/test leaks.
- Python compilation, scoped gofmt, `git diff --check`, and generated-cache removal passed. Because the repair changed candidate sources, every executable report from the rejected attempt remains superseded pending a full rerun.

### 2026-07-20 — Repair D hard-review contract fixes

- Independent hard review of frozen candidate v4 returned `VERDICT: CHANGES REQUESTED`. Dynamic evidence proved a P1 terminal transcript-loss bug: a nonzero-exit worker wrote three canonical records, but the failed run became terminal with only one durable event because `finishOwnedWorker` skipped transcript handoff whenever a lifecycle failure existed.
- Repair D now requires transcript seal/drain after every successful exact worker reap, including failed and cancelled outcomes. Deterministic regressions block event persistence and prove no terminal state becomes visible until all three events are durable.
- The canonical fake worker and in-process test helper now emit `{type, payload}` records with `source_seq`, `text`, and timezone-bearing `timestamp`; API and harness tests reject null, missing, or mismatched payloads.
- Store open now rejects future, gapped, duplicate, and non-positive migration histories before applying any migration; a valid old contiguous history still migrates to head.
- Mutation proof now parses `go test -json` and counts detection only when the exact named test emits its own `run` and `fail` events. Six focused Python tests reject exit-only, unrelated-test, compile/setup, pass-plus-unrelated-failure, incomplete, and timeout false positives.
- Independent Repair D verification: Python 25/25 PASS; focused transcript/payload/migration tests PASS; no-CGO store/supervisor/lifecycle PASS (`1.146s`, `8.405s`, `16.863s` in the first package run); scoped gofmt and `git diff --check` PASS.

### 2026-07-20 — Repair E transient handoff recovery and final candidate v6

- The first post-Repair-D stress attempt exposed a harness-only macOS Unix-socket race: `api_request` called unnecessary `shutdown(SHUT_WR)` after sending a complete JSON request, and a fast peer close produced `Errno 57`. Deterministic RED reproduced the exception; removing the half-close made the focused test and full 9-test harness suite PASS. Stress then passed crash/restart 20/20 and cancellation 20/20 with no residual processes at 0s/1s/3s.
- A subsequent full race gate exposed a production liveness gap: transient `SQLITE_BUSY` during `SealTranscript` moved the run to `cleanup_required`, after which no authority retried sealing. Repair E adds a deterministic trigger-based RED test and makes the exact live supervisor retry transient seal/read operations with capped backoff while preserving permanent invariant failures and transcript-corruption fail-closed behavior.
- Repair E GREEN evidence: deterministic seal-retry test PASS; focused transcript/retry/corruption tests repeated 3× PASS (`6.917s`); focused race PASS (`4.552s`); no-CGO store/supervisor/lifecycle PASS (`0.855s`, `9.493s`, `17.170s`); `git diff --check` PASS.
- Final v6 gate suite against source candidate `d9ddf86cd31c154c85d5057296aa75a1ee052077f33f009e43171bd4c79b0294` (65 files): verification PASS including full race; named-test-attributed mutation proof 6/6; crash/restart stress 20/20; cancellation stress 20/20; black-box success and cancellation PASS; exact process scan PASS at 0s/1s/3s.
- Frozen review manifest: `artifacts/omp/slice-001-final-review/candidate-manifest-v6.json`, SHA256 `a03a5c0c57a276b9b19a5afc0ae7591574e79e52a62ec6b072fe060ccc158518`. All four report hashes were independently rebound to this manifest. Slice-001 remains `iterate` until an independent reviewer returns explicit ACCEPT for candidate v6.

### 2026-07-20 — Candidate v6 hard review rejected

- Independent hard review session `019f7d8a-d343-7000-a609-7c52ab68f753` verified the manifest, four report hashes, all 65 source-file hashes, and recomputed source candidate with zero mismatch, then returned `VERDICT: CHANGES REQUESTED`.
- `/tmp` overlay challenges proved P1: transcript corruption can publish `failed` with `{ConsumedOffset:105 FinalSize:-1}` because supervisor treats `cleanup_required` as permission to skip seal/drain.
- `/tmp` overlay challenges proved P1: dead-supervisor `recovery_unknown` fallback commits `failed` after process quiescence without checking or completing transcript handoff.
- `/tmp` overlay challenges proved P2: daemon supervisor-start failure leaves a transcript-required terminal failed run with `{ConsumedOffset:0 FinalSize:-1}` instead of an explicit empty seal.
- Candidate v6 and its green reports are reproducible historical evidence but are rejected for promotion. Repair F must enforce the transcript handoff invariant in the central terminal transaction, durably account malformed bytes without fabricating events, let the daemon seal/drain after proven quiescence, and atomically seal no-process transcripts as empty. New source changes require new reports and a newly frozen review candidate.

### 2026-07-20 — Repair F/G terminal transcript authority

- Repair F added a store-level invariant: `CommitTerminal` atomically rejects every transcript-required run whose transcript is unsealed or not fully accounted; `CommitNoProcessFailure` atomically records an explicit empty seal.
- Malformed complete records now persist `cleanup_required` before durably advancing the consumed byte offset without fabricating an event. Supervisor terminalization no longer skips transcript handoff merely because the run is `cleanup_required`.
- After exact supervisor death, worker absence, and empty-group proof, the daemon binds the named regular transcript file, seals its observed size, tails/drains all bytes, revalidates the same file identity/size, and only then publishes terminal state. Missing process transcripts and inode replacement at offset zero now fail closed.
- Repair F focused verification: store/supervisor/lifecycle RED→GREEN tests passed; race and no-CGO three-package runs passed; Python harness 26/26 passed; the named `mutation_terminal_while_alive` test emitted its own `run` and `fail` JSON events.
- Candidate v7 exposed a stable cancellation regression in ordinary/race/no-CGO verification: cancellation could kill the fakeworker before it opened `transcript.ndjson`, so a legitimate empty transcript appeared missing and the now-correct gate retained `cleanup_required`.
- Repair G moved transcript creation authority to the supervisor before `LaunchWorker`: required transcripts are exclusively created as regular mode-0600 files under a mode-0700 parent, and the production fakeworker opens the existing inode without `O_CREATE`. The deterministic pre-launch test and `TestAcceptedCancellationSurvivesCloseAndRetriesAfterRestart` both changed RED→GREEN. A package-test fakeworker remained visible at a 1-second scan but disappeared by 4 seconds; canonical process scans below were clean at 0/1/4 seconds.

### 2026-07-20 — Slice-001 candidate v8 frozen

- Final canonical run after Repair G:
  - `python3 scripts/verify.py` → PASS, including ordinary, race, and no-CGO suites.
  - `python3 scripts/mutation_proof.py` → PASS, `6/6` named mutations detected.
  - `python3 scripts/stress_lifecycle.py` → PASS, crash/restart `20/20`, cancellation `20/20`, no survivors.
  - `python3 scripts/blackbox_lifecycle.py` → PASS, success and cancellation scenarios.
- All four reports bind the same 66-file candidate `09d0935f9057a146c373f83800df5556636cff4ff729ba007c398b5112348ac2`.
- Exact process scans after the suite passed at 0, 1, and 4 seconds.
- Frozen review manifest: `artifacts/omp/slice-001-final-review/candidate-manifest-v8.json`.
- Independent hard review of v8 remains required before commit; this row records executable evidence, not acceptance.

### 2026-07-20 — Candidate v8 hard review rejected

- Independent hard review session `019f7ddc-7300-7000-9e5b-15b2a40015ef` reverified all four report hashes, all 66 current source-file hashes, the full source set, and aggregate `09d0935f9057a146c373f83800df5556636cff4ff729ba007c398b5112348ac2` with zero mismatch.
- The deterministic overlay `TestChallengeEmptyInodeReplacementCannotTerminalize` proved P1: replacing an empty transcript inode at offset zero still allowed terminal publication with `state=failed consumed=0 final=0 events=0`. Size/offset equality alone therefore does not prove transcript identity.
- `TestChallengeTerminalRunReleasesTranscriptTail` proved P3: a terminal run remained present in `Engine.tails` with an open file handle. The separate `/bin/false` probe was invalid on this macOS host and is not evidence.
- Provider policy interrupted the reviewer after the probes; exact-resume synthesis returned `VERDICT: CHANGES REQUESTED`. Candidate v8 must not be committed.
- Repair H must make the supervisor-created transcript file identity durable across process and daemon restarts, enforce it in every tail/handoff/terminal path, and release terminal tail handles. The GUI acceptance contract also requires missing/null event payloads to follow the existing malformed-record cleanup path without fabricated events.

### 2026-07-20 — Slice-001 Repair H (durable transcript file identity)

#### Evidence

- Independent hard-review challenge copied into `internal/lifecycle/repair_h_challenge_test.go`: replacing an empty transcript inode at durable offset zero previously published `failed` with `consumed=0`, `final=0`, and zero events; terminal tails also retained an open file descriptor.
- Schema v6 adds immutable `transcript_device` / `transcript_inode` authority. A process-backed required transcript cannot terminalize unless size handoff is complete and the durable file identity is valid; the explicit no-process path remains the only zero-identity exception.
- The supervisor now creates and retains the transcript anchor before worker launch, publishes the same identity to SQLite and `identity.json`, and includes it in socket authority responses. Daemon recovery, adoption, initial tail startup, quiescent handoff, and finalization responses require the same identity.
- Terminal/outbox resolution now releases transcript tails idempotently, including runs that disappear from `ListActiveRuns` before the next tick.
- Complete event envelopes now require a non-blank `type` and a present, non-null JSON `payload`; objects, arrays, strings, numbers, and booleans remain valid. Malformed complete records are durably accounted, create no event, and enter `cleanup_required`.
- Permanent tests cover live offset-zero replacement, daemon-restart offset-zero replacement, terminal tail release, prelaunch publication failure, migration defaults, identity-file and supervisor-response mismatch, missing/null envelope fields, and valid non-null payload kinds.
- `go test ./internal/store ./internal/supervisor ./internal/lifecycle -count=1 -timeout 300s`: PASS (`artifact://29`, 24.33 s after fixture convergence).
- `go test -race ./internal/store ./internal/supervisor ./internal/lifecycle -count=1 -timeout 300s`: PASS (`artifacts/gates/repair-h-focused/race.log`: store 3.835 s, supervisor 19.161 s, lifecycle 39.522 s).
- `CGO_ENABLED=0 go test ./internal/store ./internal/supervisor ./internal/lifecycle -count=1 -timeout 300s`: PASS (`artifacts/gates/repair-h-focused/no-cgo.log`: store 1.184 s, supervisor 7.737 s, lifecycle 19.737 s).
- `python3 -m unittest scripts.test_candidate_manifest scripts.test_harness_support scripts.test_stress_lifecycle scripts.test_mutation_proof`: PASS (26/26).
- Named `mutation_terminal_while_alive` proof: `TestEngineTranscriptCorruptionStaysNonterminalWhileAlive` itself ran and failed because terminal `failed` became visible while worker/supervisor were both alive.
- Bounded process scans after explicit mutation-process cleanup: 0/1/4 seconds PASS; all three evidence files are zero bytes.

#### Decision

- Repair H focused gates are accepted.
- Canonical candidate v9 is frozen by `artifacts/omp/slice-001-final-review/candidate-manifest-v9.json`: source aggregate `0fcf25ad39ccddf3b05fccc330fed620b03a852d9a97ec2c983d7b6cfa1931c6`, 68 files.
- `python3 scripts/verify.py`: PASS for ordinary, race, and `CGO_ENABLED=0` suites.
- `python3 scripts/mutation_proof.py`: PASS; all six mutation gates detected.
- `python3 scripts/stress_lifecycle.py`: PASS; crash/restart 20/20 and cancellation 20/20.
- `python3 scripts/blackbox_lifecycle.py`: PASS; success and cancellation scenarios.
- Canonical post-gate process scans at 0/1/4 seconds: PASS.
- The four bound report SHA-256 values are recorded in candidate-manifest-v9. A fresh 900-second independent hard review is running; v9 remains non-committable until that review returns ACCEPT.

### 2026-07-20 — Slice-001 Candidate v9 independent review rejection

#### Evidence

- Independent reviewer session `019f7e15-5330-7000-9a08-7123bdfce5db` recomputed all four report hashes and the current source candidate: aggregate `0fcf25ad39ccddf3b05fccc330fed620b03a852d9a97ec2c983d7b6cfa1931c6`, 68 files, zero mismatches, and exact candidate equality across all reports.
- P1 framing finding: `tailTranscript` can append a valid JSON envelope returned with `io.EOF` before a newline or durable final seal exists. A later append can turn the physical NDJSON line into malformed concatenated JSON after an event was already published.
- P1 process finding: Darwin `GroupMembers` returns numeric PIDs from a `ps` snapshot and `cleanupGroup` later signals them individually. The supervisor pins the PGID but not each descendant PID, so an exiting/reused PID can redirect TERM/KILL to an unrelated process.
- The reviewer's custom external framing probe was invalid because its temporary per-run Unix socket path exceeded the macOS limit; it is explicitly excluded from evidence. The daemon was stopped, PIDs `73820`, `73866`, and `73867` were absent, no matching process remained, and the temporary directory was removed.
- Residual P2/P3 findings: ignored `rand.Read` error, silent terminal kqueue observation error, identity rename without file/directory `fsync`, and daemon token printed to stderr.

#### Decision

- `VERDICT: CHANGES REQUESTED`; candidate v9 must not be committed.
- Repair I must gate every non-newline EOF record on a durable final seal, and must replace snapshot-then-signal-by-PID cleanup with an architecture that preserves stable signalling authority. The selected topology keeps the supervisor outside a worker-led process group, pins the worker group by deferring leader reap, and signals the group atomically before reaping.

### 2026-07-20 — Slice-001 Repair I: durable framing and stable group authority

#### Evidence

- Session `019f7e28-37ec-7000-8ec3-8e520f0e35f5` reproduced the framing failure before implementation: a valid non-newline EOF record advanced `ConsumedOffset` and appended one event while `FinalSize == -1`.
- The permanent framing contract now retains the prior durable offset until a newline exists or the sealed final size proves the exact final record boundary. Sealed final records without a newline remain accepted.
- The old backend reproduced worker PID/PGID inheritance from the supervisor. The replacement starts a paused trampoline as `PID == owned PGID`; the supervisor remains outside the group, publishes identity/SQLite/socket/running authority, and only then releases the trampoline to `exec` the real worker in place.
- Cleanup uses only atomic negative-PGID TERM/KILL while the unreaped worker leader pins group identity. Production `SignalProcess` and `BecomeGroupLeader` paths were removed.
- A positive paused PID with a publication failure can now persist the one-way nonterminal `created -> cleanup_required` obligation. Direct `created -> terminal` transitions remain forbidden outside the existing no-process atomic exception.
- Post-start watcher, identity, SQLite, socket, transition, release and monitor failures are covered as cleanup-before-reap cases. The resistant-child integration test proves worker and descendants share the owned worker PGID and are removed by cancellation.
- Reviewer P2 items were also closed: entropy failures abort daemon/run token creation, terminal kqueue errors wake fail-closed cleanup, identity temp and parent directory are synced, and daemon stderr no longer prints credentials.
- Expanded UI reference evidence is recorded separately in `docs/ui-reference-audit.md`; no external UI source was copied.

#### Decision

- Repair I source and focused contracts are complete. Canonical candidate v11 is frozen by `artifacts/omp/slice-001-final-review/candidate-manifest-v11.json`: aggregate `93aef9afc7771cb79c35d3c7df0fa6bca6f50e8071619d0fa36473198b82dd7f`, 68 files. Verify, mutation (6/6), stress, blackbox, Python 26/26, gofmt/diff, and 0/1/4 process scans all passed. Independent hard review session `019f8099-15bf-7000-a2ac-5014079acaa2` returned `VERDICT: ACCEPT`; report `artifacts/omp/slice-001-final-review/hard-review-v11-output.md`. Residual risk is the documented ADR-0002 fail-closed `recovery_unknown` path after supervisor crash with resistant descendants; it is not a commit blocker.

### 2026-07-21 — GUI v0.1: Tauri shell over real Go lifecycle authority

#### Evidence

- Implemented `gui/` as Tauri 2 + Vanilla TypeScript/Vite. Renderer calls Rust `invoke` commands only; the daemon credential is created and retained only by Rust in restricted app-data storage.
- Added authenticated Go `list-runs` API with project/workstream filtering and focused coverage (`TestEngineListRunsByProject`).
- Rust integration tests build real Go sidecars, start the daemon, bootstrap the durable project/workstream, launch a real fakeworker event stream, and verify durable cancellation (`cargo test`: 3 passed).
- Production app build passed as `Ananke.app`; its bundle contains `ananke`, `ananke-supervisor`, and `ananke-fakeworker`. Runtime launch proved the bundle executable starts the bundle-contained daemon and creates its local socket.
- Frontend `typecheck` and Vite production build passed. Native screenshot capture was unavailable because this session has no usable display (`screencapture: could not create image from display`), so no visual-screenshot claim is made.
- Tauri's default DMG step failed only at macOS 27 `hdiutil` compatibility (`bundle_dmg.sh` uses the older `hdiutil create -srcfolder` invocation). Current proof build is explicitly scoped to the successfully generated macOS `.app` bundle.

#### Decision

- GUI v0.1 source is ready for independent review as a macOS `.app` proof, pending candidate freeze and review verdict. The window-close/daemon-persistence behavior is structurally designed (Rust does not kill the daemon) and covered by the bridge integration path; it was not separately observed by killing the Hermes-managed GUI process because that tool terminates the entire process group and is not a valid window-close simulation.

#### Final review and decision

- First hard review returned `CHANGES REQUESTED`: a predictable shared `/tmp` socket could disclose the daemon credential, bootstrap masked storage failures, the E2E bypassed the Rust bridge, release embedded the builder checkout, and `created`/`recovery_unknown` were not cancellable. All were repaired under TDD.
- Final B1 re-review (`artifacts/omp/gui-v0.1/b1-final-review-output.md`) returned `VERDICT: ACCEPT`; it verified the private `0700` runtime socket directory, auth/protocol-aware stale classification, safe Go socket removal, internal-only error detail, public Backend E2E, release-safe root, and nonterminal cancellation state.
- Final verified evidence: Rust `cargo test` 9 passed; frontend state test/typecheck/Vite production build passed; Go store/supervisor/lifecycle packages passed; `CI=true npm run tauri:build` produced `Ananke.app` with all Go sidecars.
- GUI v0.1 is accepted as a first macOS `.app` lifecycle proof. DMG generation remains deliberately outside this acceptance because the host's macOS 27 `hdiutil` command rejects Tauri's current create-dmg invocation; this is a packaging compatibility follow-up, not a runtime authority workaround.

### 2026-07-21 — P0b renderer-public bootstrap code generation

#### Evidence

- Canonical schema: `gui/contracts/renderer-public-bootstrap.schema.json` (33 LOC). Deterministic Node generator: `gui/scripts/generate-renderer-public.mjs` (151 LOC). It requires Node 22, runs the GUI-local pinned Quicktype executable with telemetry disabled, and supports generation, byte-for-byte content drift checking, and generated-public privacy checking.
- Toolchain observed: Node `v22.22.3`, Quicktype `26.0.0`, Cargo `1.97.1`. Quicktype produced three Rust/TypeScript generated artifacts totaling 62 LOC: `renderer_public_bootstrap.rs` (40), `mod.rs` (3), and `renderer-public-bootstrap.ts` (19).
- TDD RED: after the required `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm run build:go` sidecar prerequisite, `cargo test bootstrap_public_wire_json_is_frozen --lib` exited 101 with `E0433: cannot find module or crate generated`. The test was added before generated integration.
- TDD GREEN: the same focused command passed after integration (`1 passed`, `9 filtered`, `0.40s`). It serializes the real bridge bootstrap result and compares the full public project/workstream JSON object, including `project.root` and `workstream.project_id`.
- Content-drift proof: a temporary comment injected into generated Rust made `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm run check:renderer-public` exit 1 with `Generated renderer-public models drifted`; regeneration restored the artifact, then the check passed. `npm run check:renderer-public-privacy` passed and reported no generated `token` or `error` fields.
- Final checks passed: focused Rust test; `cargo fmt -- --check`; `npm --prefix gui run check:renderer-public`; `npm --prefix gui run check:renderer-public-privacy`; `npm --prefix gui run typecheck`; and `npm --prefix gui run web:build` (Vite `7.3.6`, 7 modules, 54 ms).
- `git diff --check` passed. Production-path validation listed only the approved P0b GUI paths: package manifest/lockfile, canonical schema, generator, generated Rust/TypeScript sources, `src-tauri/src/lib.rs`, and `src/main.ts`.
- Independent hard review returned **ACCEPT** after re-running the focused bridge
  test, generator/check/privacy commands, TypeScript typecheck/web build, and
  scope audit. It verified no daemon transport, private/internal DTO, or
  `JsonRun -> RunDto` behavior changed.

#### Limitations

- This proof changes only the renderer-public Tauri bootstrap model and its TypeScript consumer. Go transport, credential-bearing/internal daemon types, `JsonRun -> RunDto`, run/event/cancel/health behavior, and all other semantic adapters remain unchanged.

#### Decision

- P0b bootstrap generation and integration are independently accepted in this
  worktree. No commit or push was performed.

### 2026-07-21 — P0b.1 renderer-public Run code generation

#### Evidence

- TDD RED: `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH cargo test list_runs_public_wire_json_is_frozen --lib` exited 101 before Run integration with `E0433: cannot find renderer_public_run in generated` at the new public list-runs wire test.
- The canonical `gui/contracts/renderer-public-run.schema.json` generated `Run`/`RunDiagnostics` Rust and TypeScript models. The handwritten `JsonRun -> Run` adapter preserves the nested diagnostics projection only for `list_runs`; launch and get-run retain `RunDto`.
- TDD GREEN: `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run build:go && cargo test list_runs_public_wire_json_is_frozen --lib --manifest-path gui/src-tauri/Cargo.toml` passed (`1 passed`, `10 filtered`, `0.45s`). A post-format rerun of the focused Cargo command also passed (`1 passed`, `10 filtered`, `0.65s`). The test launches a real fixture through the bridge, lists it, and asserts exact nested diagnostics JSON including both PIDs and `committed_offset`.
- Content-drift proof: after inserting `// Controlled drift proof.` in generated `renderer_public_run.rs`, `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run check:renderer-public` exited 1 and named that Run file. Regeneration restored it; the generator content check then passed. The all-target public-field privacy check passed and reported no generated `token` or `error` fields.
- Final validation passed: `cargo fmt --manifest-path gui/src-tauri/Cargo.toml`; renderer-public content and privacy checks; `npm --prefix gui run typecheck`; and `npm --prefix gui run web:build` (Vite `7.3.6`, 7 modules, 70 ms).
- Scope evidence: `git diff --check` exited 0. `git status --short` listed only the accepted P0b/P0b.1 GUI paths plus `docs/` and `artifacts/omp/p0b*` paths; no path outside the allowed GUI/docs/artifact scope appeared.
- Independent hard review returned **ACCEPT**. It independently verified the
  live bridge list-runs fixture path, exact nested diagnostics, all generated
  target drift/privacy coverage, and zero daemon/internal/non-list-runs scope
  changes.

#### Limitations

- No Go daemon transport, Go structs, bootstrap behavior, or non-`list_runs` Tauri command was changed. No commit or push was performed.

### 2026-07-21 — P0b.2 renderer-public Event code generation

#### Evidence

- TDD RED: after `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run build:go`, `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH cargo test list_events_public_wire_json_preserves_arbitrary_payloads --lib --manifest-path gui/src-tauri/Cargo.toml` exited `101` before Event integration with `E0433: could not find renderer_public_event in generated`.
- `gui/contracts/renderer-public-event.schema.json` generated public Rust `Event` and TypeScript `Event` models. The generator now covers the Event Rust/TypeScript targets and generated Rust module in its content-drift inventory; its public-field privacy scan covers Event with every other renderer-public target.
- The real bridge test creates an executable NDJSON fixture, launches it through the real daemon bridge, calls `list_events`, and compares the exact serialized public result. It proves `seq`, wire key `type`, and object, array, string, number (`42.5`), and boolean payloads.
- TDD GREEN: the same focused bridge command passed after integration (`1 passed`, `11 filtered`, `1.44s`). After `cargo fmt --manifest-path gui/src-tauri/Cargo.toml`, the focused test passed again (`1 passed`, `11 filtered`, `1.43s`).
- Content-drift proof: inserting `// Controlled Event drift proof.` into generated `renderer_public_event.rs` made `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run check:renderer-public` exit `1` and name that Event artifact. Regeneration restored it; the content check then passed. `npm --prefix gui run check:renderer-public-privacy` passed and reported no generated `token` or `error` fields.
- Final frontend checks passed with Node `v22.22.3`: `npm --prefix gui run typecheck`; `npm --prefix gui run web:build` (Vite `7.3.6`, 7 modules, 52 ms).
- `git diff --check` passed with no output. The combined P0b/P0b.1/P0b.2 whitelist passed for all 32 changed worktree paths; no path fell outside the exact approved set.

#### Formatter-stability repair

- Correction: the earlier P0b.2 evidence established generator and formatter checks separately, but did not establish compatibility after a formatting write. `rustModuleSource` expected `bootstrap`/`run`/`event`, while rustfmt canonicalizes declarations as `bootstrap`/`event`/`run`; that made a formatted generated `mod.rs` fail content-drift checking.
- The generator now emits rustfmt's canonical module order, and `npm --prefix gui run generate:renderer-public` regenerated `gui/src-tauri/src/generated/mod.rs`.
- Order-independence verification passed in both required sequences: `npm --prefix gui run check:renderer-public && cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check`, then `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check && npm --prefix gui run check:renderer-public`. Each content check reported `Renderer-public generated models match the canonical schema`; both formatter checks exited 0 with no output.
- Rerun evidence: after `npm --prefix gui run build:go`, `cargo test list_events_public_wire_json_preserves_arbitrary_payloads --lib --manifest-path gui/src-tauri/Cargo.toml` passed (`1 passed`, `11 filtered`, `1.42s`). The all-target privacy scan passed; `npm --prefix gui run typecheck` and `npm --prefix gui run web:build` passed (Vite `7.3.6`, seven modules, `56ms`).
- Final diff/scope guard: `git diff --check` exited 0 with no output. `git status --short --untracked-files=all` reported five modified and 29 untracked paths, all within the combined P0b GUI renderer-public, `docs/`, or `artifacts/omp/p0b*` scope. No commit or push was performed.

#### Limitations

- The fixture verifies the contract's five non-null payload kinds only. The lifecycle transport rejects null payloads upstream; no null public-event fixture is claimed.
- Numeric fidelity is proven only for the frozen `42.5` fixture; arbitrary-precision numeric fidelity remains outside this P0b.2 proof.
- No Go daemon event wire struct, daemon transport, token behavior, Run/bootstrap/cancel/health/launch/get command, or non-list-events adapter was changed. No commit or push was performed.

### 2026-07-21 — P0b.2 non-null Event payload repair

#### Evidence

- Correction: the prior canonical Event schema gave `payload` only a description, so it admitted `null`; Quicktype consequently generated Rust `Option<serde_json::Value>` and TypeScript `unknown`.
- TDD RED: `cargo test generated_event_requires_present_non_null_payload --lib --manifest-path gui/src-tauri/Cargo.toml` exited `101` before the schema repair. The new generated-wire regression failed because `{"seq":1,"type":"missing-payload"}` deserialized through the nullable Event model.
- Canonical `payload.type` is now exactly `["object", "array", "string", "number", "boolean"]`, with `payload` still required. Regeneration produced Rust `Event { payload: Payload, ... }`, not `Option<_>`, and the TypeScript union `unknown[] | boolean | number | { [key: string]: unknown } | string`; the TypeScript field is required and its top-level union excludes `null` and `undefined`.
- TDD GREEN: the same generated-wire regression passed (`1 passed`, `12 filtered`). It directly deserializes and exactly reserializes object, array, string, number (`42.5`), and boolean Event payloads. It observes deserialization failure for both missing and explicit `null` payloads, so neither malformed wire form can produce an Event to serialize.
- Real bridge proof: after `npm --prefix gui run build:go`, `cargo test list_events_public_wire_json_preserves_arbitrary_payloads --lib --manifest-path gui/src-tauri/Cargo.toml` passed (`1 passed`, `12 filtered`, `1.43s`). The executable NDJSON fixture still proves exact serialized `seq`, wire `type`, and all five accepted payload kinds through `list_events`.
- Formatter/generator order-independence passed after a formatting write in both sequences: `check:renderer-public` then `cargo fmt -- --check`, and `cargo fmt -- --check` then `check:renderer-public`. Each content check reported canonical-schema agreement; each formatter check exited `0`.
- Controlled drift proof: injecting `// Controlled Event drift proof.` into generated `renderer_public_event.rs` made `npm --prefix gui run check:renderer-public` exit `1` and name that artifact. Regeneration restored it. Final `check:renderer-public`, `check:renderer-public-privacy`, focused regression, focused bridge fixture, `typecheck`, and `web:build` all passed; Vite `7.3.6` built seven modules in `68ms`.

#### Exact generator limitations

- Quicktype represents the required top-level union in Rust as untagged `Payload`, not as `serde_json::Value`. Its array and object variants contain `Option<serde_json::Value>` only for nested values, where JSON `null` remains permitted; `Event.payload` itself is non-optional and has no null variant.
- Quicktype maps a top-level JSON number to `Payload::Double(f64)`. This repair proves the frozen `42.5` value only; arbitrary-precision numeric fidelity remains unproven.
- The generated TypeScript union enforces the top-level distinction statically. Its array elements and object properties are `unknown`, intentionally leaving nested arbitrary JSON unconstrained; it supplies no runtime JSON Schema validation.

#### Terminal verdict

- **PASS:** the canonical P0b.2 Event payload contract now requires a present, non-null top-level value of exactly one of the five permitted JSON kinds. No handwritten TypeScript alias, schema weakening, commit, or push was used.
- Focused independent hard re-review returned **ACCEPT**. It verified non-null
  schema/model behavior, missing/null rejection, all five live bridge payload
  kinds, formatter/generator order stability, and list-events-only scope.

### 2026-07-22 — P0b.3 generated Run command reuse

#### Evidence

- TDD RED: `cargo test public_run_wire_json_is_frozen --lib --manifest-path gui/src-tauri/Cargo.toml` exited `101` before integration. Both new real-bridge tests failed with `E0308`, requiring generated `Run` while `Backend::launch_fixture` and `Backend::get_run` still returned handwritten `RunDto`.
- Added `launch_fixture_public_run_wire_json_is_frozen` and `get_run_public_run_wire_json_is_frozen`. Each bootstraps the real test backend and exercises the fixture launch/get path; each compares its serialized result to the complete public object, including the nested `diagnostics` project/workstream IDs, both PIDs, and `committed_offset`.
- The two launch/get backend results and their Tauri command results now return generated `Run`, using the existing explicit `JsonRun -> Run` adapter. `main.ts` already imported the generated `Run`; no renderer type edit was needed.
- TDD GREEN: after `PATH=/Users/yingliangzhang/.hermes/node/bin:$PATH npm --prefix gui run build:go`, `cargo test public_run_wire_json_is_frozen --lib --manifest-path gui/src-tauri/Cargo.toml` passed (`2 passed`, `13 filtered`, `1.57s`). After `cargo fmt --manifest-path gui/src-tauri/Cargo.toml`, the focused tests passed again (`2 passed`, `13 filtered`, `0.40s`).
- Generator validation used Node `v22.22.3`: `npm --prefix gui run check:renderer-public` reported canonical-schema agreement, and `npm --prefix gui run check:renderer-public-privacy` reported no generated `token` or `error` fields.
- `npm --prefix gui run typecheck` and `npm --prefix gui run web:build` passed. Vite `7.3.6` built seven modules in `51ms`.
- `git diff --check` passed. The combined accepted P0b/P0b.1/P0b.2/P0b.3 scope guard accepted all `41` changed worktree paths and found none outside the P0b documentation/artifacts or approved GUI renderer-public paths.
- Focused independent hard review returned **ACCEPT**: both daemon-backed tests
  compile-require generated Run, assert complete nested wire JSON, and scope is
  limited to launch/get public return conversions.

#### Terminal verdict

- **PASS:** only launch/get public results now reuse generated `Run`; no schema/generator, daemon transport, events/list-runs/bootstrap/cancel/health behavior, or private wire type was changed. No commit or push was performed.

### 2026-07-22 — P0b.4 generated Cancel and Health migration

#### Evidence

- TDD RED: before the new generated modules existed, `cargo test daemon_health_public_health_wire_json_is_frozen --lib --manifest-path gui/src-tauri/Cargo.toml` exited `101`: `renderer_public_health` and `renderer_public_cancel` were absent and `Backend::daemon_health` did not exist. After generation but before the return-path integration, the same focused compile exited `101` with `CancelDto` where generated `Cancel` was required and with no `Backend::daemon_health`.
- Added real daemon-backed `daemon_health_public_health_wire_json_is_frozen` and `cancel_run_public_cancel_wire_json_is_frozen` tests. They compile-require generated `Health` and `Cancel`, serialize exact public JSON, and exercise daemon startup plus fixture cancellation. The cancellation test waits for `running` before issuing cancellation, then proves `{ "accepted": true, "state": "cancelling" }` and eventual `cancelled`; the initial fixture race returned the valid `created` state, so an immediate cancellation assertion was intentionally rejected as nondeterministic.
- TDD GREEN: after `npm --prefix gui run build:go`, both focused tests passed individually. The final runs passed with `1 passed`, `16 filtered` each (health `0.45s`; cancellation `3.25s`).
- Canonical Cancel and Health schemas generated Rust and TypeScript artifacts. `npm --prefix gui run check:renderer-public` passed before and after formatting in both generator/formatter orders. A controlled mutation to generated Cancel Rust and Health TypeScript artifacts made content checking exit `1` and list both exact drifted targets; regeneration restored canonical output.
- Node `v22.22.3` generated the artifacts. The all-target public-field privacy scan passed after restoration: generated public models expose no `token` or `error` fields. Final `cargo fmt -- --check`, `npm --prefix gui run typecheck`, and `npm --prefix gui run web:build` passed; Vite `7.3.6` transformed seven modules and completed in `59ms`.
- `git diff --check` passed. The final scope scan found five modified tracked paths—this ledger, the renderer-public generator and Rust module export, `lib.rs`, and `main.ts`—plus 29 untracked paths. The six P0b.4 code-generation artifacts are confined to `gui/contracts/`, `gui/src/generated/`, and `gui/src-tauri/src/generated/`; all remaining untracked paths are the accepted P0b–P0b.4 OMP artifacts or the P0b.4 contract. No commit or push was performed.

#### Terminal verdict

- **PASS:** only `daemon_health` and `cancel_run` public Rust return paths and their renderer invocation types now use generated Health and Cancel models. Their public JSON remains `online`, `accepted`, and `state`; daemon transport/private/internal types and all other commands were not changed.

### 2026-07-22 — P0b full acceptance repair

#### Evidence

- Generated TypeScript now includes Quicktype runtime `Convert` decoders rather than `--just-types` interfaces. TDD RED: `node gui/scripts/test-renderer-public.mjs` initially exited `1` because `Convert.toBootstrap` was undefined. TDD GREEN: `npm --prefix gui run test:renderer-public` decoded the shared `gui/contracts/fixtures/renderer-public-golden.json` bootstrap, Run, five Event payload kinds, Cancel, and Health values, and rejected malformed values for every model.
- The same fixture is decoded and reserialized through all generated Rust models by `generated_public_models_decode_golden_json`; the focused command passed (`1 passed`, `17 filtered`). The final bridge suite passed `18` tests.
- The renderer now sends every public command result through the generated runtime decoder. `launch_fixture` and `get_run` both decode as generated `Run`; list responses validate every generated Run/Event entry before state use.
- Removed unused `RunDto` and `RunDiagnosticsDto`. `EventDto` is now private and `Deserialize`-only, retaining only the raw daemon-response adapter before generated Event conversion.
- The generator enforces recursive canonical-schema field denylisting. Its regression mutates a public schema field and proves rejection for `token`, `error`, `worker_env`, `socket_path`, `identity_file`, and `adapter_secret`. TDD RED: the original scan accepted injected `token`; TDD GREEN: `npm --prefix gui run test:renderer-public-privacy` passed. The final privacy check reported `Renderer-public schemas expose no prohibited private fields.`
- Added `.github/workflows/p0b-renderer-public.yml`. It installs pinned GUI dependencies and sidecars, regenerates checked-in models then rejects drift, and runs privacy enforcement/regression, TypeScript decoder regression, Rust format/test, renderer typecheck/state test, and web build. The workflow was added locally; no GitHub execution is claimed.
- Final local gates passed: `npm --prefix gui run build:go`; `check:renderer-public`; `check:renderer-public-privacy`; `test:renderer-public`; `test:renderer-public-privacy`; `test:state`; `typecheck`; `cargo fmt --manifest-path gui/src-tauri/Cargo.toml --check`; `cargo test --manifest-path gui/src-tauri/Cargo.toml --lib` (`18` passed); and `web:build` (Vite `7.3.6`, `12` modules, `82ms`).
- `git diff --quiet 73fc6de -- internal cmd` exited `0`, preserving the Go daemon transport and source scope. No commit or push operation was run.

#### GUI E2E host blocker

- No pre-existing Tauri WebDriver tooling was installed. `cargo install tauri-driver --locked` installed `tauri-driver v2.0.6`, but `tauri-driver --help` immediately exited `1` with `tauri-driver is not supported on this platform` on this Darwin arm64 host. `/System/Cryptexes/App/usr/bin/safaridriver` exists, but it is not a Tauri app WebDriver bridge. Therefore bootstrap → launch → events → cancel → reconnect was not GUI-E2E-exercised here; this is a concrete host blocker, not a passing E2E claim.

#### Terminal verdict

- All repairable P0b local acceptance gates pass. GUI-level Tauri E2E remains host-blocked as recorded above; no daemon Go transport or P1/P2/P3 scope was changed.

### 2026-07-22 — Mac2 cancellable fixture determinism repair

#### Evidence

- The Mac2 failure evidence showed the selector, daemon health, and launch paths working, then the selected run already `completed` before the harness observed `running`; the harness correctly did not cancel.
- TDD RED: `fixture_worker_env_scopes_cancellable_lifetime_to_debug_builds` initially failed to compile because `fixture_worker_env` did not exist. TDD GREEN: it passed after the bridge selected worker configuration by build mode.
- Debug builds now retain the fixture's six canonical events and use the existing fakeworker `ANANKE_FW_EXIT_DELAY_MS=30000` pre-exit fixture hold. The first attempted zero-event configuration broke `bridge_bootstrap_launches_lists_events_cancels_and_reconnects`; retaining the event stream repaired that regression. Release configuration remains the prior six-event, 250 ms cadence, 750 ms pre-exit fixture.
- Final local checks passed: `cargo test --lib --manifest-path gui/src-tauri/Cargo.toml` (`19` passed), `cargo fmt --check --manifest-path gui/src-tauri/Cargo.toml`, `cargo check --release --lib --manifest-path gui/src-tauri/Cargo.toml`, and `npm --prefix tests/mac2 test` (`5` passed).
- Built the debug `Ananke.app`, then used caller-provided WDA to verify static accessibility identifiers, refresh health (`● daemon online`), launch, and selected state (`● running`). Evidence: `/var/folders/fh/7dlfvrsn5938lw_4z6_pg_th0000gn/T/ananke-mac2-running-proof-final.YppDc2/result.json` and `running.png`. The proof closed only its WDA session and did not issue cancellation.

#### Terminal verdict

- **PASS:** debug/test fixture control makes the cancellable Mac2 observation deterministic without weakening terminal-state handling or changing release behavior. No commit or push was performed.

### 2026-07-22 — P1a Proposal / Revision / Approval contract fixture slice

#### Evidence

- **RED evidence:** the pre-repair five-fixture verifier accepted a rehashed
  `acceptance.create_replay.given.repository_root` field and an unpaired
  Unicode surrogate after a consistent rehash. Its create `body_hash` equaled
  the Revision snapshot hash, it had no lifecycle fixture, and its six-case
  acceptance inventory omitted append, rejected-withdrawal, restart, and
  concurrency vectors. The first strict verifier run then rejected that old
  manifest because it lacked the required request-envelope and lifecycle
  fixtures.
- **GREEN contract:** immutable `Revision` snapshots now pair one-to-one with
  mutable composite-key `(proposal_id, revision)` `RevisionLifecycle` records;
  atomic append/decision/withdrawal semantics include rejected-current
  withdrawal. Canonical create, append, decision, and withdrawal envelopes
  specify exact scope tuples, body hashes, and durable lookup before mutable
  checks.
- Added seven canonical fixtures and the updated versioned SHA-256 manifest
  under `contracts/p1a/fixtures/`. The approved golden Revision hash remains
  `sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263`.
- `node --check contracts/p1a/verify.mjs` completed successfully, and
  `node contracts/p1a/verify.mjs` printed
  `P1a proposal contract fixtures verified.` The verifier now checks
  unpaired-surrogate rejection in keys and values, exact request body hashes
  and scope ordering, closed schemas for every fixture object, full privacy
  denylist coverage, lifecycle links, and all 13 acceptance vectors.
- `node contracts/p1a/verify.mjs --self-test` printed
  `P1a fixture verifier self-test rejected drift, private fields, unpaired Unicode surrogates, request-hash conflation, and missing vectors.`
- Targeted, consistently rehashed copies were rejected for lifecycle/Approval
  state divergence, reordered operation scope, request-hash conflation,
  unpaired-surrogate key, `repository_root`, and a missing same-key concurrent
  replay vector. No commit or push operation was run.

#### Status

- **FROZEN CONTRACT / FIXTURE ONLY:** Proposal persistence, SQLite migrations,
  GUI/IPC, claims, workers, adapters, budget enforcement, OMP audit execution,
  model execution, commits, and pushes remain outside P1a.

### 2026-07-22 — P1a focused rereview repair

#### Evidence

- **RED probes executed by the self-test:** each probe used a temporary fixture
  copy and rewrote its manifest before expecting verifier rejection. The new
  probes consistently rehashed every Revision-hash link, affected append and
  decision body hash, acceptance reference, and manifest before rejecting
  `2026-99-99T99:99:99Z`, non-leap `2026-02-29T12:00:00Z`, and
  `2026-07-22T24:00:00Z`. It also rejected a consistently rehashed create
  `project_id` target and approved-decision `revision_hash` tamper.
- **GREEN:** `node --check contracts/p1a/verify.mjs && node contracts/p1a/verify.mjs && node contracts/p1a/verify.mjs --self-test` exited `0` in `0.53s`.
  The canonical verifier printed `P1a proposal contract fixtures verified.`
  The self-test printed `P1a fixture verifier self-test rejected drift, private fields, unpaired Unicode surrogates, request-hash conflation, rehashed timestamp and envelope-identity tampering, and missing vectors.`
- The acceptance fixture now has 15 frozen cases, including append-first and
  reject-first append-vs-reject linearizations. The rejection-first vector
  requires two commits, an open Proposal, a retained rejected predecessor, a
  new pending current pair, and `partial_writes: 0`; the append-first vector
  requires one append commit, `approval_conflict` for rejection, and no partial
  writes. The approved-decision race remains one-commit.
- Canonical envelope verification now links create targets/input to Proposal and
  Revision; append/decision targets and revision hashes to Proposal, Revision,
  RevisionLifecycle, and Approval; the approved decision's idempotency
  identity/decision/reason to Approval; and withdrawal to Proposal. The
  canonical acceptance digest is
  `d87ef3d21b169ca9b715061c01378d02a84daa23a7f861421bf314a74a7ca940`.

#### Terminal verdict

- **PASS:** the P1a contract/fixture-only verifier closes the focused rereview
  findings. No storage, GUI/IPC, claims, workers, adapters, runtime code,
  commit, or push was changed.

### 2026-07-22 — P1b durable Task Proposal store

#### Evidence

- **TDD RED:** `TestP1ACanonicalHashesMatchFixtures` first failed to build because `canonicalJSONHash` did not exist. Create, append, decision, and withdrawal focused tests then each first failed to build on their absent store API. `TestCreateProposalAcceptsCanonicalUTF8ControlText` then failed because valid UTF-8 control text was rejected; validation was narrowed to the frozen P1a 1–N UTF-8 byte limits.
- **GREEN:** migration v7 now stores immutable canonical revision snapshots and hashes, Proposal current pointers, one-to-one lifecycle/Approval pairs, append-only activity, and durable idempotency response identities. `CreateProposal`, `AppendProposalRevision`, `DecideProposalApproval`, and `WithdrawProposal` look up the operation-scope idempotency tuple before mutable state reads. Conflict paths roll back without durable mutation; rejected predecessors remain rejected on append and withdrawal.
- Fixture-derived tests prove the frozen Revision hash and all four request body hashes. They cover v6-to-head migration, restart replay after later state changes, same-key conflict, stale-base and competing-decision no-partial-write outcomes, pending append supersession, rejected-predecessor append, rejected withdrawal, and barrier-synchronized append/decision/replay races.
- The first full store run exposed a hard-coded v6 head assertion in `TestSchemaVersionMigrationFromV5DefaultsTranscriptIdentityUnknown`; it now asserts the migration-list head. The final commands observed after that repair were: `go test ./internal/store -count=1` and `go test -race ./internal/store -count=1`, both exited `0`. `gofmt` completed on every touched Go file with no remaining diff. `node contracts/p1a/verify.mjs` printed `P1a proposal contract fixtures verified.` and its self-test passed.

#### Scope

- P1b changes only SQLite storage and `internal/store` tests. No GUI/IPC, Grill or policy evaluation, claims, workers, adapters, process launch, commit, or push was added or run.

#### Terminal verdict

- **PASS:** the durable P1b Task Proposal store meets the frozen P1a persistence boundary under focused, full-package, and race verification. Independent review should inspect canonical-JCS edge cases beyond the frozen fixture corpus.

### 2026-07-22 — P1b independent-review blocker repair

#### TDD RED

- `go test ./internal/store -run 'TestCrossStoreProposalMutationsPreserveP1ASemantics|TestProposalIdentityForeignKeysRejectCrossLinks' -count=1` initially exited `1`. Two independent `Open` handles returned `database is locked (5) (SQLITE_BUSY)` for same-key create, same-base append, competing decisions, and append-versus-rejection. The raw-SQL adversarial updates/inserts also succeeded, proving v7 did not bind lifecycle hashes, approvals, proposal pointers, activity, or idempotency response identities to their own revision tuple.
- `TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled` initially exited `1`: `GetRevisionLifecycle`, `GetApproval`, `GetProposal`, `ListProposalActivity`, and durable create replay accepted deliberately cross-linked rows.

#### GREEN

- SQLite connections now use `_txlock=immediate`, so the mutation transactions serialize across independent handles. Real two-`Open`-handle barrier tests return only P1a outcomes: same-key/same-body create returns the winner identity twice; same-base append returns one success plus `revision_conflict`; competing decision returns one success plus `approval_conflict`; and append-versus-rejection reaches one of its two frozen allowed linearizations. Exact journal row-count assertions prove zero partial records.
- Fresh v7 schema creation and the v8 in-place rebuild bind Proposal current pointers to `(proposal_id, revision, revision_hash)` and bind approvals, lifecycles, activity, and idempotency response identities to their complete revision/lifecycle tuple with composite deferred foreign keys. `TestProposalV7DataUpgradesToCompositeIdentityForeignKeys` preserves an existing proposal across the v7-to-v8 rebuild, rejects a cross-linked pointer, passes `PRAGMA foreign_key_check`, and proves the three reviewed redundant v7 indexes are absent.
- Adversarial raw-SQL tests now reject mismatched lifecycle hashes, approval hashes, lifecycle approvals, proposal pointers, activity identities, and idempotency identities. Read and replay paths additionally validate current revision, lifecycle, approval, activity, and idempotency identities, returning `ErrProposalRecordCorrupt` for rows written while foreign keys were disabled.

#### Verification

- Focused regression command passed: `go test ./internal/store -run 'TestCrossStoreProposalMutationsPreserveP1ASemantics|TestProposalIdentityForeignKeysRejectCrossLinks|TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled|TestProposalV7DataUpgradesToCompositeIdentityForeignKeys' -count=1 -timeout=90s`.
- Full store verification passed: `go test ./internal/store -count=1 -timeout=180s`.
- Race verification passed: `go test -race ./internal/store -count=1 -timeout=180s`.
- `node contracts/p1a/verify.mjs` printed `P1a proposal contract fixtures verified.` Its self-test also passed and reported rejection of fixture drift, private fields, Unicode surrogates, request-hash conflation, rehashed timestamp/envelope tampering, and missing vectors.

#### Terminal verdict

- **PASS:** the P1b review blockers are repaired in `internal/store`; no GUI, IPC, P2, P3, commit, or push scope was used.

### 2026-07-22 — P1b focused re-review repair

#### TDD RED

- `go test ./internal/store -run 'TestProposalV7DataUpgradesToCompositeIdentityForeignKeys|TestProposalIdentityForeignKeysRejectCrossLinks|TestProposalReadsRejectCrossLinkedRowsWhenForeignKeysWereDisabled' -count=1` exited `1` before the repair. The historical-fixture assertion observed schema version `8` instead of `9`; an orphan Approval insert returned no error; and `GetApproval` returned no error after an FK-disabled lifecycle reassignment.

#### GREEN

- The same focused command passed after adding migration v9 and the Approval lifecycle-pair validation.

#### Verification

- `go test ./internal/store -count=1 -timeout=180s` passed.
- `go test -race ./internal/store -count=1 -timeout=180s` passed.
- `node contracts/p1a/verify.mjs` printed `P1a proposal contract fixtures verified.`
- `node contracts/p1a/verify.mjs --self-test` reported rejection of drift, private fields, unpaired Unicode surrogates, request-hash conflation, rehashed timestamp and envelope-identity tampering, and missing vectors.

### 2026-07-22 — P1b migration-history integrity repair

#### TDD RED

- `go test ./internal/store -run '^TestProposalV7DataUpgradesToCompositeIdentityForeignKeys$' -count=1` exited `1`: applying `migrations[:7]` produced the v8 full lifecycle identity foreign key on `task_proposal_idempotency`, where the v7 expectation is its partial `(proposal_id, revision)` foreign key to `task_proposal_revisions`.

#### GREEN

- `migrateV7` now calls `createTaskProposalSchemaV7`; `migrateV8` calls `createTaskProposalSchemaV8`; `migrateV9` retains `createTaskProposalSchemaV9`.
- `TestProposalV7DataUpgradesToCompositeIdentityForeignKeys` now applies `migrations[:7]`, asserts the historical v7 foreign-key targets, seeds a valid v7 chain, and opens that database through v8 and v9. It replays the create request, checks the v8 and v9 version records, rejects an orphan Approval, and passes `PRAGMA foreign_key_check`.

#### Verification

- `go test ./internal/store -run '^(TestSchemaVersionMigrationFromV1Fixture|TestSchemaVersionMigrationFromV2AddsOutboxDiagnostic|TestOpenRejectsInvalidSchemaVersionHistory|TestOpenMigratesValidOldSchemaHistoryToHead|TestProposalSchemaMigrationFromV6Fixture|TestProposalV7DataUpgradesToCompositeIdentityForeignKeys)$' -count=1 -timeout=90s` passed.
- `go test ./internal/store -count=1 -timeout=180s` passed.
- `go test -race ./internal/store -count=1 -timeout=180s` passed.
- `node contracts/p1a/verify.mjs` printed `P1a proposal contract fixtures verified.`

### 2026-07-22 — P1c canonical Revision hash-link repair

#### RED

- The P1c self-test copied the fixture and schemas, changed the embedded
  `get_proposal` Revision together with its immutable create-input counterpart,
  recanonicalized the copied fixture, and rewrote its copied manifest. With the
  fixed-fixture digest deliberately waived only for that probe, the verifier
  rejected the mismatched embedded Revision at
  `detail revision/proposal canonical hash link`.

#### GREEN

- `node --check contracts/p1c/verify.mjs && node contracts/p1c/verify.mjs &&
  node contracts/p1c/verify.mjs --self-test` exited `0` in `0.42s`. The
  canonical verifier printed `P1c proposal public protocol fixtures and 12 DTO
  schema targets verified.` The self-test printed rejection of the consistently
  rehashed embedded Revision/hash mismatch alongside the existing drift,
  privacy, and closed-shape probes.
- The embedded `get_proposal` Revision canonical bytes already exactly matched
  `contracts/p1a/fixtures/revision-v1.canonical.json`; its SHA-256 is
  `sha256:114a02349dc027540bb0abd3947f20c5ef238ca9b917309910f17dd068270263`,
  matching the linked Proposal current, RevisionLifecycle, and Approval hashes.
  Therefore `protocol-v1.canonical.json` and its manifest remained unchanged.

#### Terminal verdict

- **PASS:** P1c now directly requires the embedded canonical Revision hash to
  equal every current detail hash. No schema, generator, runtime, daemon,
  bridge, GUI, commit, or push was changed.

### 2026-07-22 — P1c public DTO/codegen and schema repair

#### RED

- `node contracts/p1c/verify.mjs --self-test` exited `1`: the new direct
  Proposal timestamp schema test reported `Missing expected exception`, proving
  the prior schema admitted an invalid UTC calendar timestamp.
- `npm --prefix gui run test:renderer-public` exited `1`: the test could not
  open the missing `renderer-public-proposal-list-input.ts` generated decoder.
- `npm --prefix gui run test:renderer-public-privacy` exited `1`: injecting
  `token` into `renderer-public-proposal-create-input.schema.json` left the
  privacy check at status `0`, proving P1c schemas were absent from inventory.

#### GREEN

- `node contracts/p1c/verify.mjs && node contracts/p1c/verify.mjs --self-test`
  exited `0`. The verifier confirmed 12 DTO schema targets; its self-test
  rejected the Tauri-to-daemon typo and invalid P1a timestamp, UTF-8-byte,
  Approval, and Revision semantics.
- `npm --prefix gui run generate:renderer-public && npm --prefix gui run
  check:renderer-public && npm --prefix gui run check:renderer-public-privacy`
  exited `0` after generating all 11 P1c document DTOs plus embedded
  `ProposalActivity` in Rust, TypeScript, and Rust module exports.
- `npm --prefix gui run test:renderer-public && npm --prefix gui run
  test:renderer-public-privacy` exited `0`; decoders accepted every canonical
  P1c DTO and privacy injection was rejected for every P1c target.
- `npm --prefix gui run typecheck` exited `0`; `cargo test` in `gui/src-tauri`
  passed 19 tests across 3 suites.

### 2026-07-23 — P1c missing proposal activity repair

#### TDD RED

- `go test ./internal/store -run '^TestListProposalActivityRejectsInvalidAndUnknownProposalIDs$' -count=1` exited `1`: `ListProposalActivity("proposal_missing")` returned a nil error.
- `go test ./internal/lifecycle -run '^TestListProposalActivityMissingProposalRetainsPrivateNotFoundError$' -count=1` exited `1`: the private daemon response was `ok:true` with `proposal_activity:[]`.
- `cargo test --manifest-path gui/src-tauri/Cargo.toml bridge_proposals_serialize_public_wire_replay_conflicts_and_reconnect` failed: missing activity returned `ProposalActivityList { activity: [] }` instead of an error.

#### GREEN

- `ListProposalActivity` now maps invalid identifiers and absent `task_proposals` rows to `store.ErrProposalNotFound` before querying activity.
- Store coverage exercises invalid and unknown IDs; lifecycle coverage requires the private daemon `proposal_missing` response to retain `error:"proposal not found"` and omit `proposal_activity`.
- The real bridge coverage requires missing activity to return a private `BridgeError::DaemonRejected("proposal not found")`, rejects an empty public list, and verifies the existing public message remains `The daemon rejected this request.` without raw daemon details.

#### Verification

- Focused store and lifecycle regressions passed; `go test ./internal/store ./internal/lifecycle -count=1` passed.
- `npm --prefix gui run build:go && cargo test --manifest-path gui/src-tauri/Cargo.toml bridge_proposals_serialize_public_wire_replay_conflicts_and_reconnect` passed (1 test); `cargo fmt --manifest-path gui/src-tauri/Cargo.toml --check` passed.
- `go test ./... -count=1` passed (3 packages with tests; 3 packages without tests). `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets` passed (20 tests across 2 suites).
- `node contracts/p1c/verify.mjs`, its `--self-test`, renderer-public generation/check/privacy, TypeScript typecheck, run-state, renderer-public decoder, and renderer-public privacy tests all passed.
- No commit or push command was run.

### 2026-07-23 — P2a deterministic Grill contract / fixture gate

#### Scope

- Added only the frozen P2a contract artifacts: canonical Grill, adversarial,
  and acceptance fixtures; their SHA-256 manifest; the dependency-free Node
  verifier; the implementation TDD plan; and the contract documentation.
- The fixture binds every Question, Answer, Default, and Override to the exact
  P1a root Revision tuple and freezes `ananke.grill.rules.v1` with six rule
  classes. It has no GUI, daemon, Tauri, store, claim, worker, adapter runtime,
  model, approval mutation, or command-execution gate.

#### Verification

- `node --check contracts/p2a/verify.mjs && node contracts/p2a/verify.mjs && node contracts/p2a/verify.mjs --self-test` exited `0`.
- The verifier printed that it verified six rule classes, revision-bound
  append-only records, the five-question display bound, ten-question rewrite
  cap, idempotent replay, and adversarial review-only inputs.
- The self-test printed rejection of frozen-rule drift, command and approval
  injection, unbounded attempt caps, and append-only question-sequence
  tampering.
- No commit or push command was run.

#### Ten-question-cap repair (RED/GREEN)

- RED: the independent review artifact
  `artifacts/omp/p2a/independent-review-output.md` reproduced the pre-repair
  boundary failure: nine prior Questions selected five new Questions, yielding
  fourteen Question records instead of stopping at ten.
- GREEN: `evaluate()` now bounds new Questions by
  `min(5, 10 - priorQuestionCount)` before applying the display-slot bound. The
  canonical acceptance sequence appends one Question at nine (total ten), then
  returns `needs_rewrite` and no append at ten.
- GREEN: the self-test consistently rehashes a copied acceptance fixture that
  asks for five new Questions at nine prior Questions (fourteen total); with
  only the hard digest pin waived, the evaluator/verifier rejects that forged
  outcome.
- `node --check contracts/p2a/verify.mjs`, `node contracts/p2a/verify.mjs`,
  `node contracts/p2a/verify.mjs --self-test`, `node contracts/p1a/verify.mjs`,
  and `node contracts/p1c/verify.mjs` each exited `0`.
- No runtime, UI, daemon, store, model, or commit artifact was changed.

### 2026-07-23 — P2b deterministic Grill runtime

#### Scope

- Added the review-only Grill store implementation in `internal/store/grill.go`
  and schema migration v10. The evaluator uses the frozen
  `ananke.grill.rules.v1` table, hashes only the closed declaration input, and
  stores the exact P1 Revision tuple verbatim.
- Added insert-only `grill_evaluations` and `grill_records` tables. Question
  records have contiguous per-Revision record/question sequences; Default,
  Answer, and the sole scope/compatibility Override are append-only and
  idempotent on replay. SQLite triggers reject update and delete.
- Added private daemon commands `evaluate-grill`, `record-grill-default`,
  `record-grill-answer`, and `record-grill-override`. The nested `grill`
  payload is decoded with `DisallowUnknownFields`, so raw Revision prose and
  other non-contract properties fail before reaching the store.
- Added the matching private native/Tauri daemon-wire types and bridge methods.
  No generated renderer-public DTO, registered Tauri command, renderer UI,
  model call, claim, worker, adapter, approval mutation, or execution path was
  added.

#### TDD and verification

- Focused: `go test ./internal/store ./internal/lifecycle -run Grill` exited
  `0` (both packages passed). The Store cases cover canonical JCS hashing,
  all six rules, replay/restart, append-only operator rows, waivers,
  concurrent evaluation, ten-question capacity, identity/input-hash failures,
  and migration from the P1b head. The lifecycle case exercises the live
  private daemon commands and rejects injected `task` prose.
- Focused native boundary: `cargo test --manifest-path
  gui/src-tauri/Cargo.toml private_grill_wire_is_closed_and_not_renderer_public`
  exited `0`; it proves the private wire has only the closed Grill input and no
  renderer-public proposal fields or forbidden review/execution fields.
- Full Go: `go test ./...` exited `0` (3 packages passed; 3 packages had no
  tests). The historical v7 migration test now asserts the migration-list head
  and verifies v8, v9, and v10 history rows rather than retaining an obsolete
  hard-coded v9 head.
- Full Rust: `cargo test --manifest-path gui/src-tauri/Cargo.toml` exited `0`
  (21 tests across 3 suites).
- Full TypeScript/public-contract gates all exited `0`: `npm run typecheck`,
  `check:renderer-public`, `check:renderer-public-privacy`, `test:state`,
  `test:renderer-public`, and `test:renderer-public-privacy`.
- Contract gates all exited `0`: `node --check contracts/p2a/verify.mjs`, both
  P2a verifier modes, `node contracts/p1a/verify.mjs`, and
  `node contracts/p1c/verify.mjs`. The P2a self-test rejected frozen-rule
  drift, command/approval injection, unbounded attempt caps, record-sequence
  tampering, and a forged nine-to-fourteen question overrun.
- No commit or push command was run.

### 2026-07-23 — P2b M1 Grill decoder privacy repair

#### Scope

- Repaired only the private lifecycle Grill decoder and its live daemon-socket
  regression in `internal/lifecycle/engine.go` and
  `internal/lifecycle/grill_api_test.go`.
- `DisallowUnknownFields` remains enabled. Every failed first or trailing
  `json.Decoder.Decode` now returns the fixed daemon error `invalid grill
  request`; no parser error is concatenated into the response.
- No UI, model, worker, claim, execution, approval, or renderer-public code
  was changed. No commit or push command was run.

#### TDD and verification

- RED: `go test ./internal/lifecycle -run
  '^TestGrillCommandsServeFrozenPrivateReviewProtocol$' -count=1 -timeout
  120s` exited `1`: the live daemon response exposed `json: unknown field
  "raw_revision_prose_secret"` instead of the required stable error.
- GREEN: the same command exited `0` after the decoder repair. The regression
  injects nested `raw_revision_prose_secret` with a sentinel value, asserts a
  rejected response with no `grill_evaluation`, requires the exact stable
  error, and inspects the daemon's serialized JSON to deny the field name,
  value, and `json: unknown field` diagnostic.
- `gofmt -d internal/lifecycle/engine.go
  internal/lifecycle/grill_api_test.go` produced no output.
- Focused Grill Go: `go test ./internal/store ./internal/lifecycle -run Grill
  -count=1 -timeout 120s` exited `0` (both packages passed).
- Full Go: `go test ./... -count=1 -timeout 300s` exited `0` (3 packages
  passed; 3 packages had no tests).
- Full native boundary: `cargo test --manifest-path gui/src-tauri/Cargo.toml`
  exited `0` (21 tests across 3 suites).
- GUI public gates all exited `0`: `npm --prefix gui run typecheck`,
  `check:renderer-public`, `check:renderer-public-privacy`, `test:state`,
  `test:renderer-public`, and `test:renderer-public-privacy`.
- Contract gates all exited `0`: `node --check contracts/p2a/verify.mjs`, P2a
  verifier and self-test, P1a verifier and self-test, and P1c verifier and
  self-test.

### 2026-07-23 — P2c renderer-public Grill DTO boundary repair

#### Scope

- Changed only the renderer-public code generator, its nine generated Grill
  Rust/TypeScript DTO targets and generated Rust contract test, decoder/privacy
  tests, and this ledger. The frozen P2c fixture and schemas remain canonical.
- No Tauri command, daemon bridge, renderer UI, model, approval, execution,
  commit, or push was added.

#### TDD RED

- The independent review at
  `artifacts/omp/p2c/independent-review-output.md` observed that the prior
  TypeScript decoders accepted `revision:0`, malformed SHA-256 hashes,
  six-question/new-ID arrays, `new_records:7`, reordered Questions/IDs, and
  mismatched Question identity; it also found all nine Rust DTO deserializers
  open to unknown fields.
- The generated Rust contract regression initially failed with `cargo test
  --manifest-path gui/src-tauri/Cargo.toml --lib
  generated::grill_contract_tests::generated_grill_dto_decoders_enforce_the_p2c_contract`:
  an injected private/unknown field deserialized successfully. The command
  exited `101` and panicked `P2c DTO must reject private or unknown fields`.
- The first TypeScript proposal-ID probe used `"invalid"`, which satisfies the
  schema identifier regex; it was corrected to the actually invalid `"1"`
  before recording GREEN. That probe was a test correction, not a contract
  failure.

#### GREEN

- The generator now emits schema-aware TypeScript validators for every Grill
  DTO and Rust custom deserializers backed by generated
  `#[serde(deny_unknown_fields)]` wire structs. They enforce closed fields,
  identifier/hash/timestamp patterns, integer and array bounds, constants,
  P2b fixed Question rules, root/Question Revision-tuple equality, priority
  order, ordered shown/new Question links, deferred-rule order, and bounded
  `new_records` semantics.
- Generated Rust and TypeScript adversarial decoders cover all nine targets:
  canonical acceptance; unknown/private field injection; identity regex/minimum
  failures; Question/record bounds; timestamp and const failures; and the
  review probes for overflow, identity/hash mismatch, non-blocking Questions,
  rule/ID mismatch, reordering, clear status, and record-count inconsistency.
- Generator privacy mutations inject every P2c private-field fragment into all
  nine DTO schema targets. The P2c-specific denylist now covers `model`,
  `prompt`, `approval`, `execution`, `runtime`, `raw`, and the remaining P2c
  private-field classes without restricting pre-existing P1 public fields.

#### Verification

- `node contracts/p2c/verify.mjs` and `node contracts/p2c/verify.mjs
  --self-test` exited `0`; the latter rejected fixture drift, private-field
  injection, ordering tampering, overflow, and private schema fields.
- `npm --prefix gui run check:renderer-public`,
  `npm --prefix gui run check:renderer-public-privacy`,
  `npm --prefix gui run test:renderer-public`,
  `npm --prefix gui run test:renderer-public-privacy`, and
  `npm --prefix gui run typecheck` each exited `0`.
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --lib` exited `0`:
  22 tests passed in one suite (the tool reported 18 warnings).

### 2026-07-23 — P2c generator determinism and warning repair

#### RED

- Before this repair, `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets --no-run --message-format=short` emitted 18 generated P2c `dead_code` warnings: validation helpers were appended to DTO modules that did not call them.
- The renderer-public generator emitted Rust whose bytes differed from Rustfmt output, so a manual formatting pass could make the generator content-drift check fail.

#### GREEN

- `gui/scripts/generate-renderer-public.mjs` now formats every generated Rust DTO and `generated/mod.rs` through Rustfmt (edition 2024) before it compares or writes bytes. `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check` and `npm --prefix gui run check:renderer-public` therefore agree on the same generated bytes.
- The generator selects the transitive validation-helper set for each P2c DTO. It no longer emits unused timestamp, record, question-ID, Question, priority, or evaluation helpers; generated Rust builds with no warnings.
- The existing custom P2c Rust deserializers still use `#[serde(deny_unknown_fields)]`, and the full semantic validator/test gates continue to cover identity, timestamp, Question, evaluation, record, and privacy constraints.

#### Verification

- `npm --prefix gui run generate:renderer-public` and `npm --prefix gui run check:renderer-public` exited `0`.
- `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check` exited `0`.
- `cargo test --manifest-path gui/src-tauri/Cargo.toml --all-targets` exited `0`: 22 tests passed across two suites with no warnings.
- `npm --prefix gui run typecheck`, `npm --prefix gui run check:renderer-public-privacy`, `npm --prefix gui run test:renderer-public-privacy`, and `npm --prefix gui run test:renderer-public` each exited `0`.
- `node contracts/p2c/verify.mjs` and `node contracts/p2c/verify.mjs --self-test` each exited `0`.

### 2026-07-23 — P2d Grill runtime public projection

#### Scope

- Added only the four P2c-approved Tauri commands: `evaluate_grill`,
  `record_grill_default`, `record_grill_answer`, and
  `record_grill_override`. They accept and return the generated, closed Rust
  DTOs; no renderer UI or TypeScript call site was added.
- The bridge constructs the fixed conservative, unreviewed P2a declaration
  only from the generated immutable Revision tuple, canonicalizes and hashes
  it locally, then calls the existing authenticated private Grill commands.
  It never reads Revision prose, Approval, model data, worker data, claims, or
  execution data.
- Existing private `evaluate-grill` now returns the persisted shown Question
  records, and each existing record command returns its durable record. The
  Rust bridge allowlists those private fields into generated P2c results and
  discards private input hashes, rule/schema versions, daemon envelopes, and
  raw errors. Any daemon/store rejection or invalid private result remains a
  sanitized Tauri error.
- Corrected the P2b Default writer to `deterministic_grill`; it was previously
  persisted as `local_gui_operator`, which cannot satisfy the frozen P2c
  `GrillDefaultRecord` DTO. Corrected generated `new_records` validation to
  accept a newly appended Question without a duplicate Evaluation row after a
  prior evaluation exists, while still requiring every appended Question to be
  counted.
- No model, worker, claim, approval mutation, execution, commit, or push was
  added or run.

#### TDD and verification

- RED: the initial native P2d bridge test did not compile because the private
  P2b Grill methods returned private wire types and accepted private input.
  After the public projection was added, the real sidecar test failed at the
  Default projection; the focused Store regression observed record sequence 6
  written by `local_gui_operator` rather than `deterministic_grill`.
- RED: after fixing that writer, the same real sidecar test exposed an
  over-strict generated P2c `new_records == new_question_ids + 1` invariant.
  The durable evaluator correctly appends just the deferred Question after its
  already-existing Evaluation row. Generator, Rust, TypeScript, and decoder
  regression tests now require `new_question_ids.length <= new_records <=
  new_question_ids.length + 1`.
- GREEN: `tests::bridge_grill_projects_p2c_oracle_through_sidecar_and_sanitizes_failures`
  passed against copied real sidecars. It uses the frozen P2c fixture as its
  public wire oracle; covers eval → idempotent Default → Answer → Override →
  deferred-question re-eval → idempotent replay → reconnect, a valid-but-missing
  Revision identity, a six-question cap breach at the private-to-public
  conversion boundary, and raw daemon/private-transport error denial.
- `go test ./... -count=1 -timeout=300s` exited `0` (3 packages passed; 3 had
  no tests). `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check`,
  `npm --prefix gui run build:go`, and `cargo test --manifest-path
  gui/src-tauri/Cargo.toml --all-targets` exited `0` (23 tests across two
  suites).
- `npm --prefix gui run check:renderer-public`,
  `check:renderer-public-privacy`, `test:renderer-public`,
  `test:renderer-public-privacy`, `typecheck`, and `test:state` each exited
  `0`.
- P2a (`--check`, verifier, self-test), P1a (verifier, self-test), P1c
  (verifier, self-test), and P2c (verifier, self-test) contract gates all
  exited `0`.

### 2026-07-23 — P2d Grill response identity-binding repair

#### Scope

- Changed only `gui/src-tauri/src/lib.rs` and this ledger. The four existing
  Grill commands remain the complete Tauri registration set: `evaluate_grill`,
  `record_grill_default`, `record_grill_answer`, and `record_grill_override`.
- The evaluation projector now receives the submitted public Revision tuple and
  the bridge-derived canonical private input hash, then requires exact matches
  before projecting any generated P2c DTO. Every record projector similarly
  requires the submitted tuple and Question ID before allowlisting its result.
- The native scripted-sidecar regression supplies schema-valid private results
  for a different Revision tuple, a different evaluation input hash, and
  different allowed record Question IDs. It covers Default, Answer, and
  Override paths; every mismatch becomes the fixed public bridge error without
  a DTO or private response detail.
- No UI, generated model, worker, claim, approval, execution, daemon protocol,
  commit, or push changed or ran.

#### TDD and verification

- RED: `cargo test --manifest-path gui/src-tauri/Cargo.toml
  tests::bridge_grill_rejects_schema_valid_swapped_private_sidecar_results --
  --exact` exited `101`; the schema-valid swapped private evaluation crossed
  the public boundary.
- GREEN: the same focused regression exited `0` after exact tuple/hash/Question
  checks were placed at the private-to-public conversion boundary.
- `cargo fmt --manifest-path gui/src-tauri/Cargo.toml -- --check`, `npm
  --prefix gui run build:go`, the real-sidecar P2d oracle test, and `cargo test
  --manifest-path gui/src-tauri/Cargo.toml --all-targets` exited `0`; the full
  Rust suite reported 24 passing tests.
- `node contracts/p2c/verify.mjs` and `node contracts/p2c/verify.mjs
  --self-test` exited `0`. The command registration remains exactly the four
  approved Grill commands above.

### 2026-07-23 — P2d nested shown Question identity binding

#### Scope

- `project_grill_evaluation` now rejects a returned shown Question when any of
  its `proposal_id`, `revision`, or `revision_hash` differs from the submitted
  Revision identity. That check runs after private response decoding and before
  construction of the projected public Question JSON and `GrillEvaluation` DTO.
  The existing outer evaluation tuple, locally derived `input_hash`, rule
  version, and shown-ID checks remain intact.
- Added a scripted Unix-sidecar regression whose outer evaluation tuple,
  locally derived input hash, and `shown_question_ids` remain correct while one
  nested Question carries a different individually valid tuple. It requires the
  fixed unavailable error; its error-path assertion proves no DTO is returned
  and denies the swapped tuple and private input-hash detail.
- This repair changed no UI, generated model, worker, claim, approval,
  execution, daemon protocol, command registration, commit, or push. The
  existing four-command Grill scope remains unchanged.

#### TDD and verification

- The scripted regression was added and run before the new projector guard. It
  exited `0` because the generated P2c decoder already rejects mismatched nested
  Question identities after projected public JSON is assembled. That existing
  late defense did not meet the required pre-construction boundary; the new
  guard makes the same rejection before projection. The focused regression
  exited `0` again after the guard was added.
- `npm --prefix gui run build:go`, `cargo fmt --manifest-path
  gui/src-tauri/Cargo.toml -- --check`, and `cargo test --manifest-path
  gui/src-tauri/Cargo.toml --all-targets` exited `0`; the Rust suite reported
  25 passing tests.
- `go test ./... -count=1 -timeout=300s`, renderer generator/privacy/decoder,
  TypeScript typecheck/state, and P2a/P2c plus P1a/P1c verifier and self-test
  gates each exited `0`.

### 2026-07-23 — P2e minimal deterministic Grill renderer review

#### Scope

- Added a persistent, guarded Grill panel to the existing renderer. It derives
  the current Revision tuple only from the existing public Proposal list/detail
  DTOs, requires every Proposal/Lifecycle/Approval identity to agree, and shows
  the immutable Proposal ID, Revision, Revision hash, and review status.
- The panel uses the generated P2c TypeScript inputs/results for its only Grill
  actions: `evaluate_grill`, `record_grill_default`,
  `record_grill_answer`, and `record_grill_override`. It sends no answer prose,
  declaration, hash derivation, private bridge/wire field, model, worker,
  claim, approval, or execution input.
- It renders at most five sequence-ordered Questions with risk, default,
  remedial-step, and waiver context. Every Question permits only Default or
  Acknowledge; a Waive control is exposed solely for the exact
  scope-compatibility Question. Pending operations disable all Grill controls,
  then re-evaluate on successful record. Errors use a fixed renderer message.
- Added the static Grill accessibility selector to the existing Mac harness
  contract. No backend command, daemon wire, generated model, worker, claim,
  execution path, commit, or push was added or run.

#### TDD and verification

- RED: `node gui/scripts/test-grill-review.mjs` initially exited `1` because
  `gui/src/grill-review.ts` did not exist. The state/DOM contract was written
  first for guarded visibility, tuple display, five-question ordering,
  scope-only waiver, pending controls, re-evaluation, and sanitized failures.
- RED: the disabled-control interaction regression initially exited `1` because
  a disabled DOM button could still invoke the review action handler. The
  handler now rejects `button.disabled` before dispatch.
- GREEN: `npm --prefix gui run test:grill-review` and `npm --prefix gui run
  typecheck` exited `0`. The test uses the canonical P2c response fixture and
  asserts exact action input, no non-scope Override dispatch, and raw-error
  denial.
- Browser smoke: the Vite renderer at `http://127.0.0.1:1420/` visibly rendered
  the `ananke-grill-review` ARIA region with the guarded current-Revision state
  and disabled Refresh control when no Tauri-backed Revision is available.
- Final TypeScript/web gates exited `0`: `npm --prefix gui run typecheck`,
  `web:build`, `test:state`, `test:grill-review`,
  `check:renderer-public`, `check:renderer-public-privacy`,
  `test:renderer-public`, and `test:renderer-public-privacy`.
- Full native and core gates exited `0`: `cargo test --manifest-path
  gui/src-tauri/Cargo.toml` (25 tests) and `go test ./...` (3 packages passed;
  3 packages had no tests).
- Contract verifiers exited `0`: `node contracts/p1a/verify.mjs`,
  `contracts/p1c/verify.mjs`, `contracts/p2a/verify.mjs`, and
  `contracts/p2c/verify.mjs`. `npm --prefix tests/mac2 test` also exited `0`
  with seven harness tests, including the Grill accessibility preflight.
- No commit or push command was run.
