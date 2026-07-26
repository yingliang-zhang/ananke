Finish P6 Slice-3 Repair B in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. The 900s run timed out after the full code/tests landed. No more design or API changes unless needed to make existing tests compile. Do not start another slice.

Verified current state:
- `TestP6Slice3RepairB` plus the executable registry PASS in 3.304s.
- Repair-B tests count=10 PASS in 8.010s.
- Searches find no remaining `SupervisorRecoveryAuthority`, `SupervisorRecoveryObservation`, public `Complete`, or public `Ambiguous` inputs.
- The only current focused failure is `TestP6Slice3NormativeDocumentMatchesTypesFixturesAndInventory`: document still contains Repair-A 73-vector machine contract; compiled expected machine contract is Repair-A+B status, recovery schema v1, actions `[status_only,replay_response]`, deterministic five-record recovery fixture, and 99 vectors.

Required finish only:
1. Replace the normative machine-contract JSON block in `docs/experiments/p6-controlled-repair-supervisor-intent.md` with the exact `want` value emitted by the current document test.
2. Update surrounding prose from Repair-A-open to Repair-A+B candidate pending independent frozen-source review:
   - describe canonical self-hashed recovery records and opaque private integrity-checked verified recovery snapshot;
   - no production constructor/decoder mints durability authority;
   - zero-record observation still requires verified complete unambiguous journal snapshot;
   - only status-only / exact response replay actions; all effect flags false;
   - retain explicit no runtime/store/fsync/launch/attestation-signing claim and no Slice ACCEPT/full-P6 claim.
3. Ensure exact 99 ordered vector IDs, current claim fixture hashes, recovery fixture record hashes/canonical hashes, schema version, actions, and status match the machine-parsed test.
4. Run document test, all Slice-3/RepairB/registry, package single, count=10, race count=3, vet, gofmt, diff-check. If repeated gates exceed time, prioritize document + all Slice-3 + package single + vet and report omitted repetition for orchestrator.

Return actual results. No commit, provider call, or cron job.
