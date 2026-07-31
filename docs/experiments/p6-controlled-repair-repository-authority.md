# P6 Controlled Repair Repository and Linked-Worktree Authority Contract

Status: **Slice 4 candidate pending independent frozen-source review**. Repair C closed the rejected review's P3 finding (closed enums plus orphan state); the P1 verifier-provenance and P2 installation-authority repairs are implemented below but have not yet passed independent frozen-source review. This document is normative only for the pure repository/common-`.git`/linked-worktree contract boundary. It does not claim Slice 4 acceptance, full P6 acceptance, or runtime authority.

## Scope

Slice 4 freezes canonical self-hashed repository materialization observations, closed enum domains, exact state/reason combinations, explicit status-only orphan semantics, a release-pinned verifier authority with verification-seal provenance, an authorization-bound opaque accepted-installation capability, a compiled Git 2.54 materializer profile freeze, authorization-derivable member content and worktree-slot grammar, exact common-`.git` delta semantics, configuration and attributes closure, candidate/source/writable-path closure, installed-root anti-alias, opaque verified snapshot evidence, retained and ambiguous status, and an opaque next-normal-phase capability. Canonical data alone is never filesystem or Git authority.

This slice performs no filesystem access, descriptor open, Git execution, process launch, network operation, store or SQLite operation, cleanup, deletion, prune, worktree creation, ref update, commit, push, merge, adapter launch, or automatic repair. The future no-follow descriptor opener, Git invocation/materializer, common-`.git` snapshotter, and production snapshot minter remain separately reviewed trusted runtime work. No production snapshot or capability minter exists in this slice.

## Release-pinned verifier authority

The compiled `installed_git_2_54_detached_worktree_materializer_v1` profile (schema `ananke.controlled-repair-repository-git-254-materializer-profile.v1`, self-hashed `materializer_profile_hash`, `git_version = "2.54"`) freezes the release-installed materializer policy as closed boolean semantics: detached-head-only, the exact six-member delta, commondir parent-parent only, system and global configuration disabled, no includes, no conditional includes, no hooks path, no attributes file, no filters, no fsmonitor, no external commands, system attributes disabled, and no external filter, process-filter, or command attributes. It is derived once at package initialization and the derivation panics on self-hash mismatch.

`RepositoryWorktreeVerifierAuthority` (schema `ananke.controlled-repair-repository-worktree-verifier-authority.v1`, self-hashed `verifier_authority_hash`) names verifier `controlled_repair_repository_worktree_verifier_v1`, the frozen profile ID and hash, the frozen `ReleasePinsHash`, and the ordered closed set of exactly seven verification kinds: `descriptor_verification`, `delta_verification`, `config_verification`, `attributes_verification`, `path_verification`, `uniqueness_verification`, and `ambiguity_verification`. It is derived at initialization from compiled material plus the frozen release pins and panics on self-hash mismatch. `EvaluateRepositoryWorktree` and `VerifyRepositoryWorktreeInstallation` first re-establish fresh release trust (`VerifyReleaseTrust` at the evaluation time) and require the rederived authority to equal the frozen value before any other evaluation step; every failure is closed as `ErrInvalidRepositoryWorktree`.

## Verification seals and their explicit non-claim

Each of the seven verification results is a `RepositoryWorktreeVerificationSeal` (schema `ananke.controlled-repair-repository-worktree-verification-seal.v1`, self-hashed `seal_hash`) binding its kind, the frozen verifier authority hash, the observation hash, the canonical bytes hash, and a kind-specific evidence hash. Evidence hashes are deterministic from the observation and the snapshot verification booleans: the ordered five descriptor hashes for descriptor evidence; the delta hash with before/after common-`.git` inventory hashes for delta evidence; the config, attributes, and writable-path closure hashes for config, attributes, and path evidence; the tuple/slot/ownership-certain booleans for uniqueness evidence; and the unambiguous boolean with the observation state and ambiguity reason for ambiguity evidence. The opaque snapshot and the minted capability carry the frozen verifier authority hash; the snapshot stores the seven seals and the capability stores `sha256` over the canonical encoding of the ordered seven seals. Both intact checks recompute every seal from the decoded observation and require exact equality; the capability recomputation re-establishes provenance under the frozen verifier authority and the exact-new mint invariants.

Seals name provenance and bind verifier, release, and observation. They do **not** claim that a trusted verifier physically executed, that descriptors were opened with no-follow semantics, or that Git ran: production snapshot minting remains future trusted runtime work, and no production snapshot or capability minter exists in this slice.

## Authorization-derived installation authority

The exported `RepositoryWorktreeInstallationAuthority` is a plain installed record with no authority of its own. Only `VerifyRepositoryWorktreeInstallation` can produce the opaque `VerifiedRepositoryWorktreeInstallation`: it requires fresh release trust, derived-equals-frozen verifier authority, an intact verified authorization, an intact matching supervisor intent authority, exact release-installed profile policy (profile ID and hash must equal the frozen Git 2.54 profile), the frozen authorization-derived worktree-slot grammar `attempt_<attempt_number>_materialization_worktree_001`, and the frozen slot path-hash derivation `sha256("worktrees/" + slot_id)`. Its private integrity binds the installed record, the frozen verifier authority hash, the authorization hash, and the attempt hash.

`HEAD` and `ORIG_HEAD` member content must hash exactly the authorized base commit plus a trailing newline, and `commondir` member content must hash exactly `"../.."` plus a trailing newline; these three contents are derivable from the accepted authorization and the frozen commondir value, so observed hashes must equal the derived digests. The `gitdir`, `index`, and `logs/HEAD` content hashes remain verifier-attested evidence bound by observation and snapshot integrity. Exact closure also requires the installed worktree root identity to alias no descriptor no-follow identity and no descriptor canonical path.

## Authority and retention

`EvaluateRepositoryWorktree` accepts only the opaque verified installation and requires the exact fresh phase-1 materialization `SupervisorIntentAuthority`, intact matching `VerifiedAuthorization`, intact opaque `VerifiedSupervisorIntentClaim`, a seal-rechecked opaque `VerifiedRepositoryWorktreeSnapshot` under the derived verifier authority, and the installation intact check (private integrity chain plus frozen verifier, profile, slot derivations, and authorization/attempt match). All successful assessments have `effect_allowed=false`.

Only an exact newly materialized snapshot can mint `VerifiedRepositoryWorktree` for the next normal phase, and that capability is constructed inline inside the evaluator only after every check has passed. Exact retained replay is status-only. Conflict, partial state, uncertain ownership, descriptor drift, configuration or attributes drift, protected common-`.git` drift, and path ambiguity retain the candidate for human review and mint no capability. An orphaned materialization is a distinct status-only state with exactly the `orphaned_materialization` reason; only `status_only` can classify it as waiting for human review. Orphan status mints no capability and grants no cleanup, deletion, prune, removal, ref update, commit, push, merge, launch, or second-worktree authority.

## Closed state and reason combinations

`new_exact` and `retained_exact_replay` require an empty ambiguity reason. `retain_for_human` requires exactly one of the ten closed non-orphan reasons: `conflicting_retained_state`, `partial_common_git_delta`, `uncertain_ownership`, `conflicting_worktree_slot`, `git_config_drift`, `git_attributes_drift`, `authorized_path_ambiguity`, `protected_common_git_drift`, `candidate_state_drift`, or `descriptor_identity_drift`. `orphaned_status_only` requires exactly `orphaned_materialization`. Every other state/reason pair is invalid at decode and evaluation boundaries.

## Exact common-`.git` closure

The only accepted new common-`.git` delta is one new `worktrees/<slot>/` admin subtree with six ordered members: `HEAD`, `ORIG_HEAD`, `commondir`, `gitdir`, `index`, and `logs/HEAD`. The contract freezes member IDs, repository-relative path hashes, and semantics; `HEAD`, `ORIG_HEAD`, and `commondir` contents are authorization-derivable and must equal the derived digests exactly, while the `gitdir`, `index`, and `logs/HEAD` content hashes and all descriptor hashes remain verifier-attested bound data rather than portable machine values. `HEAD` and `ORIG_HEAD` equal the authorized base commit, HEAD is detached, `commondir` means exactly `../..`, the candidate gitfile and admin `gitdir` cross-bind by canonical path hashes, the index equals the base tree, and initial candidate status is clean.

No pre-existing common-`.git` member may change or disappear. Refs, logs outside the candidate admin subtree, config, objects, hooks, info/exclude, info/attributes, alternates, shallow, grafts, replace, packed-refs, the common index, and worktree-admin siblings are protected ordered domains.

## Configuration, attributes, and paths

The common repository config descriptor and bytes are unchanged. The installed materializer profile must equal the frozen compiled Git 2.54 profile exactly; its identity disables system/global configuration and excludes includes, conditional includes, hooks paths, attributes files, filters, fsmonitor, and external commands. System attributes are disabled; common info attributes/exclude state is unchanged; base-tree `.gitattributes` inventory and effective attributes are verified; external filter, process-filter, and command attributes cannot be accepted.

The candidate is an exact child of the installed worktree root and does not alias the source or common `.git` root. Source identity and protected contents are unchanged. Common `.git` is outside adapter writable authority. Authorized writable-path records contain IDs and hashes only and must exactly match the ordered authorization set under the candidate, with no escape, symlink, hardlink, duplicate, prefix, case-fold, or Unicode-normalization alias.

## Machine-checked contract

The package document test parses the following block with unknown fields rejected and compares it to compiled constants, the frozen verifier authority and Git 2.54 materializer profile identities and hashes, the frozen slot grammar, the closed state, reason, and verification-kind inventories, the canonical deterministic fixture, the unchanged 91-vector accepted registry, the unchanged separate 99-vector Slice-3 registry, and the separate ordered executable 258-vector Slice-4 registry.

<!-- BEGIN P6 SLICE 4 MACHINE CONTRACT -->
```json
{
  "schema_version": "ananke.controlled-repair-repository-worktree-document.v1",
  "status": "slice_4_candidate_pending_independent_frozen_source_review",
  "observation_schema_version": "ananke.controlled-repair-repository-worktree-observation.v1",
  "prior_slice_vector_count": 91,
  "slice_3_vector_count": 99,
  "slice_4_vector_count": 258,
  "effect_allowed_values": [
    false
  ],
  "allowed_actions": [
    "admit_new_materialization",
    "status_only"
  ],
  "worktree_states": [
    "new_exact",
    "retained_exact_replay",
    "retain_for_human",
    "orphaned_status_only"
  ],
  "ambiguity_reasons": [
    "conflicting_retained_state",
    "partial_common_git_delta",
    "uncertain_ownership",
    "conflicting_worktree_slot",
    "git_config_drift",
    "git_attributes_drift",
    "authorized_path_ambiguity",
    "protected_common_git_drift",
    "candidate_state_drift",
    "descriptor_identity_drift",
    "orphaned_materialization"
  ],
  "dispositions": [
    "capability_ready",
    "retained_status",
    "waiting_for_human"
  ],
  "requirements": [
    "next_normal_phase",
    "no_further_effect_permitted",
    "human_review_required"
  ],
  "verifier_authority": {
    "schema_version": "ananke.controlled-repair-repository-worktree-verifier-authority.v1",
    "verifier_id": "controlled_repair_repository_worktree_verifier_v1",
    "verifier_authority_hash": "sha256:24538f3fd98a3df4208aca485cdfb398e018a649c48e72d7a0fec2aa865bab2e",
    "release_pins_hash": "sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a",
    "verification_kinds": [
      "descriptor_verification",
      "delta_verification",
      "config_verification",
      "attributes_verification",
      "path_verification",
      "uniqueness_verification",
      "ambiguity_verification"
    ]
  },
  "materializer_profile": {
    "schema_version": "ananke.controlled-repair-repository-git-254-materializer-profile.v1",
    "materializer_profile_id": "installed_git_2_54_detached_worktree_materializer_v1",
    "materializer_profile_hash": "sha256:93b63bc17d7dc891ca6a9ed99009d3161a2b2b494a4126e1bc984621689e3180",
    "git_version": "2.54"
  },
  "worktree_slot_grammar": "attempt_<attempt_number\u003e_materialization_worktree_001",
  "common_git_members": [
    {
      "sequence": 1,
      "member_id": "head",
      "repository_relative_path_hash": "sha256:b5180223165af3583fd0724209986caf2a62692654b74c525027dda592404330",
      "semantic": "detached_head_at_base_commit"
    },
    {
      "sequence": 2,
      "member_id": "orig_head",
      "repository_relative_path_hash": "sha256:4ba7921e4f29d98064d7859fb9ab180af434739f67ef323cadbe729486f711d0",
      "semantic": "orig_head_at_base_commit"
    },
    {
      "sequence": 3,
      "member_id": "commondir",
      "repository_relative_path_hash": "sha256:5535069517cdb27cf6f0fa7be0daabb0031c135577becf213837363a4fb9a2f9",
      "semantic": "commondir_parent_parent"
    },
    {
      "sequence": 4,
      "member_id": "gitdir",
      "repository_relative_path_hash": "sha256:15d5ca6f4048f2c6ae14c5c7f643f1a9392e9a441ca5239afad01e3a2adab460",
      "semantic": "admin_gitdir_candidate_backlink"
    },
    {
      "sequence": 5,
      "member_id": "index",
      "repository_relative_path_hash": "sha256:1bc04b5291c26a46d918139138b992d2de976d6851d0893b0476b85bfbdfc6e6",
      "semantic": "index_at_base_tree"
    },
    {
      "sequence": 6,
      "member_id": "logs_head",
      "repository_relative_path_hash": "sha256:12576f4e326d8ab7c09388f2b84c759f91cee4498b64a2694d6b0c57a299b768",
      "semantic": "detached_checkout_head_log"
    }
  ],
  "protected_common_git_domains": [
    "refs",
    "logs_outside_candidate_admin",
    "config",
    "objects",
    "hooks",
    "info_exclude",
    "info_attributes",
    "alternates",
    "shallow",
    "grafts",
    "replace",
    "packed_refs",
    "common_index",
    "worktree_admin_siblings"
  ],
  "canonical_fixture": {
    "observation_hash": "sha256:be625fbe21886e8651484df37308b6965ded908045c9c5a8e669ffc7af346b9e",
    "canonical_sha256": "sha256:3816a2c1119234919ea05f3b59a73540287953e65e6964e1dea78ffeeb731c63",
    "snapshot_integrity_hash": "sha256:6ca353f8a8091de3d84bddb8fd69601cd78ad7fd21bde2e8034c40eb034be39b",
    "authorization_hash": "sha256:a4ebf38cc021a2c93f7ac2dc693744e2a7e3c6928f8a26d80a9ba501d00b2a11",
    "claim_hash": "sha256:45ff41731f981cce4d5c38397b588c4c4f42f2f01c01cc315f0410dc1768bc27",
    "repository_binding_hash": "sha256:51202788cff44e7f9dc4625006dede96cee255aef8249b3bb623efd59228f3a8",
    "base_commit": "7a1f7ce102f6611a6f4ddbd6ee45263f211e9588",
    "base_tree": "9b5f88f170846bf4b5fc7595f53344f993bfde12",
    "worktree_slot_id": "attempt_1_materialization_worktree_001",
    "worktree_slot_path_hash": "sha256:6bf066f93836c8ccf72560b69f159531f8af5f923f285393e42d0c1ffa4b4e22",
    "installed_worktree_root_identity_hash": "sha256:5e6d4d5ad33191479e41faa3d4e93f17114cc0ccd93b7f10dd5cefd2fae44b27",
    "materializer_profile_id": "installed_git_2_54_detached_worktree_materializer_v1",
    "materializer_profile_hash": "sha256:93b63bc17d7dc891ca6a9ed99009d3161a2b2b494a4126e1bc984621689e3180",
    "before_common_git_inventory_hash": "sha256:859c1c8c1ecca32f460ee6264b88cf3c19761030e52a06c3667bde3875779c7d",
    "after_common_git_inventory_hash": "sha256:8285ae8cfa1d711e5f5e4dbf4b465ba361a768d15957187c80f3c5c6235c9878",
    "common_git_delta_hash": "sha256:d16e53aa7dbccf49fed6924234e1d5ef2d0c44d7685f1d238ba79aa12c42f17c",
    "config_closure_hash": "sha256:4a35bf5f34994b399a1143f4d3fb01a4c2b6c8348ab4f019724e79d312956c83",
    "attributes_closure_hash": "sha256:e239cb862aea396af8045922436bb401c481e857f31a0707f2762a01f7303d04",
    "writable_path_closure_hash": "sha256:630b3235db2d602df32c1b2f49b9c50a9da51dbbd5f32cb1db11c2ebefa84c4f",
    "authorized_path_set_hash": "sha256:e4122d52424f767d1a5e837092de3b428b09793ec9650dfb505904d57bfc4e04",
    "descriptors": [
      {
        "descriptor_id": "source_root",
        "descriptor_hash": "sha256:ad6b350bb31fd9671163d1304e240b088db66e2726ddd10ec4debf1e8dbdf60b"
      },
      {
        "descriptor_id": "common_git_root",
        "descriptor_hash": "sha256:1c8a8c226378c09a3b7a834aceef60f8e07731ee97d5cd361db5dd2419363aa3"
      },
      {
        "descriptor_id": "candidate_root",
        "descriptor_hash": "sha256:b7bf603a1b3ad16de47d56152f24be288d3697fa8efb62b941284128b0882be7"
      },
      {
        "descriptor_id": "candidate_gitfile",
        "descriptor_hash": "sha256:6f5181a87179e5b648a6f65aea4c1c4083fe58820a748ca8142020de63c3f9b5"
      },
      {
        "descriptor_id": "candidate_admin_subtree",
        "descriptor_hash": "sha256:d6b403d6c26c057f81e5d0e50ecd128e5637eb9e42b5eeed84b2a6416b290cb2"
      }
    ]
  },
  "vector_ids": [
    "canonical_exact_new_materialization",
    "opaque_snapshot_deep_copy",
    "opaque_snapshot_integrity_mutation",
    "nil_opaque_snapshot",
    "zero_opaque_snapshot",
    "unverified_descriptor_snapshot",
    "unverified_delta_snapshot",
    "unverified_config_snapshot",
    "unverified_attributes_snapshot",
    "unverified_path_snapshot",
    "unverified_uniqueness_snapshot",
    "unverified_ambiguity_snapshot",
    "second_worktree_same_tuple",
    "slot_reused_across_attempt",
    "slot_reused_across_phase",
    "preexisting_exact_retained_replay",
    "preexisting_conflicting_retained_state",
    "preexisting_partial_retained_state",
    "preexisting_ambiguous_retained_state",
    "omit_head_member",
    "omit_orig_head_member",
    "omit_commondir_member",
    "omit_gitdir_member",
    "omit_index_member",
    "omit_logs_head_member",
    "duplicate_head_member",
    "duplicate_orig_head_member",
    "duplicate_commondir_member",
    "duplicate_gitdir_member",
    "duplicate_index_member",
    "duplicate_logs_head_member",
    "reorder_head_member",
    "reorder_orig_head_member",
    "reorder_commondir_member",
    "reorder_gitdir_member",
    "reorder_index_member",
    "reorder_logs_head_member",
    "rename_head_member",
    "rename_orig_head_member",
    "rename_commondir_member",
    "rename_gitdir_member",
    "rename_index_member",
    "rename_logs_head_member",
    "swap_head_content_semantic",
    "swap_orig_head_content_semantic",
    "swap_commondir_content_semantic",
    "swap_gitdir_content_semantic",
    "swap_index_content_semantic",
    "swap_logs_head_content_semantic",
    "extra_common_git_member",
    "changed_preexisting_common_git_member",
    "removed_preexisting_common_git_member",
    "changed_common_config_bytes",
    "changed_common_config_descriptor",
    "config_include",
    "config_include_if",
    "config_hooks_path",
    "config_attributes_file",
    "config_filter",
    "config_process_filter",
    "config_fsmonitor",
    "config_external_command",
    "global_config_enabled",
    "system_config_enabled",
    "materializer_profile_mismatch",
    "system_attributes_enabled",
    "changed_info_attributes",
    "changed_info_exclude",
    "changed_base_tree_gitattributes",
    "changed_effective_attributes",
    "external_filter_attribute",
    "process_filter_attribute",
    "external_command_attribute",
    "changed_refs",
    "changed_logs_outside_candidate",
    "changed_common_git_config_domain",
    "changed_objects",
    "changed_hooks",
    "changed_info_exclude_domain",
    "changed_info_attributes_domain",
    "changed_alternates",
    "changed_shallow",
    "changed_grafts",
    "changed_replace",
    "changed_packed_refs",
    "changed_common_index",
    "changed_other_worktree_sibling",
    "branch_head",
    "wrong_head_commit",
    "wrong_orig_head_commit",
    "wrong_index_tree",
    "dirty_candidate",
    "branch_ref_created",
    "branch_ref_updated",
    "other_ref_updated",
    "candidate_gitfile_admin_crosslink_mismatch",
    "admin_gitdir_candidate_crosslink_mismatch",
    "commondir_mismatch",
    "source_candidate_alias",
    "source_common_git_alias",
    "candidate_common_git_alias",
    "source_root_identity_changed",
    "source_protected_contents_changed",
    "common_git_writable_by_adapter",
    "candidate_not_exact_child",
    "candidate_not_new",
    "authorized_path_missing",
    "authorized_path_extra",
    "authorized_path_reordered",
    "authorized_path_duplicate",
    "authorized_path_prefix_escape",
    "authorized_path_symlink",
    "authorized_path_hardlink",
    "authorized_path_case_fold_alias",
    "authorized_path_unicode_normalization_alias",
    "authorized_path_outside_candidate",
    "authorized_path_ancestor_not_no_follow",
    "claim_fresh_n_minus_1ns",
    "claim_stale_n",
    "claim_stale_n_plus_1ns",
    "authorization_fresh_n_minus_1ns",
    "authorization_stale_n",
    "authorization_stale_n_plus_1ns",
    "dispatch_fresh_n_minus_1ns",
    "dispatch_stale_n",
    "dispatch_stale_n_plus_1ns",
    "release_fresh_n_minus_1ns",
    "release_stale_n",
    "release_stale_n_plus_1ns",
    "wrong_supervisor_phase",
    "nil_verified_authorization",
    "nil_verified_claim",
    "claim_from_other_attempt",
    "request_cleanup",
    "request_delete",
    "request_prune",
    "request_remove",
    "request_ref_update",
    "request_commit",
    "request_push",
    "request_merge",
    "request_launch",
    "request_second_worktree",
    "request_second_effect",
    "observation_unknown_json_member",
    "observation_duplicate_json_member",
    "observation_trailing_json",
    "observation_noncanonical_json",
    "observation_self_hash_mismatch",
    "observation_map_order_determinism",
    "forged_head_member_content",
    "forged_orig_head_member_content",
    "forged_commondir_member_content",
    "snapshot_wrong_verifier_authority",
    "snapshot_empty_verifier_authority",
    "stale_descriptor_seal",
    "stale_delta_seal",
    "stale_config_seal",
    "stale_attributes_seal",
    "stale_path_seal",
    "stale_uniqueness_seal",
    "stale_ambiguity_seal",
    "capability_integrity_mutation",
    "capability_verifier_authority_mutation",
    "capability_verification_seals_mutation",
    "verifier_authority_release_binding",
    "unknown_materializer_profile_id",
    "unknown_materializer_profile_hash",
    "nil_verified_installation",
    "zero_verified_installation",
    "cross_authorization_installation",
    "attempt_2_canonical_worktree_slot_accepted",
    "caller_invented_worktree_slot",
    "worktree_slot_reused_across_attempts",
    "wrong_worktree_slot_path_hash",
    "installed_root_aliases_source_root_identity",
    "installed_root_aliases_common_git_root_identity",
    "installed_root_aliases_candidate_root_identity",
    "installed_root_aliases_candidate_gitfile_identity",
    "installed_root_aliases_candidate_admin_identity",
    "installed_root_aliases_descriptor_canonical_path",
    "installation_integrity_mutation",
    "unknown_ambiguity_reason_decode",
    "unknown_ambiguity_reason_evaluation",
    "unknown_descriptor_id_decode",
    "unknown_descriptor_id_evaluation",
    "unknown_member_id_decode",
    "unknown_member_id_evaluation",
    "unknown_member_semantic_decode",
    "unknown_member_semantic_evaluation",
    "unknown_added_member_id_decode",
    "unknown_added_member_id_evaluation",
    "unknown_protected_domain_id_decode",
    "unknown_protected_domain_id_evaluation",
    "state_reason_new_exact_empty",
    "state_reason_new_exact_conflicting_retained_state",
    "state_reason_new_exact_partial_common_git_delta",
    "state_reason_new_exact_uncertain_ownership",
    "state_reason_new_exact_conflicting_worktree_slot",
    "state_reason_new_exact_git_config_drift",
    "state_reason_new_exact_git_attributes_drift",
    "state_reason_new_exact_authorized_path_ambiguity",
    "state_reason_new_exact_protected_common_git_drift",
    "state_reason_new_exact_candidate_state_drift",
    "state_reason_new_exact_descriptor_identity_drift",
    "state_reason_new_exact_orphaned_materialization",
    "state_reason_retained_exact_empty",
    "state_reason_retained_exact_conflicting_retained_state",
    "state_reason_retained_exact_partial_common_git_delta",
    "state_reason_retained_exact_uncertain_ownership",
    "state_reason_retained_exact_conflicting_worktree_slot",
    "state_reason_retained_exact_git_config_drift",
    "state_reason_retained_exact_git_attributes_drift",
    "state_reason_retained_exact_authorized_path_ambiguity",
    "state_reason_retained_exact_protected_common_git_drift",
    "state_reason_retained_exact_candidate_state_drift",
    "state_reason_retained_exact_descriptor_identity_drift",
    "state_reason_retained_exact_orphaned_materialization",
    "state_reason_retain_for_human_empty",
    "state_reason_retain_for_human_conflicting_retained_state",
    "state_reason_retain_for_human_partial_common_git_delta",
    "state_reason_retain_for_human_uncertain_ownership",
    "state_reason_retain_for_human_conflicting_worktree_slot",
    "state_reason_retain_for_human_git_config_drift",
    "state_reason_retain_for_human_git_attributes_drift",
    "state_reason_retain_for_human_authorized_path_ambiguity",
    "state_reason_retain_for_human_protected_common_git_drift",
    "state_reason_retain_for_human_candidate_state_drift",
    "state_reason_retain_for_human_descriptor_identity_drift",
    "state_reason_retain_for_human_orphaned_materialization",
    "state_reason_orphaned_status_only_empty",
    "state_reason_orphaned_status_only_conflicting_retained_state",
    "state_reason_orphaned_status_only_partial_common_git_delta",
    "state_reason_orphaned_status_only_uncertain_ownership",
    "state_reason_orphaned_status_only_conflicting_worktree_slot",
    "state_reason_orphaned_status_only_git_config_drift",
    "state_reason_orphaned_status_only_git_attributes_drift",
    "state_reason_orphaned_status_only_authorized_path_ambiguity",
    "state_reason_orphaned_status_only_protected_common_git_drift",
    "state_reason_orphaned_status_only_candidate_state_drift",
    "state_reason_orphaned_status_only_descriptor_identity_drift",
    "state_reason_orphaned_status_only_orphaned_materialization",
    "orphan_status_only_waiting_for_human",
    "orphan_reject_admit_new_materialization",
    "orphan_reject_cleanup",
    "orphan_reject_delete",
    "orphan_reject_prune",
    "orphan_reject_remove",
    "orphan_reject_ref_update",
    "orphan_reject_commit",
    "orphan_reject_push",
    "orphan_reject_merge",
    "orphan_reject_launch",
    "orphan_reject_second_worktree",
    "orphan_reject_second_effect",
    "orphan_fresh_n_minus_1ns",
    "orphan_stale_n",
    "orphan_stale_n_plus_1ns"
  ]
}
```
<!-- END P6 SLICE 4 MACHINE CONTRACT -->

## Non-claims

The canonical fixture and test-only opaque minters are deterministic contract oracles, not runtime observations. Repair C closed the rejected review's P3 enum/orphan finding, and the P1 verifier-provenance and P2 installation-authority repairs are implemented as described above, but Slice 4 remains a candidate pending independent frozen-source review. Verification seals name provenance and bind verifier, release, and observation; they do not claim that a trusted verifier physically executed. Production snapshot minting remains future trusted runtime work, and no production snapshot or capability minter exists in this slice. This document makes no claim of Slice 4 acceptance, real descriptor verification, Git execution, materialization, production capability minting, cleanup safety, or adapter execution.
