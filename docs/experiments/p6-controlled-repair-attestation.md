# P6 Controlled Repair — Canonical Repair-Review Attestation (Slice 7)

> **Status:** slice_7_candidate_pending_independent_frozen_source_review

## Scope

This document defines the canonical repair-review attestation contract — the
terminal record that the supervisor signs after all phases (Slices 1–6) complete.
It binds every predecessor hash from trust bootstrap through test sandbox closure,
plus runtime-only facts (patch metadata, transport nonces, test results). The
attestation state is exactly `waiting_for_review`. The Ed25519 signature domain
is `ananke.controlled-repair.review-attestation.v1`.

This is a pure contract layer: no Ed25519 key, no filesystem, no process, no
network, no store, no GUI, and no runtime effect. The signature field is a
placeholder in the contract layer; actual signing is a future runtime step.

## Observation schema

The `RepairReviewAttestation` record (RFC 8785/JCS canonical) contains:

- **Self-hash and identity:** attestation hash, attestation ID, issued-at timestamp, state
- **Signature:** signature domain, signature (placeholder)
- **Trust (Slice 1):** release pins hash, trust bundle hash, repair attestor certificate/root/leaf SPKI
- **Transport:** request/response nonce hashes, channel hash
- **Authorization (Slice 2):** authorization/approval/request/dispatch/attempt hashes, attempt number/cap, effect-time validation timestamp
- **Phase claims (Slice 3):** materialization/adapter/test claim hashes, predecessor claim hash, supervisor journal head/predecessor hashes, boot epoch ID/hash
- **Repository (Slice 4):** repository binding/identity hashes, common-git/git-executable identity hashes, worktree parent/target/admin/descriptor hashes, worktree slot ID/path-hash, installed worktree root identity hash
- **Adapter (Slice 5):** adapter seatbelt profile hash, sandbox hash, terminal proof hash, capability hash, UID pool/lease hashes, UID, group ID
- **Patch:** patch hash/size, ordered paths hash, status/raw/numstat/ignored/filesystem-scan hashes
- **Tests (Slice 6):** toolchain manifest hash, test profile hash, candidate copy hash, test sandbox/terminal-proof/root-cleanup hashes, test result/output/command hashes, test capability hash

## Verification seals

Seven self-hashed verification seals bind the attestation to each binding domain:

1. `trust_binding_verification` — release pins, trust bundle, attestor certificate
2. `authorization_binding_verification` — authorization, approval, request, dispatch, attempt
3. `phase_claim_binding_verification` — materialization, adapter, test claim hashes, predecessor, journal, boot epoch
4. `repository_binding_verification` — repository binding, identity, common-git, git executable, worktree slot
5. `adapter_binding_verification` — seatbelt profile, sandbox, terminal proof, capability, UID pool/lease
6. `test_binding_verification` — toolchain, test profile, candidate copy, sandbox, terminal proof, root cleanup, test results
7. `attestation_integrity_verification` — attestation hash, state, signature domain, patch metadata, transport, issued-at

## Frozen compiled values

- `AttestationVerifierAuthority` — ID `controlled_repair_attestation_verifier_v1`, 7 verification kinds, release-pins hash bound, signature domain bound. Uses `mustDerive...` panic-on-mismatch init.

## Evaluator

`EvaluateAttestation` first re-establishes fresh `VerifyReleaseTrust`, derives the
frozen verifier authority, validates test-claim authority (phase=test_claim,
sequence=3), checks all predecessor capabilities intact (worktree, adapter sandbox,
test sandbox), validates the attestation snapshot, and mints an opaque
`VerifiedAttestation` capability only if state is exactly `waiting_for_review`
and all bindings match. `EffectAllowed` is always `false`; no production minter exists.

## Machine contract

<!-- BEGIN P6 SLICE 7 MACHINE CONTRACT -->
```json
{
  "schema_version": "ananke.controlled-repair-attestation-document.v1",
  "status": "slice_7_candidate_pending_independent_frozen_source_review",
  "observation_schema_version": "ananke.controlled-repair-review-attestation.v1",
  "prior_slice_1_to_2_vector_count": 91,
  "prior_slice_3_vector_count": 99,
  "prior_slice_4_vector_count": 258,
  "prior_slice_5_vector_count": 40,
  "prior_slice_6_vector_count": 46,
  "slice_7_vector_count": 24,
  "effect_allowed_values": [
    false
  ],
  "allowed_actions": [
    "admit_for_review",
    "status_only"
  ],
  "attestation_states": [
    "waiting_for_review"
  ],
  "verification_kinds": [
    "trust_binding_verification",
    "authorization_binding_verification",
    "phase_claim_binding_verification",
    "repository_binding_verification",
    "adapter_binding_verification",
    "test_binding_verification",
    "attestation_integrity_verification"
  ],
  "dispositions": [
    "capability_ready",
    "retained_status"
  ],
  "requirements": [
    "next_verification_phase",
    "no_further_effect_permitted"
  ],
  "verifier_authority": {
    "schema_version": "ananke.controlled-repair-attestation-verifier-authority.v1",
    "verifier_id": "controlled_repair_attestation_verifier_v1",
    "verifier_authority_hash": "sha256:683582aaf5e1a61d339aa178d9879e72c1438ff420dcdd2aaf571ba72fb1ab54",
    "release_pins_hash": "sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a",
    "signature_domain": "ananke.controlled-repair.review-attestation.v1",
    "verification_kinds": [
      "trust_binding_verification",
      "authorization_binding_verification",
      "phase_claim_binding_verification",
      "repository_binding_verification",
      "adapter_binding_verification",
      "test_binding_verification",
      "attestation_integrity_verification"
    ]
  },
  "canonical_fixture": {
    "attestation_hash": "sha256:3114b615a9dbdd95e516be0e052d56fc0eb09ff44d4e34c58734e4389f135dbc",
    "canonical_sha256": "sha256:a9757985b54015e9eadea22e92d9536de67937b0fcab251dd8995c38a0c07764",
    "snapshot_integrity_hash": "sha256:870bda9e4d7ac78b78394cf0b0f5c08f0a6516680c23534edd22ed83128652a2",
    "authorization_hash": "sha256:a4ebf38cc021a2c93f7ac2dc693744e2a7e3c6928f8a26d80a9ba501d00b2a11",
    "test_claim_hash": "sha256:d955a6dbc53b8bd0be79347e8c60024c0e88d09febddccc8e86aca631ee78bfa",
    "adapter_claim_hash": "sha256:9fc9223dbe7dc06cbfc9c6bbdb5642c4ad5a6c40c9a36d0a61f29e49ba5259cc",
    "materialization_claim_hash": "sha256:45ff41731f981cce4d5c38397b588c4c4f42f2f01c01cc315f0410dc1768bc27",
    "repository_binding_hash": "sha256:51202788cff44e7f9dc4625006dede96cee255aef8249b3bb623efd59228f3a8",
    "adapter_capability_hash": "sha256:9653538f232d30baee7b49b6cf3ad9df303dd6b57767278564cb2c31670fb725",
    "test_capability_hash": "sha256:039ec570c3acf62faef5e4005f6509ecdc9da60f4caf302d86a815e2e24bd086",
    "signature_domain": "ananke.controlled-repair.review-attestation.v1",
    "state": "waiting_for_review"
  },
  "vector_ids": [
    "canonical_attestation",
    "opaque_snapshot_deep_copy",
    "opaque_snapshot_mutation_isolation",
    "wrong_state_rejects",
    "wrong_signature_domain_rejects",
    "wrong_trust_bundle_hash_rejects",
    "wrong_authorization_hash_rejects",
    "wrong_repository_binding_hash_rejects",
    "wrong_adapter_capability_hash_rejects",
    "wrong_test_capability_hash_rejects",
    "status_only_returns_retained_status",
    "canonical_json_closure",
    "capability_mutation_valid_bit",
    "capability_mutation_attestation_hash",
    "capability_mutation_verifier_authority_hash",
    "capability_mutation_authorization_hash",
    "capability_mutation_test_claim_hash",
    "capability_mutation_adapter_capability_hash",
    "capability_mutation_test_capability_hash",
    "capability_mutation_canonical_bytes",
    "frozen_verifier_authority_deterministic",
    "unknown_state_rejects",
    "unknown_action_rejects",
    "attestation_hash_mismatch_rejects"
  ]
}
```
<!-- END P6 SLICE 7 MACHINE CONTRACT -->
