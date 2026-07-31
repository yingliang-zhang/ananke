package repaircontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type canonicalRepositoryWorktreeFixture struct {
	attempt      canonicalSupervisorAttempt
	authority    SupervisorIntentAuthority
	installation RepositoryWorktreeInstallationAuthority
	observation  RepositoryWorktreeMaterializationObservation
	canonical    []byte
	snapshot     *VerifiedRepositoryWorktreeSnapshot
	now          time.Time
}

func TestP6Slice4CanonicalNewMaterialization(t *testing.T) {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	decoded, err := DecodeRepositoryWorktreeObservation(fixture.canonical)
	if err != nil || !reflect.DeepEqual(decoded, fixture.observation) {
		t.Fatalf("decode canonical observation: decoded=%+v err=%v", decoded, err)
	}

	assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeAdmitNew)
	if err != nil {
		t.Fatal(err)
	}
	want := RepositoryWorktreeAssessment{
		Disposition:     RepositoryWorktreeCapabilityReady,
		NextRequirement: RepositoryWorktreeNextNormalPhase,
	}
	if assessment != want || assessment.EffectAllowed || capability == nil || !verifiedRepositoryWorktreeIntact(capability) {
		t.Fatalf("new materialization assessment=%+v capability=%v", assessment, capability)
	}
	if capability.observationHash != fixture.observation.ObservationHash ||
		capability.snapshotIntegrityHash != fixture.snapshot.integrityHash ||
		capability.claimHash != fixture.attempt.claims[0].ClaimHash ||
		capability.authorizationHash != fixture.attempt.contract.Authorization.AuthorizationHash ||
		capability.worktreeSlotID != fixture.installation.WorktreeSlotID ||
		capability.writablePathSetHash != fixture.observation.WritablePaths.AuthorizedPathSetHash {
		t.Fatal("verified worktree capability omitted exact authority bindings")
	}
}

func TestP6Slice4OpaqueSnapshotDeepCopyAndIntegrity(t *testing.T) {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	originalCanonical := append([]byte(nil), fixture.snapshot.canonical...)
	originalObservation := fixture.snapshot.observation

	fixture.canonical[0] ^= 1
	fixture.observation.CommonGitDelta.Members[0].ContentHash = testHash("caller-mutated-content")
	if !bytes.Equal(fixture.snapshot.canonical, originalCanonical) || !reflect.DeepEqual(fixture.snapshot.observation, originalObservation) {
		t.Fatal("opaque snapshot retained caller-owned observation bytes")
	}
	if _, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeAdmitNew); err != nil || capability == nil {
		t.Fatalf("caller mutation reached opaque snapshot: capability=%v err=%v", capability, err)
	}

	installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
	mutated := cloneRepositoryWorktreeSnapshotForTest(t, fixture.snapshot)
	mutated.canonical[0] ^= 1
	assessment, capability, err := EvaluateRepositoryWorktree(
		fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0],
		mutated, RepositoryWorktreeAdmitNew, fixture.now,
	)
	if !errors.Is(err, ErrInvalidRepositoryWorktree) || capability != nil || assessment.EffectAllowed {
		t.Fatalf("mutated opaque snapshot assessment=%+v capability=%v err=%v", assessment, capability, err)
	}

	// A fully self-consistent same-package forgery (seals and integrity
	// recomputed) of the authorization-derivable HEAD member content must still
	// be rejected at content closure.
	forged := fixture.snapshot.observation
	forged.CommonGitDelta.Members[0].ContentHash = testHash("private-rehashed-content")
	sealRepositoryWorktreeObservationForTest(t, &forged)
	mutated = mintRepositoryWorktreeSnapshotForTest(t, forged, true, true, true, true)
	assessment, capability, err = EvaluateRepositoryWorktree(
		fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0],
		mutated, RepositoryWorktreeAdmitNew, fixture.now,
	)
	if !errors.Is(err, ErrInvalidRepositoryWorktree) || capability != nil || assessment.EffectAllowed {
		t.Fatalf("self-consistent forged snapshot assessment=%+v capability=%v err=%v", assessment, capability, err)
	}
}

func TestP6Slice4RetainedAndAmbiguousStatesNeverMintCapability(t *testing.T) {
	t.Run("exact retained replay", func(t *testing.T) {
		fixture := retainedRepositoryWorktreeFixtureForTest(t)
		assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeStatusOnly)
		if err != nil || capability != nil || assessment.EffectAllowed ||
			assessment.Disposition != RepositoryWorktreeRetainedStatus ||
			assessment.NextRequirement != RepositoryWorktreeNoFurtherEffect {
			t.Fatalf("retained replay assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
	})

	for _, test := range []struct {
		name   string
		reason RepositoryWorktreeAmbiguityReason
		mutate func(*canonicalRepositoryWorktreeFixture)
	}{
		{name: "conflicting retained state", reason: RepositoryWorktreeConflictingRetained, mutate: func(f *canonicalRepositoryWorktreeFixture) {
			f.observation.CommonGitDelta.ChangedPreexistingMemberHashes = []string{testHash("conflicting-retained-member")}
		}},
		{name: "partial retained state", reason: RepositoryWorktreePartialDelta, mutate: func(f *canonicalRepositoryWorktreeFixture) {
			f.observation.CommonGitDelta.Members = append([]CommonGitMemberObservation(nil), f.observation.CommonGitDelta.Members[:5]...)
		}},
		{name: "ambiguous retained state", reason: RepositoryWorktreeUncertainOwnership, mutate: func(f *canonicalRepositoryWorktreeFixture) {
			f.snapshot.ownershipCertain = false
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := canonicalRepositoryWorktreeFixtureForTest(t)
			test.mutate(&fixture)
			markRepositoryWorktreeAmbiguousForTest(t, &fixture, test.reason)
			assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeStatusOnly)
			if err != nil || capability != nil || assessment.EffectAllowed ||
				assessment.Disposition != RepositoryWorktreeWaitingForHuman ||
				assessment.NextRequirement != RepositoryWorktreeHumanReview {
				t.Fatalf("ambiguous state assessment=%+v capability=%v err=%v", assessment, capability, err)
			}
		})
	}
}

func TestP6Slice4ExactSixMemberClosure(t *testing.T) {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	wantIDs := []CommonGitMemberID{
		CommonGitMemberHEAD,
		CommonGitMemberORIGHEAD,
		CommonGitMemberCommonDir,
		CommonGitMemberGitDir,
		CommonGitMemberIndex,
		CommonGitMemberLogsHEAD,
	}
	wantPaths := []string{"HEAD", "ORIG_HEAD", "commondir", "gitdir", "index", "logs/HEAD"}
	wantSemantics := []CommonGitMemberSemantic{
		CommonGitDetachedHEADAtBase,
		CommonGitORIGHEADAtBase,
		CommonGitCommonDirParentParent,
		CommonGitAdminGitDirBacklink,
		CommonGitIndexAtBaseTree,
		CommonGitDetachedCheckoutLog,
	}
	if len(fixture.observation.CommonGitDelta.Members) != len(wantIDs) ||
		!reflect.DeepEqual(fixture.observation.CommonGitDelta.AddedMemberIDs, wantIDs) {
		t.Fatalf("common-git member inventory=%+v", fixture.observation.CommonGitDelta)
	}
	for index, member := range fixture.observation.CommonGitDelta.Members {
		if member.Sequence != index+1 || member.MemberID != wantIDs[index] ||
			member.RepositoryRelativePathHash != sha256Digest([]byte(wantPaths[index])) ||
			member.Semantic != wantSemantics[index] || !validHash(member.ContentHash) || !validHash(member.DescriptorIdentityHash) {
			t.Fatalf("member %d = %+v", index, member)
		}
	}
	if fixture.observation.CommonGitDelta.Members[2].SemanticTargetHash != sha256Digest([]byte("../..")) {
		t.Fatal("commondir semantic value is not exactly ../..")
	}
	if fixture.observation.CommonGitDelta.Members[3].SemanticTargetHash != fixture.observation.CandidateGitfileDescriptor.CanonicalPathHash ||
		fixture.observation.Candidate.CandidateGitfileTargetPathHash != fixture.observation.CandidateAdminDescriptor.CanonicalPathHash ||
		fixture.observation.Candidate.AdminGitdirBacklinkPathHash != fixture.observation.CandidateGitfileDescriptor.CanonicalPathHash {
		t.Fatal("candidate gitfile and admin gitdir are not cross-bound")
	}
}

func TestP6Slice4ConfigurationAttributesAndProtectedDomainsAreClosed(t *testing.T) {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	config := fixture.observation.Config
	if !config.CommonConfigUnchanged || config.CommonConfigBytesHashBefore != config.CommonConfigBytesHashAfter ||
		config.CommonConfigDescriptorHashBefore != config.CommonConfigDescriptorHashAfter ||
		!config.SystemConfigDisabled || !config.GlobalConfigDisabled || !config.NoIncludes || !config.NoIncludeIf ||
		!config.NoHooksPath || !config.NoAttributesFile || !config.NoFilters || !config.NoFSMonitor || !config.NoExternalCommands {
		t.Fatalf("config closure=%+v", config)
	}
	attributes := fixture.observation.Attributes
	if !attributes.SystemAttributesDisabled || !attributes.InfoAttributesUnchanged || !attributes.InfoExcludeUnchanged ||
		!attributes.BaseTreeGitattributesVerified || !attributes.NoExternalFilterAttributes ||
		!attributes.NoProcessFilterAttributes || !attributes.NoExternalCommandAttributes {
		t.Fatalf("attributes closure=%+v", attributes)
	}
	wantDomains := repositoryWorktreeProtectedDomainIDs()
	if len(fixture.observation.CommonGitDelta.ProtectedDomains) != len(wantDomains) {
		t.Fatalf("protected-domain count=%d want=%d", len(fixture.observation.CommonGitDelta.ProtectedDomains), len(wantDomains))
	}
	for index, domain := range fixture.observation.CommonGitDelta.ProtectedDomains {
		if domain.Sequence != index+1 || domain.DomainID != wantDomains[index] || !domain.Unchanged || domain.BeforeHash != domain.AfterHash {
			t.Fatalf("protected domain %d=%+v", index, domain)
		}
	}
}

func TestP6Slice4CandidateSourceAndAuthorizedPathClosure(t *testing.T) {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	candidate := fixture.observation.Candidate
	if candidate.HeadMode != RepositoryCandidateDetached || candidate.HEADCommit != fixture.authority.Repository.BaseCommit ||
		candidate.ORIGHEADCommit != fixture.authority.Repository.BaseCommit || candidate.IndexTree != fixture.authority.Repository.BaseTree ||
		candidate.InitialStatus != RepositoryCandidateClean || candidate.BranchRefCreated || candidate.BranchRefUpdated || candidate.OtherRefUpdated {
		t.Fatalf("candidate closure=%+v", candidate)
	}
	source := fixture.observation.Source
	if !source.SourceUnchanged || !source.CandidateExactChild || !source.CandidateNew || source.SourceCandidateAlias ||
		source.SourceCommonGitAlias || source.CandidateCommonGitAlias || source.CommonGitWritableByAdapter ||
		source.InstalledWorktreeRootIdentityHash != fixture.installation.InstalledWorktreeRootIdentityHash {
		t.Fatalf("source closure=%+v", source)
	}
	paths := fixture.observation.WritablePaths
	if paths.AuthorizedPathSetHash != authorizedWritablePathSetHash(fixture.attempt.authorization.authorization.Scope.WritablePaths) ||
		!paths.AllPathsUnderCandidate || !paths.AncestorsNoFollowVerified || !paths.NoSymlinks || !paths.NoHardlinks ||
		!paths.NoPrefixEscapes || !paths.NoDuplicates || !paths.NoCaseFoldCollisions || !paths.NoUnicodeNormalizationCollisions {
		t.Fatalf("writable-path closure=%+v", paths)
	}
	if len(paths.Paths) != len(fixture.attempt.authorization.authorization.Scope.WritablePaths) {
		t.Fatalf("writable path count=%d", len(paths.Paths))
	}
	for index, path := range paths.Paths {
		binding := fixture.attempt.authorization.authorization.Scope.WritablePaths[index]
		if path.Sequence != binding.Sequence || path.PathID != binding.PathID || path.RepositoryRelativePathHash != binding.RepositoryRelativePathHash {
			t.Fatalf("writable path %d=%+v binding=%+v", index, path, binding)
		}
	}
}

func TestP6Slice4FreshnessAndForbiddenActions(t *testing.T) {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
	notAfter := mustTime(t, fixture.authority.NotAfter)
	for _, test := range []struct {
		name  string
		now   time.Time
		valid bool
	}{
		{name: "claim N-1", now: notAfter.Add(-time.Nanosecond), valid: true},
		{name: "claim N", now: notAfter},
		{name: "claim N+1", now: notAfter.Add(time.Nanosecond)},
	} {
		t.Run(test.name, func(t *testing.T) {
			assessment, capability, err := EvaluateRepositoryWorktree(
				fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0],
				fixture.snapshot, RepositoryWorktreeAdmitNew, test.now,
			)
			if test.valid {
				if err != nil || capability == nil || assessment.EffectAllowed {
					t.Fatalf("fresh boundary assessment=%+v capability=%v err=%v", assessment, capability, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidRepositoryWorktree) || capability != nil || assessment.EffectAllowed {
				t.Fatalf("stale boundary assessment=%+v capability=%v err=%v", assessment, capability, err)
			}
		})
	}

	for _, action := range []RepositoryWorktreeAction{
		"cleanup", "delete", "prune", "remove", "ref_update", "commit", "push", "merge", "launch", "second_worktree", "second_effect",
	} {
		t.Run(string(action), func(t *testing.T) {
			assessment, capability, err := EvaluateRepositoryWorktree(
				fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0],
				fixture.snapshot, action, fixture.now,
			)
			if !errors.Is(err, ErrInvalidRepositoryWorktree) || capability != nil || assessment.EffectAllowed {
				t.Fatalf("forbidden action %q assessment=%+v capability=%v err=%v", action, assessment, capability, err)
			}
		})
	}
}

func TestP6Slice4ObservationCanonicalJSONClosure(t *testing.T) {
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	var generic map[string]any
	if err := json.Unmarshal(fixture.canonical, &generic); err != nil {
		t.Fatal(err)
	}
	unknown := cloneGeneric(t, generic).(map[string]any)
	unknown["caller_path"] = "/tmp/forbidden"
	unknownRaw, _ := json.Marshal(unknown)
	duplicate := append([]byte(`{"schema_version":"`+RepositoryWorktreeObservationSchemaVersion+`","schema_version":"`+RepositoryWorktreeObservationSchemaVersion+`"}`), byte('\n'))
	trailing := append(append([]byte(nil), fixture.canonical...), []byte(`{}`)...)
	noncanonical := append([]byte(nil), fixture.canonical...)
	noncanonical = append(noncanonical, '\n')
	for name, raw := range map[string][]byte{
		"unknown":      unknownRaw,
		"duplicate":    duplicate,
		"trailing":     trailing,
		"noncanonical": noncanonical,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeRepositoryWorktreeObservation(raw); !errors.Is(err, ErrInvalidRepositoryWorktree) {
				t.Fatalf("decode %s error=%v", name, err)
			}
		})
	}

	left := map[string]any{"z": 1, "a": map[string]any{"y": 2, "b": 3}}
	right := map[string]any{"a": map[string]any{"b": 3, "y": 2}, "z": 1}
	leftRaw, leftErr := canonicalBytes(left)
	rightRaw, rightErr := canonicalBytes(right)
	if leftErr != nil || rightErr != nil || !bytes.Equal(leftRaw, rightRaw) {
		t.Fatalf("JCS map order drift: left=%q right=%q errors=%v,%v", leftRaw, rightRaw, leftErr, rightErr)
	}
	if bytes.Contains(fixture.canonical, []byte("/Users/")) || bytes.Contains(fixture.canonical, []byte("/tmp/")) || bytes.Contains(fixture.canonical, []byte(`"path":"`)) {
		t.Fatal("canonical observation contains a raw machine path")
	}
}

func canonicalRepositoryWorktreeFixtureForTest(t *testing.T) canonicalRepositoryWorktreeFixture {
	t.Helper()
	first, _ := canonicalSupervisorAttempts(t)
	authority := first.authorities[0]
	profile := FrozenRepositoryWorktreeMaterializerProfile()
	installation := RepositoryWorktreeInstallationAuthority{
		WorktreeSlotID:                    deriveRepositoryWorktreeSlotID(authority.AttemptNumber),
		WorktreeSlotPathHash:              deriveRepositoryWorktreeSlotPathHash(deriveRepositoryWorktreeSlotID(authority.AttemptNumber)),
		InstalledWorktreeRootIdentityHash: testHash("installed-worktree-root-identity-v1"),
		MaterializerProfileID:             profile.MaterializerProfileID,
		MaterializerProfileHash:           profile.MaterializerProfileHash,
	}

	descriptors := []RepositoryDescriptorIdentity{
		repositoryDescriptorForTest(t, RepositorySourceRootDescriptor, RepositoryDescriptorDirectory),
		repositoryDescriptorForTest(t, RepositoryCommonGitRootDescriptor, RepositoryDescriptorDirectory),
		repositoryDescriptorForTest(t, RepositoryCandidateRootDescriptor, RepositoryDescriptorDirectory),
		repositoryDescriptorForTest(t, RepositoryCandidateGitfileDescriptor, RepositoryDescriptorRegularFile),
		repositoryDescriptorForTest(t, RepositoryCandidateAdminDescriptor, RepositoryDescriptorDirectory),
	}
	beforeInventory := testHash("common-git-before-inventory")
	afterInventory := testHash("common-git-after-inventory-with-one-new-worktree")
	memberIDs := []CommonGitMemberID{CommonGitMemberHEAD, CommonGitMemberORIGHEAD, CommonGitMemberCommonDir, CommonGitMemberGitDir, CommonGitMemberIndex, CommonGitMemberLogsHEAD}
	memberPaths := []string{"HEAD", "ORIG_HEAD", "commondir", "gitdir", "index", "logs/HEAD"}
	semantics := []CommonGitMemberSemantic{CommonGitDetachedHEADAtBase, CommonGitORIGHEADAtBase, CommonGitCommonDirParentParent, CommonGitAdminGitDirBacklink, CommonGitIndexAtBaseTree, CommonGitDetachedCheckoutLog}
	semanticTargets := []string{
		sha256Digest([]byte(authority.Repository.BaseCommit)),
		sha256Digest([]byte(authority.Repository.BaseCommit)),
		sha256Digest([]byte("../..")),
		descriptors[3].CanonicalPathHash,
		sha256Digest([]byte(authority.Repository.BaseTree)),
		sha256Digest([]byte(authority.Repository.BaseCommit)),
	}
	// HEAD, ORIG_HEAD, and commondir contents are authorization-derivable; the
	// gitdir, index, and logs/HEAD contents remain verifier-attested evidence.
	memberContents := []string{
		sha256Digest([]byte(authority.Repository.BaseCommit + "\n")),
		sha256Digest([]byte(authority.Repository.BaseCommit + "\n")),
		sha256Digest([]byte("../..\n")),
		testHash("observed-member-content-" + string(CommonGitMemberGitDir)),
		testHash("observed-member-content-" + string(CommonGitMemberIndex)),
		testHash("observed-member-content-" + string(CommonGitMemberLogsHEAD)),
	}
	members := make([]CommonGitMemberObservation, len(memberIDs))
	for index := range memberIDs {
		members[index] = CommonGitMemberObservation{
			SchemaVersion:              CommonGitMemberObservationSchemaVersion,
			Sequence:                   index + 1,
			MemberID:                   memberIDs[index],
			RepositoryRelativePathHash: sha256Digest([]byte(memberPaths[index])),
			DescriptorIdentityHash:     testHash("verified-member-descriptor-" + string(memberIDs[index])),
			ContentHash:                memberContents[index],
			Semantic:                   semantics[index],
			SemanticTargetHash:         semanticTargets[index],
		}
	}
	protectedIDs := repositoryWorktreeProtectedDomainIDs()
	protected := make([]CommonGitProtectedDomainObservation, len(protectedIDs))
	for index, id := range protectedIDs {
		hash := testHash("unchanged-common-git-domain-" + string(id))
		protected[index] = CommonGitProtectedDomainObservation{
			SchemaVersion: CommonGitProtectedDomainObservationSchemaVersion,
			Sequence:      index + 1,
			DomainID:      id,
			BeforeHash:    hash,
			AfterHash:     hash,
			Unchanged:     true,
		}
	}

	bindings := first.authorization.authorization.Scope.WritablePaths
	paths := make([]RepositoryWritablePathObservation, len(bindings))
	for index, binding := range bindings {
		paths[index] = RepositoryWritablePathObservation{
			SchemaVersion:               RepositoryWritablePathObservationSchemaVersion,
			Sequence:                    binding.Sequence,
			PathID:                      binding.PathID,
			RepositoryRelativePathHash:  binding.RepositoryRelativePathHash,
			ResolvedCanonicalPathHash:   testHash("candidate-resolved-path-" + binding.PathID),
			AncestorDescriptorChainHash: testHash("candidate-ancestor-chain-" + binding.PathID),
			LeafIdentityHash:            testHash("candidate-leaf-identity-" + binding.PathID),
		}
	}
	configHash := testHash("unchanged-common-config-bytes")
	configDescriptorHash := testHash("unchanged-common-config-descriptor")
	observation := RepositoryWorktreeMaterializationObservation{
		SchemaVersion:                     RepositoryWorktreeObservationSchemaVersion,
		ObservationID:                     "attempt_1_materialization_worktree_observation_001",
		State:                             RepositoryWorktreeNewExact,
		AuthorizationHash:                 first.contract.Authorization.AuthorizationHash,
		ApprovalHash:                      first.contract.Authorization.ApprovalHash,
		RequestHash:                       first.contract.Dispatch.Request.RequestHash,
		DispatchHash:                      first.contract.Dispatch.DispatchHash,
		AttemptHash:                       authority.AttemptHash,
		AttemptNumber:                     authority.AttemptNumber,
		AttemptCap:                        authority.AttemptCap,
		ClaimHash:                         first.claims[0].ClaimHash,
		Repository:                        authority.Repository,
		WorktreeSlotID:                    installation.WorktreeSlotID,
		WorktreeSlotPathHash:              installation.WorktreeSlotPathHash,
		InstalledWorktreeRootIdentityHash: installation.InstalledWorktreeRootIdentityHash,
		MaterializerProfileID:             installation.MaterializerProfileID,
		MaterializerProfileHash:           installation.MaterializerProfileHash,
		SourceRootDescriptor:              descriptors[0],
		CommonGitRootDescriptor:           descriptors[1],
		CandidateRootDescriptor:           descriptors[2],
		CandidateGitfileDescriptor:        descriptors[3],
		CandidateAdminDescriptor:          descriptors[4],
		BeforeCommonGitInventoryHash:      beforeInventory,
		AfterCommonGitInventoryHash:       afterInventory,
		CommonGitDelta: CommonGitDeltaObservation{
			SchemaVersion:                  CommonGitDeltaObservationSchemaVersion,
			State:                          CommonGitDeltaExactNew,
			WorktreeSlotID:                 installation.WorktreeSlotID,
			CandidateAdminSubtreePathHash:  descriptors[4].CanonicalPathHash,
			BeforeInventoryHash:            beforeInventory,
			AfterInventoryHash:             afterInventory,
			Members:                        members,
			AddedMemberIDs:                 append([]CommonGitMemberID(nil), memberIDs...),
			ChangedPreexistingMemberHashes: []string{},
			RemovedPreexistingMemberHashes: []string{},
			ExtraAddedMemberHashes:         []string{},
			ProtectedDomains:               protected,
		},
		Candidate: RepositoryCandidateObservation{
			SchemaVersion:                  RepositoryCandidateObservationSchemaVersion,
			HEADCommit:                     authority.Repository.BaseCommit,
			ORIGHEADCommit:                 authority.Repository.BaseCommit,
			HeadMode:                       RepositoryCandidateDetached,
			IndexTree:                      authority.Repository.BaseTree,
			InitialStatus:                  RepositoryCandidateClean,
			CandidateGitfileTargetPathHash: descriptors[4].CanonicalPathHash,
			AdminGitdirBacklinkPathHash:    descriptors[3].CanonicalPathHash,
		},
		Source: RepositorySourceClosure{
			SchemaVersion:                     RepositorySourceClosureSchemaVersion,
			InstalledWorktreeRootIdentityHash: installation.InstalledWorktreeRootIdentityHash,
			SourceRootIdentityHashBefore:      descriptors[0].NoFollowIdentityHash,
			SourceRootIdentityHashAfter:       descriptors[0].NoFollowIdentityHash,
			ProtectedContentsHashBefore:       testHash("source-protected-contents"),
			ProtectedContentsHashAfter:        testHash("source-protected-contents"),
			SourceUnchanged:                   true,
			CandidateExactChild:               true,
			CandidateNew:                      true,
		},
		WritablePaths: RepositoryWritablePathClosure{
			SchemaVersion:                    RepositoryWritablePathClosureSchemaVersion,
			AuthorizedPathSetHash:            authorizedWritablePathSetHash(bindings),
			CandidateRootIdentityHash:        descriptors[2].NoFollowIdentityHash,
			Paths:                            paths,
			AllPathsUnderCandidate:           true,
			AncestorsNoFollowVerified:        true,
			NoSymlinks:                       true,
			NoHardlinks:                      true,
			NoPrefixEscapes:                  true,
			NoDuplicates:                     true,
			NoCaseFoldCollisions:             true,
			NoUnicodeNormalizationCollisions: true,
		},
		Config: RepositoryGitConfigClosure{
			SchemaVersion:                    RepositoryGitConfigClosureSchemaVersion,
			MaterializerProfileID:            installation.MaterializerProfileID,
			MaterializerProfileHash:          installation.MaterializerProfileHash,
			CommonConfigDescriptorHashBefore: configDescriptorHash,
			CommonConfigDescriptorHashAfter:  configDescriptorHash,
			CommonConfigBytesHashBefore:      configHash,
			CommonConfigBytesHashAfter:       configHash,
			CommonConfigUnchanged:            true,
			SystemConfigDisabled:             true,
			GlobalConfigDisabled:             true,
			NoIncludes:                       true,
			NoIncludeIf:                      true,
			NoHooksPath:                      true,
			NoAttributesFile:                 true,
			NoFilters:                        true,
			NoFSMonitor:                      true,
			NoExternalCommands:               true,
		},
		Attributes: RepositoryGitAttributesClosure{
			SchemaVersion:                      RepositoryGitAttributesClosureSchemaVersion,
			SystemAttributesDisabled:           true,
			InfoAttributesExists:               false,
			InfoAttributesUnchanged:            true,
			InfoExcludeExists:                  false,
			InfoExcludeUnchanged:               true,
			BaseTreeGitattributesInventoryHash: testHash("base-tree-gitattributes-inventory"),
			EffectiveAttributesHash:            testHash("effective-base-tree-attributes"),
			BaseTreeGitattributesVerified:      true,
			NoExternalFilterAttributes:         true,
			NoProcessFilterAttributes:          true,
			NoExternalCommandAttributes:        true,
		},
	}
	sealRepositoryWorktreeObservationForTest(t, &observation)
	canonical := canonicalTestArtifact(t, observation)
	snapshot := mintRepositoryWorktreeSnapshotForTest(t, observation, true, true, true, true)
	return canonicalRepositoryWorktreeFixture{
		attempt: first, authority: authority, installation: installation, observation: observation,
		canonical: canonical, snapshot: snapshot, now: mustTime(t, authority.CreatedAt),
	}
}

func retainedRepositoryWorktreeFixtureForTest(t *testing.T) canonicalRepositoryWorktreeFixture {
	t.Helper()
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	fixture.observation.State = RepositoryWorktreeRetainedExact
	fixture.observation.CommonGitDelta.State = CommonGitDeltaRetainedExact
	fixture.observation.CommonGitDelta.AddedMemberIDs = []CommonGitMemberID{}
	fixture.observation.BeforeCommonGitInventoryHash = fixture.observation.AfterCommonGitInventoryHash
	fixture.observation.CommonGitDelta.BeforeInventoryHash = fixture.observation.CommonGitDelta.AfterInventoryHash
	fixture.observation.Source.CandidateNew = false
	sealRepositoryWorktreeObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	fixture.snapshot = mintRepositoryWorktreeSnapshotForTest(t, fixture.observation, true, true, true, true)
	return fixture
}

func repositoryDescriptorForTest(t *testing.T, id RepositoryDescriptorID, kind RepositoryDescriptorObjectKind) RepositoryDescriptorIdentity {
	t.Helper()
	value := RepositoryDescriptorIdentity{
		SchemaVersion:        RepositoryDescriptorIdentitySchemaVersion,
		DescriptorID:         id,
		CanonicalPathHash:    testHash("canonical-path-" + string(id)),
		NoFollowIdentityHash: testHash("no-follow-identity-" + string(id)),
		ObjectKind:           kind,
	}
	value.DescriptorHash = mustRecordHash(t, value, "descriptor_hash")
	return value
}

func sealRepositoryWorktreeObservationForTest(t *testing.T, value *RepositoryWorktreeMaterializationObservation) {
	t.Helper()
	descriptors := []*RepositoryDescriptorIdentity{
		&value.SourceRootDescriptor, &value.CommonGitRootDescriptor, &value.CandidateRootDescriptor,
		&value.CandidateGitfileDescriptor, &value.CandidateAdminDescriptor,
	}
	for _, descriptor := range descriptors {
		descriptor.DescriptorHash = mustRecordHash(t, *descriptor, "descriptor_hash")
	}
	for index := range value.CommonGitDelta.Members {
		value.CommonGitDelta.Members[index].MemberHash = mustRecordHash(t, value.CommonGitDelta.Members[index], "member_hash")
	}
	for index := range value.CommonGitDelta.ProtectedDomains {
		value.CommonGitDelta.ProtectedDomains[index].DomainHash = mustRecordHash(t, value.CommonGitDelta.ProtectedDomains[index], "domain_hash")
	}
	value.CommonGitDelta.DeltaHash = mustRecordHash(t, value.CommonGitDelta, "delta_hash")
	value.Candidate.CandidateHash = mustRecordHash(t, value.Candidate, "candidate_hash")
	value.Source.SourceClosureHash = mustRecordHash(t, value.Source, "source_closure_hash")
	for index := range value.WritablePaths.Paths {
		value.WritablePaths.Paths[index].PathObservationHash = mustRecordHash(t, value.WritablePaths.Paths[index], "path_observation_hash")
	}
	value.WritablePaths.PathClosureHash = mustRecordHash(t, value.WritablePaths, "path_closure_hash")
	value.Config.ConfigClosureHash = mustRecordHash(t, value.Config, "config_closure_hash")
	value.Attributes.AttributesClosureHash = mustRecordHash(t, value.Attributes, "attributes_closure_hash")
	value.ObservationHash = mustRecordHash(t, *value, "observation_hash")
}

func mintRepositoryWorktreeSnapshotForTest(t *testing.T, observation RepositoryWorktreeMaterializationObservation, tupleUnique, slotUnique, ownershipCertain, unambiguous bool) *VerifiedRepositoryWorktreeSnapshot {
	t.Helper()
	raw := canonicalTestArtifact(t, observation)
	clone, err := decodeCanonicalRecord[RepositoryWorktreeMaterializationObservation](raw)
	if err != nil {
		t.Fatal(err)
	}
	verifier := FrozenRepositoryWorktreeVerifierAuthority()
	value := &VerifiedRepositoryWorktreeSnapshot{
		valid: true, descriptorVerified: true, deltaVerified: true, configVerified: true, attributesVerified: true,
		pathVerified: true, uniquenessVerified: true, ambiguityChecked: true, tupleUnique: tupleUnique, slotUnique: slotUnique,
		ownershipCertain: ownershipCertain, unambiguous: unambiguous, observation: clone,
		canonical: append([]byte(nil), raw...), canonicalHash: sha256Digest(raw),
		verifierAuthorityHash: verifier.VerifierAuthorityHash,
	}
	seals := deriveRepositoryWorktreeVerificationSeals(verifier, value.observation, value.canonicalHash,
		tupleUnique, slotUnique, ownershipCertain, unambiguous)
	value.descriptorVerificationSeal = seals.descriptor
	value.deltaVerificationSeal = seals.delta
	value.configVerificationSeal = seals.config
	value.attributesVerificationSeal = seals.attributes
	value.pathVerificationSeal = seals.path
	value.uniquenessVerificationSeal = seals.uniqueness
	value.ambiguityVerificationSeal = seals.ambiguity
	if !validHash(value.ambiguityVerificationSeal) {
		t.Fatal("production seal derivation failed for test snapshot")
	}
	value.integrityHash = repositoryWorktreeSnapshotIntegrityHash(value)
	return value
}

func cloneRepositoryWorktreeSnapshotForTest(t *testing.T, value *VerifiedRepositoryWorktreeSnapshot) *VerifiedRepositoryWorktreeSnapshot {
	t.Helper()
	clone := *value
	clone.canonical = append([]byte(nil), value.canonical...)
	raw := canonicalTestArtifact(t, value.observation)
	decoded, err := decodeCanonicalRecord[RepositoryWorktreeMaterializationObservation](raw)
	if err != nil {
		t.Fatal(err)
	}
	clone.observation = decoded
	return &clone
}

func markRepositoryWorktreeAmbiguousForTest(t *testing.T, fixture *canonicalRepositoryWorktreeFixture, reason RepositoryWorktreeAmbiguityReason) {
	t.Helper()
	fixture.observation.State = RepositoryWorktreeRetainForHuman
	fixture.observation.AmbiguityReason = reason
	fixture.observation.CommonGitDelta.State = CommonGitDeltaAmbiguous
	sealRepositoryWorktreeObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	tupleUnique := fixture.snapshot == nil || fixture.snapshot.tupleUnique
	slotUnique := fixture.snapshot == nil || fixture.snapshot.slotUnique
	ownershipCertain := fixture.snapshot == nil || fixture.snapshot.ownershipCertain
	fixture.snapshot = mintRepositoryWorktreeSnapshotForTest(t, fixture.observation, tupleUnique, slotUnique, ownershipCertain, false)
}

func verifiedRepositoryWorktreeInstallationForTest(t *testing.T, fixture canonicalRepositoryWorktreeFixture) *VerifiedRepositoryWorktreeInstallation {
	t.Helper()
	installation, err := VerifyRepositoryWorktreeInstallation(fixture.installation, fixture.authority, fixture.attempt.authorization, fixture.now)
	if err != nil {
		t.Fatalf("verify canonical installation: %v", err)
	}
	return installation
}

func evaluateCanonicalRepositoryWorktree(t *testing.T, fixture canonicalRepositoryWorktreeFixture, action RepositoryWorktreeAction) (RepositoryWorktreeAssessment, *VerifiedRepositoryWorktree, error) {
	t.Helper()
	installation, err := VerifyRepositoryWorktreeInstallation(fixture.installation, fixture.authority, fixture.attempt.authorization, fixture.now)
	if err != nil {
		return RepositoryWorktreeAssessment{}, nil, err
	}
	return EvaluateRepositoryWorktree(
		fixture.authority, installation, fixture.attempt.authorization, fixture.attempt.committedClaim[0],
		fixture.snapshot, action, fixture.now,
	)
}

func mutateRepositoryWorktreeForWaitingProbe(reason RepositoryWorktreeAmbiguityReason, mutate func(*canonicalRepositoryWorktreeFixture)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		mutate(&fixture)
		markRepositoryWorktreeAmbiguousForTest(t, &fixture, reason)
		assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeStatusOnly)
		if err != nil {
			return err
		}
		if capability != nil || assessment.EffectAllowed || assessment.Disposition != RepositoryWorktreeWaitingForHuman || assessment.NextRequirement != RepositoryWorktreeHumanReview {
			return errors.New("ambiguous observation did not remain status-only")
		}
		return nil
	}
}

func memberMutationProbe(kind string, index int) func(*testing.T) error {
	return mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreePartialDelta, func(f *canonicalRepositoryWorktreeFixture) {
		members := f.observation.CommonGitDelta.Members
		switch kind {
		case "omit":
			f.observation.CommonGitDelta.Members = append(append([]CommonGitMemberObservation(nil), members[:index]...), members[index+1:]...)
		case "duplicate":
			f.observation.CommonGitDelta.Members = append(members, members[index])
		case "reorder":
			other := (index + 1) % len(members)
			members[index], members[other] = members[other], members[index]
		case "rename":
			members[index].MemberID = CommonGitMemberID("renamed_" + string(members[index].MemberID))
		case "semantic":
			other := (index + 1) % len(members)
			members[index].Semantic = members[other].Semantic
			members[index].SemanticTargetHash = members[other].SemanticTargetHash
		default:
			panic("unknown member mutation " + kind)
		}
	})
}

func configMutationProbe(mutate func(*RepositoryGitConfigClosure)) func(*testing.T) error {
	return mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeConfigDrift, func(f *canonicalRepositoryWorktreeFixture) { mutate(&f.observation.Config) })
}

func attributesMutationProbe(mutate func(*RepositoryGitAttributesClosure)) func(*testing.T) error {
	return mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeAttributesDrift, func(f *canonicalRepositoryWorktreeFixture) { mutate(&f.observation.Attributes) })
}

func pathMutationProbe(mutate func(*RepositoryWritablePathClosure)) func(*testing.T) error {
	return mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreePathAmbiguity, func(f *canonicalRepositoryWorktreeFixture) { mutate(&f.observation.WritablePaths) })
}

func protectedDomainMutationProbe(index int) func(*testing.T) error {
	return mutateRepositoryWorktreeForWaitingProbe(RepositoryWorktreeProtectedCommonGitDrift, func(f *canonicalRepositoryWorktreeFixture) {
		domain := &f.observation.CommonGitDelta.ProtectedDomains[index]
		domain.AfterHash = testHash("changed-protected-domain-" + string(domain.DomainID))
		domain.Unchanged = false
	})
}

func canonicalObservationDecodeMutationProbe(mutate func(map[string]any) []byte) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		var value map[string]any
		if err := json.Unmarshal(fixture.canonical, &value); err != nil {
			return err
		}
		_, err := DecodeRepositoryWorktreeObservation(mutate(value))
		return err
	}
}

func repositoryWorktreeProtectedDomainIDs() []CommonGitProtectedDomainID {
	return []CommonGitProtectedDomainID{
		CommonGitProtectedRefs,
		CommonGitProtectedLogsOutsideCandidate,
		CommonGitProtectedConfig,
		CommonGitProtectedObjects,
		CommonGitProtectedHooks,
		CommonGitProtectedInfoExclude,
		CommonGitProtectedInfoAttributes,
		CommonGitProtectedAlternates,
		CommonGitProtectedShallow,
		CommonGitProtectedGrafts,
		CommonGitProtectedReplace,
		CommonGitProtectedPackedRefs,
		CommonGitProtectedCommonIndex,
		CommonGitProtectedWorktreeSiblings,
	}
}

func assertNoRepositoryWorktreeAuthority(t *testing.T, assessment RepositoryWorktreeAssessment, capability *VerifiedRepositoryWorktree, err error) {
	t.Helper()
	if !errors.Is(err, ErrInvalidRepositoryWorktree) || capability != nil || assessment.EffectAllowed {
		t.Fatalf("assessment=%+v capability=%v err=%v", assessment, capability, err)
	}
}

func replaceFirstJSONMember(raw []byte, member string) []byte {
	needle := []byte(`"` + member + `":`)
	index := bytes.Index(raw, needle)
	if index < 0 {
		return raw
	}
	end := index + len(needle)
	return append(append(append([]byte(nil), raw[:end]...), []byte(`null,"`+member+`":`)...), raw[end:]...)
}

func noncanonicalRepositoryObservation(value map[string]any) []byte {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return raw
}

func trailingRepositoryObservation(value map[string]any) []byte {
	raw, _ := canonicalBytes(value)
	return append(raw, []byte(`{}`)...)
}

func unknownRepositoryObservation(value map[string]any) []byte {
	value["unknown"] = true
	raw, _ := canonicalBytes(value)
	return raw
}

func duplicateRepositoryObservation(value map[string]any) []byte {
	raw, _ := canonicalBytes(value)
	return replaceFirstJSONMember(raw, "observation_id")
}

func repositoryObservationHashMismatch(value map[string]any) []byte {
	value["observation_hash"] = testHash("mismatched-observation-hash")
	raw, _ := canonicalBytes(value)
	return raw
}

func stringHasRawPathField(value string) bool {
	return strings.Contains(value, `"path":`) || strings.Contains(value, `"argv":`) || strings.Contains(value, `"env":`)
}
