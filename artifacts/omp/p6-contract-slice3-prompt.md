Implement P6 Contract Slice 3 (supervisor intent, phase claims, and recovery contract only) in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` with strict RED→GREEN TDD. No commit.

Accepted baseline:
- HEAD `20f224c8e64435d987b2877b8d45fe4f487807f7`.
- Contract Slices 1–2 are independently accepted; final evidence is `artifacts/omp/p6a/contract-slices1-2-closure-review-3.md` and the normative plan is `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md:71-82`.
- Preserve every existing release/authorization/replay behavior and 91-vector inventory.

Scope and hard non-goals:
- Edit/create only `internal/repaircontract/**` and a new normative experiment document `docs/experiments/p6-controlled-repair-supervisor-intent.md`.
- Contract/types/canonical fixtures/verifier/classifier/tests only.
- No SQLite/store migration, file open/write/fsync implementation, socket/transport, process launch, worktree/Git effect, sandbox, UID, private key, signature generation, production command, or trustedsupervisor changes.
- Never infer trusted journal/boot/runtime identity from claim bytes. Fixture helpers are test oracles only.

Freeze exactly three phases and one slot per `(attempt_hash, phase)`:
1. `materialization_claim` before any common-.git/worktree mutation.
2. `adapter_claim` before any adapter spawn.
3. `test_claim` before any test-root creation/spawn.

Each canonical immutable claim must bind at minimum:
- schema/version, claim ID/hash, exact phase, sequence 1/2/3, attempt hash and attempt number/cap;
- authorization hash, policy hash, approval hash, full P4 fact/fence hashes, immutable request/dispatch hash, channel binding and peer identity hash;
- predecessor claim hash and predecessor terminal-event hash where required;
- supervisor boot epoch identity/hash supplied by trusted external authority;
- repository/base identity, executable identity, sandbox profile identity, namespace/root identity for the relevant phase;
- durability policy ID exactly `FULL+fullfsync` (or a clearer closed constant with the same required semantics);
- created-at/not-after and complete record hash.

External authority and opaque proof:
- Introduce a closed `SupervisorIntentAuthority` supplied by the trusted supervisor/journal from independently verified boot, journal head/slot, installed executable/sandbox/namespace, repository and accepted authorization/request state.
- Do not provide any production helper that derives this authority from claim/request/fixture bytes.
- Verify claims against the existing opaque `VerifiedAuthorization` and external authority.
- If an opaque verified-claim capability is used, private fields, deep copies, canonical-byte integrity, exact predecessor, and mutation isolation are required.
- Attempt 2 starts a distinct chain bound to its new authorization/approval; no claim from attempt 1 can be predecessor or occupy attempt-2 slots.

Admission/replay/recovery semantics:
- A new phase may be admitted only when its trusted slot is empty and exact predecessor/terminal event is present as required.
- Exact canonical-byte replay of an already committed slot returns exact-replay/no-effect; any byte or semantic difference is conflict/no-effect.
- Duplicate callers perform zero effects. No boolean/caller claim can grant launch permission.
- Contract API must not expose a recovery disposition that authorizes launch.
- A recovery sweep of pending/claimed work performs zero effects. Any prior-boot-epoch nonterminal committed claim classifies as `waiting_for_human` with zero effect.
- Current-epoch effect authority is not reconstructed by replay/recovery classification; only a future live executor that receives actual journal commit confirmation may launch. Document this later runtime obligation without faking it in this slice.

Executable crash matrix:
Freeze and test ordered cut points before/after:
- claim commit;
- phase launch;
- terminal proof persistence;
- attestation signature persistence;
- response persistence.
For every restart/recovery input, assert exact durable observation, disposition, `effect_allowed=false`, and next human/status requirement. Before claim commit has no durable claim/effect; prior-epoch nonterminal states become waiting-for-human; terminal persisted response only replays response with no new effect. Missing/truncated/unknown/duplicate/out-of-order journal observations fail closed.

Required negative vectors, all executable and consistently rehashed:
- wrong/missing/duplicate phase, wrong sequence, duplicate slot, same slot changed bytes;
- swapped authorization/approval/P4/fence/request/dispatch/channel/peer/policy;
- wrong boot epoch or prior-epoch live claim;
- wrong repo/executable/sandbox/namespace identity;
- missing/wrong predecessor claim or terminal event;
- attempt-1 claim reused in attempt 2, attempt-2 chain with old approval;
- noncanonical JSON, unknown/duplicate key, changed timestamp/lifetime;
- recovery sweep tries to produce launch, ambiguous/truncated crash observation, response replay that produces a second effect.

TDD workflow:
1. Add RED tests and show expected failures before production implementation.
2. Implement minimal GREEN with stable sentinel errors and RFC 8785/JCS canonical bytes.
3. Add a separate ordered executable Slice-3 vector registry with canonical ID inventory and execution/order completeness checks; do not alter the accepted Slice 1–2 registry except where a shared helper is truly necessary.
4. Add canonical attempt-1 and attempt-2 claim-chain fixtures or deterministic constructors; fixture is never authority.
5. Keep docs/types/fixtures/vector inventory exact and machine-checkable.
6. Run focused tests, package single, count=10, race count=3, vet, gofmt, diff-check.

Return exact RED/GREEN commands/results, changed files, and unresolved obligations. Do not claim Slice ACCEPT; an independent frozen-source hard review follows. Do not create cron jobs.
