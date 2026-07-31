# P6 Controlled Repair — Ananke Verification and Persistence (Slice 8)

> **Status:** slice_8_candidate_pending_independent_frozen_source_review

## Scope

This document defines the Ananke-side verification and persistence contract —
what Ananke does when it receives a signed repair-review attestation from the
supervisor (Slice 7). Ananke verifies the signature against release-pinned
trust, checks signer role/certificate/root validity, validates delivery
freshness, channel, request binding, and head consistency, and produces a
durable, re-verifiable record with state exactly `waiting_for_review`.

This is a pure contract layer: no Ed25519 signature verification, no SQLite
persistence, no GUI, and no runtime effect. The contract defines the rules;
the runtime implements them.

## Verification kinds

Seven self-hashed verification seals bind the record to each verification domain:

1. `signature_verification` — attestation hash, signature domain
2. `signer_role_verification` — repair attestor certificate, root ID, leaf SPKI
3. `certificate_validity_verification` — certificate hash, verified-at timestamp
4. `freshness_verification` — attestation issued-at, freshness checked-at
5. `channel_verification` — request/response nonce hashes, channel hash
6. `request_binding_verification` — authorization hash, attempt hash, attempt number
7. `head_consistency_verification` — supervisor journal head hash

## Frozen compiled values

- `AnankeVerifierAuthority` — ID `controlled_repair_ananke_verifier_v1`, 7 verification
  kinds, release-pins hash bound, signature domain bound. Uses `mustDerive...`
  panic-on-mismatch init.

## Evaluator

`EvaluateAnankeVerification` first re-establishes fresh `VerifyReleaseTrust`,
derives the frozen Ananke verifier authority, checks the attestation capability
intact, verifies signer role and certificate validity against release pins,
checks freshness, channel, request binding, and head consistency, and mints an
opaque `VerifiedAnankeCapability` only if state is exactly `waiting_for_review`
and all bindings match. `EffectAllowed` is always `false`.

Every read re-verifies the signature against release pins; raw DB insertion
cannot produce an accepted state — the capability can only be minted by the
evaluator after full verification.

## Machine contract

<!-- BEGIN P6 SLICE 8 MACHINE CONTRACT -->
```json
{
  "schema_version": "ananke.controlled-repair-ananke-verification-document.v1",
  "status": "slice_8_candidate_pending_independent_frozen_source_review",
  "observation_schema_version": "ananke.controlled-repair-ananke-verification.v1",
  "prior_slice_1_to_2_vector_count": 91,
  "prior_slice_3_vector_count": 99,
  "prior_slice_4_vector_count": 258,
  "prior_slice_5_vector_count": 40,
  "prior_slice_6_vector_count": 46,
  "prior_slice_7_vector_count": 24,
  "slice_8_vector_count": 22,
  "effect_allowed_values": [
    false
  ],
  "allowed_actions": [
    "admit_for_review",
    "status_only"
  ],
  "verification_states": [
    "waiting_for_review"
  ],
  "verification_kinds": [
    "signature_verification",
    "signer_role_verification",
    "certificate_validity_verification",
    "freshness_verification",
    "channel_verification",
    "request_binding_verification",
    "head_consistency_verification"
  ],
  "dispositions": [
    "record_ready",
    "retained_status"
  ],
  "requirements": [
    "human_review_required",
    "no_further_effect_permitted"
  ],
  "verifier_authority": {
    "schema_version": "ananke.controlled-repair-ananke-verifier-authority.v1",
    "verifier_id": "controlled_repair_ananke_verifier_v1",
    "verifier_authority_hash": "sha256:c859e1e5ef48757ad23da70e80fc60f99606af72ee1335b49f65c8092fc9a227",
    "release_pins_hash": "sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a",
    "signature_domain": "ananke.controlled-repair.review-attestation.v1",
    "verification_kinds": [
      "signature_verification",
      "signer_role_verification",
      "certificate_validity_verification",
      "freshness_verification",
      "channel_verification",
      "request_binding_verification",
      "head_consistency_verification"
    ]
  },
  "canonical_fixture": {
    "verification_hash": "sha256:19bc6c404ca73cd8e5d2238cfedf6c559084dabc4d5ae8e7b3e9f465f2f78e18",
    "canonical_sha256": "sha256:7ca1b4463cad7f621c6f8a30def9bf29de582a5cce29fc778f8a7eedd0310790",
    "record_integrity_hash": "sha256:0480d14366832e4d371c87f35edeec53475e4e08da62df49ab19dab562175374",
    "attestation_hash": "sha256:3114b615a9dbdd95e516be0e052d56fc0eb09ff44d4e34c58734e4389f135dbc",
    "authorization_hash": "sha256:a4ebf38cc021a2c93f7ac2dc693744e2a7e3c6928f8a26d80a9ba501d00b2a11",
    "signature_domain": "ananke.controlled-repair.review-attestation.v1",
    "state": "waiting_for_review"
  },
  "vector_ids": [
    "canonical_verification",
    "opaque_snapshot_deep_copy",
    "opaque_snapshot_mutation_isolation",
    "wrong_state_rejects",
    "wrong_signer_rejects",
    "wrong_release_pins_rejects",
    "wrong_trust_bundle_rejects",
    "wrong_authorization_hash_rejects",
    "foreign_attestation_rejects",
    "status_only_returns_retained_status",
    "canonical_json_closure",
    "capability_mutation_valid_bit",
    "capability_mutation_verification_hash",
    "capability_mutation_verifier_authority_hash",
    "capability_mutation_attestation_hash",
    "capability_mutation_authorization_hash",
    "capability_mutation_verification_seals_hash",
    "capability_mutation_canonical_bytes",
    "frozen_verifier_authority_deterministic",
    "unknown_state_rejects",
    "unknown_action_rejects",
    "verification_hash_mismatch_rejects"
  ]
}
```
<!-- END P6 SLICE 8 MACHINE CONTRACT -->
