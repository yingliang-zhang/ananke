Working...
VERDICT: ACCEPT

## Candidate binding

- Base: `b8e21eac0b808a9ad35b90e59e1eacd925753d28`.
- Recomputed source manifest: `93aef9afc7771cb79c35d3c7df0fa6bca6f50e8071619d0fa36473198b82dd7f`, **68 files**. Exact manifest match.
- Recomputed report SHA-256 values exactly match the frozen manifest:
  - `reports/verification.json`: `b9b091de2b5826ba1f5b32587eeeee8e01d556fe6a23f192d2a842b2bb508d75`
  - `reports/mutation-proof.json`: `ce5beb9aebdbcc87f3210f3ae67383f18d7836ce2cda5dd9bf6aacd358be797a`
  - `reports/stress-lifecycle.json`: `5f10cde5139c7275aaa755c1d7bd21f7ac4ad9eb02c7b007c6c839039939c834`
  - `reports/blackbox-lifecycle.json`: `2751d191235ea076101decbb88883fde003ca7b19d5771bf5496b40e7218d6b0`
- Every report declares the same aggregate, file count, and complete per-file manifest. Verification/stress/blackbox report `all_pass: true`; mutation report `all_detected: true`.

## Confirmed findings

- **P1 — none. Transcript framing holds.**
  - `internal/lifecycle/engine.go:1443-1459` withholds an unterminated EOF suffix unless its end offset equals a durable final seal; it rewinds to the committed tail offset before retry.
  - `engine.go:1474-1548` appends only after framing is proven; `internal/store/events.go:26-62` atomically persists event and consumed offset.
  - Repro PASS: `TestEngineTranscriptValidEOFWaitsForDurableFraming`, `TestEngineTranscriptAppendFailureRetriesWithoutOffsetSkip`, `TestEngineCompletionIncludesFinalLineWithoutNewline`.
  - The withheld-valid-prefix-plus-later-record case becomes one malformed physical line and transitions to `cleanup_required`, without publishing either event: `internal/lifecycle/engine_test.go:670-718`.

- **P1 — none. Process authority and cleanup topology hold.**
  - Paused group-leading trampoline: `internal/lifecycle/backend.go:76-114, 198-285`; the worker cannot exec before a release-pipe byte, and `Setpgid: true` makes trampoline PID equal PGID.
  - Publication-before-release ordering: `internal/supervisor/supervisor.go:223-282` publishes identity, run authority, authenticated socket, and `running` before `ReleaseWorker`.
  - Cleanup is group-only and pre-reap: `internal/supervisor/supervisor.go:578-704`; production TERM/KILL routes through `SignalGroup`, whose Darwin implementation is `kill(-pgid, sig)` at `internal/lifecycle/backend.go:347-353`.
  - Permanent post-fork authority failures retain `cleanup_required` with no terminal outbox until authority recovers: `internal/supervisor/authority_test.go:243-267`.
  - No production `SignalProcess` or `BecomeGroupLeader` backdoors found.
  - Repro PASS: authority-before-release, post-start cleanup-before-reap, resistant-child cleanup, durable nonterminal-authority, and paused-trampoline group-leader probes.

- **P2 — none. Hardening is present.**
  - Entropy failures propagate and prevent run creation: `internal/lifecycle/engine.go:195-202, 1657-1664`; probe PASS: `TestEngineRunTokenEntropyFailureCreatesNoRun`.
  - Terminal kqueue failures close watcher readiness: `internal/lifecycle/backend.go:158-175`; probe PASS: `TestProcessExitWatcherTerminalErrorClosesReadiness`.
  - Identity persistence fsyncs the temporary file and containing directory around rename: `internal/lifecycle/identity.go:69-91`; probe PASS: `TestIdentityFileRoundTrip`.
  - Production stderr sites print generic errors/status only; inspected error construction does not format authentication token values. `cmd/ananke/main.go:34-55`; `cmd/ananke-supervisor/main.go:62-70`.

- **P3 — none. Test hygiene does not alter production semantics or mask mutants.**
  - Cleanup changes are confined to `_test.go`: temporary-store fixture cleanup in `internal/lifecycle/engine_test.go:116-161`; exact known fixture-child cleanup in `internal/supervisor/mutation_test.go:95-109` and `internal/supervisor/supervisor_test.go:254-268`.
  - Mutation cleanup runs via `t.Cleanup` after test execution; it cannot convert an assertion failure into a pass.
  - Bound mutation evidence records behavioral rejection for all six mutants; normal-mode `TestMutationReapBeforeCleanupOrder` also passed independently.

## Evidence

Focused, untagged probes run and passed:

```text
go test ./internal/lifecycle -count=1 -run '^(TestEngineTranscriptValidEOFWaitsForDurableFraming|TestEngineTranscriptAppendFailureRetriesWithoutOffsetSkip|TestEngineCompletionIncludesFinalLineWithoutNewline)$'
go test ./internal/supervisor -count=1 -run '^(TestSupervisorPublishesAuthorityBeforeWorkerRelease|TestSupervisorPostStartFailuresCleanupBeforeReap|TestForkSupervisorCleanupKillsOwnedGroup|TestSupervisorResistantChildEscalation)$'
go test ./internal/lifecycle ./internal/supervisor -count=1 -run '^(TestEngineRunTokenEntropyFailureCreatesNoRun|TestProcessExitWatcherTerminalErrorClosesReadiness|TestIdentityFileRoundTrip|TestMutationReapBeforeCleanupOrder)$'
go test ./internal/supervisor -count=1 -run '^TestSupervisorPermanentAuthorityFailuresStayNonterminal$'
go test ./internal/lifecycle -count=1 -run '^TestLaunchWorkerStartsPausedDistinctGroupLeader$'
```

No mutation-tag build, report regeneration, process-kill action, or source edit was performed by this review.

## Residual risks

- Accepted ADR-0002 limitation: if the supervisor crashes while resistant descendants remain, recovery deliberately refuses to signal a potentially reused PGID and enters `recovery_unknown`. `docs/adr/0002-supervisor-lifecycle-identity-model.md:249-258`.

## Required changes

None.
