package repaircontract

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const repositoryWorktreeDocumentPath = "../../docs/experiments/p6-controlled-repair-repository-authority.md"

const (
	repositoryWorktreeMachineContractStart = "<!-- BEGIN P6 SLICE 4 MACHINE CONTRACT -->\n```json\n"
	repositoryWorktreeMachineContractEnd   = "\n```\n<!-- END P6 SLICE 4 MACHINE CONTRACT -->"
)

type repositoryWorktreeDocumentManifest struct {
	SchemaVersion         string                                        `json:"schema_version"`
	Status                string                                        `json:"status"`
	ObservationSchema     string                                        `json:"observation_schema_version"`
	PriorSliceVectorCount int                                           `json:"prior_slice_vector_count"`
	Slice3VectorCount     int                                           `json:"slice_3_vector_count"`
	Slice4VectorCount     int                                           `json:"slice_4_vector_count"`
	EffectAllowedValues   []bool                                        `json:"effect_allowed_values"`
	AllowedActions        []RepositoryWorktreeAction                    `json:"allowed_actions"`
	WorktreeStates        []RepositoryWorktreeState                     `json:"worktree_states"`
	AmbiguityReasons      []RepositoryWorktreeAmbiguityReason           `json:"ambiguity_reasons"`
	Dispositions          []RepositoryWorktreeDisposition               `json:"dispositions"`
	Requirements          []RepositoryWorktreeRequirement               `json:"requirements"`
	VerifierAuthority     repositoryWorktreeDocumentVerifierAuthority   `json:"verifier_authority"`
	MaterializerProfile   repositoryWorktreeDocumentMaterializerProfile `json:"materializer_profile"`
	WorktreeSlotGrammar   string                                        `json:"worktree_slot_grammar"`
	CommonGitMembers      []repositoryWorktreeDocumentMember            `json:"common_git_members"`
	ProtectedDomains      []CommonGitProtectedDomainID                  `json:"protected_common_git_domains"`
	CanonicalFixture      repositoryWorktreeDocumentCanonicalFixture    `json:"canonical_fixture"`
	VectorIDs             []string                                      `json:"vector_ids"`
}

type repositoryWorktreeDocumentVerifierAuthority struct {
	SchemaVersion         string                               `json:"schema_version"`
	VerifierID            string                               `json:"verifier_id"`
	VerifierAuthorityHash string                               `json:"verifier_authority_hash"`
	ReleasePinsHash       string                               `json:"release_pins_hash"`
	VerificationKinds     []RepositoryWorktreeVerificationKind `json:"verification_kinds"`
}

type repositoryWorktreeDocumentMaterializerProfile struct {
	SchemaVersion           string `json:"schema_version"`
	MaterializerProfileID   string `json:"materializer_profile_id"`
	MaterializerProfileHash string `json:"materializer_profile_hash"`
	GitVersion              string `json:"git_version"`
}

type repositoryWorktreeDocumentMember struct {
	Sequence                   int                     `json:"sequence"`
	MemberID                   CommonGitMemberID       `json:"member_id"`
	RepositoryRelativePathHash string                  `json:"repository_relative_path_hash"`
	Semantic                   CommonGitMemberSemantic `json:"semantic"`
}

type repositoryWorktreeDocumentDescriptor struct {
	DescriptorID   RepositoryDescriptorID `json:"descriptor_id"`
	DescriptorHash string                 `json:"descriptor_hash"`
}

type repositoryWorktreeDocumentCanonicalFixture struct {
	ObservationHash                   string                                 `json:"observation_hash"`
	CanonicalSHA256                   string                                 `json:"canonical_sha256"`
	SnapshotIntegrityHash             string                                 `json:"snapshot_integrity_hash"`
	AuthorizationHash                 string                                 `json:"authorization_hash"`
	ClaimHash                         string                                 `json:"claim_hash"`
	RepositoryBindingHash             string                                 `json:"repository_binding_hash"`
	BaseCommit                        string                                 `json:"base_commit"`
	BaseTree                          string                                 `json:"base_tree"`
	WorktreeSlotID                    string                                 `json:"worktree_slot_id"`
	WorktreeSlotPathHash              string                                 `json:"worktree_slot_path_hash"`
	InstalledWorktreeRootIdentityHash string                                 `json:"installed_worktree_root_identity_hash"`
	MaterializerProfileID             string                                 `json:"materializer_profile_id"`
	MaterializerProfileHash           string                                 `json:"materializer_profile_hash"`
	BeforeCommonGitInventoryHash      string                                 `json:"before_common_git_inventory_hash"`
	AfterCommonGitInventoryHash       string                                 `json:"after_common_git_inventory_hash"`
	CommonGitDeltaHash                string                                 `json:"common_git_delta_hash"`
	ConfigClosureHash                 string                                 `json:"config_closure_hash"`
	AttributesClosureHash             string                                 `json:"attributes_closure_hash"`
	WritablePathClosureHash           string                                 `json:"writable_path_closure_hash"`
	AuthorizedPathSetHash             string                                 `json:"authorized_path_set_hash"`
	Descriptors                       []repositoryWorktreeDocumentDescriptor `json:"descriptors"`
}

func TestP6Slice4NormativeDocumentMatchesTypesFixtureAndInventory(t *testing.T) {
	raw, err := os.ReadFile(repositoryWorktreeDocumentPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, repositoryWorktreeMachineContractStart)
	end := strings.Index(text, repositoryWorktreeMachineContractEnd)
	if start < 0 || end < 0 || end <= start || strings.Count(text, repositoryWorktreeMachineContractStart) != 1 || strings.Count(text, repositoryWorktreeMachineContractEnd) != 1 {
		t.Fatal("normative document must contain exactly one Slice-4 machine-contract JSON block")
	}
	start += len(repositoryWorktreeMachineContractStart)
	var got repositoryWorktreeDocumentManifest
	decoder := json.NewDecoder(strings.NewReader(text[start:end]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&got); err != nil {
		t.Fatalf("decode Slice-4 normative machine contract: %v", err)
	}
	want := expectedRepositoryWorktreeDocumentManifest(t)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("Slice-4 normative machine contract drifted\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func expectedRepositoryWorktreeDocumentManifest(t *testing.T) repositoryWorktreeDocumentManifest {
	t.Helper()
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	members := make([]repositoryWorktreeDocumentMember, len(fixture.observation.CommonGitDelta.Members))
	for index, member := range fixture.observation.CommonGitDelta.Members {
		members[index] = repositoryWorktreeDocumentMember{
			Sequence: member.Sequence, MemberID: member.MemberID,
			RepositoryRelativePathHash: member.RepositoryRelativePathHash, Semantic: member.Semantic,
		}
	}
	descriptorValues := []RepositoryDescriptorIdentity{
		fixture.observation.SourceRootDescriptor,
		fixture.observation.CommonGitRootDescriptor,
		fixture.observation.CandidateRootDescriptor,
		fixture.observation.CandidateGitfileDescriptor,
		fixture.observation.CandidateAdminDescriptor,
	}
	descriptors := make([]repositoryWorktreeDocumentDescriptor, len(descriptorValues))
	for index, descriptor := range descriptorValues {
		descriptors[index] = repositoryWorktreeDocumentDescriptor{DescriptorID: descriptor.DescriptorID, DescriptorHash: descriptor.DescriptorHash}
	}
	verifier := FrozenRepositoryWorktreeVerifierAuthority()
	profile := FrozenRepositoryWorktreeMaterializerProfile()
	return repositoryWorktreeDocumentManifest{
		SchemaVersion:         "ananke.controlled-repair-repository-worktree-document.v1",
		Status:                "slice_4_candidate_pending_independent_frozen_source_review",
		ObservationSchema:     RepositoryWorktreeObservationSchemaVersion,
		PriorSliceVectorCount: len(canonicalAcceptanceVectorIDs),
		Slice3VectorCount:     len(canonicalSupervisorIntentVectorIDs),
		Slice4VectorCount:     len(canonicalRepositoryWorktreeVectorIDs),
		EffectAllowedValues:   []bool{false},
		AllowedActions:        []RepositoryWorktreeAction{RepositoryWorktreeAdmitNew, RepositoryWorktreeStatusOnly},
		WorktreeStates: []RepositoryWorktreeState{
			RepositoryWorktreeNewExact,
			RepositoryWorktreeRetainedExact,
			RepositoryWorktreeRetainForHuman,
			RepositoryWorktreeOrphanedStatusOnly,
		},
		AmbiguityReasons: []RepositoryWorktreeAmbiguityReason{
			RepositoryWorktreeConflictingRetained,
			RepositoryWorktreePartialDelta,
			RepositoryWorktreeUncertainOwnership,
			RepositoryWorktreeConflictingSlot,
			RepositoryWorktreeConfigDrift,
			RepositoryWorktreeAttributesDrift,
			RepositoryWorktreePathAmbiguity,
			RepositoryWorktreeProtectedCommonGitDrift,
			RepositoryWorktreeCandidateDrift,
			RepositoryWorktreeDescriptorDrift,
			RepositoryWorktreeOrphanedMaterialization,
		},
		Dispositions: []RepositoryWorktreeDisposition{
			RepositoryWorktreeCapabilityReady,
			RepositoryWorktreeRetainedStatus,
			RepositoryWorktreeWaitingForHuman,
		},
		Requirements: []RepositoryWorktreeRequirement{
			RepositoryWorktreeNextNormalPhase,
			RepositoryWorktreeNoFurtherEffect,
			RepositoryWorktreeHumanReview,
		},
		VerifierAuthority: repositoryWorktreeDocumentVerifierAuthority{
			SchemaVersion:         RepositoryWorktreeVerifierAuthoritySchemaVersion,
			VerifierID:            verifier.VerifierID,
			VerifierAuthorityHash: verifier.VerifierAuthorityHash,
			ReleasePinsHash:       verifier.ReleasePinsHash,
			VerificationKinds:     verifier.VerificationKinds,
		},
		MaterializerProfile: repositoryWorktreeDocumentMaterializerProfile{
			SchemaVersion:           RepositoryWorktreeMaterializerProfileSchemaVersion,
			MaterializerProfileID:   profile.MaterializerProfileID,
			MaterializerProfileHash: profile.MaterializerProfileHash,
			GitVersion:              profile.GitVersion,
		},
		WorktreeSlotGrammar: repositoryWorktreeSlotPrefix + "<attempt_number>" + repositoryWorktreeSlotSuffix,
		CommonGitMembers:    members,
		ProtectedDomains:    repositoryWorktreeProtectedDomainIDs(),
		CanonicalFixture: repositoryWorktreeDocumentCanonicalFixture{
			ObservationHash:                   fixture.observation.ObservationHash,
			CanonicalSHA256:                   sha256Digest(fixture.canonical),
			SnapshotIntegrityHash:             fixture.snapshot.integrityHash,
			AuthorizationHash:                 fixture.observation.AuthorizationHash,
			ClaimHash:                         fixture.observation.ClaimHash,
			RepositoryBindingHash:             fixture.observation.Repository.RepositoryBindingHash,
			BaseCommit:                        fixture.observation.Repository.BaseCommit,
			BaseTree:                          fixture.observation.Repository.BaseTree,
			WorktreeSlotID:                    fixture.observation.WorktreeSlotID,
			WorktreeSlotPathHash:              fixture.observation.WorktreeSlotPathHash,
			InstalledWorktreeRootIdentityHash: fixture.observation.InstalledWorktreeRootIdentityHash,
			MaterializerProfileID:             fixture.observation.MaterializerProfileID,
			MaterializerProfileHash:           fixture.observation.MaterializerProfileHash,
			BeforeCommonGitInventoryHash:      fixture.observation.BeforeCommonGitInventoryHash,
			AfterCommonGitInventoryHash:       fixture.observation.AfterCommonGitInventoryHash,
			CommonGitDeltaHash:                fixture.observation.CommonGitDelta.DeltaHash,
			ConfigClosureHash:                 fixture.observation.Config.ConfigClosureHash,
			AttributesClosureHash:             fixture.observation.Attributes.AttributesClosureHash,
			WritablePathClosureHash:           fixture.observation.WritablePaths.PathClosureHash,
			AuthorizedPathSetHash:             fixture.observation.WritablePaths.AuthorizedPathSetHash,
			Descriptors:                       descriptors,
		},
		VectorIDs: append([]string(nil), canonicalRepositoryWorktreeVectorIDs[:]...),
	}
}
