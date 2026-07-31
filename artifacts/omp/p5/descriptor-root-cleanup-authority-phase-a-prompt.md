Repair fifth-review blocker 2: invocation/finalization cleanup authority path-reopen TOCTOU. Read fifth-review-report-recovered.md lines 77-144 and authority-contract/TDD principles. This is phase A of the repair; strict vertical TDD, no provider/model, no commit. Do not weaken blocker-4 runtime authority or blocker-5 finalizing state.

Threat: concurrent untrusted actor can rename original owned root/controlling parent, put same-UID 0700 decoy at expected name, let cleanup delete decoy, restore policy path, retain original under another name, and obtain signed completed. Path absence is not identity absence.

First trace all production creation/use/cleanup/recovery paths and define one central authority contract. Implement:
1. Production namespace admission must fail closed unless each invocation root controlling parent and complete physical ancestry are descriptor-opened component-by-component, non-symlink, pinned identity, trusted-owner controlled, and not writable by the configured untrusted runtime UID/groups/ACL. For the guarded MVP, a root-owned immutable ancestry plus explicit distinct nonzero runtime UID/GID is acceptable; do not claim current user-owned temp roots are production-safe. No env/flag/config test bypass.
2. Retain CLOEXEC parent directory descriptors for server/policy lifetime and duplicate per invocation. All owned-root mkdir/open/verify/remove operations use mkdirat/openat/fstatat/unlinkat relative to retained descriptors; never initial path reopen after admission. Deterministically close on all paths.
3. Drop the OMP/wrapper child to the explicit distinct runtime UID/GID in production; the trusted supervisor retains cleanup authority. Fail closed if effective supervisor privilege/ownership cannot establish this boundary. Test-only compile-tag/internal dependency may emulate it, but normal production cannot enable bypass.
4. At creation, capture device/inode/owner/group/mode plus pinned-parent identity for every actual cleanup root (all attempt-specific prompt/output/temp roots and shared session/work/snapshot/wrapper-transport roots). Bind the exact ordered identities into authenticated finalizing authority/evidence; bump schemas consistently.
5. Cleanup/recovery opens expected children relative to the authenticated parent descriptor and compares signed identity before scrub. A decoy/mismatch is nonterminal and never deleted as the original. Under a trusted stable parent, expected-name absence may mean already cleaned; under any untrusted/mismatched parent it may not complete.
6. Recovery reopens only the trusted root-owned ancestry component-by-component and compares every pinned identity. If any mismatch/ambiguity occurs, remain nonterminal; never completed/hot-loop/provider rerun.

Required RED then GREEN tests:
- mutable/same-UID controlling ancestor rejected before mkdir/gateway/credential/child;
- deterministic gate between effect validation and mkdir swaps parent/root; no operation under replacement;
- afterFinalizingPersist for each attempt root and shared session/work/snapshot/transport: rename original, place decoy, restore policy parent A→B→A; no completed/callback, decoy not accepted/deleted as original, original cannot be claimed cleaned; restart same;
- signed identity tamper/reorder/omission rejected;
- descriptor counts/CLOEXEC/close on success,start failure,timeout,cancel,recovery;
- child credential is distinct in production; production config cannot enable test bypass;
- current user-owned fixture gets precise typed unsupported namespace error before effects.

Because this is broad, prioritize a compiling coherent vertical slice: namespace admission + signed identities + cleanup/recovery mismatch closure. Run focused tests count=10/race=3 and full package if time. Report incomplete items honestly for exact-session resume.