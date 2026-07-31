Implement namespace-authority Phase B on the now-green worktree. Read fifth-review blocker 2 and existing namespace_authority.go/audit_executor OwnedRoots. Strict TDD, no provider/model, no commit. Do not add a parallel journal column if the authenticated finalizing EvidenceJSON can carry the contract cleanly.

Goal: the identities already captured in `auditInvocation.OwnedRoots` must become durable signed authority and must drive finalization/restart/callback cleanup. Current path-only `auditFinalizingOwnedRoots` is forbidden for completion authority.

Vertical contract:
1. Extend canonical read-only audit evidence with an exact ordered `owned_roots` array containing role,path,parent path, device/inode, owner UID/GID, mode, parent identity, cleanup_root for every owned root. Include all attempt prompt/output/temp roots, nested wrapper-state/agent/home transport roots, and shared session/work/source-snapshot cleanup authorities. Bump evidence schema and docs consistently. No private file contents.
2. Evidence generation takes identities from `result.boundInvocation.OwnedRoots` plus identities captured descriptor-relatively for shared work/snapshot roots. Validate exact role inventory, unique paths, parent-before-child order, cleanup-root semantics and path/event bindings. Evidence hash and signed finalizing event authenticate the array; completed must preserve exact EvidenceJSON/hash and FinalizingEventHash.
3. `resumeFinalizing` parses and validates identities from the signed finalizing evidence, reopens only via the stable `auditNamespaceAuthority`, compares parent and child identities, and calls `scrubAndRemoveAuthenticatedAuditRoots`. No path-only cleanup/absence check may authorize completed. Decoy/missing-in-untrusted/mismatch remains nonterminal; no provider/test rerun or hot loop.
4. Live cleanup after finalizing uses the same authenticated identity function. Failure/timedout/cancelled cleanup uses live captured identities and cannot silently fall back to path-only cleanup.
5. Callback validation proves exact signed identities are absent under stable trusted parents; recreating same path with different inode rejects callback. Multi-attempt + shared roots covered.
6. Remove/dead-end legacy path-only helpers from any completion-authorizing production call path; tests may only use them for explicit rejection/legacy cleanup that cannot complete.

RED/GREEN regressions:
- finalizing evidence tamper/reorder/omit one role/duplicate path/parent identity mutation all reject;
- each root renamed + decoy after finalizing: no completed/callback, decoy survives, original not claimed absent; restart same;
- recreated output/path after completed with different inode rejects callback;
- multi-attempt includes every attempt root and shared session/work exactly once;
- path-only finalizing event cannot recover/complete;
- count=10, race=3, full package.

Keep journal migration minimal and authenticated. Report any truly necessary schema migration separately.