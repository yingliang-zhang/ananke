Implement P6 Slice-3 Repair B in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`, continuing the completed Repair A. No commit. Preserve accepted Slices 1–2 and Repair A. Contract-only: no SQLite/store/fsync/process/runtime code.

Review blocker to close: current recovery classification trusts public `SupervisorRecoveryAuthority`, public `SupervisorRecoveryObservation.Ambiguous`, public record `Complete`, and caller-matched kind/hash arrays. Invented hashes plus `Complete=true` classify as durable response replay. Replace this trust model, not merely add more comparisons.

Required architecture:
1. Define a closed canonical recovery record schema (v1) with RFC 8785/JCS self-hash. Every record must bind at least:
   - schema version, record ID/hash, exact ordered sequence and record kind;
   - attempt hash/number/cap, phase/claim hash, unique slot ID;
   - supervisor boot epoch hash, journal head hash;
   - predecessor record hash (empty only for the first record);
   - exact `FULL+fullfsync` durability policy;
   - canonical UTC occurrence time;
   - a bounded semantic payload hash appropriate to the record.
   Reject unknown/duplicate/trailing/noncanonical JSON, malformed hashes/identifiers/timestamps, wrong sequence/kind/predecessor, and record-hash mismatch.
2. `ClassifySupervisorRecovery` must accept only an opaque `VerifiedSupervisorRecoverySnapshot` (or equivalently named proof) plus requested status/replay action. Remove public caller booleans as trust assertions.
3. The opaque snapshot must have all fields private and recheck integrity on every call. It must retain deep-copied canonical record bytes and bind:
   - current and recorded boot epochs;
   - exact attempt/phase/claim/slot/journal lineage;
   - cut point and exact durable ordered prefix;
   - private journal verification, durability verification, completeness verification, and ambiguity-check results;
   - canonical record hashes/bytes and snapshot integrity hash.
   No production constructor/decoder may mint it from claim/request/fixture bytes. Future trusted journal verification must live in trusted in-package code or a separately reviewed seam. Test-only `_test.go` minters are allowed.
4. Before-claim-commit zero-record recovery still requires an opaque verified empty-journal observation proving completeness and non-ambiguity; a nil snapshot or public empty slice must fail.
5. Remove or stop exposing public `SupervisorRecoveryAuthority`, `SupervisorRecoveryObservation`, caller `Complete`, and caller `Ambiguous` as accepted classifier inputs. Public record values may exist as canonical data types, but cannot establish durability without the opaque snapshot.
6. Keep no successful launch/effect action. Export only legitimate `status_only` and `replay_response` actions; invalid launch/effect strings exist only in tests and always reject. Every successful result has `EffectAllowed=false`.
7. Preserve exact 10-cut-point outcomes:
   - prior-epoch nonterminal committed/started states -> waiting_for_human;
   - current-epoch nonterminal -> status-only with live commit confirmation required;
   - terminal proof/signature states -> status requirements only;
   - fully persisted response -> response replay only/no further effect.
8. Add real RED/GREEN adversarial probes for:
   - invented current/recorded boot hashes and five unrelated hashes;
   - forged `Complete=true`/non-ambiguous assertions (should become unrepresentable as accepted input);
   - nil/zero opaque snapshot;
   - mutation of every private proof flag/identity/canonical byte/hash;
   - truncated/missing/unknown/duplicate/out-of-order canonical records;
   - wrong attempt/phase/claim/slot/boot/journal/predecessor/durability/payload/time;
   - one record or slot chain aliased into another attempt/phase;
   - invalid launch and response+effect requests;
   - response replay exactness and no second effect.
9. Use ordered tables only. Every new/renamed vector ID must map to a named executable semantic probe; no decorative probes.
10. Update normative prose and machine contract from Repair-A-open to Repair-A+B candidate pending independent frozen-source review. Include recovery record schema version and deterministic canonical recovery fixture record hashes/canonical SHA-256s. Update exact vector count/order and retain no Slice ACCEPT/full-P6/runtime claim.

TDD/gates:
- RED blocker reproduction before implementation;
- focused recovery/schema/opaque proof tests and full vector registry;
- all Slice-3 tests; package single; count=10; race count=3; vet; gofmt; diff-check.

Return exact RED/GREEN evidence and changed files. Do not create cron jobs or call a real provider.
