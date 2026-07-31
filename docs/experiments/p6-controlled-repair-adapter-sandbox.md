# P6 controlled-repair adapter sandbox contract: Slice 5

**Status:** pure contract freeze. This document does not claim the full P6 contract acceptance gate or `DESIGN ACCEPT`.

**Scope:** adapter sandbox, UID pool/lease, Seatbelt profile, and terminal proof. The artifacts are:

- `internal/repaircontract/adapter_sandbox.go` — closed Go schemas, frozen UID pool, frozen Seatbelt profile, terminal proof, sandbox observation, opaque capabilities, verifier authority, verification seals, and evaluator;
- `internal/repaircontract/adapter_sandbox_test.go` — RED→GREEN contract vectors and mutation-isolation tests;
- `internal/repaircontract/adapter_sandbox_registry_test.go` — the separately frozen canonical vector-ID inventory and its ordered executable registry;
- `internal/repaircontract/adapter_sandbox_document_test.go` — normative document verification;
- this document.

These artifacts implement **no storage, migration, process launch, sandbox execution, seatbelt activation, UID lease acquisition, child spawn, signal delivery, descriptor close, root freeze, network broker, or runtime dispatch**. Terminal proof collection, UID-wide process enumeration/kill, sandbox containment, and automatic reuse safety are future implementation obligations. The values here are pure contracts and verifier inputs only.

## Canonical and closed-record rules

Every named record has an exact `schema_version` and an exact Go-typed key set. `DecodeAdapterSandboxObservation` rejects unknown members at every nesting level. It also rejects duplicate object keys, trailing JSON, BOM, invalid UTF-8, lone surrogates, Unicode noncharacters, malformed JSON, and any byte sequence that is not already its RFC 8785/JCS representation.

All fields ending in `_hash` are `sha256:` followed by exactly 64 lowercase hexadecimal digits. Every self-hashed record is hashed over its JCS object after deleting only that record's own hash member.

## UID pool

The frozen release-provisioned UID pool is compiled at package initialization from the provisioning facts recorded in the experiment ledger (2026-07-26):

```text
pool_id        controlled_repair_runtime_uid_pool_v1
group_id       62000
group_name     _ananke_repair
pool_size      4
entries:
  1  uid=62001  user=_ananke_repair_1  group=62000
  2  uid=62002  user=_ananke_repair_2  group=62000
  3  uid=62003  user=_ananke_repair_3  group=62000
  4  uid=62004  user=_ananke_repair_4  group=62000
```

`FrozenAdapterUIDPool()` is derived at package initialization. `mustDeriveAdapterUIDPool` panics if the derived pool hash does not match the computed record hash. The pool binds the exact group, pool size, and ordered UIDs; no concurrent attempt may share a UID.

## UID lease

`AdapterUIDLease` is an exclusive lease from the closed UID pool. It is journaled before spawn and binds the attempt:

```text
lease_id       attempt_<N>_adapter_uid_lease_001
attempt_hash   bound to authorization and dispatch
uid            one of 62001–62004
group_id       62000
pool_id        controlled_repair_runtime_uid_pool_v1
exclusive      true
```

The lease ID follows the frozen authorization-derived grammar: `attempt_` + attempt number + `_adapter_uid_lease_001`. Validation requires the UID to be in the frozen pool, the group to match, the pool ID to match, and exclusivity to be true.

## Seatbelt profile

The compiled Darwin Seatbelt profile freezes the exact adapter sandbox capabilities:

```text
profile_id                              controlled_repair_adapter_seatbelt_v1
adapter_child_process_only              true
no_in_process_interface                 true
write_disposable_roots_only            true
read_pinned_roots_only                 true
network_broker_only                    true
exec_pinned_adapter_only               true
no_credentials_in_argv                 true
no_credentials_in_policy_or_evidence    true
descriptor_closure_required            true
uid_wide_process_enumeration_required   true
root_freeze_before_continue             true
```

`FrozenAdapterSeatbeltProfile()` is derived at package initialization. Every boolean must be true; any false value is rejected. The adapter is a child process only; there is no production in-process interface. Provider credentials pass only through the separately reviewed broker channel, never through child argv, raw policy, or evidence.

## Terminal proof

`AdapterTerminalProof` binds:

- UID lease hash;
- leader identity hash (combines PID and boot epoch to prevent stale PID reuse);
- process-group identity hash;
- UID-empty observation hash (the result of enumerating all processes under the leased UID);
- sandbox hash;
- root identity hash;
- descriptor closure hash;
- boot epoch hash;
- cleanup result (closed enum);
- `uid_empty_verified`, `descriptors_closed`, `roots_frozen` booleans;
- observed-at timestamp.

The cleanup result is one of:

| Value | Meaning |
| --- | --- |
| `uid_empty_roots_frozen` | All UID processes gone, roots frozen — success |
| `uid_nonempty_retained` | Some UID processes still alive — retained for human |
| `partial_retained` | Partial cleanup — retained for human |

On exit/timeout, the supervisor must: reap leader, TERM/KILL original PGID, enumerate/kill every leased-UID process including new sessions, repeat until UID-empty, close descriptors, freeze roots, persist terminal proof, then and only then continue. The terminal proof is the durable evidence that this cleanup completed.

## Adapter sandbox observation

`AdapterSandboxObservation` is canonical, closed, self-hashed data. It binds:

- authorization, approval, request, dispatch, attempt hashes and numbers;
- adapter claim hash and predecessor materialization claim hash;
- predecessor terminal event hash;
- repository binding and identity hashes;
- worktree slot ID, path hash, capability hash, and installed root identity hash (from Slice 4);
- UID pool hash, UID lease hash, UID, group ID;
- seatbelt profile ID and hash;
- terminal proof (complete record);
- root identity hash, sandbox hash, descriptor closure hash;
- boot epoch ID and hash.

The observation has three states:

| State | Ambiguity reason | Action | Disposition |
| --- | --- | --- | --- |
| `terminal_proven` | empty | `admit_terminal_proof` | `capability_ready` → next test phase |
| `retained_terminal_replay` | empty | `status_only` | `retained_status` → no further effect |
| `retain_for_human` | one of 12 closed reasons | `status_only` | `waiting_for_human` → human review required |

The 12 closed ambiguity reasons are: `uid_not_empty`, `descriptors_not_closed`, `roots_not_frozen`, `stale_pid_epoch`, `uid_reuse_contention`, `child_still_alive`, `broker_network_escape`, `ignored_context`, `double_fork_escape`, `setsid_new_session`, `closed_stdio_evade`, `delayed_write_ref_update`.

## Verifier authority and verification seals

`FrozenAdapterSandboxVerifierAuthority()` binds:

- verifier ID `controlled_repair_adapter_sandbox_verifier_v1`;
- frozen seatbelt profile ID and hash;
- frozen UID pool hash;
- frozen release pins hash;
- seven ordered verification kinds.

The seven verification kinds, in order:

1. `uid_lease_verification`
2. `seatbelt_profile_verification`
3. `terminal_proof_verification`
4. `sandbox_boundary_verification`
5. `descriptor_closure_verification`
6. `root_identity_verification`
7. `uid_empty_verification`

Each kind produces a self-hashed `AdapterSandboxVerificationSeal` binding the verifier authority hash, observation hash, canonical hash, and kind-specific evidence hash. The aggregate verification-seals hash is the SHA-256 of the canonical array of the seven seal hashes in order.

## Evaluator

`EvaluateAdapterSandbox` first re-establishes fresh `VerifyReleaseTrust`, derives the frozen verifier authority, and requires exact equality. It then validates the adapter-claim authority (phase = `adapter_claim`, sequence = 2), checks authorization intactness, claim intactness, freshness, the predecessor materialization claim and terminal event, the Slice 4 worktree capability intactness, snapshot intactness (all seven seals recomputed), and authority matching.

The only capability construction occurs inside `EvaluateAdapterSandbox` after all checks pass for a `terminal_proven` state with the `admit_terminal_proof` action. No standalone production minter exists. `EffectAllowed` is always `false`.

## Opaque capability boundary

`VerifiedAdapterSandbox` is opaque. Its private integrity binds the observation hash, snapshot integrity hash, verifier authority hash, verification seals hash, authorization hash, claim hash, attempt hash, predecessor claim hash, worktree capability hash, UID lease hash, terminal proof hash, and canonical hash. `verifiedAdapterSandboxIntact` re-derives all seals from the decoded observation under the frozen verifier authority and requires exact reproduction of the aggregate verification-seals hash.

`VerifiedAdapterSandboxSnapshot` is opaque snapshot evidence. Its private integrity binds all seven verification booleans, the observation, canonical bytes/hash, verifier authority hash, seven seal hashes, and integrity hash. `verifiedAdapterSandboxSnapshotIntact` recomputes every seal from the decoded observation and requires exact match.

No production constructor or decoder from caller bytes exists. Test helpers within the package mint snapshots and capabilities; external callers cannot forge either.

## Closed schema inventory

| Record | Schema |
| --- | --- |
| adapter sandbox observation | `ananke.controlled-repair-adapter-sandbox-observation.v1` |
| adapter UID pool | `ananke.controlled-repair-adapter-uid-pool.v1` |
| adapter UID pool entry | `ananke.controlled-repair-adapter-uid-pool-entry.v1` |
| adapter UID lease | `ananke.controlled-repair-adapter-uid-lease.v1` |
| adapter seatbelt profile | `ananke.controlled-repair-adapter-seatbelt-profile.v1` |
| adapter terminal proof | `ananke.controlled-repair-adapter-terminal-proof.v1` |
| adapter sandbox verifier authority | `ananke.controlled-repair-adapter-sandbox-verifier-authority.v1` |
| adapter sandbox verification seal | `ananke.controlled-repair-adapter-sandbox-verification-seal.v1` |

## Ordered acceptance-vector inventory

The registry test `TestP6Slice5ExecutableVectorRegistry` invokes all 40 entries in this exact order, records actual execution order, rejects missing/duplicate/renamed/nil entries, and compares with the canonical inventory:

1. `canonical_terminal_proven`
2. `opaque_snapshot_deep_copy`
3. `opaque_snapshot_mutation_isolation`
4. `retained_terminal_replay`
5. `uid_not_empty_ambiguous`
6. `descriptors_not_closed_ambiguous`
7. `roots_not_frozen_ambiguous`
8. `stale_pid_epoch_ambiguous`
9. `child_still_alive_ambiguous`
10. `broker_network_escape_ambiguous`
11. `ignored_context_ambiguous`
12. `double_fork_escape_ambiguous`
13. `setsid_new_session_ambiguous`
14. `closed_stdio_evade_ambiguous`
15. `delayed_write_ref_update_ambiguous`
16. `uid_reuse_contention_ambiguous`
17. `expired_freshness_rejects`
18. `wrong_phase_rejects`
19. `status_only_on_terminal_proven_rejects`
20. `admit_on_retain_for_human_rejects`
21. `canonical_json_closure`
22. `capability_mutation_valid_bit`
23. `capability_mutation_observation_hash`
24. `capability_mutation_verifier_authority_hash`
25. `capability_mutation_authorization_hash`
26. `capability_mutation_claim_hash`
27. `capability_mutation_uid_lease_hash`
28. `capability_mutation_terminal_proof_hash`
29. `capability_mutation_canonical_bytes`
30. `frozen_uid_pool_deterministic`
31. `frozen_seatbelt_profile_deterministic`
32. `frozen_verifier_authority_deterministic`
33. `uid_not_in_pool_rejects`
34. `wrong_pool_hash_rejects`
35. `wrong_seatbelt_profile_rejects`
36. `unknown_cleanup_result_rejects`
37. `unknown_state_rejects`
38. `unknown_ambiguity_reason_rejects`
39. `terminal_proof_hash_mismatch_rejects`
40. `observation_hash_mismatch_rejects`

## Gate and remaining work

The focused package tests prove only these pure Slice 5 records, canonical bytes, semantic relations, and in-memory mutations. No P6 store, outbox persistence, transport, supervisor runtime, signature, sandbox execution, process launch, UID lease acquisition, signal delivery, or migration is implemented or claimed. Contract Slices 6–9, a fresh independent hard review, and the plan's full `DESIGN ACCEPT` gate remain separate prerequisites before runtime or storage implementation.

<!-- BEGIN P6 SLICE 5 MACHINE CONTRACT -->
```json
{
  "schema_version": "ananke.controlled-repair-adapter-sandbox-document.v1",
  "status": "slice_5_candidate_pending_independent_frozen_source_review",
  "observation_schema_version": "ananke.controlled-repair-adapter-sandbox-observation.v1",
  "prior_slice_1_to_2_vector_count": 91,
  "prior_slice_3_vector_count": 99,
  "prior_slice_4_vector_count": 258,
  "slice_5_vector_count": 40,
  "effect_allowed_values": [
    false
  ],
  "allowed_actions": [
    "admit_terminal_proof",
    "status_only"
  ],
  "sandbox_states": [
    "terminal_proven",
    "retained_terminal_replay",
    "retain_for_human"
  ],
  "ambiguity_reasons": [
    "uid_not_empty",
    "descriptors_not_closed",
    "roots_not_frozen",
    "stale_pid_epoch",
    "uid_reuse_contention",
    "child_still_alive",
    "broker_network_escape",
    "ignored_context",
    "double_fork_escape",
    "setsid_new_session",
    "closed_stdio_evade",
    "delayed_write_ref_update"
  ],
  "cleanup_results": [
    "uid_empty_roots_frozen",
    "uid_nonempty_retained",
    "partial_retained"
  ],
  "dispositions": [
    "capability_ready",
    "retained_status",
    "waiting_for_human"
  ],
  "requirements": [
    "next_test_phase",
    "no_further_effect_permitted",
    "human_review_required"
  ],
  "verifier_authority": {
    "schema_version": "ananke.controlled-repair-adapter-sandbox-verifier-authority.v1",
    "verifier_id": "controlled_repair_adapter_sandbox_verifier_v1",
    "verifier_authority_hash": "sha256:84d3c9cac3b244972e4a18dc0e857358396c2064126bc8109382d25559f7e424",
    "release_pins_hash": "sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a",
    "verification_kinds": [
      "uid_lease_verification",
      "seatbelt_profile_verification",
      "terminal_proof_verification",
      "sandbox_boundary_verification",
      "descriptor_closure_verification",
      "root_identity_verification",
      "uid_empty_verification"
    ]
  },
  "seatbelt_profile": {
    "schema_version": "ananke.controlled-repair-adapter-seatbelt-profile.v1",
    "profile_id": "controlled_repair_adapter_seatbelt_v1",
    "profile_hash": "sha256:d35476624740cbc1b1623e459a80e38d78a909f4625710173fc0a658e47f5adb"
  },
  "uid_pool": {
    "schema_version": "ananke.controlled-repair-adapter-uid-pool.v1",
    "pool_id": "controlled_repair_runtime_uid_pool_v1",
    "pool_hash": "sha256:e66ca8742c9bc74acf99878be8165c55628f91827cacf244f2d0920bbc2de12b",
    "group_id": 62000,
    "group_name": "_ananke_repair",
    "pool_size": 4
  },
  "uid_lease_grammar": "attempt_<attempt_number>_adapter_uid_lease_001",
  "canonical_fixture": {
    "observation_hash": "sha256:7bf86db92a2712e44d1f7a7aa62fcfaa6a0087e9bd46f6153bb3e6bbf250599e",
    "canonical_sha256": "sha256:ed2272178bc29e8199ab6558b9b22edef022ee8f721febc1c30cae84bdad3d4a",
    "snapshot_integrity_hash": "sha256:7dd06b6e654776c2d52824aec0cc52e39a2a473465c8be754a9becc656641dca",
    "authorization_hash": "sha256:a4ebf38cc021a2c93f7ac2dc693744e2a7e3c6928f8a26d80a9ba501d00b2a11",
    "claim_hash": "sha256:9fc9223dbe7dc06cbfc9c6bbdb5642c4ad5a6c40c9a36d0a61f29e49ba5259cc",
    "predecessor_claim_hash": "sha256:45ff41731f981cce4d5c38397b588c4c4f42f2f01c01cc315f0410dc1768bc27",
    "attempt_hash": "sha256:77ebb05bc3d8277f9835283b84f9db6272c61c97c2975fe45bb6fb8cdb581dae",
    "worktree_capability_hash": "sha256:6ca353f8a8091de3d84bddb8fd69601cd78ad7fd21bde2e8034c40eb034be39b",
    "uid_lease_hash": "sha256:a0ded424cd53ddace37dc7274ff8e416a3ac0b5ef4c27cb5fdcd97405d93fbbb",
    "terminal_proof_hash": "sha256:d4ed047e89c2b3375d7d550ba892af0accb6ae1be6ce1c2dfdbc56e7ea09b2af",
    "uid": 62001,
    "group_id": 62000
  },
  "vector_ids": [
    "canonical_terminal_proven",
    "opaque_snapshot_deep_copy",
    "opaque_snapshot_mutation_isolation",
    "retained_terminal_replay",
    "uid_not_empty_ambiguous",
    "descriptors_not_closed_ambiguous",
    "roots_not_frozen_ambiguous",
    "stale_pid_epoch_ambiguous",
    "child_still_alive_ambiguous",
    "broker_network_escape_ambiguous",
    "ignored_context_ambiguous",
    "double_fork_escape_ambiguous",
    "setsid_new_session_ambiguous",
    "closed_stdio_evade_ambiguous",
    "delayed_write_ref_update_ambiguous",
    "uid_reuse_contention_ambiguous",
    "expired_freshness_rejects",
    "wrong_phase_rejects",
    "status_only_on_terminal_proven_rejects",
    "admit_on_retain_for_human_rejects",
    "canonical_json_closure",
    "capability_mutation_valid_bit",
    "capability_mutation_observation_hash",
    "capability_mutation_verifier_authority_hash",
    "capability_mutation_authorization_hash",
    "capability_mutation_claim_hash",
    "capability_mutation_uid_lease_hash",
    "capability_mutation_terminal_proof_hash",
    "capability_mutation_canonical_bytes",
    "frozen_uid_pool_deterministic",
    "frozen_seatbelt_profile_deterministic",
    "frozen_verifier_authority_deterministic",
    "uid_not_in_pool_rejects",
    "wrong_pool_hash_rejects",
    "wrong_seatbelt_profile_rejects",
    "unknown_cleanup_result_rejects",
    "unknown_state_rejects",
    "unknown_ambiguity_reason_rejects",
    "terminal_proof_hash_mismatch_rejects",
    "observation_hash_mismatch_rejects"
  ]
}

```
<!-- END P6 SLICE 5 MACHINE CONTRACT -->
