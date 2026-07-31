Working...
Implemented only the focused termination residual.

Changes:
- Known PID/start-identity mismatch now persists `waiting_for_human/process_identity_mismatch` and returns immediately without signaling or retaining an impossible waiter.
- Supervisor context-shutdown fixture deterministically installs default TERM behavior before cancellation; exact process-group exit and reap remain asserted.
- Production fixture now:
  - emits a valid canonical model report;
  - proves the supervisor-owned test started;
  - waits until the test installed its TERM-ignore handler;
  - injects the KILL failure;
  - verifies typed `waiting_for_human/kill_signal_failed`;
  - rejects `completed`, `cancelled`, `timed_out`, or evidence authority;
  - verifies resources/key remain live until external reap and successful `Close` retry.
- Avoided mutating command capture buffers while an unjoined child may still be writing, eliminating the race.
- Reverted the policy-history validation change; no policy-history edits remain.

Verification passed:

```text
go test ./internal/trustedsupervisor -run '^$'
go test ./internal/trustedsupervisor \
  -run '^(TestAuditExecutorRestartFailsClosedOnWrongPIDStartIdentity|TestSupervisorAuditTestContextShutdownUsesOwnedTermination|TestProductionServerSupervisorTestTerminationFailureRetainsResourcesUntilCloseRetry)$' \
  -count=10
go test -race ./internal/trustedsupervisor \
  -run '^(TestAuditExecutorRestartFailsClosedOnWrongPIDStartIdentity|TestSupervisorAuditTestContextShutdownUsesOwnedTermination|TestProductionServerSupervisorTestTerminationFailureRetainsResourcesUntilCloseRetry)$' \
  -count=3
go test ./internal/trustedsupervisor \
  -run '(SupervisorAuditTest|Unconfirmed|Termination)' \
  -count=1
```

No real OMP, documentation changes, commit, or push.
