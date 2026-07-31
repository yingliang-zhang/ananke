package repaircontract

import (
	"errors"
	"reflect"
	"strconv"
	"time"
)

const (
	RepositoryWorktreeObservationSchemaVersion         = "ananke.controlled-repair-repository-worktree-observation.v1"
	RepositoryDescriptorIdentitySchemaVersion          = "ananke.controlled-repair-repository-descriptor-identity.v1"
	CommonGitMemberObservationSchemaVersion            = "ananke.controlled-repair-common-git-member-observation.v1"
	CommonGitProtectedDomainObservationSchemaVersion   = "ananke.controlled-repair-common-git-protected-domain-observation.v1"
	CommonGitDeltaObservationSchemaVersion             = "ananke.controlled-repair-common-git-delta-observation.v1"
	RepositoryCandidateObservationSchemaVersion        = "ananke.controlled-repair-repository-candidate-observation.v1"
	RepositorySourceClosureSchemaVersion               = "ananke.controlled-repair-repository-source-closure.v1"
	RepositoryWritablePathObservationSchemaVersion     = "ananke.controlled-repair-repository-writable-path-observation.v1"
	RepositoryWritablePathClosureSchemaVersion         = "ananke.controlled-repair-repository-writable-path-closure.v1"
	RepositoryGitConfigClosureSchemaVersion            = "ananke.controlled-repair-repository-git-config-closure.v1"
	RepositoryGitAttributesClosureSchemaVersion        = "ananke.controlled-repair-repository-git-attributes-closure.v1"
	RepositoryWorktreeMaterializerProfileSchemaVersion = "ananke.controlled-repair-repository-git-254-materializer-profile.v1"
	RepositoryWorktreeVerifierAuthoritySchemaVersion   = "ananke.controlled-repair-repository-worktree-verifier-authority.v1"
	RepositoryWorktreeVerificationSealSchemaVersion    = "ananke.controlled-repair-repository-worktree-verification-seal.v1"

	RepositoryWorktreeDescriptorVerification RepositoryWorktreeVerificationKind = "descriptor_verification"
	RepositoryWorktreeDeltaVerification      RepositoryWorktreeVerificationKind = "delta_verification"
	RepositoryWorktreeConfigVerification     RepositoryWorktreeVerificationKind = "config_verification"
	RepositoryWorktreeAttributesVerification RepositoryWorktreeVerificationKind = "attributes_verification"
	RepositoryWorktreePathVerification       RepositoryWorktreeVerificationKind = "path_verification"
	RepositoryWorktreeUniquenessVerification RepositoryWorktreeVerificationKind = "uniqueness_verification"
	RepositoryWorktreeAmbiguityVerification  RepositoryWorktreeVerificationKind = "ambiguity_verification"

	repositoryWorktreeVerifierID             = "controlled_repair_repository_worktree_verifier_v1"
	repositoryWorktreeMaterializerProfileID  = "installed_git_2_54_detached_worktree_materializer_v1"
	repositoryWorktreeMaterializerGitVersion = "2.54"
	repositoryWorktreeSlotPrefix             = "attempt_"
	repositoryWorktreeSlotSuffix             = "_materialization_worktree_001"
	repositoryWorktreeSlotPathPrefix         = "worktrees/"

	RepositoryWorktreeAdmitNew   RepositoryWorktreeAction = "admit_new_materialization"
	RepositoryWorktreeStatusOnly RepositoryWorktreeAction = "status_only"

	RepositoryWorktreeCapabilityReady RepositoryWorktreeDisposition = "capability_ready"
	RepositoryWorktreeRetainedStatus  RepositoryWorktreeDisposition = "retained_status"
	RepositoryWorktreeWaitingForHuman RepositoryWorktreeDisposition = "waiting_for_human"

	RepositoryWorktreeNextNormalPhase RepositoryWorktreeRequirement = "next_normal_phase"
	RepositoryWorktreeNoFurtherEffect RepositoryWorktreeRequirement = "no_further_effect_permitted"
	RepositoryWorktreeHumanReview     RepositoryWorktreeRequirement = "human_review_required"

	RepositoryWorktreeNewExact           RepositoryWorktreeState = "new_exact"
	RepositoryWorktreeRetainedExact      RepositoryWorktreeState = "retained_exact_replay"
	RepositoryWorktreeRetainForHuman     RepositoryWorktreeState = "retain_for_human"
	RepositoryWorktreeOrphanedStatusOnly RepositoryWorktreeState = "orphaned_status_only"

	RepositoryWorktreeConflictingRetained     RepositoryWorktreeAmbiguityReason = "conflicting_retained_state"
	RepositoryWorktreePartialDelta            RepositoryWorktreeAmbiguityReason = "partial_common_git_delta"
	RepositoryWorktreeUncertainOwnership      RepositoryWorktreeAmbiguityReason = "uncertain_ownership"
	RepositoryWorktreeConflictingSlot         RepositoryWorktreeAmbiguityReason = "conflicting_worktree_slot"
	RepositoryWorktreeConfigDrift             RepositoryWorktreeAmbiguityReason = "git_config_drift"
	RepositoryWorktreeAttributesDrift         RepositoryWorktreeAmbiguityReason = "git_attributes_drift"
	RepositoryWorktreePathAmbiguity           RepositoryWorktreeAmbiguityReason = "authorized_path_ambiguity"
	RepositoryWorktreeProtectedCommonGitDrift RepositoryWorktreeAmbiguityReason = "protected_common_git_drift"
	RepositoryWorktreeCandidateDrift          RepositoryWorktreeAmbiguityReason = "candidate_state_drift"
	RepositoryWorktreeDescriptorDrift         RepositoryWorktreeAmbiguityReason = "descriptor_identity_drift"
	RepositoryWorktreeOrphanedMaterialization RepositoryWorktreeAmbiguityReason = "orphaned_materialization"

	RepositorySourceRootDescriptor       RepositoryDescriptorID = "source_root"
	RepositoryCommonGitRootDescriptor    RepositoryDescriptorID = "common_git_root"
	RepositoryCandidateRootDescriptor    RepositoryDescriptorID = "candidate_root"
	RepositoryCandidateGitfileDescriptor RepositoryDescriptorID = "candidate_gitfile"
	RepositoryCandidateAdminDescriptor   RepositoryDescriptorID = "candidate_admin_subtree"

	RepositoryDescriptorDirectory   RepositoryDescriptorObjectKind = "directory"
	RepositoryDescriptorRegularFile RepositoryDescriptorObjectKind = "regular_file"

	CommonGitMemberHEAD      CommonGitMemberID = "head"
	CommonGitMemberORIGHEAD  CommonGitMemberID = "orig_head"
	CommonGitMemberCommonDir CommonGitMemberID = "commondir"
	CommonGitMemberGitDir    CommonGitMemberID = "gitdir"
	CommonGitMemberIndex     CommonGitMemberID = "index"
	CommonGitMemberLogsHEAD  CommonGitMemberID = "logs_head"

	CommonGitDetachedHEADAtBase    CommonGitMemberSemantic = "detached_head_at_base_commit"
	CommonGitORIGHEADAtBase        CommonGitMemberSemantic = "orig_head_at_base_commit"
	CommonGitCommonDirParentParent CommonGitMemberSemantic = "commondir_parent_parent"
	CommonGitAdminGitDirBacklink   CommonGitMemberSemantic = "admin_gitdir_candidate_backlink"
	CommonGitIndexAtBaseTree       CommonGitMemberSemantic = "index_at_base_tree"
	CommonGitDetachedCheckoutLog   CommonGitMemberSemantic = "detached_checkout_head_log"

	CommonGitDeltaExactNew      CommonGitDeltaState = "exact_new_subtree"
	CommonGitDeltaRetainedExact CommonGitDeltaState = "exact_retained_subtree"
	CommonGitDeltaAmbiguous     CommonGitDeltaState = "ambiguous_or_partial"

	CommonGitProtectedRefs                 CommonGitProtectedDomainID = "refs"
	CommonGitProtectedLogsOutsideCandidate CommonGitProtectedDomainID = "logs_outside_candidate_admin"
	CommonGitProtectedConfig               CommonGitProtectedDomainID = "config"
	CommonGitProtectedObjects              CommonGitProtectedDomainID = "objects"
	CommonGitProtectedHooks                CommonGitProtectedDomainID = "hooks"
	CommonGitProtectedInfoExclude          CommonGitProtectedDomainID = "info_exclude"
	CommonGitProtectedInfoAttributes       CommonGitProtectedDomainID = "info_attributes"
	CommonGitProtectedAlternates           CommonGitProtectedDomainID = "alternates"
	CommonGitProtectedShallow              CommonGitProtectedDomainID = "shallow"
	CommonGitProtectedGrafts               CommonGitProtectedDomainID = "grafts"
	CommonGitProtectedReplace              CommonGitProtectedDomainID = "replace"
	CommonGitProtectedPackedRefs           CommonGitProtectedDomainID = "packed_refs"
	CommonGitProtectedCommonIndex          CommonGitProtectedDomainID = "common_index"
	CommonGitProtectedWorktreeSiblings     CommonGitProtectedDomainID = "worktree_admin_siblings"

	RepositoryCandidateDetached RepositoryCandidateHeadMode = "detached"
	RepositoryCandidateBranch   RepositoryCandidateHeadMode = "branch"
	RepositoryCandidateClean    RepositoryCandidateStatus   = "clean"
	RepositoryCandidateDirty    RepositoryCandidateStatus   = "dirty"

	maxRepositoryWorktreeIDBytes = 192
	maxRepositoryWorktreeMembers = 64
	maxRepositoryWritablePaths   = 256
)

var ErrInvalidRepositoryWorktree = errors.New("controlled repair repository worktree observation is invalid")

type RepositoryWorktreeAction string
type RepositoryWorktreeDisposition string
type RepositoryWorktreeRequirement string
type RepositoryWorktreeState string
type RepositoryWorktreeAmbiguityReason string
type RepositoryDescriptorID string
type RepositoryDescriptorObjectKind string
type CommonGitMemberID string
type CommonGitMemberSemantic string
type CommonGitDeltaState string
type CommonGitProtectedDomainID string
type RepositoryCandidateHeadMode string
type RepositoryCandidateStatus string
type RepositoryWorktreeVerificationKind string

// RepositoryWorktreeInstallationAuthority is the plain installed record. It
// becomes authority only through VerifyRepositoryWorktreeInstallation, which
// binds it to fresh release trust, the verified authorization, the frozen
// authorization-derived slot grammar, and the release-installed Git 2.54
// profile. It names no raw filesystem path.
type RepositoryWorktreeInstallationAuthority struct {
	WorktreeSlotID                    string
	WorktreeSlotPathHash              string
	InstalledWorktreeRootIdentityHash string
	MaterializerProfileID             string
	MaterializerProfileHash           string
}

// RepositoryWorktreeMaterializerProfile is the compiled Git 2.54 detached
// worktree materializer policy. Its closed boolean semantics freeze the only
// release-installed profile this contract accepts.
type RepositoryWorktreeMaterializerProfile struct {
	SchemaVersion               string `json:"schema_version"`
	MaterializerProfileHash     string `json:"materializer_profile_hash"`
	MaterializerProfileID       string `json:"materializer_profile_id"`
	GitVersion                  string `json:"git_version"`
	DetachedHeadOnly            bool   `json:"detached_head_only"`
	ExactSixMemberDelta         bool   `json:"exact_six_member_delta"`
	CommonDirParentParentOnly   bool   `json:"commondir_parent_parent_only"`
	SystemConfigDisabled        bool   `json:"system_config_disabled"`
	GlobalConfigDisabled        bool   `json:"global_config_disabled"`
	NoIncludes                  bool   `json:"no_includes"`
	NoIncludeIf                 bool   `json:"no_include_if"`
	NoHooksPath                 bool   `json:"no_hooks_path"`
	NoAttributesFile            bool   `json:"no_attributes_file"`
	NoFilters                   bool   `json:"no_filters"`
	NoFSMonitor                 bool   `json:"no_fsmonitor"`
	NoExternalCommands          bool   `json:"no_external_commands"`
	SystemAttributesDisabled    bool   `json:"system_attributes_disabled"`
	NoExternalFilterAttributes  bool   `json:"no_external_filter_attributes"`
	NoProcessFilterAttributes   bool   `json:"no_process_filter_attributes"`
	NoExternalCommandAttributes bool   `json:"no_external_command_attributes"`
}

// RepositoryWorktreeVerifierAuthority is the release-pinned verifier identity.
// It binds the verifier to the compiled Git 2.54 materializer profile, the
// frozen release pins, and the ordered closed set of verification kinds.
type RepositoryWorktreeVerifierAuthority struct {
	SchemaVersion           string                               `json:"schema_version"`
	VerifierAuthorityHash   string                               `json:"verifier_authority_hash"`
	VerifierID              string                               `json:"verifier_id"`
	MaterializerProfileID   string                               `json:"materializer_profile_id"`
	MaterializerProfileHash string                               `json:"materializer_profile_hash"`
	ReleasePinsHash         string                               `json:"release_pins_hash"`
	VerificationKinds       []RepositoryWorktreeVerificationKind `json:"verification_kinds"`
}

// RepositoryWorktreeVerificationSeal is a self-hashed provenance record for
// one verification kind. It binds the frozen verifier authority, the exact
// observation and canonical hashes, and kind-specific evidence. It names
// provenance only: it does not claim that a trusted verifier physically
// executed, and production snapshot minting remains future trusted runtime
// work with no production minter in this slice.
type RepositoryWorktreeVerificationSeal struct {
	SchemaVersion         string                             `json:"schema_version"`
	SealHash              string                             `json:"seal_hash"`
	SealKind              RepositoryWorktreeVerificationKind `json:"seal_kind"`
	VerifierAuthorityHash string                             `json:"verifier_authority_hash"`
	ObservationHash       string                             `json:"observation_hash"`
	CanonicalHash         string                             `json:"canonical_hash"`
	EvidenceHash          string                             `json:"evidence_hash"`
}

// RepositoryDescriptorIdentity is canonical no-follow descriptor data. It
// deliberately carries hashes rather than portable or machine-local paths.
type RepositoryDescriptorIdentity struct {
	SchemaVersion        string                         `json:"schema_version"`
	DescriptorHash       string                         `json:"descriptor_hash"`
	DescriptorID         RepositoryDescriptorID         `json:"descriptor_id"`
	CanonicalPathHash    string                         `json:"canonical_path_hash"`
	NoFollowIdentityHash string                         `json:"no_follow_identity_hash"`
	ObjectKind           RepositoryDescriptorObjectKind `json:"object_kind"`
}

type CommonGitMemberObservation struct {
	SchemaVersion              string                  `json:"schema_version"`
	MemberHash                 string                  `json:"member_hash"`
	Sequence                   int                     `json:"sequence"`
	MemberID                   CommonGitMemberID       `json:"member_id"`
	RepositoryRelativePathHash string                  `json:"repository_relative_path_hash"`
	DescriptorIdentityHash     string                  `json:"descriptor_identity_hash"`
	ContentHash                string                  `json:"content_hash"`
	Semantic                   CommonGitMemberSemantic `json:"semantic"`
	SemanticTargetHash         string                  `json:"semantic_target_hash"`
}

type CommonGitProtectedDomainObservation struct {
	SchemaVersion string                     `json:"schema_version"`
	DomainHash    string                     `json:"domain_hash"`
	Sequence      int                        `json:"sequence"`
	DomainID      CommonGitProtectedDomainID `json:"domain_id"`
	BeforeHash    string                     `json:"before_hash"`
	AfterHash     string                     `json:"after_hash"`
	Unchanged     bool                       `json:"unchanged"`
}

type CommonGitDeltaObservation struct {
	SchemaVersion                  string                                `json:"schema_version"`
	DeltaHash                      string                                `json:"delta_hash"`
	State                          CommonGitDeltaState                   `json:"state"`
	WorktreeSlotID                 string                                `json:"worktree_slot_id"`
	CandidateAdminSubtreePathHash  string                                `json:"candidate_admin_subtree_path_hash"`
	BeforeInventoryHash            string                                `json:"before_inventory_hash"`
	AfterInventoryHash             string                                `json:"after_inventory_hash"`
	Members                        []CommonGitMemberObservation          `json:"members"`
	AddedMemberIDs                 []CommonGitMemberID                   `json:"added_member_ids"`
	ChangedPreexistingMemberHashes []string                              `json:"changed_preexisting_member_hashes"`
	RemovedPreexistingMemberHashes []string                              `json:"removed_preexisting_member_hashes"`
	ExtraAddedMemberHashes         []string                              `json:"extra_added_member_hashes"`
	ProtectedDomains               []CommonGitProtectedDomainObservation `json:"protected_domains"`
}

type RepositoryCandidateObservation struct {
	SchemaVersion                  string                      `json:"schema_version"`
	CandidateHash                  string                      `json:"candidate_hash"`
	HEADCommit                     string                      `json:"head_commit"`
	ORIGHEADCommit                 string                      `json:"orig_head_commit"`
	HeadMode                       RepositoryCandidateHeadMode `json:"head_mode"`
	IndexTree                      string                      `json:"index_tree"`
	InitialStatus                  RepositoryCandidateStatus   `json:"initial_status"`
	BranchRefCreated               bool                        `json:"branch_ref_created"`
	BranchRefUpdated               bool                        `json:"branch_ref_updated"`
	OtherRefUpdated                bool                        `json:"other_ref_updated"`
	CandidateGitfileTargetPathHash string                      `json:"candidate_gitfile_target_path_hash"`
	AdminGitdirBacklinkPathHash    string                      `json:"admin_gitdir_backlink_path_hash"`
}

type RepositorySourceClosure struct {
	SchemaVersion                     string `json:"schema_version"`
	SourceClosureHash                 string `json:"source_closure_hash"`
	InstalledWorktreeRootIdentityHash string `json:"installed_worktree_root_identity_hash"`
	SourceRootIdentityHashBefore      string `json:"source_root_identity_hash_before"`
	SourceRootIdentityHashAfter       string `json:"source_root_identity_hash_after"`
	ProtectedContentsHashBefore       string `json:"protected_contents_hash_before"`
	ProtectedContentsHashAfter        string `json:"protected_contents_hash_after"`
	SourceUnchanged                   bool   `json:"source_unchanged"`
	CandidateExactChild               bool   `json:"candidate_exact_child"`
	CandidateNew                      bool   `json:"candidate_new"`
	SourceCandidateAlias              bool   `json:"source_candidate_alias"`
	SourceCommonGitAlias              bool   `json:"source_common_git_alias"`
	CandidateCommonGitAlias           bool   `json:"candidate_common_git_alias"`
	CommonGitWritableByAdapter        bool   `json:"common_git_writable_by_adapter"`
}

type RepositoryWritablePathObservation struct {
	SchemaVersion               string `json:"schema_version"`
	PathObservationHash         string `json:"path_observation_hash"`
	Sequence                    int    `json:"sequence"`
	PathID                      string `json:"path_id"`
	RepositoryRelativePathHash  string `json:"repository_relative_path_hash"`
	ResolvedCanonicalPathHash   string `json:"resolved_canonical_path_hash"`
	AncestorDescriptorChainHash string `json:"ancestor_descriptor_chain_hash"`
	LeafIdentityHash            string `json:"leaf_identity_hash"`
}

type RepositoryWritablePathClosure struct {
	SchemaVersion                    string                              `json:"schema_version"`
	PathClosureHash                  string                              `json:"path_closure_hash"`
	AuthorizedPathSetHash            string                              `json:"authorized_path_set_hash"`
	CandidateRootIdentityHash        string                              `json:"candidate_root_identity_hash"`
	Paths                            []RepositoryWritablePathObservation `json:"paths"`
	AllPathsUnderCandidate           bool                                `json:"all_paths_under_candidate"`
	AncestorsNoFollowVerified        bool                                `json:"ancestors_no_follow_verified"`
	NoSymlinks                       bool                                `json:"no_symlinks"`
	NoHardlinks                      bool                                `json:"no_hardlinks"`
	NoPrefixEscapes                  bool                                `json:"no_prefix_escapes"`
	NoDuplicates                     bool                                `json:"no_duplicates"`
	NoCaseFoldCollisions             bool                                `json:"no_case_fold_collisions"`
	NoUnicodeNormalizationCollisions bool                                `json:"no_unicode_normalization_collisions"`
}

type RepositoryGitConfigClosure struct {
	SchemaVersion                    string `json:"schema_version"`
	ConfigClosureHash                string `json:"config_closure_hash"`
	MaterializerProfileID            string `json:"materializer_profile_id"`
	MaterializerProfileHash          string `json:"materializer_profile_hash"`
	CommonConfigDescriptorHashBefore string `json:"common_config_descriptor_hash_before"`
	CommonConfigDescriptorHashAfter  string `json:"common_config_descriptor_hash_after"`
	CommonConfigBytesHashBefore      string `json:"common_config_bytes_hash_before"`
	CommonConfigBytesHashAfter       string `json:"common_config_bytes_hash_after"`
	CommonConfigUnchanged            bool   `json:"common_config_unchanged"`
	SystemConfigDisabled             bool   `json:"system_config_disabled"`
	GlobalConfigDisabled             bool   `json:"global_config_disabled"`
	NoIncludes                       bool   `json:"no_includes"`
	NoIncludeIf                      bool   `json:"no_include_if"`
	NoHooksPath                      bool   `json:"no_hooks_path"`
	NoAttributesFile                 bool   `json:"no_attributes_file"`
	NoFilters                        bool   `json:"no_filters"`
	NoFSMonitor                      bool   `json:"no_fsmonitor"`
	NoExternalCommands               bool   `json:"no_external_commands"`
}

type RepositoryGitAttributesClosure struct {
	SchemaVersion                      string `json:"schema_version"`
	AttributesClosureHash              string `json:"attributes_closure_hash"`
	SystemAttributesDisabled           bool   `json:"system_attributes_disabled"`
	InfoAttributesExists               bool   `json:"info_attributes_exists"`
	InfoAttributesDescriptorHashBefore string `json:"info_attributes_descriptor_hash_before"`
	InfoAttributesDescriptorHashAfter  string `json:"info_attributes_descriptor_hash_after"`
	InfoAttributesContentHashBefore    string `json:"info_attributes_content_hash_before"`
	InfoAttributesContentHashAfter     string `json:"info_attributes_content_hash_after"`
	InfoAttributesUnchanged            bool   `json:"info_attributes_unchanged"`
	InfoExcludeExists                  bool   `json:"info_exclude_exists"`
	InfoExcludeDescriptorHashBefore    string `json:"info_exclude_descriptor_hash_before"`
	InfoExcludeDescriptorHashAfter     string `json:"info_exclude_descriptor_hash_after"`
	InfoExcludeContentHashBefore       string `json:"info_exclude_content_hash_before"`
	InfoExcludeContentHashAfter        string `json:"info_exclude_content_hash_after"`
	InfoExcludeUnchanged               bool   `json:"info_exclude_unchanged"`
	BaseTreeGitattributesInventoryHash string `json:"base_tree_gitattributes_inventory_hash"`
	EffectiveAttributesHash            string `json:"effective_attributes_hash"`
	BaseTreeGitattributesVerified      bool   `json:"base_tree_gitattributes_verified"`
	NoExternalFilterAttributes         bool   `json:"no_external_filter_attributes"`
	NoProcessFilterAttributes          bool   `json:"no_process_filter_attributes"`
	NoExternalCommandAttributes        bool   `json:"no_external_command_attributes"`
}

// RepositoryWorktreeMaterializationObservation is canonical, closed,
// self-hashed data. Decoding it proves no filesystem or Git fact.
type RepositoryWorktreeMaterializationObservation struct {
	SchemaVersion                     string                            `json:"schema_version"`
	ObservationHash                   string                            `json:"observation_hash"`
	ObservationID                     string                            `json:"observation_id"`
	State                             RepositoryWorktreeState           `json:"state"`
	AmbiguityReason                   RepositoryWorktreeAmbiguityReason `json:"ambiguity_reason"`
	AuthorizationHash                 string                            `json:"authorization_hash"`
	ApprovalHash                      string                            `json:"approval_hash"`
	RequestHash                       string                            `json:"request_hash"`
	DispatchHash                      string                            `json:"dispatch_hash"`
	AttemptHash                       string                            `json:"attempt_hash"`
	AttemptNumber                     int                               `json:"attempt_number"`
	AttemptCap                        int                               `json:"attempt_cap"`
	ClaimHash                         string                            `json:"claim_hash"`
	Repository                        RepositoryBinding                 `json:"repository"`
	WorktreeSlotID                    string                            `json:"worktree_slot_id"`
	WorktreeSlotPathHash              string                            `json:"worktree_slot_path_hash"`
	InstalledWorktreeRootIdentityHash string                            `json:"installed_worktree_root_identity_hash"`
	MaterializerProfileID             string                            `json:"materializer_profile_id"`
	MaterializerProfileHash           string                            `json:"materializer_profile_hash"`
	SourceRootDescriptor              RepositoryDescriptorIdentity      `json:"source_root_descriptor"`
	CommonGitRootDescriptor           RepositoryDescriptorIdentity      `json:"common_git_root_descriptor"`
	CandidateRootDescriptor           RepositoryDescriptorIdentity      `json:"candidate_root_descriptor"`
	CandidateGitfileDescriptor        RepositoryDescriptorIdentity      `json:"candidate_gitfile_descriptor"`
	CandidateAdminDescriptor          RepositoryDescriptorIdentity      `json:"candidate_admin_descriptor"`
	BeforeCommonGitInventoryHash      string                            `json:"before_common_git_inventory_hash"`
	AfterCommonGitInventoryHash       string                            `json:"after_common_git_inventory_hash"`
	CommonGitDelta                    CommonGitDeltaObservation         `json:"common_git_delta"`
	Candidate                         RepositoryCandidateObservation    `json:"candidate"`
	Source                            RepositorySourceClosure           `json:"source"`
	WritablePaths                     RepositoryWritablePathClosure     `json:"writable_paths"`
	Config                            RepositoryGitConfigClosure        `json:"config"`
	Attributes                        RepositoryGitAttributesClosure    `json:"attributes"`
}

// VerifiedRepositoryWorktreeSnapshot is opaque evidence from a future trusted
// no-follow descriptor/Git verifier. No production constructor or decoder from
// caller bytes exists in this slice.
type VerifiedRepositoryWorktreeSnapshot struct {
	valid                      bool
	descriptorVerified         bool
	deltaVerified              bool
	configVerified             bool
	attributesVerified         bool
	pathVerified               bool
	uniquenessVerified         bool
	ambiguityChecked           bool
	tupleUnique                bool
	slotUnique                 bool
	ownershipCertain           bool
	unambiguous                bool
	observation                RepositoryWorktreeMaterializationObservation
	canonical                  []byte
	canonicalHash              string
	verifierAuthorityHash      string
	descriptorVerificationSeal string
	deltaVerificationSeal      string
	configVerificationSeal     string
	attributesVerificationSeal string
	pathVerificationSeal       string
	uniquenessVerificationSeal string
	ambiguityVerificationSeal  string
	integrityHash              string
}

type repositoryWorktreeSnapshotIntegrity struct {
	IntegrityHash              string `json:"integrity_hash"`
	Valid                      bool   `json:"valid"`
	DescriptorVerified         bool   `json:"descriptor_verified"`
	DeltaVerified              bool   `json:"delta_verified"`
	ConfigVerified             bool   `json:"config_verified"`
	AttributesVerified         bool   `json:"attributes_verified"`
	PathVerified               bool   `json:"path_verified"`
	UniquenessVerified         bool   `json:"uniqueness_verified"`
	AmbiguityChecked           bool   `json:"ambiguity_checked"`
	TupleUnique                bool   `json:"tuple_unique"`
	SlotUnique                 bool   `json:"slot_unique"`
	OwnershipCertain           bool   `json:"ownership_certain"`
	Unambiguous                bool   `json:"unambiguous"`
	ObservationHash            string `json:"observation_hash"`
	CanonicalHash              string `json:"canonical_hash"`
	VerifierAuthorityHash      string `json:"verifier_authority_hash"`
	DescriptorVerificationSeal string `json:"descriptor_verification_seal"`
	DeltaVerificationSeal      string `json:"delta_verification_seal"`
	ConfigVerificationSeal     string `json:"config_verification_seal"`
	AttributesVerificationSeal string `json:"attributes_verification_seal"`
	PathVerificationSeal       string `json:"path_verification_seal"`
	UniquenessVerificationSeal string `json:"uniqueness_verification_seal"`
	AmbiguityVerificationSeal  string `json:"ambiguity_verification_seal"`
}

// RepositoryWorktreeAssessment is classification only. EffectAllowed is always
// false and is never accepted as cleanup, launch, ref, commit, or Git authority.
type RepositoryWorktreeAssessment struct {
	Disposition     RepositoryWorktreeDisposition
	EffectAllowed   bool
	NextRequirement RepositoryWorktreeRequirement
}

// VerifiedRepositoryWorktree is an opaque predecessor capability for the next
// normal phase. It grants no filesystem, Git, cleanup, or launch effect itself.
type VerifiedRepositoryWorktree struct {
	valid                     bool
	observationHash           string
	snapshotIntegrityHash     string
	verifierAuthorityHash     string
	verificationSealsHash     string
	authorizationHash         string
	claimHash                 string
	attemptHash               string
	worktreeSlotID            string
	writablePathSetHash       string
	candidateRootIdentityHash string
	canonical                 []byte
	canonicalHash             string
	integrityHash             string
}

type verifiedRepositoryWorktreeIntegrity struct {
	IntegrityHash             string `json:"integrity_hash"`
	Valid                     bool   `json:"valid"`
	ObservationHash           string `json:"observation_hash"`
	SnapshotIntegrityHash     string `json:"snapshot_integrity_hash"`
	VerifierAuthorityHash     string `json:"verifier_authority_hash"`
	VerificationSealsHash     string `json:"verification_seals_hash"`
	AuthorizationHash         string `json:"authorization_hash"`
	ClaimHash                 string `json:"claim_hash"`
	AttemptHash               string `json:"attempt_hash"`
	WorktreeSlotID            string `json:"worktree_slot_id"`
	WritablePathSetHash       string `json:"writable_path_set_hash"`
	CandidateRootIdentityHash string `json:"candidate_root_identity_hash"`
	CanonicalHash             string `json:"canonical_hash"`
}

// VerifiedRepositoryWorktreeInstallation is the opaque accepted-installation
// capability. Only VerifyRepositoryWorktreeInstallation can produce it; its
// private integrity binds the installed record, the frozen verifier authority,
// the verified authorization, and the attempt.
type VerifiedRepositoryWorktreeInstallation struct {
	valid                 bool
	installation          RepositoryWorktreeInstallationAuthority
	verifierAuthorityHash string
	authorizationHash     string
	attemptHash           string
	integrityHash         string
}

type verifiedRepositoryWorktreeInstallationIntegrity struct {
	IntegrityHash                     string `json:"integrity_hash"`
	Valid                             bool   `json:"valid"`
	WorktreeSlotID                    string `json:"worktree_slot_id"`
	WorktreeSlotPathHash              string `json:"worktree_slot_path_hash"`
	InstalledWorktreeRootIdentityHash string `json:"installed_worktree_root_identity_hash"`
	MaterializerProfileID             string `json:"materializer_profile_id"`
	MaterializerProfileHash           string `json:"materializer_profile_hash"`
	VerifierAuthorityHash             string `json:"verifier_authority_hash"`
	AuthorizationHash                 string `json:"authorization_hash"`
	AttemptHash                       string `json:"attempt_hash"`
}

// DecodeRepositoryWorktreeObservation accepts canonical data only. Success is
// never descriptor, filesystem, Git, uniqueness, or materialization authority.
func DecodeRepositoryWorktreeObservation(raw []byte) (RepositoryWorktreeMaterializationObservation, error) {
	value, err := decodeCanonicalRecord[RepositoryWorktreeMaterializationObservation](raw)
	if err != nil || validateRepositoryWorktreeObservation(value) != nil {
		return RepositoryWorktreeMaterializationObservation{}, ErrInvalidRepositoryWorktree
	}
	return value, nil
}

// EvaluateRepositoryWorktree first re-establishes fresh release trust and the
// release-pinned verifier authority, then rechecks all opaque evidence and
// complete effect-time freshness. Its only capability result is for an exact
// newly materialized snapshot and that result remains non-effect authority.
func EvaluateRepositoryWorktree(expected SupervisorIntentAuthority, installation *VerifiedRepositoryWorktreeInstallation, authorization *VerifiedAuthorization, claim *VerifiedSupervisorIntentClaim, snapshot *VerifiedRepositoryWorktreeSnapshot, action RepositoryWorktreeAction, now time.Time) (RepositoryWorktreeAssessment, *VerifiedRepositoryWorktree, error) {
	invalid := RepositoryWorktreeAssessment{}
	now = now.UTC()
	verifier, err := deriveFrozenRepositoryWorktreeVerifierAuthority()
	if err != nil || VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now) != nil {
		return invalid, nil, ErrInvalidRepositoryWorktree
	}
	if expected.Phase != MaterializationClaimPhase || expected.Sequence != 1 || expected.PredecessorClaimHash != "" ||
		validateSupervisorIntentAuthority(expected, authorization) != nil ||
		validateSupervisorIntentFreshness(expected, authorization, now) != nil ||
		!verifiedSupervisorIntentClaimIntact(claim) ||
		validateSupervisorIntentClaim(expected, authorization, nil, nil, claim.claim) != nil ||
		!verifiedRepositoryWorktreeInstallationIntact(installation, expected, authorization) ||
		!verifiedRepositoryWorktreeSnapshotIntact(snapshot, verifier) ||
		!repositoryWorktreeSnapshotMatchesAuthority(snapshot, expected, installation.installation, authorization, claim) {
		return invalid, nil, ErrInvalidRepositoryWorktree
	}

	observation := snapshot.observation
	record := installation.installation
	switch observation.State {
	case RepositoryWorktreeNewExact:
		if action != RepositoryWorktreeAdmitNew || !snapshot.unambiguous || !snapshot.tupleUnique || !snapshot.slotUnique ||
			!snapshot.ownershipCertain || !exactRepositoryWorktreeClosure(observation, record, authorization.authorization.Scope.WritablePaths, true) {
			return invalid, nil, ErrInvalidRepositoryWorktree
		}
		// The only capability construction in this package: after fresh release
		// trust, verifier-authority equality, installation intactness, snapshot
		// seal recomputation, uniqueness booleans, exact closure, and the admit
		// action have all passed. No standalone production minter exists.
		seals := deriveRepositoryWorktreeVerificationSeals(verifier, observation, snapshot.canonicalHash, true, true, true, true)
		capability := &VerifiedRepositoryWorktree{
			valid:                     true,
			observationHash:           observation.ObservationHash,
			snapshotIntegrityHash:     snapshot.integrityHash,
			verifierAuthorityHash:     verifier.VerifierAuthorityHash,
			verificationSealsHash:     repositoryWorktreeVerificationSealsHash(seals),
			authorizationHash:         observation.AuthorizationHash,
			claimHash:                 observation.ClaimHash,
			attemptHash:               observation.AttemptHash,
			worktreeSlotID:            observation.WorktreeSlotID,
			writablePathSetHash:       observation.WritablePaths.AuthorizedPathSetHash,
			candidateRootIdentityHash: observation.CandidateRootDescriptor.NoFollowIdentityHash,
			canonical:                 append([]byte(nil), snapshot.canonical...),
			canonicalHash:             snapshot.canonicalHash,
		}
		capability.integrityHash = verifiedRepositoryWorktreeIntegrityHash(capability)
		if !verifiedRepositoryWorktreeIntact(capability) {
			return invalid, nil, ErrInvalidRepositoryWorktree
		}
		return RepositoryWorktreeAssessment{
			Disposition:     RepositoryWorktreeCapabilityReady,
			NextRequirement: RepositoryWorktreeNextNormalPhase,
		}, capability, nil
	case RepositoryWorktreeRetainedExact:
		if action != RepositoryWorktreeStatusOnly || !snapshot.unambiguous || !snapshot.tupleUnique || !snapshot.slotUnique ||
			!snapshot.ownershipCertain || !exactRepositoryWorktreeClosure(observation, record, authorization.authorization.Scope.WritablePaths, false) {
			return invalid, nil, ErrInvalidRepositoryWorktree
		}
		return RepositoryWorktreeAssessment{
			Disposition:     RepositoryWorktreeRetainedStatus,
			NextRequirement: RepositoryWorktreeNoFurtherEffect,
		}, nil, nil
	case RepositoryWorktreeRetainForHuman:
		if action != RepositoryWorktreeStatusOnly || snapshot.unambiguous ||
			!validRepositoryWorktreeRetainForHumanReason(observation.AmbiguityReason) {
			return invalid, nil, ErrInvalidRepositoryWorktree
		}
		return RepositoryWorktreeAssessment{
			Disposition:     RepositoryWorktreeWaitingForHuman,
			NextRequirement: RepositoryWorktreeHumanReview,
		}, nil, nil
	case RepositoryWorktreeOrphanedStatusOnly:
		if action != RepositoryWorktreeStatusOnly || snapshot.unambiguous ||
			observation.AmbiguityReason != RepositoryWorktreeOrphanedMaterialization {
			return invalid, nil, ErrInvalidRepositoryWorktree
		}
		return RepositoryWorktreeAssessment{
			Disposition:     RepositoryWorktreeWaitingForHuman,
			NextRequirement: RepositoryWorktreeHumanReview,
		}, nil, nil
	default:
		return invalid, nil, ErrInvalidRepositoryWorktree
	}
}

func validateRepositoryWorktreeInstallationAuthority(value RepositoryWorktreeInstallationAuthority) error {
	if !validClosedIdentifier(value.WorktreeSlotID, maxRepositoryWorktreeIDBytes) ||
		!validClosedIdentifier(value.MaterializerProfileID, maxRepositoryWorktreeIDBytes) ||
		!validHash(value.WorktreeSlotPathHash) || !validHash(value.InstalledWorktreeRootIdentityHash) ||
		!validHash(value.MaterializerProfileHash) {
		return ErrInvalidRepositoryWorktree
	}
	return nil
}

func validateRepositoryWorktreeObservation(value RepositoryWorktreeMaterializationObservation) error {
	if value.SchemaVersion != RepositoryWorktreeObservationSchemaVersion ||
		!validClosedIdentifier(value.ObservationID, maxRepositoryWorktreeIDBytes) ||
		!recordHashMatches(value, "observation_hash", value.ObservationHash) ||
		!validHash(value.AuthorizationHash) || !validHash(value.ApprovalHash) || !validHash(value.RequestHash) ||
		!validHash(value.DispatchHash) || !validHash(value.AttemptHash) || !validHash(value.ClaimHash) ||
		value.AttemptNumber < 1 || value.AttemptNumber > AttemptCap || value.AttemptCap != AttemptCap ||
		!validClosedIdentifier(value.WorktreeSlotID, maxRepositoryWorktreeIDBytes) ||
		!validClosedIdentifier(value.MaterializerProfileID, maxRepositoryWorktreeIDBytes) ||
		!validHash(value.WorktreeSlotPathHash) || !validHash(value.InstalledWorktreeRootIdentityHash) ||
		!validHash(value.MaterializerProfileHash) || !validHash(value.BeforeCommonGitInventoryHash) ||
		!validHash(value.AfterCommonGitInventoryHash) ||
		value.Repository.SchemaVersion != RepositoryBindingSchemaVersion ||
		!recordHashMatches(value.Repository, "repository_binding_hash", value.Repository.RepositoryBindingHash) ||
		!gitObjectPattern.MatchString(value.Repository.BaseCommit) || !gitObjectPattern.MatchString(value.Repository.BaseTree) {
		return ErrInvalidRepositoryWorktree
	}
	if !validRepositoryWorktreeStateReason(value.State, value.AmbiguityReason) {
		return ErrInvalidRepositoryWorktree
	}
	if validateRepositoryDescriptor(value.SourceRootDescriptor) != nil || validateRepositoryDescriptor(value.CommonGitRootDescriptor) != nil ||
		validateRepositoryDescriptor(value.CandidateRootDescriptor) != nil || validateRepositoryDescriptor(value.CandidateGitfileDescriptor) != nil ||
		validateRepositoryDescriptor(value.CandidateAdminDescriptor) != nil || validateCommonGitDelta(value.CommonGitDelta) != nil ||
		validateRepositoryCandidate(value.Candidate) != nil || validateRepositorySourceClosure(value.Source) != nil ||
		validateRepositoryWritablePaths(value.WritablePaths) != nil || validateRepositoryConfigClosure(value.Config) != nil ||
		validateRepositoryAttributesClosure(value.Attributes) != nil {
		return ErrInvalidRepositoryWorktree
	}
	return nil
}

func validRepositoryWorktreeStateReason(state RepositoryWorktreeState, reason RepositoryWorktreeAmbiguityReason) bool {
	switch state {
	case RepositoryWorktreeNewExact, RepositoryWorktreeRetainedExact:
		return reason == ""
	case RepositoryWorktreeRetainForHuman:
		return validRepositoryWorktreeRetainForHumanReason(reason)
	case RepositoryWorktreeOrphanedStatusOnly:
		return reason == RepositoryWorktreeOrphanedMaterialization
	default:
		return false
	}
}

func validRepositoryWorktreeRetainForHumanReason(value RepositoryWorktreeAmbiguityReason) bool {
	return value != RepositoryWorktreeOrphanedMaterialization && validRepositoryWorktreeAmbiguityReason(value)
}

func validRepositoryWorktreeAmbiguityReason(value RepositoryWorktreeAmbiguityReason) bool {
	switch value {
	case RepositoryWorktreeConflictingRetained, RepositoryWorktreePartialDelta,
		RepositoryWorktreeUncertainOwnership, RepositoryWorktreeConflictingSlot,
		RepositoryWorktreeConfigDrift, RepositoryWorktreeAttributesDrift,
		RepositoryWorktreePathAmbiguity, RepositoryWorktreeProtectedCommonGitDrift,
		RepositoryWorktreeCandidateDrift, RepositoryWorktreeDescriptorDrift,
		RepositoryWorktreeOrphanedMaterialization:
		return true
	default:
		return false
	}
}

func validRepositoryDescriptorID(value RepositoryDescriptorID) bool {
	switch value {
	case RepositorySourceRootDescriptor, RepositoryCommonGitRootDescriptor,
		RepositoryCandidateRootDescriptor, RepositoryCandidateGitfileDescriptor,
		RepositoryCandidateAdminDescriptor:
		return true
	default:
		return false
	}
}

func validRepositoryDescriptorObjectKind(value RepositoryDescriptorObjectKind) bool {
	return value == RepositoryDescriptorDirectory || value == RepositoryDescriptorRegularFile
}

func validCommonGitMemberID(value CommonGitMemberID) bool {
	switch value {
	case CommonGitMemberHEAD, CommonGitMemberORIGHEAD, CommonGitMemberCommonDir,
		CommonGitMemberGitDir, CommonGitMemberIndex, CommonGitMemberLogsHEAD:
		return true
	default:
		return false
	}
}

func validCommonGitMemberSemantic(value CommonGitMemberSemantic) bool {
	switch value {
	case CommonGitDetachedHEADAtBase, CommonGitORIGHEADAtBase, CommonGitCommonDirParentParent,
		CommonGitAdminGitDirBacklink, CommonGitIndexAtBaseTree, CommonGitDetachedCheckoutLog:
		return true
	default:
		return false
	}
}

func validCommonGitDeltaState(value CommonGitDeltaState) bool {
	return value == CommonGitDeltaExactNew || value == CommonGitDeltaRetainedExact || value == CommonGitDeltaAmbiguous
}

func validCommonGitProtectedDomainID(value CommonGitProtectedDomainID) bool {
	switch value {
	case CommonGitProtectedRefs, CommonGitProtectedLogsOutsideCandidate, CommonGitProtectedConfig,
		CommonGitProtectedObjects, CommonGitProtectedHooks, CommonGitProtectedInfoExclude,
		CommonGitProtectedInfoAttributes, CommonGitProtectedAlternates, CommonGitProtectedShallow,
		CommonGitProtectedGrafts, CommonGitProtectedReplace, CommonGitProtectedPackedRefs,
		CommonGitProtectedCommonIndex, CommonGitProtectedWorktreeSiblings:
		return true
	default:
		return false
	}
}

func validRepositoryCandidateHeadMode(value RepositoryCandidateHeadMode) bool {
	return value == RepositoryCandidateDetached || value == RepositoryCandidateBranch
}

func validRepositoryCandidateStatus(value RepositoryCandidateStatus) bool {
	return value == RepositoryCandidateClean || value == RepositoryCandidateDirty
}

func validateRepositoryDescriptor(value RepositoryDescriptorIdentity) error {
	if value.SchemaVersion != RepositoryDescriptorIdentitySchemaVersion ||
		!validRepositoryDescriptorID(value.DescriptorID) ||
		!validHash(value.CanonicalPathHash) || !validHash(value.NoFollowIdentityHash) ||
		!recordHashMatches(value, "descriptor_hash", value.DescriptorHash) ||
		!validRepositoryDescriptorObjectKind(value.ObjectKind) {
		return ErrInvalidRepositoryWorktree
	}
	return nil
}

func validateCommonGitDelta(value CommonGitDeltaObservation) error {
	if value.SchemaVersion != CommonGitDeltaObservationSchemaVersion ||
		!recordHashMatches(value, "delta_hash", value.DeltaHash) ||
		!validClosedIdentifier(value.WorktreeSlotID, maxRepositoryWorktreeIDBytes) ||
		!validHash(value.CandidateAdminSubtreePathHash) || !validHash(value.BeforeInventoryHash) ||
		!validHash(value.AfterInventoryHash) || len(value.Members) > maxRepositoryWorktreeMembers ||
		len(value.AddedMemberIDs) > maxRepositoryWorktreeMembers || len(value.ProtectedDomains) > maxRepositoryWorktreeMembers {
		return ErrInvalidRepositoryWorktree
	}
	if !validCommonGitDeltaState(value.State) {
		return ErrInvalidRepositoryWorktree
	}
	for _, member := range value.Members {
		if member.SchemaVersion != CommonGitMemberObservationSchemaVersion || member.Sequence < 1 ||
			!validCommonGitMemberID(member.MemberID) ||
			!validCommonGitMemberSemantic(member.Semantic) ||
			!validHash(member.RepositoryRelativePathHash) || !validHash(member.DescriptorIdentityHash) ||
			!validHash(member.ContentHash) || !validHash(member.SemanticTargetHash) ||
			!recordHashMatches(member, "member_hash", member.MemberHash) {
			return ErrInvalidRepositoryWorktree
		}
	}
	for _, id := range value.AddedMemberIDs {
		if !validCommonGitMemberID(id) {
			return ErrInvalidRepositoryWorktree
		}
	}
	for _, hashes := range [][]string{value.ChangedPreexistingMemberHashes, value.RemovedPreexistingMemberHashes, value.ExtraAddedMemberHashes} {
		if len(hashes) > maxRepositoryWorktreeMembers {
			return ErrInvalidRepositoryWorktree
		}
		for _, hash := range hashes {
			if !validHash(hash) {
				return ErrInvalidRepositoryWorktree
			}
		}
	}
	for _, domain := range value.ProtectedDomains {
		if domain.SchemaVersion != CommonGitProtectedDomainObservationSchemaVersion || domain.Sequence < 1 ||
			!validCommonGitProtectedDomainID(domain.DomainID) ||
			!validHash(domain.BeforeHash) || !validHash(domain.AfterHash) ||
			!recordHashMatches(domain, "domain_hash", domain.DomainHash) {
			return ErrInvalidRepositoryWorktree
		}
	}
	return nil
}

func validateRepositoryCandidate(value RepositoryCandidateObservation) error {
	if value.SchemaVersion != RepositoryCandidateObservationSchemaVersion ||
		!recordHashMatches(value, "candidate_hash", value.CandidateHash) ||
		!gitObjectPattern.MatchString(value.HEADCommit) || !gitObjectPattern.MatchString(value.ORIGHEADCommit) ||
		!gitObjectPattern.MatchString(value.IndexTree) || !validHash(value.CandidateGitfileTargetPathHash) ||
		!validHash(value.AdminGitdirBacklinkPathHash) ||
		!validRepositoryCandidateHeadMode(value.HeadMode) ||
		!validRepositoryCandidateStatus(value.InitialStatus) {
		return ErrInvalidRepositoryWorktree
	}
	return nil
}

func validateRepositorySourceClosure(value RepositorySourceClosure) error {
	if value.SchemaVersion != RepositorySourceClosureSchemaVersion ||
		!recordHashMatches(value, "source_closure_hash", value.SourceClosureHash) ||
		!validHash(value.InstalledWorktreeRootIdentityHash) || !validHash(value.SourceRootIdentityHashBefore) ||
		!validHash(value.SourceRootIdentityHashAfter) || !validHash(value.ProtectedContentsHashBefore) ||
		!validHash(value.ProtectedContentsHashAfter) {
		return ErrInvalidRepositoryWorktree
	}
	return nil
}

func validateRepositoryWritablePaths(value RepositoryWritablePathClosure) error {
	if value.SchemaVersion != RepositoryWritablePathClosureSchemaVersion ||
		!recordHashMatches(value, "path_closure_hash", value.PathClosureHash) ||
		!validHash(value.AuthorizedPathSetHash) || !validHash(value.CandidateRootIdentityHash) ||
		len(value.Paths) > maxRepositoryWritablePaths {
		return ErrInvalidRepositoryWorktree
	}
	for _, path := range value.Paths {
		if path.SchemaVersion != RepositoryWritablePathObservationSchemaVersion || path.Sequence < 1 ||
			!validClosedIdentifier(path.PathID, maxRepositoryWorktreeIDBytes) ||
			!validHash(path.RepositoryRelativePathHash) || !validHash(path.ResolvedCanonicalPathHash) ||
			!validHash(path.AncestorDescriptorChainHash) || !validHash(path.LeafIdentityHash) ||
			!recordHashMatches(path, "path_observation_hash", path.PathObservationHash) {
			return ErrInvalidRepositoryWorktree
		}
	}
	return nil
}

func validateRepositoryConfigClosure(value RepositoryGitConfigClosure) error {
	if value.SchemaVersion != RepositoryGitConfigClosureSchemaVersion ||
		!recordHashMatches(value, "config_closure_hash", value.ConfigClosureHash) ||
		!validClosedIdentifier(value.MaterializerProfileID, maxRepositoryWorktreeIDBytes) ||
		!validHash(value.MaterializerProfileHash) || !validHash(value.CommonConfigDescriptorHashBefore) ||
		!validHash(value.CommonConfigDescriptorHashAfter) || !validHash(value.CommonConfigBytesHashBefore) ||
		!validHash(value.CommonConfigBytesHashAfter) {
		return ErrInvalidRepositoryWorktree
	}
	return nil
}

func validateRepositoryAttributesClosure(value RepositoryGitAttributesClosure) error {
	if value.SchemaVersion != RepositoryGitAttributesClosureSchemaVersion ||
		!recordHashMatches(value, "attributes_closure_hash", value.AttributesClosureHash) ||
		!validHash(value.BaseTreeGitattributesInventoryHash) || !validHash(value.EffectiveAttributesHash) ||
		!validOptionalDescriptorContentPair(value.InfoAttributesExists,
			value.InfoAttributesDescriptorHashBefore, value.InfoAttributesDescriptorHashAfter,
			value.InfoAttributesContentHashBefore, value.InfoAttributesContentHashAfter) ||
		!validOptionalDescriptorContentPair(value.InfoExcludeExists,
			value.InfoExcludeDescriptorHashBefore, value.InfoExcludeDescriptorHashAfter,
			value.InfoExcludeContentHashBefore, value.InfoExcludeContentHashAfter) {
		return ErrInvalidRepositoryWorktree
	}
	return nil
}

func validOptionalDescriptorContentPair(exists bool, values ...string) bool {
	for _, value := range values {
		if exists {
			if !validHash(value) {
				return false
			}
		} else if value != "" {
			return false
		}
	}
	return true
}

var compiledRepositoryWorktreeMaterializerProfile = mustDeriveRepositoryWorktreeMaterializerProfile()

var compiledRepositoryWorktreeVerifierAuthority = mustDeriveRepositoryWorktreeVerifierAuthority()

// FrozenRepositoryWorktreeMaterializerProfile returns the only accepted
// release-installed Git 2.54 detached worktree materializer profile.
func FrozenRepositoryWorktreeMaterializerProfile() RepositoryWorktreeMaterializerProfile {
	return compiledRepositoryWorktreeMaterializerProfile
}

// FrozenRepositoryWorktreeVerifierAuthority returns the release-pinned verifier
// authority derived only from compiled material and the frozen release pins.
func FrozenRepositoryWorktreeVerifierAuthority() RepositoryWorktreeVerifierAuthority {
	return compiledRepositoryWorktreeVerifierAuthority
}

func mustDeriveRepositoryWorktreeMaterializerProfile() RepositoryWorktreeMaterializerProfile {
	profile, err := deriveRepositoryWorktreeMaterializerProfile()
	if err != nil || !recordHashMatches(profile, "materializer_profile_hash", profile.MaterializerProfileHash) {
		panic("invalid compiled Git 2.54 detached worktree materializer profile")
	}
	return profile
}

func deriveRepositoryWorktreeMaterializerProfile() (RepositoryWorktreeMaterializerProfile, error) {
	profile := RepositoryWorktreeMaterializerProfile{
		SchemaVersion:               RepositoryWorktreeMaterializerProfileSchemaVersion,
		MaterializerProfileID:       repositoryWorktreeMaterializerProfileID,
		GitVersion:                  repositoryWorktreeMaterializerGitVersion,
		DetachedHeadOnly:            true,
		ExactSixMemberDelta:         true,
		CommonDirParentParentOnly:   true,
		SystemConfigDisabled:        true,
		GlobalConfigDisabled:        true,
		NoIncludes:                  true,
		NoIncludeIf:                 true,
		NoHooksPath:                 true,
		NoAttributesFile:            true,
		NoFilters:                   true,
		NoFSMonitor:                 true,
		NoExternalCommands:          true,
		SystemAttributesDisabled:    true,
		NoExternalFilterAttributes:  true,
		NoProcessFilterAttributes:   true,
		NoExternalCommandAttributes: true,
	}
	profile.MaterializerProfileHash = mustHashRecord(profile, "materializer_profile_hash")
	if !recordHashMatches(profile, "materializer_profile_hash", profile.MaterializerProfileHash) {
		return RepositoryWorktreeMaterializerProfile{}, ErrInvalidRepositoryWorktree
	}
	return profile, nil
}

func mustDeriveRepositoryWorktreeVerifierAuthority() RepositoryWorktreeVerifierAuthority {
	authority, err := deriveRepositoryWorktreeVerifierAuthority()
	if err != nil || !recordHashMatches(authority, "verifier_authority_hash", authority.VerifierAuthorityHash) {
		panic("invalid compiled repository worktree verifier authority")
	}
	return authority
}

func deriveRepositoryWorktreeVerifierAuthority() (RepositoryWorktreeVerifierAuthority, error) {
	profile, err := deriveRepositoryWorktreeMaterializerProfile()
	if err != nil || profile != FrozenRepositoryWorktreeMaterializerProfile() {
		return RepositoryWorktreeVerifierAuthority{}, ErrInvalidRepositoryWorktree
	}
	authority := RepositoryWorktreeVerifierAuthority{
		SchemaVersion:           RepositoryWorktreeVerifierAuthoritySchemaVersion,
		VerifierID:              repositoryWorktreeVerifierID,
		MaterializerProfileID:   profile.MaterializerProfileID,
		MaterializerProfileHash: profile.MaterializerProfileHash,
		ReleasePinsHash:         FrozenReleasePins().ReleasePinsHash,
		VerificationKinds:       repositoryWorktreeVerificationKinds(),
	}
	authority.VerifierAuthorityHash = mustHashRecord(authority, "verifier_authority_hash")
	if !recordHashMatches(authority, "verifier_authority_hash", authority.VerifierAuthorityHash) {
		return RepositoryWorktreeVerifierAuthority{}, ErrInvalidRepositoryWorktree
	}
	return authority, nil
}

// deriveFrozenRepositoryWorktreeVerifierAuthority rederives the verifier
// authority and requires it to equal the frozen compiled value exactly.
func deriveFrozenRepositoryWorktreeVerifierAuthority() (RepositoryWorktreeVerifierAuthority, error) {
	derived, err := deriveRepositoryWorktreeVerifierAuthority()
	if err != nil || !reflect.DeepEqual(derived, FrozenRepositoryWorktreeVerifierAuthority()) {
		return RepositoryWorktreeVerifierAuthority{}, ErrInvalidRepositoryWorktree
	}
	return derived, nil
}

func repositoryWorktreeVerificationKinds() []RepositoryWorktreeVerificationKind {
	return []RepositoryWorktreeVerificationKind{
		RepositoryWorktreeDescriptorVerification,
		RepositoryWorktreeDeltaVerification,
		RepositoryWorktreeConfigVerification,
		RepositoryWorktreeAttributesVerification,
		RepositoryWorktreePathVerification,
		RepositoryWorktreeUniquenessVerification,
		RepositoryWorktreeAmbiguityVerification,
	}
}

func validRepositoryWorktreeVerificationKind(value RepositoryWorktreeVerificationKind) bool {
	switch value {
	case RepositoryWorktreeDescriptorVerification, RepositoryWorktreeDeltaVerification,
		RepositoryWorktreeConfigVerification, RepositoryWorktreeAttributesVerification,
		RepositoryWorktreePathVerification, RepositoryWorktreeUniquenessVerification,
		RepositoryWorktreeAmbiguityVerification:
		return true
	default:
		return false
	}
}

type repositoryWorktreeDescriptorSealEvidence struct {
	DescriptorHashes []string `json:"descriptor_hashes"`
}

type repositoryWorktreeDeltaSealEvidence struct {
	DeltaHash                    string `json:"delta_hash"`
	BeforeCommonGitInventoryHash string `json:"before_common_git_inventory_hash"`
	AfterCommonGitInventoryHash  string `json:"after_common_git_inventory_hash"`
}

type repositoryWorktreeConfigSealEvidence struct {
	ConfigClosureHash string `json:"config_closure_hash"`
}

type repositoryWorktreeAttributesSealEvidence struct {
	AttributesClosureHash string `json:"attributes_closure_hash"`
}

type repositoryWorktreePathSealEvidence struct {
	PathClosureHash string `json:"path_closure_hash"`
}

type repositoryWorktreeUniquenessSealEvidence struct {
	TupleUnique      bool `json:"tuple_unique"`
	SlotUnique       bool `json:"slot_unique"`
	OwnershipCertain bool `json:"ownership_certain"`
}

type repositoryWorktreeAmbiguitySealEvidence struct {
	Unambiguous     bool                              `json:"unambiguous"`
	State           RepositoryWorktreeState           `json:"state"`
	AmbiguityReason RepositoryWorktreeAmbiguityReason `json:"ambiguity_reason"`
}

type repositoryWorktreeVerificationSealSet struct {
	descriptor string
	delta      string
	config     string
	attributes string
	path       string
	uniqueness string
	ambiguity  string
}

func (seals repositoryWorktreeVerificationSealSet) ordered() []string {
	return []string{seals.descriptor, seals.delta, seals.config, seals.attributes, seals.path, seals.uniqueness, seals.ambiguity}
}

func repositoryWorktreeSealEvidenceHash(kind RepositoryWorktreeVerificationKind, observation RepositoryWorktreeMaterializationObservation, tupleUnique, slotUnique, ownershipCertain, unambiguous bool) string {
	var evidence any
	switch kind {
	case RepositoryWorktreeDescriptorVerification:
		descriptors := []RepositoryDescriptorIdentity{
			observation.SourceRootDescriptor, observation.CommonGitRootDescriptor, observation.CandidateRootDescriptor,
			observation.CandidateGitfileDescriptor, observation.CandidateAdminDescriptor,
		}
		hashes := make([]string, 0, len(descriptors))
		for _, descriptor := range descriptors {
			hashes = append(hashes, descriptor.DescriptorHash)
		}
		evidence = repositoryWorktreeDescriptorSealEvidence{DescriptorHashes: hashes}
	case RepositoryWorktreeDeltaVerification:
		evidence = repositoryWorktreeDeltaSealEvidence{
			DeltaHash:                    observation.CommonGitDelta.DeltaHash,
			BeforeCommonGitInventoryHash: observation.BeforeCommonGitInventoryHash,
			AfterCommonGitInventoryHash:  observation.AfterCommonGitInventoryHash,
		}
	case RepositoryWorktreeConfigVerification:
		evidence = repositoryWorktreeConfigSealEvidence{ConfigClosureHash: observation.Config.ConfigClosureHash}
	case RepositoryWorktreeAttributesVerification:
		evidence = repositoryWorktreeAttributesSealEvidence{AttributesClosureHash: observation.Attributes.AttributesClosureHash}
	case RepositoryWorktreePathVerification:
		evidence = repositoryWorktreePathSealEvidence{PathClosureHash: observation.WritablePaths.PathClosureHash}
	case RepositoryWorktreeUniquenessVerification:
		evidence = repositoryWorktreeUniquenessSealEvidence{TupleUnique: tupleUnique, SlotUnique: slotUnique, OwnershipCertain: ownershipCertain}
	case RepositoryWorktreeAmbiguityVerification:
		evidence = repositoryWorktreeAmbiguitySealEvidence{Unambiguous: unambiguous, State: observation.State, AmbiguityReason: observation.AmbiguityReason}
	default:
		return ""
	}
	raw, err := canonicalBytes(evidence)
	if err != nil {
		return ""
	}
	return sha256Digest(raw)
}

func deriveRepositoryWorktreeVerificationSeal(kind RepositoryWorktreeVerificationKind, verifier RepositoryWorktreeVerifierAuthority, observation RepositoryWorktreeMaterializationObservation, canonicalHash string, tupleUnique, slotUnique, ownershipCertain, unambiguous bool) string {
	evidenceHash := repositoryWorktreeSealEvidenceHash(kind, observation, tupleUnique, slotUnique, ownershipCertain, unambiguous)
	if !validHash(evidenceHash) {
		return ""
	}
	seal := RepositoryWorktreeVerificationSeal{
		SchemaVersion:         RepositoryWorktreeVerificationSealSchemaVersion,
		SealKind:              kind,
		VerifierAuthorityHash: verifier.VerifierAuthorityHash,
		ObservationHash:       observation.ObservationHash,
		CanonicalHash:         canonicalHash,
		EvidenceHash:          evidenceHash,
	}
	seal.SealHash = mustHashRecord(seal, "seal_hash")
	return seal.SealHash
}

func deriveRepositoryWorktreeVerificationSeals(verifier RepositoryWorktreeVerifierAuthority, observation RepositoryWorktreeMaterializationObservation, canonicalHash string, tupleUnique, slotUnique, ownershipCertain, unambiguous bool) repositoryWorktreeVerificationSealSet {
	return repositoryWorktreeVerificationSealSet{
		descriptor: deriveRepositoryWorktreeVerificationSeal(RepositoryWorktreeDescriptorVerification, verifier, observation, canonicalHash, tupleUnique, slotUnique, ownershipCertain, unambiguous),
		delta:      deriveRepositoryWorktreeVerificationSeal(RepositoryWorktreeDeltaVerification, verifier, observation, canonicalHash, tupleUnique, slotUnique, ownershipCertain, unambiguous),
		config:     deriveRepositoryWorktreeVerificationSeal(RepositoryWorktreeConfigVerification, verifier, observation, canonicalHash, tupleUnique, slotUnique, ownershipCertain, unambiguous),
		attributes: deriveRepositoryWorktreeVerificationSeal(RepositoryWorktreeAttributesVerification, verifier, observation, canonicalHash, tupleUnique, slotUnique, ownershipCertain, unambiguous),
		path:       deriveRepositoryWorktreeVerificationSeal(RepositoryWorktreePathVerification, verifier, observation, canonicalHash, tupleUnique, slotUnique, ownershipCertain, unambiguous),
		uniqueness: deriveRepositoryWorktreeVerificationSeal(RepositoryWorktreeUniquenessVerification, verifier, observation, canonicalHash, tupleUnique, slotUnique, ownershipCertain, unambiguous),
		ambiguity:  deriveRepositoryWorktreeVerificationSeal(RepositoryWorktreeAmbiguityVerification, verifier, observation, canonicalHash, tupleUnique, slotUnique, ownershipCertain, unambiguous),
	}
}

func repositoryWorktreeVerificationSealsHash(seals repositoryWorktreeVerificationSealSet) string {
	ordered := seals.ordered()
	if len(ordered) != len(repositoryWorktreeVerificationKinds()) {
		return ""
	}
	for _, seal := range ordered {
		if !validHash(seal) {
			return ""
		}
	}
	raw, err := canonicalBytes(ordered)
	if err != nil {
		return ""
	}
	return sha256Digest(raw)
}

// deriveRepositoryWorktreeSlotID is the frozen authorization-derived worktree
// slot grammar.
func deriveRepositoryWorktreeSlotID(attemptNumber int) string {
	return repositoryWorktreeSlotPrefix + strconv.Itoa(attemptNumber) + repositoryWorktreeSlotSuffix
}

// deriveRepositoryWorktreeSlotPathHash is the frozen slot path-hash derivation.
func deriveRepositoryWorktreeSlotPathHash(slotID string) string {
	return sha256Digest([]byte(repositoryWorktreeSlotPathPrefix + slotID))
}

// VerifyRepositoryWorktreeInstallation accepts an installed record only while
// fresh release trust holds, the derived verifier authority equals the frozen
// value, the supervisor authority is intact against the verified
// authorization, the materializer profile exactly equals the frozen Git 2.54
// release-installed profile, and the worktree slot follows the frozen
// authorization-derived grammar with the frozen path-hash derivation.
func VerifyRepositoryWorktreeInstallation(installation RepositoryWorktreeInstallationAuthority, expected SupervisorIntentAuthority, authorization *VerifiedAuthorization, now time.Time) (*VerifiedRepositoryWorktreeInstallation, error) {
	verifier, err := deriveFrozenRepositoryWorktreeVerifierAuthority()
	if err != nil || VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now.UTC()) != nil ||
		!verifiedAuthorizationIntact(authorization) ||
		validateSupervisorIntentAuthority(expected, authorization) != nil ||
		validateRepositoryWorktreeInstallationAuthority(installation) != nil {
		return nil, ErrInvalidRepositoryWorktree
	}
	profile := FrozenRepositoryWorktreeMaterializerProfile()
	if installation.MaterializerProfileID != profile.MaterializerProfileID ||
		installation.MaterializerProfileHash != profile.MaterializerProfileHash ||
		installation.WorktreeSlotID != deriveRepositoryWorktreeSlotID(expected.AttemptNumber) ||
		installation.WorktreeSlotPathHash != deriveRepositoryWorktreeSlotPathHash(installation.WorktreeSlotID) {
		return nil, ErrInvalidRepositoryWorktree
	}
	value := &VerifiedRepositoryWorktreeInstallation{
		valid:                 true,
		installation:          installation,
		verifierAuthorityHash: verifier.VerifierAuthorityHash,
		authorizationHash:     authorization.authorization.AuthorizationHash,
		attemptHash:           expected.AttemptHash,
	}
	value.integrityHash = repositoryWorktreeInstallationIntegrityHash(value)
	return value, nil
}

func repositoryWorktreeInstallationIntegrityHash(value *VerifiedRepositoryWorktreeInstallation) string {
	if value == nil {
		return ""
	}
	record := verifiedRepositoryWorktreeInstallationIntegrity{
		Valid:                             value.valid,
		WorktreeSlotID:                    value.installation.WorktreeSlotID,
		WorktreeSlotPathHash:              value.installation.WorktreeSlotPathHash,
		InstalledWorktreeRootIdentityHash: value.installation.InstalledWorktreeRootIdentityHash,
		MaterializerProfileID:             value.installation.MaterializerProfileID,
		MaterializerProfileHash:           value.installation.MaterializerProfileHash,
		VerifierAuthorityHash:             value.verifierAuthorityHash,
		AuthorizationHash:                 value.authorizationHash,
		AttemptHash:                       value.attemptHash,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

// verifiedRepositoryWorktreeInstallationIntact re-establishes the private
// integrity chain, the frozen verifier binding, the frozen profile and slot
// derivations, and the authorization/attempt match.
func verifiedRepositoryWorktreeInstallationIntact(value *VerifiedRepositoryWorktreeInstallation, expected SupervisorIntentAuthority, authorization *VerifiedAuthorization) bool {
	if value == nil || !value.valid || !verifiedAuthorizationIntact(authorization) ||
		!validHash(value.integrityHash) || value.integrityHash != repositoryWorktreeInstallationIntegrityHash(value) ||
		validateRepositoryWorktreeInstallationAuthority(value.installation) != nil ||
		value.verifierAuthorityHash != FrozenRepositoryWorktreeVerifierAuthority().VerifierAuthorityHash ||
		value.authorizationHash != authorization.authorization.AuthorizationHash ||
		value.attemptHash != expected.AttemptHash {
		return false
	}
	profile := FrozenRepositoryWorktreeMaterializerProfile()
	return value.installation.MaterializerProfileID == profile.MaterializerProfileID &&
		value.installation.MaterializerProfileHash == profile.MaterializerProfileHash &&
		value.installation.WorktreeSlotID == deriveRepositoryWorktreeSlotID(expected.AttemptNumber) &&
		value.installation.WorktreeSlotPathHash == deriveRepositoryWorktreeSlotPathHash(value.installation.WorktreeSlotID)
}

func repositoryWorktreeSnapshotIntegrityHash(value *VerifiedRepositoryWorktreeSnapshot) string {
	if value == nil {
		return ""
	}
	record := repositoryWorktreeSnapshotIntegrity{
		Valid: value.valid, DescriptorVerified: value.descriptorVerified, DeltaVerified: value.deltaVerified,
		ConfigVerified: value.configVerified, AttributesVerified: value.attributesVerified, PathVerified: value.pathVerified,
		UniquenessVerified: value.uniquenessVerified, AmbiguityChecked: value.ambiguityChecked,
		TupleUnique: value.tupleUnique, SlotUnique: value.slotUnique, OwnershipCertain: value.ownershipCertain,
		Unambiguous: value.unambiguous, ObservationHash: value.observation.ObservationHash, CanonicalHash: value.canonicalHash,
		VerifierAuthorityHash:      value.verifierAuthorityHash,
		DescriptorVerificationSeal: value.descriptorVerificationSeal, DeltaVerificationSeal: value.deltaVerificationSeal,
		ConfigVerificationSeal: value.configVerificationSeal, AttributesVerificationSeal: value.attributesVerificationSeal,
		PathVerificationSeal: value.pathVerificationSeal, UniquenessVerificationSeal: value.uniquenessVerificationSeal,
		AmbiguityVerificationSeal: value.ambiguityVerificationSeal,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

// verifiedRepositoryWorktreeSnapshotIntact re-establishes verifier provenance:
// every seal is recomputed from the decoded observation, the snapshot
// verification booleans, and the evaluator-derived release-pinned verifier
// authority, and must match the stored seal exactly.
func verifiedRepositoryWorktreeSnapshotIntact(value *VerifiedRepositoryWorktreeSnapshot, verifier RepositoryWorktreeVerifierAuthority) bool {
	if value == nil || !recordHashMatches(verifier, "verifier_authority_hash", verifier.VerifierAuthorityHash) ||
		!value.valid || !value.descriptorVerified || !value.deltaVerified || !value.configVerified ||
		!value.attributesVerified || !value.pathVerified || !value.uniquenessVerified || !value.ambiguityChecked ||
		!validHash(value.integrityHash) || value.integrityHash != repositoryWorktreeSnapshotIntegrityHash(value) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		value.verifierAuthorityHash != verifier.VerifierAuthorityHash {
		return false
	}
	decoded, err := DecodeRepositoryWorktreeObservation(value.canonical)
	if err != nil || !reflect.DeepEqual(decoded, value.observation) {
		return false
	}
	seals := deriveRepositoryWorktreeVerificationSeals(verifier, decoded, value.canonicalHash,
		value.tupleUnique, value.slotUnique, value.ownershipCertain, value.unambiguous)
	return value.descriptorVerificationSeal == seals.descriptor &&
		value.deltaVerificationSeal == seals.delta &&
		value.configVerificationSeal == seals.config &&
		value.attributesVerificationSeal == seals.attributes &&
		value.pathVerificationSeal == seals.path &&
		value.uniquenessVerificationSeal == seals.uniqueness &&
		value.ambiguityVerificationSeal == seals.ambiguity
}

func repositoryWorktreeSnapshotMatchesAuthority(value *VerifiedRepositoryWorktreeSnapshot, expected SupervisorIntentAuthority, installation RepositoryWorktreeInstallationAuthority, authorization *VerifiedAuthorization, claim *VerifiedSupervisorIntentClaim) bool {
	observation := value.observation
	return observation.AuthorizationHash == authorization.authorization.AuthorizationHash &&
		observation.ApprovalHash == authorization.authorization.ApprovalHash &&
		observation.RequestHash == expected.AcceptedDispatch.Request.RequestHash &&
		observation.DispatchHash == expected.AcceptedDispatch.DispatchHash &&
		observation.AttemptHash == expected.AttemptHash && observation.AttemptNumber == expected.AttemptNumber &&
		observation.AttemptCap == expected.AttemptCap && observation.ClaimHash == claim.claim.ClaimHash &&
		observation.Repository == expected.Repository &&
		observation.WorktreeSlotID == installation.WorktreeSlotID &&
		observation.WorktreeSlotPathHash == installation.WorktreeSlotPathHash &&
		observation.InstalledWorktreeRootIdentityHash == installation.InstalledWorktreeRootIdentityHash &&
		observation.MaterializerProfileID == installation.MaterializerProfileID &&
		observation.MaterializerProfileHash == installation.MaterializerProfileHash
}

func exactRepositoryWorktreeClosure(value RepositoryWorktreeMaterializationObservation, installation RepositoryWorktreeInstallationAuthority, authorized []WritablePathBinding, newlyMaterialized bool) bool {
	wantState := CommonGitDeltaRetainedExact
	if newlyMaterialized {
		wantState = CommonGitDeltaExactNew
	}
	if value.CommonGitDelta.State != wantState || value.CommonGitDelta.WorktreeSlotID != installation.WorktreeSlotID ||
		value.CommonGitDelta.CandidateAdminSubtreePathHash != value.CandidateAdminDescriptor.CanonicalPathHash ||
		value.CommonGitDelta.BeforeInventoryHash != value.BeforeCommonGitInventoryHash ||
		value.CommonGitDelta.AfterInventoryHash != value.AfterCommonGitInventoryHash ||
		value.Source.CandidateNew != newlyMaterialized || !exactRepositoryDescriptorClosure(value) ||
		!installedWorktreeRootAntiAlias(value) ||
		!exactCommonGitMemberClosure(value) || !exactCommonGitProtectedDomainClosure(value.CommonGitDelta) ||
		!exactRepositoryCandidateClosure(value) || !exactRepositorySourceClosure(value.Source, value, installation) ||
		!exactRepositoryWritablePathClosure(value.WritablePaths, value.CandidateRootDescriptor, authorized) ||
		!exactRepositoryConfigClosure(value.Config, installation) || !exactRepositoryAttributesClosure(value.Attributes) {
		return false
	}
	if len(value.CommonGitDelta.ChangedPreexistingMemberHashes) != 0 ||
		len(value.CommonGitDelta.RemovedPreexistingMemberHashes) != 0 || len(value.CommonGitDelta.ExtraAddedMemberHashes) != 0 {
		return false
	}
	if newlyMaterialized {
		return reflect.DeepEqual(value.CommonGitDelta.AddedMemberIDs, commonGitMemberIDs()) &&
			value.BeforeCommonGitInventoryHash != value.AfterCommonGitInventoryHash
	}
	return len(value.CommonGitDelta.AddedMemberIDs) == 0 &&
		value.BeforeCommonGitInventoryHash == value.AfterCommonGitInventoryHash
}

func exactRepositoryDescriptorClosure(value RepositoryWorktreeMaterializationObservation) bool {
	descriptors := []RepositoryDescriptorIdentity{
		value.SourceRootDescriptor, value.CommonGitRootDescriptor, value.CandidateRootDescriptor,
		value.CandidateGitfileDescriptor, value.CandidateAdminDescriptor,
	}
	wantIDs := []RepositoryDescriptorID{
		RepositorySourceRootDescriptor, RepositoryCommonGitRootDescriptor, RepositoryCandidateRootDescriptor,
		RepositoryCandidateGitfileDescriptor, RepositoryCandidateAdminDescriptor,
	}
	wantKinds := []RepositoryDescriptorObjectKind{
		RepositoryDescriptorDirectory, RepositoryDescriptorDirectory, RepositoryDescriptorDirectory,
		RepositoryDescriptorRegularFile, RepositoryDescriptorDirectory,
	}
	identities := make(map[string]struct{}, len(descriptors))
	paths := make(map[string]struct{}, len(descriptors))
	for index, descriptor := range descriptors {
		if descriptor.DescriptorID != wantIDs[index] || descriptor.ObjectKind != wantKinds[index] {
			return false
		}
		if _, duplicate := identities[descriptor.NoFollowIdentityHash]; duplicate {
			return false
		}
		if _, duplicate := paths[descriptor.CanonicalPathHash]; duplicate {
			return false
		}
		identities[descriptor.NoFollowIdentityHash] = struct{}{}
		paths[descriptor.CanonicalPathHash] = struct{}{}
	}
	return true
}

// installedWorktreeRootAntiAlias requires the installed worktree root identity
// to alias no descriptor no-follow identity and no descriptor canonical path.
func installedWorktreeRootAntiAlias(value RepositoryWorktreeMaterializationObservation) bool {
	root := value.InstalledWorktreeRootIdentityHash
	descriptors := []RepositoryDescriptorIdentity{
		value.SourceRootDescriptor, value.CommonGitRootDescriptor, value.CandidateRootDescriptor,
		value.CandidateGitfileDescriptor, value.CandidateAdminDescriptor,
	}
	for _, descriptor := range descriptors {
		if root == descriptor.NoFollowIdentityHash || root == descriptor.CanonicalPathHash {
			return false
		}
	}
	return true
}

func exactCommonGitMemberClosure(value RepositoryWorktreeMaterializationObservation) bool {
	members := value.CommonGitDelta.Members
	ids := commonGitMemberIDs()
	paths := []string{"HEAD", "ORIG_HEAD", "commondir", "gitdir", "index", "logs/HEAD"}
	semantics := []CommonGitMemberSemantic{
		CommonGitDetachedHEADAtBase, CommonGitORIGHEADAtBase, CommonGitCommonDirParentParent,
		CommonGitAdminGitDirBacklink, CommonGitIndexAtBaseTree, CommonGitDetachedCheckoutLog,
	}
	targets := []string{
		sha256Digest([]byte(value.Repository.BaseCommit)),
		sha256Digest([]byte(value.Repository.BaseCommit)),
		sha256Digest([]byte("../..")),
		value.CandidateGitfileDescriptor.CanonicalPathHash,
		sha256Digest([]byte(value.Repository.BaseTree)),
		sha256Digest([]byte(value.Repository.BaseCommit)),
	}
	// HEAD, ORIG_HEAD, and commondir member contents are derivable from the
	// accepted authorization base commit and the frozen commondir value, so
	// their observed content hashes must equal the derived digests exactly.
	// The gitdir, index, and logs/HEAD content hashes remain verifier-attested
	// evidence bound by observation and snapshot integrity.
	derivedContents := []string{
		sha256Digest([]byte(value.Repository.BaseCommit + "\n")),
		sha256Digest([]byte(value.Repository.BaseCommit + "\n")),
		sha256Digest([]byte("../..\n")),
	}
	if len(members) != len(ids) {
		return false
	}
	descriptors := make(map[string]struct{}, len(members))
	for index, member := range members {
		if member.Sequence != index+1 || member.MemberID != ids[index] ||
			member.RepositoryRelativePathHash != sha256Digest([]byte(paths[index])) ||
			member.Semantic != semantics[index] || member.SemanticTargetHash != targets[index] {
			return false
		}
		if index < len(derivedContents) && member.ContentHash != derivedContents[index] {
			return false
		}
		if _, duplicate := descriptors[member.DescriptorIdentityHash]; duplicate {
			return false
		}
		descriptors[member.DescriptorIdentityHash] = struct{}{}
	}
	return true
}

func commonGitMemberIDs() []CommonGitMemberID {
	return []CommonGitMemberID{
		CommonGitMemberHEAD, CommonGitMemberORIGHEAD, CommonGitMemberCommonDir,
		CommonGitMemberGitDir, CommonGitMemberIndex, CommonGitMemberLogsHEAD,
	}
}

func exactCommonGitProtectedDomainClosure(value CommonGitDeltaObservation) bool {
	want := []CommonGitProtectedDomainID{
		CommonGitProtectedRefs, CommonGitProtectedLogsOutsideCandidate, CommonGitProtectedConfig,
		CommonGitProtectedObjects, CommonGitProtectedHooks, CommonGitProtectedInfoExclude,
		CommonGitProtectedInfoAttributes, CommonGitProtectedAlternates, CommonGitProtectedShallow,
		CommonGitProtectedGrafts, CommonGitProtectedReplace, CommonGitProtectedPackedRefs,
		CommonGitProtectedCommonIndex, CommonGitProtectedWorktreeSiblings,
	}
	if len(value.ProtectedDomains) != len(want) {
		return false
	}
	for index, domain := range value.ProtectedDomains {
		if domain.Sequence != index+1 || domain.DomainID != want[index] || !domain.Unchanged || domain.BeforeHash != domain.AfterHash {
			return false
		}
	}
	return true
}

func exactRepositoryCandidateClosure(value RepositoryWorktreeMaterializationObservation) bool {
	candidate := value.Candidate
	return candidate.HEADCommit == value.Repository.BaseCommit && candidate.ORIGHEADCommit == value.Repository.BaseCommit &&
		candidate.HeadMode == RepositoryCandidateDetached && candidate.IndexTree == value.Repository.BaseTree &&
		candidate.InitialStatus == RepositoryCandidateClean && !candidate.BranchRefCreated && !candidate.BranchRefUpdated &&
		!candidate.OtherRefUpdated &&
		candidate.CandidateGitfileTargetPathHash == value.CandidateAdminDescriptor.CanonicalPathHash &&
		candidate.AdminGitdirBacklinkPathHash == value.CandidateGitfileDescriptor.CanonicalPathHash
}

func exactRepositorySourceClosure(source RepositorySourceClosure, value RepositoryWorktreeMaterializationObservation, installation RepositoryWorktreeInstallationAuthority) bool {
	return source.InstalledWorktreeRootIdentityHash == installation.InstalledWorktreeRootIdentityHash &&
		source.SourceRootIdentityHashBefore == value.SourceRootDescriptor.NoFollowIdentityHash &&
		source.SourceRootIdentityHashAfter == value.SourceRootDescriptor.NoFollowIdentityHash &&
		source.SourceRootIdentityHashBefore == source.SourceRootIdentityHashAfter &&
		source.ProtectedContentsHashBefore == source.ProtectedContentsHashAfter && source.SourceUnchanged &&
		source.CandidateExactChild && !source.SourceCandidateAlias && !source.SourceCommonGitAlias &&
		!source.CandidateCommonGitAlias && !source.CommonGitWritableByAdapter
}

func exactRepositoryWritablePathClosure(value RepositoryWritablePathClosure, candidate RepositoryDescriptorIdentity, authorized []WritablePathBinding) bool {
	if value.AuthorizedPathSetHash != authorizedWritablePathSetHash(authorized) ||
		value.CandidateRootIdentityHash != candidate.NoFollowIdentityHash || len(value.Paths) != len(authorized) ||
		!value.AllPathsUnderCandidate || !value.AncestorsNoFollowVerified || !value.NoSymlinks || !value.NoHardlinks ||
		!value.NoPrefixEscapes || !value.NoDuplicates || !value.NoCaseFoldCollisions || !value.NoUnicodeNormalizationCollisions {
		return false
	}
	seenIDs := make(map[string]struct{}, len(value.Paths))
	seenPaths := make(map[string]struct{}, len(value.Paths))
	seenResolved := make(map[string]struct{}, len(value.Paths))
	seenLeaf := make(map[string]struct{}, len(value.Paths))
	for index, path := range value.Paths {
		binding := authorized[index]
		if path.Sequence != index+1 || path.Sequence != binding.Sequence || path.PathID != binding.PathID ||
			path.RepositoryRelativePathHash != binding.RepositoryRelativePathHash {
			return false
		}
		if _, duplicate := seenIDs[path.PathID]; duplicate {
			return false
		}
		if _, duplicate := seenPaths[path.RepositoryRelativePathHash]; duplicate {
			return false
		}
		if _, duplicate := seenResolved[path.ResolvedCanonicalPathHash]; duplicate {
			return false
		}
		if _, duplicate := seenLeaf[path.LeafIdentityHash]; duplicate {
			return false
		}
		seenIDs[path.PathID] = struct{}{}
		seenPaths[path.RepositoryRelativePathHash] = struct{}{}
		seenResolved[path.ResolvedCanonicalPathHash] = struct{}{}
		seenLeaf[path.LeafIdentityHash] = struct{}{}
	}
	return true
}

func exactRepositoryConfigClosure(value RepositoryGitConfigClosure, installation RepositoryWorktreeInstallationAuthority) bool {
	return value.MaterializerProfileID == installation.MaterializerProfileID &&
		value.MaterializerProfileHash == installation.MaterializerProfileHash &&
		value.CommonConfigDescriptorHashBefore == value.CommonConfigDescriptorHashAfter &&
		value.CommonConfigBytesHashBefore == value.CommonConfigBytesHashAfter && value.CommonConfigUnchanged &&
		value.SystemConfigDisabled && value.GlobalConfigDisabled && value.NoIncludes && value.NoIncludeIf &&
		value.NoHooksPath && value.NoAttributesFile && value.NoFilters && value.NoFSMonitor && value.NoExternalCommands
}

func exactRepositoryAttributesClosure(value RepositoryGitAttributesClosure) bool {
	if !value.SystemAttributesDisabled || !value.InfoAttributesUnchanged || !value.InfoExcludeUnchanged ||
		!value.BaseTreeGitattributesVerified || !value.NoExternalFilterAttributes ||
		!value.NoProcessFilterAttributes || !value.NoExternalCommandAttributes {
		return false
	}
	if value.InfoAttributesExists && (value.InfoAttributesDescriptorHashBefore != value.InfoAttributesDescriptorHashAfter ||
		value.InfoAttributesContentHashBefore != value.InfoAttributesContentHashAfter) {
		return false
	}
	if value.InfoExcludeExists && (value.InfoExcludeDescriptorHashBefore != value.InfoExcludeDescriptorHashAfter ||
		value.InfoExcludeContentHashBefore != value.InfoExcludeContentHashAfter) {
		return false
	}
	return true
}

func authorizedWritablePathSetHash(value []WritablePathBinding) string {
	canonical, err := canonicalBytes(value)
	if err != nil {
		return ""
	}
	return sha256Digest(canonical)
}

func verifiedRepositoryWorktreeIntegrityHash(value *VerifiedRepositoryWorktree) string {
	if value == nil {
		return ""
	}
	record := verifiedRepositoryWorktreeIntegrity{
		Valid: value.valid, ObservationHash: value.observationHash, SnapshotIntegrityHash: value.snapshotIntegrityHash,
		VerifierAuthorityHash: value.verifierAuthorityHash, VerificationSealsHash: value.verificationSealsHash,
		AuthorizationHash: value.authorizationHash, ClaimHash: value.claimHash, AttemptHash: value.attemptHash,
		WorktreeSlotID: value.worktreeSlotID, WritablePathSetHash: value.writablePathSetHash,
		CandidateRootIdentityHash: value.candidateRootIdentityHash, CanonicalHash: value.canonicalHash,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

// verifiedRepositoryWorktreeIntact re-establishes provenance: the seven seals
// are recomputed from the decoded observation under the frozen verifier
// authority and the exact-new mint invariants, and must reproduce the
// aggregate verification-seals hash exactly.
func verifiedRepositoryWorktreeIntact(value *VerifiedRepositoryWorktree) bool {
	if value == nil || !value.valid || !validHash(value.integrityHash) ||
		value.integrityHash != verifiedRepositoryWorktreeIntegrityHash(value) ||
		!validHash(value.snapshotIntegrityHash) || !validHash(value.authorizationHash) || !validHash(value.claimHash) ||
		!validHash(value.attemptHash) || !validHash(value.writablePathSetHash) || !validHash(value.candidateRootIdentityHash) ||
		!validClosedIdentifier(value.worktreeSlotID, maxRepositoryWorktreeIDBytes) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		!validHash(value.verifierAuthorityHash) || !validHash(value.verificationSealsHash) {
		return false
	}
	verifier := FrozenRepositoryWorktreeVerifierAuthority()
	if value.verifierAuthorityHash != verifier.VerifierAuthorityHash {
		return false
	}
	observation, err := DecodeRepositoryWorktreeObservation(value.canonical)
	if err != nil || observation.State != RepositoryWorktreeNewExact ||
		observation.ObservationHash != value.observationHash || observation.AuthorizationHash != value.authorizationHash ||
		observation.ClaimHash != value.claimHash || observation.AttemptHash != value.attemptHash ||
		observation.WorktreeSlotID != value.worktreeSlotID ||
		observation.WritablePaths.AuthorizedPathSetHash != value.writablePathSetHash ||
		observation.CandidateRootDescriptor.NoFollowIdentityHash != value.candidateRootIdentityHash {
		return false
	}
	seals := deriveRepositoryWorktreeVerificationSeals(verifier, observation, value.canonicalHash, true, true, true, true)
	return value.verificationSealsHash == repositoryWorktreeVerificationSealsHash(seals)
}
