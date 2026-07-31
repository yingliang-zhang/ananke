# P6 Controlled Repair — Pre-release Schema/API Cutover Contract

> **Slice 9 of the P6 controlled-repair contract-first sequence.** This slice
> freezes the pre-release schema/API cutover boundary before any runtime or
> storage implementation begins.

## Objective

Because v15/v16 store migrations and `internal/repairrunner` are uncommitted and
unreleased, this contract:

1. **Binds** the accepted store schema version (14) to the accepted P6 contract
   (Slices 1–8). Any DB at v15/v16 is foreign and must be rejected during
   migration instead of interpreted.
2. **Removes** rejected exported effect/evidence APIs from the accepted contract.
3. **Rejects** any populated local rejected-schema DB during migration.
4. **Proves** production binaries contain no in-process adapter, arbitrary-test
   runner, rejected API marker, or reused P5 protocol key.

## Trust boundary

| Component | Trusted authority | Explicitly absent authority |
|---|---|---|
| Schema cutover contract | frozen release-pinned cutover authority, accepted store schema version binding, accepted contract schema binding, rejected schema/foreign API/binary marker inventory | store migration execution, DB interpretation, runtime launch, effect authority |

## Canonical record

`SchemaCutoverRecord` is a canonical, closed, self-hashed RFC 8785/JCS record.
State is always exactly `cutover_accepted`. Raw DB insertion cannot produce this
record — it can only be minted by the evaluator after full verification.

The record binds:

- Accepted store schema version (14) and its hash.
- Ordered accepted contract schema versions (Slices 1–8) and their aggregate hash.
- Rejected store schema versions (v15, v16) and their aggregate hash.
- Rejected API markers and their aggregate hash.
- Forbidden binary markers and their aggregate hash.
- Forbidden P5 protocol adapter SPKI hash (must differ from accepted repair
  attestor leaf SPKI).
- Accepted repair attestor leaf SPKI (from release pins).
- Release pins hash and frozen cutover authority hash.
- Verification timestamp.

## Seals

Six verification seals follow the Slice 4/8 pattern:

1. `store_schema_binding_verification` — accepted store schema version matches
   frozen authority.
2. `contract_schema_binding_verification` — accepted contract schemas match
   frozen authority.
3. `rejected_schema_foreign_verification` — rejected schema versions are foreign.
4. `rejected_api_absence_verification` — rejected API markers are absent.
5. `binary_purity_verification` — forbidden binary markers are absent.
6. `protocol_key_separation_verification` — P5 protocol adapter SPKI hash
   differs from accepted repair attestor leaf SPKI.

Each seal is self-hashed with kind-specific evidence, the aggregate seals hash
is the SHA-256 of the canonical ordered array, and
`verifiedSchemaCutoverRecordIntact` recomputes every seal from the decoded
record under the frozen cutover authority.

## Evaluator

`EvaluateSchemaCutover` first re-establishes fresh `VerifyReleaseTrust`, derives
the frozen cutover authority, validates the snapshot intact (all seals
recomputed from decoded canonical under frozen authority), cross-binds the
record's values to the frozen authority and release pins, and mints an opaque
`VerifiedSchemaCutoverCapability` only if state is exactly `cutover_accepted` and
all bindings match. `EffectAllowed` is always `false`; no production minter
exists.

## Negative vectors

- Wrong store schema version (v15/v16 accepted as current) rejects.
- Foreign accepted contract schemas reject.
- Extra rejected schema versions reject.
- Extra rejected API markers reject.
- Extra forbidden binary markers reject.
- Protocol key reuse (P5 SPKI == repair attestor SPKI) rejects.
- Foreign cutover authority hash rejects.
- Unknown state rejects.
- Unknown action rejects.
- Cutover hash mismatch rejects.
- Capability mutation (valid bit, cutover hash, authority hash, seals hash,
  store schema version, release pins hash, canonical bytes) breaks intactness.

## Pure-contract scope

No filesystem access, Git execution, process launch, network operation,
store/SQLite operation, migration, DB interpretation, cleanup, deletion, or
automatic repair anywhere in production code. All effects are classification-only
with `EffectAllowed == false`.

<!-- BEGIN P6 SLICE 9 MACHINE CONTRACT -->
```json
{
  "schema_version": "ananke.controlled-repair-schema-cutover-document.v1",
  "status": "slice_9_candidate_pending_independent_frozen_source_review",
  "observation_schema_version": "ananke.controlled-repair-schema-cutover.v1",
  "prior_slice_1_to_2_vector_count": 91,
  "prior_slice_3_vector_count": 99,
  "prior_slice_4_vector_count": 258,
  "prior_slice_5_vector_count": 40,
  "prior_slice_6_vector_count": 46,
  "prior_slice_7_vector_count": 24,
  "prior_slice_8_vector_count": 22,
  "slice_9_vector_count": 27,
  "effect_allowed_values": [
    false
  ],
  "allowed_actions": [
    "admit_cutover",
    "status_only"
  ],
  "cutover_states": [
    "cutover_accepted"
  ],
  "verification_kinds": [
    "store_schema_binding_verification",
    "contract_schema_binding_verification",
    "rejected_schema_foreign_verification",
    "rejected_api_absence_verification",
    "binary_purity_verification",
    "protocol_key_separation_verification"
  ],
  "dispositions": [
    "record_ready",
    "retained_status"
  ],
  "requirements": [
    "cutover_accepted_no_effect",
    "no_further_effect_permitted"
  ],
  "cutover_authority": {
    "schema_version": "ananke.controlled-repair-schema-cutover-authority.v1",
    "cutover_id": "controlled_repair_schema_cutover_v1",
    "cutover_authority_hash": "sha256:34ad5b6cacfdcdf7011aa2cf4f16b197ea902a8c546c14970f64049853017dd7",
    "release_pins_hash": "sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a",
    "accepted_store_schema_version": 14,
    "accepted_contract_schemas": [
      "ananke.controlled-repair-contract-fixture.v1",
      "ananke.controlled-repair-release-pins.v1",
      "ananke.controlled-repair-trust-bundle.v1",
      "ananke.controlled-repair-authorization.v1",
      "ananke.controlled-repair-immutable-dispatch.v1",
      "ananke.controlled-repair-supervisor-intent-claim.v1",
      "ananke.controlled-repair-supervisor-attempt-identity.v1",
      "ananke.controlled-repair-repository-worktree-observation.v1",
      "ananke.controlled-repair-repository-worktree-verifier-authority.v1",
      "ananke.controlled-repair-adapter-sandbox-observation.v1",
      "ananke.controlled-repair-adapter-sandbox-verifier-authority.v1",
      "ananke.controlled-repair-go-test-profile.v1",
      "ananke.controlled-repair-test-sandbox-verifier-authority.v1",
      "ananke.controlled-repair-review-attestation.v1",
      "ananke.controlled-repair-attestation-verifier-authority.v1",
      "ananke.controlled-repair-ananke-verification.v1",
      "ananke.controlled-repair-ananke-verifier-authority.v1"
    ],
    "rejected_schema_versions": [
      15,
      16
    ],
    "rejected_api_markers": [
      "repairrunner_effect_dispatch",
      "repairrunner_evidence_persist",
      "unsigned_review_evidence_persist",
      "in_process_adapter_launch",
      "arbitrary_test_runner_launch"
    ],
    "forbidden_binary_markers": [
      "in_process_adapter",
      "arbitrary_test_runner",
      "rejected_api_marker",
      "reused_p5_protocol_key"
    ],
    "forbidden_protocol_adapter_spki_hash": "sha256:992b7a6da10ae5bdbffca27986dc3e7d29b05472e417bf12e8218d8751b25ab7",
    "accepted_repair_attestor_leaf_spki": "sha256:79392ee343c0e72e6433fefd50c6faff129ad9a4956b73f00c3bda266af8b493",
    "verification_kinds": [
      "store_schema_binding_verification",
      "contract_schema_binding_verification",
      "rejected_schema_foreign_verification",
      "rejected_api_absence_verification",
      "binary_purity_verification",
      "protocol_key_separation_verification"
    ]
  },
  "canonical_fixture": {
    "cutover_hash": "sha256:6c0271e669b92a7e063e541abf6a5b60628ef0e900f78004bc34b8615d7fb89f",
    "canonical_sha256": "sha256:d20df91058cc1be61245489aeec1b68cd18db8bc70fb4b854c7a99b8542501e3",
    "record_integrity_hash": "sha256:f394d6bea7ea242e01af600ce4d2fb2e5e0e5ec45d6d36e9790917067b9f73e7",
    "accepted_store_schema_version": 14,
    "release_pins_hash": "sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a",
    "forbidden_protocol_adapter_spki_hash": "sha256:992b7a6da10ae5bdbffca27986dc3e7d29b05472e417bf12e8218d8751b25ab7",
    "accepted_repair_attestor_leaf_spki": "sha256:79392ee343c0e72e6433fefd50c6faff129ad9a4956b73f00c3bda266af8b493",
    "state": "cutover_accepted"
  },
  "vector_ids": [
    "canonical_cutover",
    "opaque_snapshot_deep_copy",
    "opaque_snapshot_mutation_isolation",
    "wrong_state_rejects",
    "wrong_cutover_id_rejects",
    "wrong_store_schema_version_rejects",
    "wrong_release_pins_rejects",
    "wrong_trust_bundle_rejects",
    "wrong_accepted_contract_schemas_rejects",
    "wrong_rejected_schema_versions_rejects",
    "wrong_rejected_api_markers_rejects",
    "wrong_forbidden_binary_markers_rejects",
    "protocol_key_reuse_rejects",
    "foreign_cutover_rejects",
    "status_only_returns_retained_status",
    "canonical_json_closure",
    "capability_mutation_valid_bit",
    "capability_mutation_cutover_hash",
    "capability_mutation_cutover_authority_hash",
    "capability_mutation_cutover_seals_hash",
    "capability_mutation_accepted_store_schema_version",
    "capability_mutation_release_pins_hash",
    "capability_mutation_canonical_bytes",
    "frozen_cutover_authority_deterministic",
    "unknown_state_rejects",
    "unknown_action_rejects",
    "cutover_hash_mismatch_rejects"
  ]
}
```
<!-- END P6 SLICE 9 MACHINE CONTRACT -->
