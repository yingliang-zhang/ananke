package repaircontract

import (
	"fmt"
	"testing"
	"time"
)

const (
	repairCOrphanState  RepositoryWorktreeState           = "orphaned_status_only"
	repairCOrphanReason RepositoryWorktreeAmbiguityReason = "orphaned_materialization"
)

type repositoryWorktreeRepairCEnumMutation struct {
	id     string
	mutate func(*RepositoryWorktreeMaterializationObservation)
}

type repositoryWorktreeRepairCState struct {
	id    string
	value RepositoryWorktreeState
}

type repositoryWorktreeRepairCReason struct {
	id             string
	value          RepositoryWorktreeAmbiguityReason
	retainForHuman bool
}

type repositoryWorktreeRepairCAction struct {
	id    string
	value RepositoryWorktreeAction
}

var repositoryWorktreeRepairCEnumMutations = []repositoryWorktreeRepairCEnumMutation{
	{id: "unknown_ambiguity_reason", mutate: func(value *RepositoryWorktreeMaterializationObservation) {
		value.AmbiguityReason = "unknown_ambiguity_reason"
	}},
	{id: "unknown_descriptor_id", mutate: func(value *RepositoryWorktreeMaterializationObservation) {
		value.SourceRootDescriptor.DescriptorID = "unknown_descriptor"
	}},
	{id: "unknown_member_id", mutate: func(value *RepositoryWorktreeMaterializationObservation) {
		value.CommonGitDelta.Members[0].MemberID = "unknown_member"
	}},
	{id: "unknown_member_semantic", mutate: func(value *RepositoryWorktreeMaterializationObservation) {
		value.CommonGitDelta.Members[0].Semantic = "unknown_semantic"
	}},
	{id: "unknown_added_member_id", mutate: func(value *RepositoryWorktreeMaterializationObservation) {
		value.CommonGitDelta.AddedMemberIDs[0] = "unknown_added_member"
	}},
	{id: "unknown_protected_domain_id", mutate: func(value *RepositoryWorktreeMaterializationObservation) {
		value.CommonGitDelta.ProtectedDomains[0].DomainID = "unknown_domain"
	}},
}

var repositoryWorktreeRepairCStates = []repositoryWorktreeRepairCState{
	{id: "new_exact", value: RepositoryWorktreeNewExact},
	{id: "retained_exact", value: RepositoryWorktreeRetainedExact},
	{id: "retain_for_human", value: RepositoryWorktreeRetainForHuman},
	{id: "orphaned_status_only", value: repairCOrphanState},
}

var repositoryWorktreeRepairCReasons = []repositoryWorktreeRepairCReason{
	{id: "empty"},
	{id: "conflicting_retained_state", value: RepositoryWorktreeConflictingRetained, retainForHuman: true},
	{id: "partial_common_git_delta", value: RepositoryWorktreePartialDelta, retainForHuman: true},
	{id: "uncertain_ownership", value: RepositoryWorktreeUncertainOwnership, retainForHuman: true},
	{id: "conflicting_worktree_slot", value: RepositoryWorktreeConflictingSlot, retainForHuman: true},
	{id: "git_config_drift", value: RepositoryWorktreeConfigDrift, retainForHuman: true},
	{id: "git_attributes_drift", value: RepositoryWorktreeAttributesDrift, retainForHuman: true},
	{id: "authorized_path_ambiguity", value: RepositoryWorktreePathAmbiguity, retainForHuman: true},
	{id: "protected_common_git_drift", value: RepositoryWorktreeProtectedCommonGitDrift, retainForHuman: true},
	{id: "candidate_state_drift", value: RepositoryWorktreeCandidateDrift, retainForHuman: true},
	{id: "descriptor_identity_drift", value: RepositoryWorktreeDescriptorDrift, retainForHuman: true},
	{id: "orphaned_materialization", value: repairCOrphanReason},
}

var repositoryWorktreeRepairCActions = []repositoryWorktreeRepairCAction{
	{id: "admit_new_materialization", value: RepositoryWorktreeAdmitNew},
	{id: "cleanup", value: "cleanup"},
	{id: "delete", value: "delete"},
	{id: "prune", value: "prune"},
	{id: "remove", value: "remove"},
	{id: "ref_update", value: "ref_update"},
	{id: "commit", value: "commit"},
	{id: "push", value: "push"},
	{id: "merge", value: "merge"},
	{id: "launch", value: "launch"},
	{id: "second_worktree", value: "second_worktree"},
	{id: "second_effect", value: "second_effect"},
}

var repositoryWorktreeRepairCVectorRegistry = buildRepositoryWorktreeRepairCVectorRegistry()

var repositoryWorktreeRepairCVectorIDs = func() []string {
	ids := make([]string, len(repositoryWorktreeRepairCVectorRegistry))
	for index, vector := range repositoryWorktreeRepairCVectorRegistry {
		ids[index] = vector.id
	}
	return ids
}()

func buildRepositoryWorktreeRepairCVectorRegistry() []repositoryWorktreeExecutableVector {
	const vectorCount = 76
	vectors := make([]repositoryWorktreeExecutableVector, 0, vectorCount)
	for _, mutation := range repositoryWorktreeRepairCEnumMutations {
		vectors = append(vectors,
			repositoryWorktreeExecutableVector{
				id:      mutation.id + "_decode",
				wantErr: ErrInvalidRepositoryWorktree,
				run:     repositoryWorktreeRepairCUnknownEnumDecodeProbe(mutation.mutate),
			},
			repositoryWorktreeExecutableVector{
				id:      mutation.id + "_evaluation",
				wantErr: ErrInvalidRepositoryWorktree,
				run:     repositoryWorktreeRepairCUnknownEnumEvaluationProbe(mutation.mutate),
			},
		)
	}
	for _, state := range repositoryWorktreeRepairCStates {
		for _, reason := range repositoryWorktreeRepairCReasons {
			valid := repositoryWorktreeRepairCValidStateReason(state.value, reason)
			vector := repositoryWorktreeExecutableVector{
				id:  "state_reason_" + state.id + "_" + reason.id,
				run: repositoryWorktreeRepairCStateReasonProbe(state.value, reason.value, valid),
			}
			if !valid {
				vector.wantErr = ErrInvalidRepositoryWorktree
			}
			vectors = append(vectors, vector)
		}
	}
	vectors = append(vectors, repositoryWorktreeExecutableVector{
		id:  "orphan_status_only_waiting_for_human",
		run: repositoryWorktreeRepairCOrphanStatusProbe,
	})
	for _, action := range repositoryWorktreeRepairCActions {
		vectors = append(vectors, repositoryWorktreeExecutableVector{
			id:      "orphan_reject_" + action.id,
			wantErr: ErrInvalidRepositoryWorktree,
			run:     repositoryWorktreeRepairCOrphanActionProbe(action.value),
		})
	}
	vectors = append(vectors,
		repositoryWorktreeExecutableVector{id: "orphan_fresh_n_minus_1ns", run: repositoryWorktreeRepairCFreshnessProbe(-time.Nanosecond, true)},
		repositoryWorktreeExecutableVector{id: "orphan_stale_n", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeRepairCFreshnessProbe(0, false)},
		repositoryWorktreeExecutableVector{id: "orphan_stale_n_plus_1ns", wantErr: ErrInvalidRepositoryWorktree, run: repositoryWorktreeRepairCFreshnessProbe(time.Nanosecond, false)},
	)
	if len(vectors) != vectorCount {
		panic(fmt.Sprintf("Repair C vector count=%d want=%d", len(vectors), vectorCount))
	}
	return vectors
}

func repositoryWorktreeRepairCUnknownEnumDecodeProbe(mutate func(*RepositoryWorktreeMaterializationObservation)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := repositoryWorktreeRepairCStatusFixtureForTest(t, mutate)
		_, err := DecodeRepositoryWorktreeObservation(fixture.canonical)
		return err
	}
}

func repositoryWorktreeRepairCUnknownEnumEvaluationProbe(mutate func(*RepositoryWorktreeMaterializationObservation)) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := repositoryWorktreeRepairCStatusFixtureForTest(t, mutate)
		assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeStatusOnly)
		if capability != nil || assessment.EffectAllowed {
			return fmt.Errorf("unknown enum minted authority: assessment=%+v capability=%v err=%v", assessment, capability, err)
		}
		return err
	}
}

func repositoryWorktreeRepairCStatusFixtureForTest(t *testing.T, mutate func(*RepositoryWorktreeMaterializationObservation)) canonicalRepositoryWorktreeFixture {
	t.Helper()
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	fixture.observation.State = RepositoryWorktreeRetainForHuman
	fixture.observation.AmbiguityReason = RepositoryWorktreeDescriptorDrift
	fixture.observation.CommonGitDelta.State = CommonGitDeltaAmbiguous
	mutate(&fixture.observation)
	sealRepositoryWorktreeObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	fixture.snapshot = repositoryWorktreeRepairCUncheckedSnapshotForTest(t, fixture.snapshot, fixture.observation, false)
	return fixture
}

func repositoryWorktreeRepairCValidStateReason(state RepositoryWorktreeState, reason repositoryWorktreeRepairCReason) bool {
	switch state {
	case RepositoryWorktreeNewExact, RepositoryWorktreeRetainedExact:
		return reason.value == ""
	case RepositoryWorktreeRetainForHuman:
		return reason.retainForHuman
	case repairCOrphanState:
		return reason.value == repairCOrphanReason
	default:
		return false
	}
}

func repositoryWorktreeRepairCStateReasonProbe(state RepositoryWorktreeState, reason RepositoryWorktreeAmbiguityReason, valid bool) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := canonicalRepositoryWorktreeFixtureForTest(t)
		action := RepositoryWorktreeAdmitNew
		unambiguous := true
		switch state {
		case RepositoryWorktreeRetainedExact:
			fixture = retainedRepositoryWorktreeFixtureForTest(t)
			action = RepositoryWorktreeStatusOnly
		case RepositoryWorktreeRetainForHuman, repairCOrphanState:
			fixture.observation.CommonGitDelta.State = CommonGitDeltaAmbiguous
			action = RepositoryWorktreeStatusOnly
			unambiguous = false
		}
		fixture.observation.State = state
		fixture.observation.AmbiguityReason = reason
		sealRepositoryWorktreeObservationForTest(t, &fixture.observation)
		fixture.canonical = canonicalTestArtifact(t, fixture.observation)
		decoded, err := DecodeRepositoryWorktreeObservation(fixture.canonical)
		if err != nil || !valid {
			return err
		}
		fixture.observation = decoded
		fixture.snapshot = repositoryWorktreeRepairCUncheckedSnapshotForTest(t, fixture.snapshot, decoded, unambiguous)
		assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, action)
		if err != nil {
			return err
		}
		if assessment.EffectAllowed {
			return fmt.Errorf("state %q reason %q allowed an effect", state, reason)
		}
		switch state {
		case RepositoryWorktreeNewExact:
			if capability == nil || assessment.Disposition != RepositoryWorktreeCapabilityReady {
				return fmt.Errorf("exact new combination omitted capability: assessment=%+v capability=%v", assessment, capability)
			}
		case RepositoryWorktreeRetainedExact:
			if capability != nil || assessment.Disposition != RepositoryWorktreeRetainedStatus {
				return fmt.Errorf("retained combination granted authority: assessment=%+v capability=%v", assessment, capability)
			}
		default:
			if capability != nil || assessment.Disposition != RepositoryWorktreeWaitingForHuman || assessment.NextRequirement != RepositoryWorktreeHumanReview {
				return fmt.Errorf("status-only combination misclassified: assessment=%+v capability=%v", assessment, capability)
			}
		}
		return nil
	}
}

func repositoryWorktreeRepairCOrphanFixtureForTest(t *testing.T) canonicalRepositoryWorktreeFixture {
	t.Helper()
	fixture := canonicalRepositoryWorktreeFixtureForTest(t)
	fixture.observation.State = repairCOrphanState
	fixture.observation.AmbiguityReason = repairCOrphanReason
	fixture.observation.CommonGitDelta.State = CommonGitDeltaAmbiguous
	sealRepositoryWorktreeObservationForTest(t, &fixture.observation)
	fixture.canonical = canonicalTestArtifact(t, fixture.observation)
	fixture.snapshot = repositoryWorktreeRepairCUncheckedSnapshotForTest(t, fixture.snapshot, fixture.observation, false)
	return fixture
}

func repositoryWorktreeRepairCOrphanStatusProbe(t *testing.T) error {
	fixture := repositoryWorktreeRepairCOrphanFixtureForTest(t)
	assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, RepositoryWorktreeStatusOnly)
	if err != nil {
		return err
	}
	if capability != nil || assessment.EffectAllowed || assessment.Disposition != RepositoryWorktreeWaitingForHuman || assessment.NextRequirement != RepositoryWorktreeHumanReview {
		return fmt.Errorf("orphan status granted authority: assessment=%+v capability=%v", assessment, capability)
	}
	return nil
}

func repositoryWorktreeRepairCOrphanActionProbe(action RepositoryWorktreeAction) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := repositoryWorktreeRepairCOrphanFixtureForTest(t)
		assessment, capability, err := evaluateCanonicalRepositoryWorktree(t, fixture, action)
		if capability != nil || assessment.EffectAllowed {
			return fmt.Errorf("orphan action %q granted authority: assessment=%+v capability=%v err=%v", action, assessment, capability, err)
		}
		return err
	}
}

func repositoryWorktreeRepairCFreshnessProbe(offset time.Duration, valid bool) func(*testing.T) error {
	return func(t *testing.T) error {
		fixture := repositoryWorktreeRepairCOrphanFixtureForTest(t)
		installation := verifiedRepositoryWorktreeInstallationForTest(t, fixture)
		now := mustTime(t, fixture.authority.NotAfter).Add(offset)
		assessment, capability, err := EvaluateRepositoryWorktree(
			fixture.authority,
			installation,
			fixture.attempt.authorization,
			fixture.attempt.committedClaim[0],
			fixture.snapshot,
			RepositoryWorktreeStatusOnly,
			now,
		)
		if capability != nil || assessment.EffectAllowed {
			return fmt.Errorf("orphan freshness offset %s granted authority: assessment=%+v capability=%v err=%v", offset, assessment, capability, err)
		}
		if valid && err == nil && (assessment.Disposition != RepositoryWorktreeWaitingForHuman || assessment.NextRequirement != RepositoryWorktreeHumanReview) {
			return fmt.Errorf("fresh orphan misclassified: assessment=%+v", assessment)
		}
		return err
	}
}

func repositoryWorktreeRepairCUncheckedSnapshotForTest(t *testing.T, base *VerifiedRepositoryWorktreeSnapshot, observation RepositoryWorktreeMaterializationObservation, unambiguous bool) *VerifiedRepositoryWorktreeSnapshot {
	t.Helper()
	value := *base
	value.observation = observation
	value.canonical = canonicalTestArtifact(t, observation)
	value.canonicalHash = sha256Digest(value.canonical)
	value.unambiguous = unambiguous
	verifier := FrozenRepositoryWorktreeVerifierAuthority()
	value.verifierAuthorityHash = verifier.VerifierAuthorityHash
	seals := deriveRepositoryWorktreeVerificationSeals(verifier, value.observation, value.canonicalHash,
		value.tupleUnique, value.slotUnique, value.ownershipCertain, value.unambiguous)
	value.descriptorVerificationSeal = seals.descriptor
	value.deltaVerificationSeal = seals.delta
	value.configVerificationSeal = seals.config
	value.attributesVerificationSeal = seals.attributes
	value.pathVerificationSeal = seals.path
	value.uniquenessVerificationSeal = seals.uniqueness
	value.ambiguityVerificationSeal = seals.ambiguity
	if !validHash(value.ambiguityVerificationSeal) {
		t.Fatal("production seal derivation failed for Repair C snapshot")
	}
	value.integrityHash = repositoryWorktreeSnapshotIntegrityHash(&value)
	return &value
}
