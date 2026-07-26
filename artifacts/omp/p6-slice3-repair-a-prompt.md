Implement P6 Slice-3 Repair A only in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`, based on `artifacts/omp/p6a/slice3-first-review.md`. No commit. Preserve Slices 1–2.

Scope: opaque journal slot/commit capability, opaque predecessor terminal-event capability, stale replay capability gating, and directly related vectors/docs. Do not redesign recovery records yet; Repair B follows. No runtime/store/process code.

Required correction:
1. Remove public `SupervisorClaimSlotAuthority` status/tuple/slot/committed-bytes trust from capability minting. `SupervisorIntentAuthority` may retain expected immutable phase/slot identifiers, but caller-provided status/bytes must never prove journal commit.
2. Introduce an opaque journal-verified slot/commit capability with all fields private and integrity rechecked on every use. It must retain:
   - `(attempt_hash, phase)`, exact unique slot ID, journal head, committed canonical bytes/hash;
   - boot epoch, claim ID/hash, sequence, authorization/approval/request/dispatch binding;
   - a private uniqueness/commit-verification state that public callers cannot set.
   Provide no production constructor/decoder from claim/request/fixture bytes. Future journal implementation is intentionally required to live in trusted in-package code or a separately reviewed seam. Test-only fixtures in `_test.go` may mint it.
3. Empty-slot admission remains possible from expected authority but returns only `awaiting_journal_commit`, no capability/effect. Committed replay/capability path must require the opaque slot capability, not a public enum or bytes.
4. Enforce both uniqueness dimensions through the opaque proof and validation:
   - one slot for one `(attempt_hash, phase)`;
   - one slot ID cannot serve another tuple/phase.
   Add real negative probes that construct conflicting opaque test proofs and demonstrate rejection; do not simulate duplicate by setting a public `duplicate` enum.
5. Replace raw `PredecessorTerminalEventHash` authority with an opaque verified terminal-event capability for phases 2 and 3. Private integrity must bind prior claim hash, attempt, prior phase/sequence, terminal-event hash, boot epoch, slot/journal lineage, and terminal status. No public constructor. Invented hash without capability and wrong/aliased event capability reject.
6. Stale committed exact replay at claim `not_after`, stale authorization/dispatch, or invalid current embedded release may still classify bytes as historical exact replay if useful, but must return nil predecessor capability. A capability can be returned only after effect-time freshness succeeds. Add N-1ns/N/N+1ns tests and executable vectors.
7. Ensure opaque capabilities deep-copy retained bytes and fail after any in-package field/canonical/authority mutation.
8. Replace decorative `duplicate_phase` with a real two-claim same tuple/phase conflict sequence. Remove/rename decorative duplicate-slot vector accordingly. Use an ordered table instead of map iteration in `priorEpochWaitingForHumanProbe`.
9. Update the normative doc and machine contract/vector inventory exactly. Do not claim Repair B/recovery closure or Slice ACCEPT.

TDD:
- first add RED probes reproducing reviewer sequences: second committed slot same tuple, slot ID aliased across phases, invented terminal-event hash, stale exact replay capability at N and N+1;
- implement minimal GREEN;
- run focused Repair-A tests/registry, package single, count=10, race count=3, vet, gofmt, diff-check.

Return RED/GREEN commands/results and changed files. Do not create cron jobs.
