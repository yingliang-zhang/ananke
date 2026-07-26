# P6 Controlled Repair Supervisor Intent, Phase Claim, and Recovery Contract

Status: **Repair A+B candidate pending independent frozen-source review**. This document is normative for the Slice-3 supervisor-intent and recovery-contract boundary. It does not claim Slice 3 acceptance, full P6 design acceptance, trusted journal implementation, or runtime authorization.

## Scope boundary

Repairs A and B freeze pure supervisor-intent claim types, RFC 8785/JCS verification, opaque slot-commit and predecessor terminal-event capabilities, stale exact-replay gating, a closed canonical recovery-record schema, an opaque verified recovery snapshot, deterministic fixture oracles, and a separate ordered executable vector registry. This slice performs no SQLite/store migration, journal write, file open/write/fsync, socket or transport operation, process launch, worktree or Git effect, sandbox operation, UID operation, private-key operation, attestation signing, signature generation, production command, or `trustedsupervisor` change.

The accepted Contract Slices 1–2 surface remains unchanged. Its independent registry remains exactly 91 vectors. Slice 3 has a separate 99-vector registry; it does not append to or reinterpret the accepted registry.

## External authority boundary

`SupervisorIntentAuthority` contains expected immutable phase/slot identifiers plus independently accepted authorization, installation, repository, boot-epoch, and journal-head state. It contains no public journal status, committed bytes, uniqueness assertion, or terminal-event verification state.

`VerifiedSupervisorClaimSlotCommit` is the sole commit-evidence input. All fields are private and integrity-rechecked on every use. It binds `(attempt_hash, phase)`, sequence, exact slot ID, journal head, boot epoch ID/hash, claim ID/hash, authorization/approval/request/dispatch hashes, committed canonical bytes and their hash, verified commit state, unique tuple-to-slot state, and unique slot-to-tuple state. No production constructor or decoder exists. A future journal implementation must mint it in trusted in-package code or through a separately reviewed seam.

`VerifiedSupervisorTerminalEvent` is the sole terminal-predecessor evidence for phases 2 and 3. Its private integrity binds the prior claim hash, attempt, prior phase/sequence, terminal-event hash, boot epoch, prior slot, claim and terminal journal heads, and successful terminal status. No production constructor exists. Invented hashes, wrong-attempt capabilities, aliased prior-phase capabilities, and mutated capabilities reject.

Claim, request, dispatch, and fixture bytes never establish trusted journal state, boot identity, installation identity, repository identity, uniqueness, commit, terminal status, or runtime authority. Test-only `_test.go` fixtures may mint deterministic opaque proofs as oracles.

`EvaluateSupervisorIntentClaim` also requires the existing opaque `VerifiedAuthorization`. For phases 2 and 3 it requires both the exact intact `VerifiedSupervisorIntentClaim` and exact intact `VerifiedSupervisorTerminalEvent`. All opaque capabilities deep-copy retained bytes and recheck private integrity on every use. They are predecessor evidence only, never launch permission.

## Attempt identity

`attempt_hash` is the RFC 8785/JCS record hash of `ananke.controlled-repair-supervisor-attempt-identity.v1`, excluding its own `attempt_hash`, over:

- `attempt_number` and the fixed cap `2`;
- `authorization_hash` and `approval_hash`;
- immutable `request_hash` and `dispatch_hash`.

Attempt 2 therefore has a distinct hash and claim chain bound to its new authorization, approval, request, and dispatch. An attempt-1 claim or opaque predecessor cannot occupy or precede an attempt-2 slot.

## Frozen phases and slots

Exactly three phases exist, in this order, with exactly one trusted slot per `(attempt_hash, phase)` and no slot ID reusable by another tuple:

1. `materialization_claim`, sequence 1, before any common-`.git` or worktree mutation;
2. `adapter_claim`, sequence 2, before any adapter spawn;
3. `test_claim`, sequence 3, before any test-root creation or test spawn.

Sequence 1 has no predecessor. Sequences 2 and 3 bind both the exact preceding committed claim hash and its opaque verified terminal-event capability. All claims use durability policy ID `FULL+fullfsync`; no alias or weaker mode is accepted.

## Immutable claim

Schema `ananke.controlled-repair-supervisor-intent-claim.v1` is a closed canonical JSON object. Every claim binds:

- schema version, claim ID, complete `claim_hash`, phase, sequence;
- attempt hash, attempt number, and cap;
- authorization, approval, policy, all P4 fact/proposal/input/evidence/admission hashes, fence hash, and fence claim-token hash;
- immutable request and dispatch hashes, channel binding, and peer identity hash;
- predecessor claim and predecessor terminal-event hashes when required;
- supervisor boot epoch ID/hash, journal head hash, and journal slot ID;
- repository binding/identity hashes and base commit/tree;
- phase executable hash, sandbox profile ID/hash, namespace ID/hash, and root identity hash;
- exact durability policy ID, `created_at`, exclusive `not_after`, and complete record hash.

Claim timestamps are canonical UTC. `not_after` is the exclusive minimum of `created_at + 60s`, dispatch expiry, authorization expiry, and the inclusive approval-age boundary represented as `approved_at + MaxApprovalAge + 1ns`. The intersection must be positive. Authority validation additionally requires the last representable live instant, `not_after - 1ns`, to pass complete effect-time dispatch, authorization, and embedded-release freshness. A claim whose tail could never mint a capability is rejected.

Unknown keys, duplicate keys, invalid UTF-8/scalars, noncanonical JSON, malformed hashes, changed times, overlong lifetime, or a consistently rehashed semantic mismatch fail with `ErrInvalidSupervisorIntent`.

## Admission and replay

Evaluation without an opaque slot-commit proof can only yield `awaiting_journal_commit`, `effect_allowed=false`, and no verified-claim capability. This means only “eligible for a future journal commit attempt”; caller bytes cannot prove an empty or committed slot and do not authorize launch.

A supplied opaque slot-commit proof behaves as follows:

- exact canonical-byte replay while claim, authorization, dispatch, and embedded release are effect-time fresh: `exact_replay`, `effect_allowed=false`, plus an opaque predecessor capability;
- exact historical replay at claim `not_after`, stale authorization/dispatch, or invalid current embedded release: `exact_replay`, `effect_allowed=false`, and **nil** predecessor capability;
- changed bytes for the proven slot: `conflict`, `effect_allowed=false`, and `ErrSupervisorClaimConflict`;
- failed commit verification, a second slot for one tuple, a slot ID aliased across tuples/phases, or mutated proof integrity: fail closed with no capability and no effect.

Duplicate callers therefore perform zero effects. No caller boolean, disposition, claim, slot proof, terminal proof, or verified-claim capability grants process or filesystem authority. Only a future live executor receiving actual journal commit confirmation may launch; replay cannot reconstruct that confirmation.

## Recovery contract — Repair A+B candidate

`SupervisorRecoveryRecord` is canonical data under the closed schema `ananke.controlled-repair-supervisor-recovery-record.v1`. Each RFC 8785/JCS self-hashed record binds its record ID/hash, exact sequence and kind, attempt hash/number/cap, phase, claim hash, unique slot ID, supervisor boot epoch hash, journal head hash, predecessor record hash, exact `FULL+fullfsync` policy, canonical UTC occurrence time, and bounded semantic payload hash. The predecessor is empty only for sequence 1. Unknown, duplicate, trailing, noncanonical, malformed, misordered, wrongly linked, or self-hash-mismatched data rejects. Decoding a valid public record establishes canonical data only; it never establishes durability.

`ClassifySupervisorRecovery` accepts only `*VerifiedSupervisorRecoverySnapshot` plus one requested action. The snapshot's journal-verification, durability-verification, completeness-verification, and ambiguity-check results are private. Its private integrity binds current and recorded boot epochs, exact attempt/phase/claim/slot/journal lineage, crash cut point, exact ordered durable prefix, deep-copied canonical record bytes and hashes, and the snapshot integrity hash; all are rechecked on every classification. Nil, zero, mutated, aliased, incomplete, ambiguous, or unverified snapshots reject. No production constructor or decoder mints this capability from claim, request, fixture, or recovery-record bytes. Future production minting requires trusted in-package journal verification or a separately reviewed seam.

The `before_claim_commit` case is not a public empty slice: it still requires an intact opaque snapshot proving the journal observation is complete, durability-checked, ambiguity-checked, and empty at the bound cut point. Its successful result remains status-only with `effect_allowed=false`.

The exact durable prefix is:

1. `claim_commit`;
2. `phase_launch`;
3. `terminal_proof`;
4. `attestation_signature`;
5. `response`.

Only `status_only` and `replay_response` are legitimate actions. Response replay requires the exact five-record prefix. No successful classification launches or repeats an effect; every successful result has `effect_allowed=false`. Prior-epoch nonterminal states wait for human review, current-epoch nonterminal states require live commit confirmation, terminal states expose status requirements only, and the fully persisted response permits response replay only:

| Ordered cut point | Verified durable observation | Disposition | Next requirement |
|---|---|---|---|
| before claim commit | `no_durable_claim` | `no_durable_claim` | `fresh_live_intent_required` |
| after claim commit | `claim_committed` | `waiting_for_human` for a prior epoch | `human_review_required` |
| before phase launch | `claim_committed` | `waiting_for_human` for a prior epoch | `human_review_required` |
| after phase launch | `phase_launched` | `waiting_for_human` for a prior epoch | `human_review_required` |
| before terminal-proof persistence | `phase_launched` | `waiting_for_human` for a prior epoch | `human_review_required` |
| after terminal-proof persistence | `terminal_proof_persisted` | `terminal_status` | `attestation_status_required` |
| before attestation-signature persistence | `terminal_proof_persisted` | `terminal_status` | `attestation_status_required` |
| after attestation-signature persistence | `attestation_signature_persisted` | `terminal_status` | `response_status_required` |
| before response persistence | `attestation_signature_persisted` | `terminal_status` | `response_status_required` |
| after response persistence | `response_persisted` | `response_replay` | `no_further_effect_permitted` |

This candidate closes the caller-trusted recovery input model at the contract boundary. It does not implement or claim real journal reads, durable writes, `fsync`, runtime snapshot minting, process launch, effect execution, attestation signing, response persistence, Slice 3 acceptance, or full P6 acceptance; those remain separate implementation and independent-review obligations.

## Machine-checked contract and fixtures

`TestP6Slice3NormativeDocumentMatchesTypesFixturesAndInventory` parses the following block with unknown fields rejected and compares it to compiled constants, deterministic attempt-1/attempt-2 claim chains, the deterministic five-record recovery fixture, the unchanged prior 91-vector count, and the exact ordered 99-vector Repair A+B registry.

<!-- BEGIN P6 SLICE 3 MACHINE CONTRACT -->
```json
{
  "schema_version": "ananke.controlled-repair-supervisor-intent-document.v1",
  "status": "repair_a_b_candidate_pending_independent_frozen_source_review",
  "claim_schema_version": "ananke.controlled-repair-supervisor-intent-claim.v1",
  "attempt_identity_schema_version": "ananke.controlled-repair-supervisor-attempt-identity.v1",
  "recovery_record_schema_version": "ananke.controlled-repair-supervisor-recovery-record.v1",
  "durability_policy_id": "FULL+fullfsync",
  "max_claim_lifetime_nanoseconds": 60000000000,
  "prior_slice_vector_count": 91,
  "slice_3_vector_count": 99,
  "effect_allowed_values": [
    false
  ],
  "phases": [
    {
      "phase": "materialization_claim",
      "sequence": 1
    },
    {
      "phase": "adapter_claim",
      "sequence": 2
    },
    {
      "phase": "test_claim",
      "sequence": 3
    }
  ],
  "claim_dispositions": [
    "awaiting_journal_commit",
    "exact_replay",
    "conflict"
  ],
  "recovery_actions": [
    "status_only",
    "replay_response"
  ],
  "recovery_dispositions": [
    "no_durable_claim",
    "waiting_for_human",
    "current_epoch_status_only",
    "terminal_status",
    "response_replay"
  ],
  "crash_cut_points": [
    "before_claim_commit",
    "after_claim_commit",
    "before_phase_launch",
    "after_phase_launch",
    "before_terminal_proof_persistence",
    "after_terminal_proof_persistence",
    "before_attestation_signature_persistence",
    "after_attestation_signature_persistence",
    "before_response_persistence",
    "after_response_persistence"
  ],
  "canonical_fixtures": [
    {
      "attempt_number": 1,
      "attempt_hash": "sha256:77ebb05bc3d8277f9835283b84f9db6272c61c97c2975fe45bb6fb8cdb581dae",
      "authorization_hash": "sha256:a4ebf38cc021a2c93f7ac2dc693744e2a7e3c6928f8a26d80a9ba501d00b2a11",
      "approval_hash": "sha256:e8e687fc8e0e889096eb1e0c91677666017f4336304ca6c1061d3b91a54744e9",
      "dispatch_hash": "sha256:5a8abbc3fffcb67ddfe349957b238d18a30f79eedfdebd8aed804afb9df3ea62",
      "claims": [
        {
          "phase": "materialization_claim",
          "sequence": 1,
          "claim_hash": "sha256:45ff41731f981cce4d5c38397b588c4c4f42f2f01c01cc315f0410dc1768bc27",
          "canonical_sha256": "sha256:ce24487a798fe6b4975beb6a0c18f6cc9cac6c58e9e9178c09f931c1a7f6e490"
        },
        {
          "phase": "adapter_claim",
          "sequence": 2,
          "claim_hash": "sha256:9fc9223dbe7dc06cbfc9c6bbdb5642c4ad5a6c40c9a36d0a61f29e49ba5259cc",
          "canonical_sha256": "sha256:37a49aef0b68d8754b477b5ad51ae9379fcd8fc613450fd657c0b857460f4b8e"
        },
        {
          "phase": "test_claim",
          "sequence": 3,
          "claim_hash": "sha256:d955a6dbc53b8bd0be79347e8c60024c0e88d09febddccc8e86aca631ee78bfa",
          "canonical_sha256": "sha256:75760a3e8c70698a3ffbec7140897239ee45947a74e863c6dc69c2875dba0eef"
        }
      ]
    },
    {
      "attempt_number": 2,
      "attempt_hash": "sha256:3dcfffe3cdc75dd28e16d23dc9e3d704a7e2f98687b95038f21d7c8d71b55726",
      "authorization_hash": "sha256:bc624ae6ccc0216dd349fabb2ae24395aebc81a9309fd19922d6977d2a4ee1f0",
      "approval_hash": "sha256:eafb36790a2028920eb359566d693878ed5393c85347e46d13000978cd7a10d9",
      "dispatch_hash": "sha256:0cf196f3bd2843f6e56d6d4b163c44451b6f9dc9da0dcfdf01e3b8f5af41672c",
      "claims": [
        {
          "phase": "materialization_claim",
          "sequence": 1,
          "claim_hash": "sha256:d5166ff816de8aa081bcd3a57dc90d34b65d4ad8d444c1fe40d841cc1688f616",
          "canonical_sha256": "sha256:5fc8dd9527df9d0473d9d0da8c945718795a19eb67acbc5da387a8a50e8bc915"
        },
        {
          "phase": "adapter_claim",
          "sequence": 2,
          "claim_hash": "sha256:45f8f8e1f5f602e3636f58a2c1ca012e8d5271699fd006988a5349ac821bb3d8",
          "canonical_sha256": "sha256:093f199354f15653ff7ae3bbd1a75fa38dd5a17903228216f656023a28b99470"
        },
        {
          "phase": "test_claim",
          "sequence": 3,
          "claim_hash": "sha256:1bf8f40ad1105f2a7c6c3d0db8c38375a8cd2cc1a0714cd4ae000cf910d0d7b3",
          "canonical_sha256": "sha256:d1fe0c5b5e194d9412450425916207fa9efd9b99f708485a4ece88a926bfd58a"
        }
      ]
    }
  ],
  "canonical_recovery_fixture": {
    "current_boot_epoch_hash": "sha256:e85f29ae754134b530a4804ad19f94eaddf3284b9d41e7129f868c73177c9270",
    "recorded_boot_epoch_hash": "sha256:ae03436fadc7b4ac2e74a9828ac0abb797fb406507d1e092cc3880f1c110b235",
    "attempt_hash": "sha256:77ebb05bc3d8277f9835283b84f9db6272c61c97c2975fe45bb6fb8cdb581dae",
    "phase": "materialization_claim",
    "claim_hash": "sha256:45ff41731f981cce4d5c38397b588c4c4f42f2f01c01cc315f0410dc1768bc27",
    "slot_id": "attempt_1_materialization_claim_slot",
    "records": [
      {
        "sequence": 1,
        "record_kind": "claim_commit",
        "record_hash": "sha256:177802049a8a15c541e0c22355a9ac27f865c28583dad8e860866ed14dd5ada7",
        "canonical_sha256": "sha256:141981206f0e9215eb394cd9c1256d58bbf34bc7e91057401466b49f4374e58b"
      },
      {
        "sequence": 2,
        "record_kind": "phase_launch",
        "record_hash": "sha256:a07a2cef8913f3186b7255b0445eef6c967f8901735ca9c3825b9a7139b78665",
        "canonical_sha256": "sha256:bdf8784f6c71cfb2767e908eabc11e6fa4d3e669dd69c297dfc915a2401327fd"
      },
      {
        "sequence": 3,
        "record_kind": "terminal_proof",
        "record_hash": "sha256:7cf3288e91c855d1d925fe415181ef28f89b8a7f2512a3b105d0210487152329",
        "canonical_sha256": "sha256:0e69048333f8c6bd3bc90c9d246f9cba8e8d2d9082cf92c01daf58c8a4be2dcc"
      },
      {
        "sequence": 4,
        "record_kind": "attestation_signature",
        "record_hash": "sha256:56ea9d06fdc1e7b2b3636f357e891353d8dd6e44948ae0151f4f03a3d1884d08",
        "canonical_sha256": "sha256:f2324df9196b5dbe838d7db52a807d2ad5496669c57f25d71e37dca5bc95f41e"
      },
      {
        "sequence": 5,
        "record_kind": "response",
        "record_hash": "sha256:6535e6bab6b427f3fd0fcfa3547c298dd3c3de496ae9e4b2fdc3acdbfdc505d2",
        "canonical_sha256": "sha256:c7b3f526d3acd8261e267e6f0c1b736f8c59c5f522e58f11c2e44a338d54a3f2"
      }
    ]
  },
  "vector_ids": [
    "canonical_attempt_1_chain",
    "canonical_attempt_2_chain",
    "wrong_phase",
    "missing_phase",
    "same_tuple_second_committed_slot",
    "wrong_sequence",
    "slot_id_aliased_across_phases",
    "same_slot_changed_bytes",
    "swapped_authorization",
    "swapped_approval",
    "swapped_p4_fact",
    "swapped_p4_proposal",
    "swapped_p4_input",
    "swapped_p4_evidence_bundle",
    "swapped_p4_admission",
    "swapped_fence",
    "swapped_fence_claim_token",
    "swapped_request",
    "swapped_dispatch",
    "swapped_channel",
    "swapped_peer",
    "swapped_policy",
    "wrong_boot_epoch",
    "prior_epoch_live_claim",
    "wrong_repository_identity",
    "wrong_base_identity",
    "wrong_executable_identity",
    "wrong_sandbox_identity",
    "wrong_namespace_identity",
    "wrong_root_identity",
    "missing_predecessor_claim",
    "wrong_predecessor_claim",
    "missing_predecessor_terminal_event",
    "wrong_predecessor_terminal_event",
    "invented_predecessor_terminal_event",
    "wrong_predecessor_terminal_event_capability",
    "aliased_predecessor_terminal_event_capability",
    "missing_verified_predecessor",
    "wrong_verified_predecessor",
    "attempt_1_claim_reused_in_attempt_2",
    "attempt_2_chain_with_old_approval",
    "attempt_2_chain_with_attempt_1_predecessor",
    "noncanonical_json",
    "unknown_key",
    "duplicate_key",
    "changed_timestamp",
    "overlong_lifetime",
    "exact_replay_capability_n_minus_1ns",
    "exact_replay_no_capability_n",
    "exact_replay_no_capability_n_plus_1ns",
    "stale_authorization_exact_replay_no_capability",
    "stale_dispatch_exact_replay_no_capability",
    "expired_release_exact_replay_no_capability",
    "canonical_recovery_record_chain",
    "recovery_record_unknown_key",
    "recovery_record_duplicate_key",
    "recovery_record_trailing_data",
    "recovery_record_noncanonical_json",
    "recovery_record_hash_mismatch",
    "recovery_record_wrong_sequence",
    "recovery_record_wrong_kind",
    "recovery_record_wrong_attempt",
    "recovery_record_wrong_phase",
    "recovery_record_wrong_claim",
    "recovery_record_wrong_slot",
    "recovery_record_wrong_boot",
    "recovery_record_wrong_journal",
    "recovery_record_wrong_predecessor",
    "recovery_record_wrong_durability",
    "recovery_record_wrong_payload",
    "recovery_record_wrong_time",
    "nil_opaque_recovery_snapshot",
    "zero_opaque_recovery_snapshot",
    "invented_unverified_recovery_snapshot",
    "opaque_recovery_snapshot_private_mutation",
    "recovery_chain_aliased_across_attempt_phase_slot",
    "verified_empty_journal_observation",
    "crash_before_claim_commit",
    "crash_after_claim_commit",
    "crash_before_phase_launch",
    "crash_after_phase_launch",
    "crash_before_terminal_proof_persistence",
    "crash_after_terminal_proof_persistence",
    "crash_before_attestation_signature_persistence",
    "crash_after_attestation_signature_persistence",
    "crash_before_response_persistence",
    "crash_after_response_persistence",
    "prior_epoch_nonterminal_waiting_for_human",
    "current_epoch_recovery_status_only",
    "exact_response_replay_only",
    "invalid_recovery_launch_action",
    "forged_recovery_completeness",
    "forged_recovery_ambiguity",
    "truncated_recovery_record",
    "missing_recovery_record",
    "unknown_recovery_record",
    "duplicate_recovery_record",
    "out_of_order_recovery_record",
    "response_replay_tries_second_effect"
  ]
}
```
<!-- END P6 SLICE 3 MACHINE CONTRACT -->

## Executable evidence boundary

The canonical constructors and opaque proof minters are test-only oracles; they create no production authority. The package deliberately contains no journal implementation, live commit-confirmation seam, executor, launch API, recovery writer, attestation signer, response persister, SQLite/store/filesystem durability path, or production snapshot minter. Repair A+B remains a candidate pending independent frozen-source review; this document makes no Slice ACCEPT, full-P6, runtime, storage, or effect claim.
