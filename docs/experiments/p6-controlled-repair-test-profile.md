# P6 controlled-repair test sandbox contract: Slice 6

**Status:** pure contract freeze. This document does not claim the full P6 contract acceptance gate or `DESIGN ACCEPT`.

**Scope:** closed offline Go test profile, Go toolchain manifest, candidate copy observation, test sandbox, and UID terminal proof with root cleanup. The artifacts are:

- `internal/repaircontract/test_profile.go` — closed Go schemas, frozen toolchain manifest, frozen test profile, terminal proof with root cleanup, sandbox observation, opaque capabilities, verifier authority, verification seals, and evaluator;
- `internal/repaircontract/test_profile_test.go` — RED→GREEN contract vectors and mutation-isolation tests;
- `internal/repaircontract/test_profile_registry_test.go` — the separately frozen canonical vector-ID inventory and its ordered executable registry;
- `internal/repaircontract/test_profile_document_test.go` — normative document verification;
- this document.

These artifacts implement **no storage, migration, process launch, test execution, sandbox activation, Go toolchain invocation, module cache access, candidate copy, signal delivery, descriptor close, root scrub, or runtime dispatch**. Test execution, UID-wide process enumeration/kill, sandbox containment, root cleanup, and automatic reuse safety are future implementation obligations. The values here are pure contracts and verifier inputs only.

## Go toolchain manifest

The frozen release-pinned root-owned Go toolchain manifest:

```text
manifest_id              controlled_repair_go_toolchain_v1
go_version               1.24.0
executable_hash          sha256:0000...0000 (release-pinned placeholder)
module_cache_hash         sha256:0000...0000 (release-pinned placeholder)
root_owned                true
read_only_module_cache    true
```

`FrozenGoToolchainManifest()` is derived at package initialization. `mustDeriveGoToolchainManifest` panics if the derived manifest hash does not match the computed record hash. The manifest binds the exact Go version, executable hash, and module-cache hash; all are release-pinned values.

## Go test profile

The compiled closed offline Go test profile freezes the exact command, environment, timeout, and resource bounds:

```text
profile_id                controlled_repair_go_test_profile_v1
command                   go test ./... -count=1 -mod=readonly
timeout_seconds           300
max_output_bytes          1048576
max_cpu_percent           100
max_memory_bytes          1073741824
cgo_enabled               0
goenv                     off
gotoolchain               local
goproxy                   off
gosumdb                   off
govcs                     *:off
gowork                    off
no_network                true
private_home              true
private_tmpdir            true
private_gocache           true
read_only_module_cache    true
```

`FrozenGoTestProfile()` is derived at package initialization. Validation requires exact equality of every field with the frozen compiled values.

## Candidate copy observation

`TestCandidateCopyObservation` records the clean candidate copy facts. All seven "no" booleans must be true:

- `no_dot_git` — no `.git` directory
- `no_remotes` — no Git remotes
- `no_credentials` — no credential files
- `no_original_repo` — no original repository path
- `no_retained_worktree` — no retained worktree
- `no_journal_paths` — no journal paths
- `no_key_paths` — no key paths

The `candidate_root_identity_hash` binds the disposable candidate root identity.

## Terminal proof with root cleanup

`TestTerminalProof` binds:

- UID lease hash;
- leader identity hash (combines PID and boot epoch);
- process-group identity hash;
- UID-empty observation hash;
- sandbox hash;
- root identity hash;
- descriptor closure hash;
- boot epoch hash;
- cleanup result (closed enum);
- `root_scrubbed_and_proven_absent` — disposable root scrubbed and proven absent;
- `uid_empty_verified`, `descriptors_closed`, `roots_scrubbed` booleans;
- observed-at timestamp.

The cleanup result is one of:

| Value | Meaning |
| --- | --- |
| `uid_empty_roots_scrubbed` | All UID processes gone, roots scrubbed and proven absent — success |
| `uid_nonempty_retained` | Some UID processes still alive — retained for human |
| `partial_retained` | Partial cleanup — retained for human |

On exit/timeout, the supervisor uses the same exclusive UID terminal proof as the adapter sandbox, plus the disposable root must be scrubbed and proven absent before signing.

## Test sandbox observation

`TestSandboxObservation` is canonical, closed, self-hashed data. It binds:

- authorization, approval, request, dispatch, attempt hashes and numbers;
- test claim hash and predecessor adapter claim hash;
- predecessor terminal event hash;
- repository binding and identity hashes;
- worktree slot ID and adapter sandbox capability hash (from Slice 5);
- UID pool hash, UID lease hash, UID, group ID;
- toolchain manifest ID and hash;
- test profile ID and hash;
- candidate copy observation;
- terminal proof (complete record);
- root identity hash, sandbox hash, descriptor closure hash;
- boot epoch ID and hash.

The observation has three states:

| State | Ambiguity reason | Action | Disposition |
| --- | --- | --- | --- |
| `terminal_proven` | empty | `admit_terminal_proof` | `capability_ready` → next attestation phase |
| `retained_terminal_replay` | empty | `status_only` | `retained_status` → no further effect |
| `retain_for_human` | one of 16 closed reasons | `status_only` | `waiting_for_human` → human review required |

The 16 closed ambiguity reasons are: `uid_not_empty`, `roots_not_scrubbed`, `stale_pid_epoch`, `uid_reuse_contention`, `git_push_attempt`, `local_ref_write_attempt`, `network_access_attempt`, `external_write_attempt`, `original_worktree_mutation`, `arbitrary_exec_attempt`, `fork_escape`, `setsid_new_session`, `delayed_write_ref_update`, `missing_module`, `cache_drift`, `toolchain_replacement`.

## Verifier authority and verification seals

`FrozenTestSandboxVerifierAuthority()` binds:

- verifier ID `controlled_repair_test_sandbox_verifier_v1`;
- frozen toolchain manifest ID and hash;
- frozen test profile ID and hash;
- frozen UID pool hash (reused from Slice 5);
- frozen release pins hash;
- seven ordered verification kinds.

The seven verification kinds, in order:

1. `toolchain_manifest_verification`
2. `test_profile_verification`
3. `candidate_copy_verification`
4. `test_sandbox_boundary_verification`
5. `terminal_proof_verification`
6. `root_cleanup_verification`
7. `uid_empty_verification`

Each kind produces a self-hashed `TestSandboxVerificationSeal` binding the verifier authority hash, observation hash, canonical hash, and kind-specific evidence hash. The aggregate verification-seals hash is the SHA-256 of the canonical array of the seven seal hashes in order.

## Evaluator

`EvaluateTestSandbox` first re-establishes fresh `VerifyReleaseTrust`, derives the frozen verifier authority, and requires exact equality. It then validates the test-claim authority (phase = `test_claim`, sequence = 3), checks authorization intactness, claim intactness, freshness, the predecessor adapter claim and terminal event, the Slice 5 adapter sandbox capability intactness, snapshot intactness (all seven seals recomputed), and authority matching.

The only capability construction occurs inside `EvaluateTestSandbox` after all checks pass for a `terminal_proven` state with the `admit_terminal_proof` action. No standalone production minter exists. `EffectAllowed` is always `false`.

## Opaque capability boundary

`VerifiedTestSandbox` is opaque. Its private integrity binds the observation hash, snapshot integrity hash, verifier authority hash, verification seals hash, authorization hash, claim hash, attempt hash, predecessor claim hash, adapter capability hash, UID lease hash, terminal proof hash, and canonical hash. `verifiedTestSandboxIntact` re-derives all seals from the decoded observation under the frozen verifier authority and requires exact reproduction of the aggregate verification-seals hash.

`VerifiedTestSandboxSnapshot` is opaque snapshot evidence. Its private integrity binds all seven verification booleans, the observation, canonical bytes/hash, verifier authority hash, seven seal hashes, and integrity hash. `verifiedTestSandboxSnapshotIntact` recomputes every seal from the decoded observation and requires exact match.

No production constructor or decoder from caller bytes exists. Test helpers within the package mint snapshots and capabilities; external callers cannot forge either.

## P6a non-goals

- cgo
- network/integration/service tests
- mutable external caches
- Git metadata
- missing downloads
- custom caller commands

## Closed schema inventory

| Record | Schema |
| --- | --- |
| Go toolchain manifest | `ananke.controlled-repair-go-toolchain-manifest.v1` |
| Go test profile | `ananke.controlled-repair-go-test-profile.v1` |
| test candidate copy observation | `ananke.controlled-repair-test-candidate-copy-observation.v1` |
| test sandbox observation | `ananke.controlled-repair-test-sandbox-observation.v1` |
| test terminal proof | `ananke.controlled-repair-test-terminal-proof.v1` |
| test sandbox verifier authority | `ananke.controlled-repair-test-sandbox-verifier-authority.v1` |
| test sandbox verification seal | `ananke.controlled-repair-test-sandbox-verification-seal.v1` |

## Ordered acceptance-vector inventory

The registry test `TestP6Slice6ExecutableVectorRegistry` invokes all 46 entries in this exact order, records actual execution order, rejects missing/duplicate/renamed/nil entries, and compares with the canonical inventory:

1. `canonical_terminal_proven`
2. `opaque_snapshot_deep_copy`
3. `opaque_snapshot_mutation_isolation`
4. `retained_terminal_replay`
5. `uid_not_empty_ambiguous`
6. `roots_not_scrubbed_ambiguous`
7. `stale_pid_epoch_ambiguous`
8. `uid_reuse_contention_ambiguous`
9. `git_push_attempt_ambiguous`
10. `ref_write_attempt_ambiguous`
11. `network_access_attempt_ambiguous`
12. `external_write_attempt_ambiguous`
13. `original_worktree_mutation_ambiguous`
14. `arbitrary_exec_attempt_ambiguous`
15. `fork_escape_ambiguous`
16. `setsid_new_session_ambiguous`
17. `delayed_write_ref_update_ambiguous`
18. `missing_module_ambiguous`
19. `cache_drift_ambiguous`
20. `toolchain_replacement_ambiguous`
21. `expired_freshness_rejects`
22. `wrong_phase_rejects`
23. `status_only_on_terminal_proven_rejects`
24. `admit_on_retain_for_human_rejects`
25. `canonical_json_closure`
26. `capability_mutation_valid_bit`
27. `capability_mutation_observation_hash`
28. `capability_mutation_verifier_authority_hash`
29. `capability_mutation_authorization_hash`
30. `capability_mutation_claim_hash`
31. `capability_mutation_adapter_capability_hash`
32. `capability_mutation_uid_lease_hash`
33. `capability_mutation_terminal_proof_hash`
34. `capability_mutation_canonical_bytes`
35. `frozen_toolchain_manifest_deterministic`
36. `frozen_test_profile_deterministic`
37. `frozen_verifier_authority_deterministic`
38. `uid_not_in_pool_rejects`
39. `wrong_pool_hash_rejects`
40. `wrong_toolchain_manifest_rejects`
41. `wrong_test_profile_rejects`
42. `unknown_cleanup_result_rejects`
43. `unknown_state_rejects`
44. `unknown_ambiguity_reason_rejects`
45. `terminal_proof_hash_mismatch_rejects`
46. `observation_hash_mismatch_rejects`

## Gate and remaining work

The focused package tests prove only these pure Slice 6 records, canonical bytes, semantic relations, and in-memory mutations. No P6 store, outbox persistence, transport, supervisor runtime, signature, test execution, sandbox activation, Go toolchain invocation, or migration is implemented or claimed. Contract Slices 7–9, a fresh independent hard review, and the plan's full `DESIGN ACCEPT` gate remain separate prerequisites before runtime or storage implementation.

<!-- BEGIN P6 SLICE 6 MACHINE CONTRACT -->
```json
{
  "schema_version": "ananke.controlled-repair-test-sandbox-document.v1",
  "status": "slice_6_candidate_pending_independent_frozen_source_review",
  "observation_schema_version": "ananke.controlled-repair-test-sandbox-observation.v1",
  "prior_slice_1_to_2_vector_count": 91,
  "prior_slice_3_vector_count": 99,
  "prior_slice_4_vector_count": 258,
  "prior_slice_5_vector_count": 40,
  "slice_6_vector_count": 46,
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
    "roots_not_scrubbed",
    "stale_pid_epoch",
    "uid_reuse_contention",
    "git_push_attempt",
    "local_ref_write_attempt",
    "network_access_attempt",
    "external_write_attempt",
    "original_worktree_mutation",
    "arbitrary_exec_attempt",
    "fork_escape",
    "setsid_new_session",
    "delayed_write_ref_update",
    "missing_module",
    "cache_drift",
    "toolchain_replacement"
  ],
  "cleanup_results": [
    "uid_empty_roots_scrubbed",
    "uid_nonempty_retained",
    "partial_retained"
  ],
  "dispositions": [
    "capability_ready",
    "retained_status",
    "waiting_for_human"
  ],
  "requirements": [
    "next_attestation_phase",
    "no_further_effect_permitted",
    "human_review_required"
  ],
  "verifier_authority": {
    "schema_version": "ananke.controlled-repair-test-sandbox-verifier-authority.v1",
    "verifier_id": "controlled_repair_test_sandbox_verifier_v1",
    "verifier_authority_hash": "sha256:1d4c5e49185f02abf13afca54fd28d8acc6f22d7827383771e40a5867f035bb4",
    "release_pins_hash": "sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a",
    "verification_kinds": [
      "toolchain_manifest_verification",
      "test_profile_verification",
      "candidate_copy_verification",
      "test_sandbox_boundary_verification",
      "terminal_proof_verification",
      "root_cleanup_verification",
      "uid_empty_verification"
    ]
  },
  "toolchain_manifest": {
    "schema_version": "ananke.controlled-repair-go-toolchain-manifest.v1",
    "manifest_id": "controlled_repair_go_toolchain_v1",
    "manifest_hash": "sha256:8e7a8962f3182915052ca5afc49b7d120040778e6263a7d86d3a9780625ed965",
    "go_version": "1.24.0"
  },
  "test_profile": {
    "schema_version": "ananke.controlled-repair-go-test-profile.v1",
    "profile_id": "controlled_repair_go_test_profile_v1",
    "profile_hash": "sha256:ad0ba689aba7f7b876616e59d8709dfc6f8298133d4dbb72a45187eafad0d283",
    "command": "go test ./... -count=1 -mod=readonly"
  },
  "uid_pool": {
    "schema_version": "ananke.controlled-repair-adapter-uid-pool.v1",
    "pool_id": "controlled_repair_runtime_uid_pool_v1",
    "pool_hash": "sha256:e66ca8742c9bc74acf99878be8165c55628f91827cacf244f2d0920bbc2de12b",
    "group_id": 62000,
    "group_name": "_ananke_repair",
    "pool_size": 4
  },
  "uid_lease_grammar": "attempt_<attempt_number>_test_uid_lease_001",
  "canonical_fixture": {
    "observation_hash": "sha256:0fb1753250c87e9f260b67d83ed1f9a328324d4fd6368ac0834d68737a1bcd25",
    "canonical_sha256": "sha256:2f18c77e4b98df4a09c07405b245924f9c000d887fce3ba72e37916d569844b3",
    "snapshot_integrity_hash": "sha256:039ec570c3acf62faef5e4005f6509ecdc9da60f4caf302d86a815e2e24bd086",
    "authorization_hash": "sha256:a4ebf38cc021a2c93f7ac2dc693744e2a7e3c6928f8a26d80a9ba501d00b2a11",
    "claim_hash": "sha256:d955a6dbc53b8bd0be79347e8c60024c0e88d09febddccc8e86aca631ee78bfa",
    "predecessor_claim_hash": "sha256:9fc9223dbe7dc06cbfc9c6bbdb5642c4ad5a6c40c9a36d0a61f29e49ba5259cc",
    "attempt_hash": "sha256:77ebb05bc3d8277f9835283b84f9db6272c61c97c2975fe45bb6fb8cdb581dae",
    "adapter_capability_hash": "sha256:9653538f232d30baee7b49b6cf3ad9df303dd6b57767278564cb2c31670fb725",
    "uid_lease_hash": "sha256:e26bb16a343d3353293c07b82facef220af1a41b125ceea16e12f358e08c64d7",
    "terminal_proof_hash": "sha256:41e928810cb0f9b768017966fac3c7d003dc9ae9573235901c126b6f54b6d226",
    "uid": 62002,
    "group_id": 62000
  },
  "vector_ids": [
    "canonical_terminal_proven",
    "opaque_snapshot_deep_copy",
    "opaque_snapshot_mutation_isolation",
    "retained_terminal_replay",
    "uid_not_empty_ambiguous",
    "roots_not_scrubbed_ambiguous",
    "stale_pid_epoch_ambiguous",
    "uid_reuse_contention_ambiguous",
    "git_push_attempt_ambiguous",
    "ref_write_attempt_ambiguous",
    "network_access_attempt_ambiguous",
    "external_write_attempt_ambiguous",
    "original_worktree_mutation_ambiguous",
    "arbitrary_exec_attempt_ambiguous",
    "fork_escape_ambiguous",
    "setsid_new_session_ambiguous",
    "delayed_write_ref_update_ambiguous",
    "missing_module_ambiguous",
    "cache_drift_ambiguous",
    "toolchain_replacement_ambiguous",
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
    "capability_mutation_adapter_capability_hash",
    "capability_mutation_uid_lease_hash",
    "capability_mutation_terminal_proof_hash",
    "capability_mutation_canonical_bytes",
    "frozen_toolchain_manifest_deterministic",
    "frozen_test_profile_deterministic",
    "frozen_verifier_authority_deterministic",
    "uid_not_in_pool_rejects",
    "wrong_pool_hash_rejects",
    "wrong_toolchain_manifest_rejects",
    "wrong_test_profile_rejects",
    "unknown_cleanup_result_rejects",
    "unknown_state_rejects",
    "unknown_ambiguity_reason_rejects",
    "terminal_proof_hash_mismatch_rejects",
    "observation_hash_mismatch_rejects"
  ]
}
```
<!-- END P6 SLICE 6 MACHINE CONTRACT -->
