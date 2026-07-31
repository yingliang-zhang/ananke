package repaircontract

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"
)

type repositoryWorktreeExecutableVector struct {
	id      string
	wantErr error
	run     func(*testing.T) error
}

var canonicalRepositoryWorktreeVectorIDs = append([]string{
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
}, repositoryWorktreeRepairCVectorIDs...)

var repositoryWorktreeVectorRegistry = append([]repositoryWorktreeExecutableVector{
	{id: "canonical_exact_new_materialization", run: canonicalRepositoryWorktreeRegistryProbe},
	{id: "opaque_snapshot_deep_copy", run: repositoryWorktreeDeepCopyProbe},
	{id: "opaque_snapshot_integrity_mutation", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) { v.integrityHash = testHash("wrong-integrity") })},
	{id: "nil_opaque_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: nilRepositoryWorktreeSnapshotProbe},
	{id: "zero_opaque_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotValueProbe(&VerifiedRepositoryWorktreeSnapshot{})},
	{id: "unverified_descriptor_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.descriptorVerified = false
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "unverified_delta_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.deltaVerified = false
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "unverified_config_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.configVerified = false
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "unverified_attributes_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.attributesVerified = false
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "unverified_path_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.pathVerified = false
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "unverified_uniqueness_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.uniquenessVerified = false
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "unverified_ambiguity_snapshot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.ambiguityChecked = false
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "second_worktree_same_tuple", run: repositoryWorktreeUniquenessWaitingProbe(false, true)},
	{id: "slot_reused_across_attempt", run: repositoryWorktreeUniquenessWaitingProbe(true, false)},
	{id: "slot_reused_across_phase", run: repositoryWorktreeUniquenessWaitingProbe(true, false)},
	{id: "preexisting_exact_retained_replay", run: exactRetainedRepositoryWorktreeProbe},
	{id: "preexisting_conflicting_retained_state", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeConflictingRetained, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.CommonGitDelta.ChangedPreexistingMemberHashes = []string{testHash("conflict")}
	})},
	{id: "preexisting_partial_retained_state", run: memberMutationProbe("omit", 0)},
	{id: "preexisting_ambiguous_retained_state", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeUncertainOwnership, func(f *canonicalRepositoryWorktreeFixture) { f.snapshot.ownershipCertain = false })},

	{id: "omit_head_member", run: memberMutationProbe("omit", 0)},
	{id: "omit_orig_head_member", run: memberMutationProbe("omit", 1)},
	{id: "omit_commondir_member", run: memberMutationProbe("omit", 2)},
	{id: "omit_gitdir_member", run: memberMutationProbe("omit", 3)},
	{id: "omit_index_member", run: memberMutationProbe("omit", 4)},
	{id: "omit_logs_head_member", run: memberMutationProbe("omit", 5)},
	{id: "duplicate_head_member", run: memberMutationProbe("duplicate", 0)},
	{id: "duplicate_orig_head_member", run: memberMutationProbe("duplicate", 1)},
	{id: "duplicate_commondir_member", run: memberMutationProbe("duplicate", 2)},
	{id: "duplicate_gitdir_member", run: memberMutationProbe("duplicate", 3)},
	{id: "duplicate_index_member", run: memberMutationProbe("duplicate", 4)},
	{id: "duplicate_logs_head_member", run: memberMutationProbe("duplicate", 5)},
	{id: "reorder_head_member", run: memberMutationProbe("reorder", 0)},
	{id: "reorder_orig_head_member", run: memberMutationProbe("reorder", 1)},
	{id: "reorder_commondir_member", run: memberMutationProbe("reorder", 2)},
	{id: "reorder_gitdir_member", run: memberMutationProbe("reorder", 3)},
	{id: "reorder_index_member", run: memberMutationProbe("reorder", 4)},
	{id: "reorder_logs_head_member", run: memberMutationProbe("reorder", 5)},
	{id: "rename_head_member", wantErr: ErrInvalidRepositoryWorktree, run: memberMutationProbe("rename", 0)},
	{id: "rename_orig_head_member", wantErr: ErrInvalidRepositoryWorktree, run: memberMutationProbe("rename", 1)},
	{id: "rename_commondir_member", wantErr: ErrInvalidRepositoryWorktree, run: memberMutationProbe("rename", 2)},
	{id: "rename_gitdir_member", wantErr: ErrInvalidRepositoryWorktree, run: memberMutationProbe("rename", 3)},
	{id: "rename_index_member", wantErr: ErrInvalidRepositoryWorktree, run: memberMutationProbe("rename", 4)},
	{id: "rename_logs_head_member", wantErr: ErrInvalidRepositoryWorktree, run: memberMutationProbe("rename", 5)},
	{id: "swap_head_content_semantic", run: memberMutationProbe("semantic", 0)},
	{id: "swap_orig_head_content_semantic", run: memberMutationProbe("semantic", 1)},
	{id: "swap_commondir_content_semantic", run: memberMutationProbe("semantic", 2)},
	{id: "swap_gitdir_content_semantic", run: memberMutationProbe("semantic", 3)},
	{id: "swap_index_content_semantic", run: memberMutationProbe("semantic", 4)},
	{id: "swap_logs_head_content_semantic", run: memberMutationProbe("semantic", 5)},
	{id: "extra_common_git_member", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreePartialDelta, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.CommonGitDelta.ExtraAddedMemberHashes = []string{testHash("extra-common-git-member")}
	})},
	{id: "changed_preexisting_common_git_member", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeProtectedCommonGitDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.CommonGitDelta.ChangedPreexistingMemberHashes = []string{testHash("changed-preexisting")}
	})},
	{id: "removed_preexisting_common_git_member", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeProtectedCommonGitDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.CommonGitDelta.RemovedPreexistingMemberHashes = []string{testHash("removed-preexisting")}
	})},

	{id: "changed_common_config_bytes", run: configMutationProbe(func(v *RepositoryGitConfigClosure) {
		v.CommonConfigBytesHashAfter = testHash("changed-config")
		v.CommonConfigUnchanged = false
	})},
	{id: "changed_common_config_descriptor", run: configMutationProbe(func(v *RepositoryGitConfigClosure) {
		v.CommonConfigDescriptorHashAfter = testHash("changed-config-descriptor")
		v.CommonConfigUnchanged = false
	})},
	{id: "config_include", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.NoIncludes = false })},
	{id: "config_include_if", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.NoIncludeIf = false })},
	{id: "config_hooks_path", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.NoHooksPath = false })},
	{id: "config_attributes_file", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.NoAttributesFile = false })},
	{id: "config_filter", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.NoFilters = false })},
	{id: "config_process_filter", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.NoFilters = false; v.NoExternalCommands = false })},
	{id: "config_fsmonitor", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.NoFSMonitor = false })},
	{id: "config_external_command", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.NoExternalCommands = false })},
	{id: "global_config_enabled", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.GlobalConfigDisabled = false })},
	{id: "system_config_enabled", run: configMutationProbe(func(v *RepositoryGitConfigClosure) { v.SystemConfigDisabled = false })},
	{id: "materializer_profile_mismatch", run: configMutationProbe(func(v *RepositoryGitConfigClosure) {
		v.MaterializerProfileHash = testHash("other-materializer-profile")
	})},

	{id: "system_attributes_enabled", run: attributesMutationProbe(func(v *RepositoryGitAttributesClosure) { v.SystemAttributesDisabled = false })},
	{id: "changed_info_attributes", run: attributesMutationProbe(func(v *RepositoryGitAttributesClosure) {
		v.InfoAttributesExists = true
		v.InfoAttributesDescriptorHashBefore = testHash("info-attributes-before")
		v.InfoAttributesDescriptorHashAfter = testHash("info-attributes-after")
		v.InfoAttributesContentHashBefore = testHash("info-attributes-content-before")
		v.InfoAttributesContentHashAfter = testHash("info-attributes-content-after")
		v.InfoAttributesUnchanged = false
	})},
	{id: "changed_info_exclude", run: attributesMutationProbe(func(v *RepositoryGitAttributesClosure) {
		v.InfoExcludeExists = true
		v.InfoExcludeDescriptorHashBefore = testHash("info-exclude-before")
		v.InfoExcludeDescriptorHashAfter = testHash("info-exclude-after")
		v.InfoExcludeContentHashBefore = testHash("info-exclude-content-before")
		v.InfoExcludeContentHashAfter = testHash("info-exclude-content-after")
		v.InfoExcludeUnchanged = false
	})},
	{id: "changed_base_tree_gitattributes", run: attributesMutationProbe(func(v *RepositoryGitAttributesClosure) {
		v.BaseTreeGitattributesInventoryHash = testHash("changed-gitattributes-inventory")
		v.BaseTreeGitattributesVerified = false
	})},
	{id: "changed_effective_attributes", run: attributesMutationProbe(func(v *RepositoryGitAttributesClosure) {
		v.EffectiveAttributesHash = testHash("changed-effective-attributes")
	})},
	{id: "external_filter_attribute", run: attributesMutationProbe(func(v *RepositoryGitAttributesClosure) { v.NoExternalFilterAttributes = false })},
	{id: "process_filter_attribute", run: attributesMutationProbe(func(v *RepositoryGitAttributesClosure) { v.NoProcessFilterAttributes = false })},
	{id: "external_command_attribute", run: attributesMutationProbe(func(v *RepositoryGitAttributesClosure) { v.NoExternalCommandAttributes = false })},

	{id: "changed_refs", run: protectedDomainMutationProbe(0)},
	{id: "changed_logs_outside_candidate", run: protectedDomainMutationProbe(1)},
	{id: "changed_common_git_config_domain", run: protectedDomainMutationProbe(2)},
	{id: "changed_objects", run: protectedDomainMutationProbe(3)},
	{id: "changed_hooks", run: protectedDomainMutationProbe(4)},
	{id: "changed_info_exclude_domain", run: protectedDomainMutationProbe(5)},
	{id: "changed_info_attributes_domain", run: protectedDomainMutationProbe(6)},
	{id: "changed_alternates", run: protectedDomainMutationProbe(7)},
	{id: "changed_shallow", run: protectedDomainMutationProbe(8)},
	{id: "changed_grafts", run: protectedDomainMutationProbe(9)},
	{id: "changed_replace", run: protectedDomainMutationProbe(10)},
	{id: "changed_packed_refs", run: protectedDomainMutationProbe(11)},
	{id: "changed_common_index", run: protectedDomainMutationProbe(12)},
	{id: "changed_other_worktree_sibling", run: protectedDomainMutationProbe(13)},

	{id: "branch_head", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeCandidateDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.Candidate.HeadMode = RepositoryCandidateBranch
	})},
	{id: "wrong_head_commit", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeCandidateDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.Candidate.HEADCommit = stringsRepeatOID("a")
	})},
	{id: "wrong_orig_head_commit", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeCandidateDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.Candidate.ORIGHEADCommit = stringsRepeatOID("b")
	})},
	{id: "wrong_index_tree", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeCandidateDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Candidate.IndexTree = stringsRepeatOID("c") })},
	{id: "dirty_candidate", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeCandidateDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.Candidate.InitialStatus = RepositoryCandidateDirty
	})},
	{id: "branch_ref_created", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeCandidateDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Candidate.BranchRefCreated = true })},
	{id: "branch_ref_updated", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeCandidateDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Candidate.BranchRefUpdated = true })},
	{id: "other_ref_updated", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeCandidateDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Candidate.OtherRefUpdated = true })},
	{id: "candidate_gitfile_admin_crosslink_mismatch", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.Candidate.CandidateGitfileTargetPathHash = testHash("wrong-admin-target")
	})},
	{id: "admin_gitdir_candidate_crosslink_mismatch", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.Candidate.AdminGitdirBacklinkPathHash = testHash("wrong-gitfile-backlink")
	})},
	{id: "commondir_mismatch", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreePartialDelta, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.CommonGitDelta.Members[2].SemanticTargetHash = testHash("not-parent-parent")
	})},
	{id: "source_candidate_alias", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Source.SourceCandidateAlias = true })},
	{id: "source_common_git_alias", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Source.SourceCommonGitAlias = true })},
	{id: "candidate_common_git_alias", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Source.CandidateCommonGitAlias = true })},
	{id: "source_root_identity_changed", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.Source.SourceRootIdentityHashAfter = testHash("changed-source-root")
		f.observation.Source.SourceUnchanged = false
	})},
	{id: "source_protected_contents_changed", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) {
		f.observation.Source.ProtectedContentsHashAfter = testHash("changed-source-contents")
		f.observation.Source.SourceUnchanged = false
	})},
	{id: "common_git_writable_by_adapter", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Source.CommonGitWritableByAdapter = true })},
	{id: "candidate_not_exact_child", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Source.CandidateExactChild = false })},
	{id: "candidate_not_new", run: mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeDescriptorDrift, func(f *canonicalRepositoryWorktreeFixture) { f.observation.Source.CandidateNew = false })},

	{id: "authorized_path_missing", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) {
		v.Paths = append([]RepositoryWritablePathObservation(nil), v.Paths[:len(v.Paths)-1]...)
	})},
	{id: "authorized_path_extra", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) {
		extra := v.Paths[0]
		extra.Sequence = len(v.Paths) + 1
		extra.PathID = "extra_authorized_path"
		extra.RepositoryRelativePathHash = testHash("extra-authorized-path")
		v.Paths = append(v.Paths, extra)
	})},
	{id: "authorized_path_reordered", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) { v.Paths[0], v.Paths[1] = v.Paths[1], v.Paths[0] })},
	{id: "authorized_path_duplicate", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) { v.Paths = append(v.Paths, v.Paths[0]); v.NoDuplicates = false })},
	{id: "authorized_path_prefix_escape", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) { v.NoPrefixEscapes = false })},
	{id: "authorized_path_symlink", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) { v.NoSymlinks = false })},
	{id: "authorized_path_hardlink", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) {
		v.Paths[1].LeafIdentityHash = v.Paths[0].LeafIdentityHash
		v.NoHardlinks = false
	})},
	{id: "authorized_path_case_fold_alias", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) { v.NoCaseFoldCollisions = false })},
	{id: "authorized_path_unicode_normalization_alias", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) { v.NoUnicodeNormalizationCollisions = false })},
	{id: "authorized_path_outside_candidate", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) { v.AllPathsUnderCandidate = false })},
	{id: "authorized_path_ancestor_not_no_follow", run: pathMutationProbe(func(v *RepositoryWritablePathClosure) { v.AncestorsNoFollowVerified = false })},

	{id: "claim_fresh_n_minus_1ns", run: repositoryWorktreeClaimBoundaryProbe(-time.Nanosecond, true)},
	{id: "claim_stale_n", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeClaimBoundaryProbe(0, false)},
	{id: "claim_stale_n_plus_1ns", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeClaimBoundaryProbe(time.Nanosecond, false)},
	{id: "authorization_fresh_n_minus_1ns", run: repositoryWorktreeIndependentBoundaryProbe("authorization", -time.Nanosecond)},
	{id: "authorization_stale_n", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeIndependentBoundaryProbe("authorization", 0)},
	{id: "authorization_stale_n_plus_1ns", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeIndependentBoundaryProbe("authorization", time.Nanosecond)},
	{id: "dispatch_fresh_n_minus_1ns", run: repositoryWorktreeIndependentBoundaryProbe("dispatch", -time.Nanosecond)},
	{id: "dispatch_stale_n", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeIndependentBoundaryProbe("dispatch", 0)},
	{id: "dispatch_stale_n_plus_1ns", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeIndependentBoundaryProbe("dispatch", time.Nanosecond)},
	{id: "release_fresh_n_minus_1ns", run: repositoryWorktreeIndependentBoundaryProbe("release", -time.Nanosecond)},
	{id: "release_stale_n", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeIndependentBoundaryProbe("release", 0)},
	{id: "release_stale_n_plus_1ns", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeIndependentBoundaryProbe("release", time.Nanosecond)},
	{id: "wrong_supervisor_phase", wantErr: ErrInvalidRepositoryWorktree, run: wrongRepositoryWorktreePhaseProbe},
	{id: "nil_verified_authorization", wantErr: ErrInvalidRepositoryWorktree, run: nilRepositoryWorktreeAuthorizationProbe},
	{id: "nil_verified_claim", wantErr: ErrInvalidRepositoryWorktree, run: nilRepositoryWorktreeClaimProbe},
	{id: "claim_from_other_attempt", wantErr: ErrInvalidRepositoryWorktree, run: otherAttemptRepositoryWorktreeClaimProbe},

	{id: "request_cleanup", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("cleanup")},
	{id: "request_delete", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("delete")},
	{id: "request_prune", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("prune")},
	{id: "request_remove", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("remove")},
	{id: "request_ref_update", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("ref_update")},
	{id: "request_commit", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("commit")},
	{id: "request_push", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("push")},
	{id: "request_merge", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("merge")},
	{id: "request_launch", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("launch")},
	{id: "request_second_worktree", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("second_worktree")},
	{id: "request_second_effect", wantErr: ErrInvalidRepositoryWorktree, run: forbiddenRepositoryWorktreeActionProbe("second_effect")},
	{id: "observation_unknown_json_member", wantErr: ErrInvalidRepositoryWorktree, run: canonicalObservationDecodeMutationProbe(unknownRepositoryObservation)},
	{id: "observation_duplicate_json_member", wantErr: ErrInvalidRepositoryWorktree, run: canonicalObservationDecodeMutationProbe(duplicateRepositoryObservation)},
	{id: "observation_trailing_json", wantErr: ErrInvalidRepositoryWorktree, run: canonicalObservationDecodeMutationProbe(trailingRepositoryObservation)},
	{id: "observation_noncanonical_json", wantErr: ErrInvalidRepositoryWorktree, run: canonicalObservationDecodeMutationProbe(noncanonicalRepositoryObservation)},
	{id: "observation_self_hash_mismatch", wantErr: ErrInvalidRepositoryWorktree, run: canonicalObservationDecodeMutationProbe(repositoryObservationHashMismatch)},
	{id: "observation_map_order_determinism", run: repositoryWorktreeMapOrderProbe},

	{id: "forged_head_member_content", wantErr: ErrInvalidRepositoryWorktree, run: forgedMemberContentProbe(0)},
	{id: "forged_orig_head_member_content", wantErr: ErrInvalidRepositoryWorktree, run: forgedMemberContentProbe(1)},
	{id: "forged_commondir_member_content", wantErr: ErrInvalidRepositoryWorktree, run: forgedMemberContentProbe(2)},
	{id: "snapshot_wrong_verifier_authority", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.verifierAuthorityHash = testHash("wrong-verifier-authority")
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "snapshot_empty_verifier_authority", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.verifierAuthorityHash = ""
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})},
	{id: "stale_descriptor_seal", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeStaleSealMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.descriptorVerificationSeal = v.deltaVerificationSeal
	})},
	{id: "stale_delta_seal", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeStaleSealMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.deltaVerificationSeal = v.configVerificationSeal
	})},
	{id: "stale_config_seal", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeStaleSealMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.configVerificationSeal = v.attributesVerificationSeal
	})},
	{id: "stale_attributes_seal", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeStaleSealMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.attributesVerificationSeal = v.pathVerificationSeal
	})},
	{id: "stale_path_seal", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeStaleSealMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.pathVerificationSeal = v.uniquenessVerificationSeal
	})},
	{id: "stale_uniqueness_seal", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeStaleSealMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.uniquenessVerificationSeal = v.ambiguityVerificationSeal
	})},
	{id: "stale_ambiguity_seal", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeStaleSealMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		v.ambiguityVerificationSeal = v.descriptorVerificationSeal
	})},
	{id: "capability_integrity_mutation", run: repositoryWorktreeCapabilityMutationProbe(func(v *VerifiedRepositoryWorktree) {
		v.integrityHash = testHash("wrong-capability-integrity")
	})},
	{id: "capability_verifier_authority_mutation", run: repositoryWorktreeCapabilityMutationProbe(func(v *VerifiedRepositoryWorktree) {
		v.verifierAuthorityHash = testHash("wrong-capability-verifier-authority")
		v.integrityHash = verifiedRepositoryWorktreeIntegrityHash(v)
	})},
	{id: "capability_verification_seals_mutation", run: repositoryWorktreeCapabilityMutationProbe(func(v *VerifiedRepositoryWorktree) {
		v.verificationSealsHash = testHash("wrong-capability-verification-seals")
		v.integrityHash = verifiedRepositoryWorktreeIntegrityHash(v)
	})},
	{id: "verifier_authority_release_binding", run: repositoryWorktreeVerifierAuthorityReleaseBindingProbe},

	{id: "unknown_materializer_profile_id", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeInstallationVerifyProbe(func(v *RepositoryWorktreeInstallationAuthority) {
		v.MaterializerProfileID = "unknown_materializer_profile"
		v.MaterializerProfileHash = FrozenRepositoryWorktreeMaterializerProfile().MaterializerProfileHash
	})},
	{id: "unknown_materializer_profile_hash", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeInstallationVerifyProbe(func(v *RepositoryWorktreeInstallationAuthority) {
		v.MaterializerProfileHash = testHash("unknown-materializer-profile")
	})},
	{id: "nil_verified_installation", wantErr: ErrInvalidRepositoryWorktree, run: nilRepositoryWorktreeInstallationProbe},
	{id: "zero_verified_installation", wantErr: ErrInvalidRepositoryWorktree, run: zeroRepositoryWorktreeInstallationProbe},
	{id: "cross_authorization_installation", wantErr: ErrInvalidRepositoryWorktree, run: crossAuthorizationRepositoryWorktreeInstallationProbe},
	{id: "attempt_2_canonical_worktree_slot_accepted", run: repositoryWorktreeAttemptTwoSlotProbe},
	{id: "caller_invented_worktree_slot", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeInstallationVerifyProbe(func(v *RepositoryWorktreeInstallationAuthority) {
		v.WorktreeSlotID = "attempt_1_materialization_worktree_002"
		v.WorktreeSlotPathHash = deriveRepositoryWorktreeSlotPathHash(v.WorktreeSlotID)
	})},
	{id: "worktree_slot_reused_across_attempts", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeReusedSlotProbe},
	{id: "wrong_worktree_slot_path_hash", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeInstallationVerifyProbe(func(v *RepositoryWorktreeInstallationAuthority) {
		v.WorktreeSlotPathHash = testHash("wrong-worktree-slot-path")
	})},
	{id: "installed_root_aliases_source_root_identity", wantErr: ErrInvalidRepositoryWorktree, run: installedRootAliasProbe(0, false)},
	{id: "installed_root_aliases_common_git_root_identity", wantErr: ErrInvalidRepositoryWorktree, run: installedRootAliasProbe(1, false)},
	{id: "installed_root_aliases_candidate_root_identity", wantErr: ErrInvalidRepositoryWorktree, run: installedRootAliasProbe(2, false)},
	{id: "installed_root_aliases_candidate_gitfile_identity", wantErr: ErrInvalidRepositoryWorktree, run: installedRootAliasProbe(3, false)},
	{id: "installed_root_aliases_candidate_admin_identity", wantErr: ErrInvalidRepositoryWorktree, run: installedRootAliasProbe(4, false)},
	{id: "installed_root_aliases_descriptor_canonical_path", wantErr: ErrInvalidRepositoryWorktree, run: installedRootAliasProbe(0, true)},
	{id: "installation_integrity_mutation", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeInstallationIntegrityMutationProbe},
}, repositoryWorktreeRepairCVectorRegistry...)

func TestP6Slice4ExecutableVectorRegistry(t *testing.T) {
	if len(repositoryWorktreeVectorRegistry) != len(canonicalRepositoryWorktreeVectorIDs) {
		t.Fatalf("Slice-4 executable registry length=%d canonical inventory length=%d", len(repositoryWorktreeVectorRegistry), len(canonicalRepositoryWorktreeVectorIDs))
	}
	seen := make(map[string]struct{}, len(repositoryWorktreeVectorRegistry))
	executed := make([]string, 0, len(repositoryWorktreeVectorRegistry))
	for _, vector := range repositoryWorktreeVectorRegistry {
		vector := vector
		if vector.id == "" || vector.run == nil {
			t.Fatalf("unexecutable Slice-4 vector: %+v", vector)
		}
		if _, duplicate := seen[vector.id]; duplicate {
			t.Fatalf("duplicate Slice-4 vector ID %q", vector.id)
		}
		seen[vector.id] = struct{}{}
		t.Run(vector.id, func(t *testing.T) {
			executed = append(executed, vector.id)
			err := vector.run(t)
			if vector.wantErr == nil {
				if err != nil {
					t.Fatalf("accepted Slice-4 vector returned %v", err)
				}
				return
			}
			if !errors.Is(err, vector.wantErr) {
				t.Fatalf("rejected Slice-4 vector error=%v want=%v", err, vector.wantErr)
			}
		})
	}
	assertExecutedVectorOrder(t, executed, canonicalRepositoryWorktreeVectorIDs[:])
}

func canonicalRepositoryWorktreeRegistryProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeAdmitNew)
	if err != nil {
		return err
	}
	if capability == nil || assessment.EffectAllowed || assessment.Disposition != RepositoryWorktreeCapabilityReady {
		return errors.New("canonical worktree did not mint status-only next-phase capability")
	}
	return nil
}

func repositoryWorktreeDeepCopyProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	want := append([]byte(nil), fixture.snapshot.canonical...)
	fixture.canonical[0] ^= 1
	if !bytes.Equal(fixture.snapshot.canonical, want) {
		return errors.New("snapshot aliased caller bytes")
	}
	return nil
}

func repositoryWorktreeSnapshotMutationProbe(mutate func(*VerifiedRepositoryWorktreeSnapshot)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
		value := cloneRepositoryWorktreeSnapshotForTest(t, fixture.snapshot)
		mutate(value)
		_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0], value, RepositoryWorktreeAdmitNew, fixture.now)
		return err
	}
}

func nilRepositoryWorktreeSnapshotProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
	_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0], nil, RepositoryWorktreeAdmitNew, fixture.now)
	return err
}

func repositoryWorktreeSnapshotValueProbe(snapshot *VerifiedRepositoryWorktreeSnapshot) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
		_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0], snapshot, RepositoryWorktreeAdmitNew, fixture.now)
		return err
	}
}

func repositoryWorktreeUniquenessWaitingProbe(tupleUnique, slotUnique bool) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		fixture.snapshot.tupleUnique = tupleUnique
		fixture.snapshot.slotUnique = slotUnique
		markRepositoryWorktreeAmbiguousForTest(t, &fixture, RepositoryWorktreeConflictingSlot)
		assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeStatusOnly)
		if err != nil {
			return err
		}
		if capability != nil || assessment.EffectAllowed || assessment.Disposition != RepositoryWorktreeWaitingForHuman {
			return errors.New("uniqueness conflict did not retain for human")
		}
		return nil
	}
}

func exactRetainedRepositoryWorktreeProbe(t *testing.T) error {
	fixture := retainedRepositoryWorktreeFixtureForTest(t)
	assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeStatusOnly)
	if err != nil {
		return err
	}
	if capability != nil || assessment.EffectAllowed || assessment.Disposition != RepositoryWorktreeRetainedStatus {
		return errors.New("retained replay minted authority")
	}
	return nil
}

func repositoryWorktreeClaimBoundaryProbe(offset time.Duration, valid bool) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
		now := mustTime(t, fixture.authority.NotAfter).Add(offset)
		assessment, capability, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0], fixture.snapshot, RepositoryWorktreeAdmitNew, now)
		if valid {
			if err != nil {
				return err
			}
			if capability == nil || assessment.EffectAllowed {
				return errors.New("fresh claim boundary omitted capability")
			}
			return nil
		}
		return err
	}
}

func repositoryWorktreeIndependentBoundaryProbe(kind string, offset time.Duration) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture, boundary := repositoryWorktreeBoundaryFixtureForTest(t, kind)
		installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
		_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0], fixture.snapshot, RepositoryWorktreeAdmitNew, boundary.Add(offset))
		return err
	}
}

func repositoryWorktreeBoundaryFixtureForTest(t *testing.T, kind string) (canonicalRepositoryWorktreeFixture, time.Time) {
	t.Helper()
	var boundary time.Time
	switch kind {
	case "authorization", "dispatch":
		boundary = time.Date(2026, 7, 26, 12, 4, 0, 0, time.UTC)
	case "release":
		boundary = compiledRelease.leaf.NotAfter.UTC()
	default:
		t.Fatalf("unknown boundary %q", kind)
	}
	fixture := cloneFixture(t, CanonicalFixture())
	approvedAt := boundary.Add(-2 * time.Minute)
	authorizationNotAfter := boundary.Add(2 * time.Minute)
	dispatchCreatedAt := boundary.Add(-time.Minute)
	dispatchNotAfter := boundary.Add(time.Minute)
	if kind == "authorization" {
		authorizationNotAfter = boundary
		dispatchNotAfter = boundary
	}
	if kind == "dispatch" {
		dispatchNotAfter = boundary
	}
	fixture.Authorization.Approval.ApprovedAt = approvedAt.Format(time.RFC3339Nano)
	fixture.Authorization.Approval.NotAfter = authorizationNotAfter.Format(time.RFC3339Nano)
	fixture.Dispatch.CreatedAt = dispatchCreatedAt.Format(time.RFC3339Nano)
	fixture.Dispatch.DispatchNotAfter = dispatchNotAfter.Format(time.RFC3339Nano)
	rehashAuthorizationChain(t, &fixture)
	expectedAuthorization := authorityFromAuthorization(fixture.Authorization)
	verified, err := VerifyAuthorization(expectedAuthorization, fixture.Authorization, nil, dispatchCreatedAt, ValidationAdmission)
	if err != nil {
		t.Fatalf("verify boundary authorization: %v", err)
	}
	claimCreatedAt := boundary.Add(-30 * time.Second)
	claimNotAfter := boundary
	return repositoryWorktreeFixtureWithWindowForTest(t, fixture, verified, claimCreatedAt, claimNotAfter), boundary
}

func repositoryWorktreeFixtureWithWindowForTest(t *testing.T, contract ContractFixture, verified *VerifiedAuthorization, createdAt, notAfter time.Time) canonicalRepositoryWorktreeFixture {
	t.Helper()
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	authority := fixture.authority
	authority.AcceptedDispatch = contract.Dispatch
	authority.Repository = contract.Authorization.Scope.Repository
	authority.AttemptHash = supervisorAttemptIdentityHash(contract.Authorization, contract.Dispatch)
	authority.AttemptNumber = contract.Authorization.Scope.Attempt.AttemptNumber
	authority.AttemptCap = contract.Authorization.Scope.Attempt.AttemptCap
	authority.CreatedAt = createdAt.Format(time.RFC3339Nano)
	authority.NotAfter = notAfter.Format(time.RFC3339Nano)
	claim := supervisorClaimFromTestAuthority(contract, authority, "")
	claim.ClaimHash = mustRecordHash(t, claim, "claim_hash")
	raw := canonicalTestArtifact(t, claim)
	slot := mintSupervisorClaimSlotCommitForTest(t, authority, claim, raw, true, true)
	assessment, committed, err := EvaluateSupervisorIntentClaim(authority, verified, slot, nil, nil, raw, createdAt)
	if err != nil || committed == nil || assessment.Disposition != SupervisorClaimExactReplay {
		t.Fatalf("mint boundary claim: assessment=%+v claim=%v err=%v", assessment, committed, err)
	}
	var attempt canonicalSupervisorAttempt
	attempt.contract = contract
	attempt.authorization = verified
	attempt.authorities[0] = authority
	attempt.claims[0] = claim
	attempt.canonical[0] = raw
	attempt.slotCommits[0] = slot
	attempt.committedClaim[0] = committed
	fixture.attempt = attempt
	fixture.authority = authority
	fixture.now = createdAt
	fixture.observation.AuthorizationHash = contract.Authorization.AuthorizationHash
	fixture.observation.ApprovalHash = contract.Authorization.ApprovalHash
	fixture.observation.RequestHash = contract.Dispatch.Request.RequestHash
	fixture.observation.DispatchHash = contract.Dispatch.DispatchHash
	fixture.observation.AttemptHash = authority.AttemptHash
	fixture.observation.AttemptNumber = authority.AttemptNumber
	fixture.observation.AttemptCap = authority.AttemptCap
	fixture.observation.ClaimHash = claim.ClaimHash
	fixture.observation.Repository = authority.Repository
	fixture.observation.Candidate.HEADCommit = authority.Repository.BaseCommit
	fixture.observation.Candidate.ORIGHEADCommit = authority.Repository.BaseCommit
	fixture.observation.Candidate.IndexTree = authority.Repository.BaseTree
	fixture.observation.CommonGitDelta.Members[0].SemanticTargetHash = sha256Digest([]byte(authority.Repository.BaseCommit))
	fixture.observation.CommonGitDelta.Members[1].SemanticTargetHash = sha256Digest([]byte(authority.Repository.BaseCommit))
	fixture.observation.CommonGitDelta.Members[4].SemanticTargetHash = sha256Digest([]byte(authority.Repository.BaseTree))
	fixture.observation.CommonGitDelta.Members[5].SemanticTargetHash = sha256Digest([]byte(authority.Repository.BaseCommit))
	sealRepositoryWorktreeObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	fixture.snapshot = mintRepositoryWorktreeSnapshotForTest(t, fixture.observation, true, true, true, true)
	return fixture
}

func wrongRepositoryWorktreePhaseProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	fixture.authority = fixture.attempt.authorities[1]
	_, _, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeAdmitNew)
	return err
}

func nilRepositoryWorktreeAuthorizationProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
	_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, nil, fixture.attempt.committedClaim[0], fixture.snapshot, RepositoryWorktreeAdmitNew, fixture.now)
	return err
}

func nilRepositoryWorktreeClaimProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
	_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, nil, fixture.snapshot, RepositoryWorktreeAdmitNew, fixture.now)
	return err
}

func otherAttemptRepositoryWorktreeClaimProbe(t *testing.T) error {
	first, second := canonicalSupervisorAttempts(t)
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	fixture.attempt = first
	installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
	_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, second.committedClaim[0], fixture.snapshot, RepositoryWorktreeAdmitNew, fixture.now)
	return err
}

func forbiddenRepositoryWorktreeActionProbe(action RepositoryWorktreeAction) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
		_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0], fixture.snapshot, action, fixture.now)
		return err
	}
}

func forgedMemberContentProbe(index int) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		// A fully self-consistent same-package forgery (seals and integrity
		// recomputed) of an authorization-derivable member content hash.
		forged := fixture.snapshot.observation
		forged.CommonGitDelta.Members[index].ContentHash = testHash("forged-member-content")
		sealRepositoryWorktreeObservationForTest(t, &forged)
		fixture.snapshot = mintRepositoryWorktreeSnapshotForTest(t, forged, true, true, true, true)
		_, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeAdmitNew)
		if capability != nil {
			return errors.New("forged member content minted capability")
		}
		return err
	}
}

func repositoryWorktreeStaleSealMutationProbe(mutate func(*VerifiedRepositoryWorktreeSnapshot)) func(*testing.T) error {
	return repositoryWorktreeSnapshotMutationProbe(func(v *VerifiedRepositoryWorktreeSnapshot) {
		mutate(v)
		v.integrityHash = repositoryWorktreeSnapshotIntegrityHash(v)
	})
}

func repositoryWorktreeCapabilityMutationProbe(mutate func(*VerifiedRepositoryWorktree)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		_, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeAdmitNew)
		if err != nil {
			return err
		}
		if capability == nil {
			return errors.New("canonical worktree omitted capability")
		}
		mutate(capability)
		if verifiedRepositoryWorktreeIntact(capability) {
			return errors.New("mutated capability remained intact")
		}
		return nil
	}
}

func repositoryWorktreeVerifierAuthorityReleaseBindingProbe(t *testing.T) error {
	derived, err := deriveFrozenRepositoryWorktreeVerifierAuthority()
	if err != nil {
		return err
	}
	frozen := FrozenRepositoryWorktreeVerifierAuthority()
	if !reflect.DeepEqual(derived, frozen) {
		return errors.New("derived verifier authority drifted from the frozen value")
	}
	if !recordHashMatches(frozen, "verifier_authority_hash", frozen.VerifierAuthorityHash) {
		return errors.New("frozen verifier authority self-hash mismatch")
	}
	if frozen.VerifierID != "controlled_repair_repository_worktree_verifier_v1" ||
		frozen.ReleasePinsHash != FrozenReleasePins().ReleasePinsHash {
		return errors.New("verifier authority is not bound to the frozen verifier identity and release pins")
	}
	profile := FrozenRepositoryWorktreeMaterializerProfile()
	if !recordHashMatches(profile, "materializer_profile_hash", profile.MaterializerProfileHash) ||
		profile.GitVersion != "2.54" || profile.MaterializerProfileID != "installed_git_2_54_detached_worktree_materializer_v1" ||
		frozen.MaterializerProfileID != profile.MaterializerProfileID || frozen.MaterializerProfileHash != profile.MaterializerProfileHash {
		return errors.New("verifier authority is not bound to the frozen Git 2.54 materializer profile")
	}
	kinds := repositoryWorktreeVerificationKinds()
	if len(frozen.VerificationKinds) != len(kinds) {
		return errors.New("frozen verifier authority kind inventory drifted")
	}
	for index, kind := range kinds {
		if frozen.VerificationKinds[index] != kind || !validRepositoryWorktreeVerificationKind(kind) {
			return errors.New("frozen verifier authority enumerates a non-closed verification kind")
		}
	}
	return nil
}

func repositoryWorktreeInstallationVerifyProbe(mutate func(*RepositoryWorktreeInstallationAuthority)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		mutate(&fixture.installation)
		_, err := VerifyRepositoryWorktreeInstallation(fixture.installation, fixture.authority, fixture.attempt.authorization, fixture.now)
		return err
	}
}

func nilRepositoryWorktreeInstallationProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	_, _, err := EvaluateRepositoryWorktree(fixture.authority, nil, fixture.attempt.authorization, fixture.attempt.committedClaim[0], fixture.snapshot, RepositoryWorktreeAdmitNew, fixture.now)
	return err
}

func zeroRepositoryWorktreeInstallationProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	_, _, err := EvaluateRepositoryWorktree(fixture.authority, &VerifiedRepositoryWorktreeInstallation{}, fixture.attempt.authorization, fixture.attempt.committedClaim[0], fixture.snapshot, RepositoryWorktreeAdmitNew, fixture.now)
	return err
}

func crossAuthorizationRepositoryWorktreeInstallationProbe(t *testing.T) error {
	_, second := canonicalSupervisorAttempts(t)
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	secondAuthority := second.authorities[0]
	secondSlot := deriveRepositoryWorktreeSlotID(secondAuthority.AttemptNumber)
	profile := FrozenRepositoryWorktreeMaterializerProfile()
	verified, err := VerifyRepositoryWorktreeInstallation(RepositoryWorktreeInstallationAuthority{
		WorktreeSlotID:                    secondSlot,
		WorktreeSlotPathHash:              deriveRepositoryWorktreeSlotPathHash(secondSlot),
		InstalledWorktreeRootIdentityHash: fixture.installation.InstalledWorktreeRootIdentityHash,
		MaterializerProfileID:             profile.MaterializerProfileID,
		MaterializerProfileHash:           profile.MaterializerProfileHash,
	}, secondAuthority, second.authorization, mustTime(t, secondAuthority.CreatedAt))
	if err != nil {
		return err
	}
	_, _, err = EvaluateRepositoryWorktree(fixture.authority, verified, fixture.attempt.authorization, fixture.attempt.committedClaim[0], fixture.snapshot, RepositoryWorktreeAdmitNew, fixture.now)
	return err
}

func repositoryWorktreeAttemptTwoSlotProbe(t *testing.T) error {
	_, second := canonicalSupervisorAttempts(t)
	authority := second.authorities[0]
	slot := deriveRepositoryWorktreeSlotID(authority.AttemptNumber)
	if slot != "attempt_2_materialization_worktree_001" {
		return errors.New("frozen slot grammar drifted for attempt 2")
	}
	profile := FrozenRepositoryWorktreeMaterializerProfile()
	verified, err := VerifyRepositoryWorktreeInstallation(RepositoryWorktreeInstallationAuthority{
		WorktreeSlotID:                    slot,
		WorktreeSlotPathHash:              deriveRepositoryWorktreeSlotPathHash(slot),
		InstalledWorktreeRootIdentityHash: testHash("installed-worktree-root-identity-v1"),
		MaterializerProfileID:             profile.MaterializerProfileID,
		MaterializerProfileHash:           profile.MaterializerProfileHash,
	}, authority, second.authorization, mustTime(t, authority.CreatedAt))
	if err != nil {
		return err
	}
	if !verifiedRepositoryWorktreeInstallationIntact(verified, authority, second.authorization) {
		return errors.New("attempt-2 verified installation is not intact")
	}
	return nil
}

func repositoryWorktreeReusedSlotProbe(t *testing.T) error {
	_, second := canonicalSupervisorAttempts(t)
	authority := second.authorities[0]
	reusedSlot := deriveRepositoryWorktreeSlotID(1)
	profile := FrozenRepositoryWorktreeMaterializerProfile()
	_, err := VerifyRepositoryWorktreeInstallation(RepositoryWorktreeInstallationAuthority{
		WorktreeSlotID:                    reusedSlot,
		WorktreeSlotPathHash:              deriveRepositoryWorktreeSlotPathHash(reusedSlot),
		InstalledWorktreeRootIdentityHash: testHash("installed-worktree-root-identity-v1"),
		MaterializerProfileID:             profile.MaterializerProfileID,
		MaterializerProfileHash:           profile.MaterializerProfileHash,
	}, authority, second.authorization, mustTime(t, authority.CreatedAt))
	return err
}

func installedRootAliasProbe(index int, canonicalPath bool) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		descriptors := []RepositoryDescriptorIdentity{
			fixture.observation.SourceRootDescriptor, fixture.observation.CommonGitRootDescriptor,
			fixture.observation.CandidateRootDescriptor, fixture.observation.CandidateGitfileDescriptor,
			fixture.observation.CandidateAdminDescriptor,
		}
		alias := descriptors[index].NoFollowIdentityHash
		if canonicalPath {
			alias = descriptors[index].CanonicalPathHash
		}
		fixture.installation.InstalledWorktreeRootIdentityHash = alias
		fixture.observation.InstalledWorktreeRootIdentityHash = alias
		fixture.observation.Source.InstalledWorktreeRootIdentityHash = alias
		sealRepositoryWorktreeObservationForTest(t, &fixture.observation)
		fixture.canonical = canonicalTestArtifact(t, fixture.observation)
		fixture.snapshot = mintRepositoryWorktreeSnapshotForTest(t, fixture.observation, true, true, true, true)
		_, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeAdmitNew)
		if capability != nil {
			return errors.New("installed root alias minted capability")
		}
		return err
	}
}

func repositoryWorktreeInstallationIntegrityMutationProbe(t *testing.T) error {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
	installation.installation.WorktreeSlotID = "attempt_1_materialization_worktree_002"
	_, _, err := EvaluateRepositoryWorktree(fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0], fixture.snapshot, RepositoryWorktreeAdmitNew, fixture.now)
	return err
}

func repositoryWorktreeMapOrderProbe(t *testing.T) error {
	left := map[string]any{"z": 1, "a": map[string]any{"y": 2, "b": 3}}
	right := map[string]any{"a": map[string]any{"b": 3, "y": 2}, "z": 1}
	leftRaw, leftErr := canonicalBytes(left)
	rightRaw, rightErr := canonicalBytes(right)
	if leftErr != nil {
		return leftErr
	}
	if rightErr != nil {
		return rightErr
	}
	if !bytes.Equal(leftRaw, rightRaw) {
		return errors.New("map order changed canonical bytes")
	}
	return nil
}

func stringsRepeatOID(character string) string {
	return character + character + character + character + character + character + character + character + character + character +
		character + character + character + character + character + character + character + character + character + character +
		character + character + character + character + character + character + character + character + character + character +
		character + character + character + character + character + character + character + character + character + character
}
