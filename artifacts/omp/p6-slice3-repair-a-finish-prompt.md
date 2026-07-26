Continue exact-session P6 Slice-3 Repair A in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. The prior run timed out after code landed. Do not start Repair B.

Current focused failures:
1. `TestP6Slice3RepairAStaleExactReplayCapabilityBoundary/N-1ns`: capability nil.
2. Normative document still has old 64 vectors/hashes while compiled contract now has 73 vectors/new hashes.

Root cause for N-1: attempt-1 claim 1 has `not_after=12:05:10`, but GUI approval was at `12:00:00` and `MaxApprovalAge=5m`; therefore claim N-1 is already 9.999999999s beyond embedded authorization effect freshness. Do not weaken freshness or change the N-1 expectation.

Required finish:
1. In production authority validation, require that the claim's last representable live instant (`not_after - 1ns`) still passes the complete effect-time dispatch/authorization/release freshness validation. This prevents publishing a claim lifetime whose tail can never mint capability.
2. In canonical test fixtures, compute each claim `not_after` as the exclusive intersection of:
   - `created_at + MaxSupervisorIntentLifetime`;
   - dispatch exclusive expiry;
   - authorization exclusive expiry;
   - approval-age valid through `approved_at + MaxApprovalAge`, represented as an exclusive horizon one nanosecond later.
   Use deterministic UTC nanosecond timestamps. Reject non-positive intersections.
3. Recompute canonical claim and canonical-byte hashes naturally; do not hard-code old values.
4. Re-run N-1/N/N+1 and stale authorization/dispatch/release probes.
5. Synchronize `docs/experiments/p6-controlled-repair-supervisor-intent.md` machine contract and prose to exactly 73 ordered vectors and current fixture hashes. Keep status candidate/pending review and explicitly leave recovery Repair B open.
6. Ensure all Repair-A opaque slot/commit/terminal-event mutation and alias tests pass. Use ordered tables only.
7. Run focused Slice3/RepairA/document/registry, package single, vet, gofmt, diff-check. If time permits count=10 and race count=3; otherwise leave those for orchestrator verification.

Return actual results and changed files. No commit, provider call, or cron job.
