Resume exact durable-finalization session 019f9ce2-797a-7000-a13a-461a64a06afb and continue with tools/edits. Current focused test failures:
- TestAuditFinalizingHistoryIsSignedNonterminalAndReplayClosed: ErrProtocol constructing/signing finalizing fixture
- TestProductionServerFinalizingRemainsPendingBeforeCleanup: verified evidence not persisted before cleanup (hook never reached)
- every owned-root cleanup failure subtest: ErrProtocol at finalizingAuditExecutionHistoryForTest
- both restart recovery subtests: same ErrProtocol

Trace the exact protocol/event validation causing rejection; do not relax closed invariants broadly. Ensure finalizing event carries all required evidence fields, paths, process finish fields, command/session binding and authentication expected by state-specific validation. Its transition must be running -> finalizing only; completed must derive from finalizing, not running. Ensure runtime appends finalizing before cleanup and invokes deterministic hook at correct boundary.

Finish production recovery and callback root-absence verification, then run finalizing focused count=10, race=3, full package, diff check. No provider/model, no blocker 6, no commit. Report exact results.