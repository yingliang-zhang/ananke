package repaircontract

import (
	"errors"
	"reflect"
	"strconv"
	"time"
)

const (
	AdapterSandboxObservationSchemaVersion       = "ananke.controlled-repair-adapter-sandbox-observation.v1"
	AdapterUIDPoolSchemaVersion                  = "ananke.controlled-repair-adapter-uid-pool.v1"
	AdapterUIDPoolEntrySchemaVersion             = "ananke.controlled-repair-adapter-uid-pool-entry.v1"
	AdapterUIDLeaseSchemaVersion                 = "ananke.controlled-repair-adapter-uid-lease.v1"
	AdapterSeatbeltProfileSchemaVersion          = "ananke.controlled-repair-adapter-seatbelt-profile.v1"
	AdapterTerminalProofSchemaVersion            = "ananke.controlled-repair-adapter-terminal-proof.v1"
	AdapterSandboxVerifierAuthoritySchemaVersion = "ananke.controlled-repair-adapter-sandbox-verifier-authority.v1"
	AdapterSandboxVerificationSealSchemaVersion  = "ananke.controlled-repair-adapter-sandbox-verification-seal.v1"

	adapterUIDPoolID                 = "controlled_repair_runtime_uid_pool_v1"
	adapterSeatbeltProfileID         = "controlled_repair_adapter_seatbelt_v1"
	adapterSandboxVerifierID         = "controlled_repair_adapter_sandbox_verifier_v1"
	adapterUIDLeaseSlotPrefix        = "attempt_"
	adapterUIDLeaseSlotSuffix        = "_adapter_uid_lease_001"
	adapterUIDPoolGroupID     uint32 = 62000
	adapterUIDPoolGroupName          = "_ananke_repair"
	adapterUIDPoolSize               = 4
	adapterUIDPoolBaseUID     uint32 = 62001

	AdapterSandboxAdmitTerminal AdapterSandboxAction = "admit_terminal_proof"
	AdapterSandboxStatusOnly    AdapterSandboxAction = "status_only"

	AdapterSandboxCapabilityReady AdapterSandboxDisposition = "capability_ready"
	AdapterSandboxRetainedStatus  AdapterSandboxDisposition = "retained_status"
	AdapterSandboxWaitingForHuman AdapterSandboxDisposition = "waiting_for_human"

	AdapterSandboxNextTestPhase       AdapterSandboxRequirement = "next_test_phase"
	AdapterSandboxNoFurtherEffect     AdapterSandboxRequirement = "no_further_effect_permitted"
	AdapterSandboxHumanReviewRequired AdapterSandboxRequirement = "human_review_required"

	AdapterSandboxTerminalProven AdapterSandboxState = "terminal_proven"
	AdapterSandboxRetainedReplay AdapterSandboxState = "retained_terminal_replay"
	AdapterSandboxRetainForHuman AdapterSandboxState = "retain_for_human"

	AdapterCleanupUIDEmptyRootsFrozen AdapterTerminalCleanupResult = "uid_empty_roots_frozen"
	AdapterCleanupUIDNonemptyRetained AdapterTerminalCleanupResult = "uid_nonempty_retained"
	AdapterCleanupPartialRetained     AdapterTerminalCleanupResult = "partial_retained"

	AdapterAmbiguityUIDNotEmpty     AdapterSandboxAmbiguityReason = "uid_not_empty"
	AdapterAmbiguityDescriptorsOpen AdapterSandboxAmbiguityReason = "descriptors_not_closed"
	AdapterAmbiguityRootsNotFrozen  AdapterSandboxAmbiguityReason = "roots_not_frozen"
	AdapterAmbiguityStalePID        AdapterSandboxAmbiguityReason = "stale_pid_epoch"
	AdapterAmbiguityUIDReuse        AdapterSandboxAmbiguityReason = "uid_reuse_contention"
	AdapterAmbiguityChildAlive      AdapterSandboxAmbiguityReason = "child_still_alive"
	AdapterAmbiguityBrokerEscape    AdapterSandboxAmbiguityReason = "broker_network_escape"
	AdapterAmbiguityIgnoredContext  AdapterSandboxAmbiguityReason = "ignored_context"
	AdapterAmbiguityDoubleFork      AdapterSandboxAmbiguityReason = "double_fork_escape"
	AdapterAmbiguitySetsidEscape    AdapterSandboxAmbiguityReason = "setsid_new_session"
	AdapterAmbiguityClosedStdio     AdapterSandboxAmbiguityReason = "closed_stdio_evade"
	AdapterAmbiguityDelayedMutation AdapterSandboxAmbiguityReason = "delayed_write_ref_update"

	AdapterUIDLeaseVerification          AdapterSandboxVerificationKind = "uid_lease_verification"
	AdapterSeatbeltProfileVerification   AdapterSandboxVerificationKind = "seatbelt_profile_verification"
	AdapterTerminalProofVerification     AdapterSandboxVerificationKind = "terminal_proof_verification"
	AdapterSandboxBoundaryVerification   AdapterSandboxVerificationKind = "sandbox_boundary_verification"
	AdapterDescriptorClosureVerification AdapterSandboxVerificationKind = "descriptor_closure_verification"
	AdapterRootIdentityVerification      AdapterSandboxVerificationKind = "root_identity_verification"
	AdapterUIDEmptyVerification          AdapterSandboxVerificationKind = "uid_empty_verification"

	maxAdapterSandboxIDBytes = 192
)

var ErrInvalidAdapterSandbox = errors.New("controlled repair adapter sandbox observation is invalid")

type AdapterSandboxAction string
type AdapterSandboxDisposition string
type AdapterSandboxRequirement string
type AdapterSandboxState string
type AdapterSandboxAmbiguityReason string
type AdapterTerminalCleanupResult string
type AdapterSandboxVerificationKind string

// AdapterUIDPoolEntry is one release-provisioned runtime UID.
type AdapterUIDPoolEntry struct {
	SchemaVersion string `json:"schema_version"`
	EntryHash     string `json:"entry_hash"`
	Sequence      int    `json:"sequence"`
	UID           uint32 `json:"uid"`
	UserName      string `json:"user_name"`
	GroupID       uint32 `json:"group_id"`
}

// AdapterUIDPool is the frozen release-provisioned UID pool. It is compiled
// from the provisioning facts and binds the exact group, pool size, and UIDs.
type AdapterUIDPool struct {
	SchemaVersion string                `json:"schema_version"`
	PoolHash      string                `json:"pool_hash"`
	PoolID        string                `json:"pool_id"`
	GroupID       uint32                `json:"group_id"`
	GroupName     string                `json:"group_name"`
	PoolSize      int                   `json:"pool_size"`
	Entries       []AdapterUIDPoolEntry `json:"entries"`
}

// AdapterUIDLease is an exclusive lease from the closed UID pool. It is
// journaled before spawn and binds the attempt; no concurrent attempt may
// share the same UID.
type AdapterUIDLease struct {
	SchemaVersion string `json:"schema_version"`
	LeaseHash     string `json:"lease_hash"`
	LeaseID       string `json:"lease_id"`
	AttemptHash   string `json:"attempt_hash"`
	AttemptNumber int    `json:"attempt_number"`
	UID           uint32 `json:"uid"`
	GroupID       uint32 `json:"group_id"`
	PoolID        string `json:"pool_id"`
	AcquiredAt    string `json:"acquired_at"`
	Exclusive     bool   `json:"exclusive"`
}

// AdapterSeatbeltProfile is the compiled Darwin Seatbelt profile. Its closed
// boolean semantics freeze the exact capabilities the adapter sandbox has.
type AdapterSeatbeltProfile struct {
	SchemaVersion                     string `json:"schema_version"`
	ProfileHash                       string `json:"profile_hash"`
	ProfileID                         string `json:"profile_id"`
	AdapterChildProcessOnly           bool   `json:"adapter_child_process_only"`
	NoInProcessInterface              bool   `json:"no_in_process_interface"`
	WriteDisposableRootsOnly          bool   `json:"write_disposable_roots_only"`
	ReadPinnedRootsOnly               bool   `json:"read_pinned_roots_only"`
	NetworkBrokerOnly                 bool   `json:"network_broker_only"`
	ExecPinnedAdapterOnly             bool   `json:"exec_pinned_adapter_only"`
	NoCredentialsInArgv               bool   `json:"no_credentials_in_argv"`
	NoCredentialsInPolicyOrEvidence   bool   `json:"no_credentials_in_policy_or_evidence"`
	DescriptorClosureRequired         bool   `json:"descriptor_closure_required"`
	UIDWideProcessEnumerationRequired bool   `json:"uid_wide_process_enumeration_required"`
	RootFreezeBeforeContinue          bool   `json:"root_freeze_before_continue"`
}

// AdapterTerminalProof is canonical, closed, self-hashed data. It binds the
// UID lease, leader and process-group identities, the UID-empty observation,
// sandbox hash, root identities, descriptor closure, and cleanup result.
type AdapterTerminalProof struct {
	SchemaVersion            string                       `json:"schema_version"`
	ProofHash                string                       `json:"proof_hash"`
	ProofID                  string                       `json:"proof_id"`
	UIDLeaseHash             string                       `json:"uid_lease_hash"`
	LeaderIdentityHash       string                       `json:"leader_identity_hash"`
	ProcessGroupIdentityHash string                       `json:"process_group_identity_hash"`
	UIDEmptyObservationHash  string                       `json:"uid_empty_observation_hash"`
	SandboxHash              string                       `json:"sandbox_hash"`
	RootIdentityHash         string                       `json:"root_identity_hash"`
	DescriptorClosureHash    string                       `json:"descriptor_closure_hash"`
	BootEpochHash            string                       `json:"boot_epoch_hash"`
	CleanupResult            AdapterTerminalCleanupResult `json:"cleanup_result"`
	UIDEmptyVerified         bool                         `json:"uid_empty_verified"`
	DescriptorsClosed        bool                         `json:"descriptors_closed"`
	RootsFrozen              bool                         `json:"roots_frozen"`
	ObservedAt               string                       `json:"observed_at"`
}

// AdapterSandboxObservation is canonical, closed, self-hashed data. Decoding
// it proves no process, sandbox, or terminal fact.
type AdapterSandboxObservation struct {
	SchemaVersion                     string                        `json:"schema_version"`
	ObservationHash                   string                        `json:"observation_hash"`
	ObservationID                     string                        `json:"observation_id"`
	State                             AdapterSandboxState           `json:"state"`
	AmbiguityReason                   AdapterSandboxAmbiguityReason `json:"ambiguity_reason"`
	AuthorizationHash                 string                        `json:"authorization_hash"`
	ApprovalHash                      string                        `json:"approval_hash"`
	RequestHash                       string                        `json:"request_hash"`
	DispatchHash                      string                        `json:"dispatch_hash"`
	AttemptHash                       string                        `json:"attempt_hash"`
	AttemptNumber                     int                           `json:"attempt_number"`
	AttemptCap                        int                           `json:"attempt_cap"`
	ClaimHash                         string                        `json:"claim_hash"`
	PredecessorClaimHash              string                        `json:"predecessor_claim_hash"`
	PredecessorTerminalEventHash      string                        `json:"predecessor_terminal_event_hash"`
	RepositoryBindingHash             string                        `json:"repository_binding_hash"`
	RepositoryIdentityHash            string                        `json:"repository_identity_hash"`
	WorktreeSlotID                    string                        `json:"worktree_slot_id"`
	WorktreeSlotPathHash              string                        `json:"worktree_slot_path_hash"`
	WorktreeCapabilityHash            string                        `json:"worktree_capability_hash"`
	InstalledWorktreeRootIdentityHash string                        `json:"installed_worktree_root_identity_hash"`
	UIDPoolHash                       string                        `json:"uid_pool_hash"`
	UIDLeaseHash                      string                        `json:"uid_lease_hash"`
	UID                               uint32                        `json:"uid"`
	GroupID                           uint32                        `json:"group_id"`
	SeatbeltProfileID                 string                        `json:"seatbelt_profile_id"`
	SeatbeltProfileHash               string                        `json:"seatbelt_profile_hash"`
	TerminalProof                     AdapterTerminalProof          `json:"terminal_proof"`
	RootIdentityHash                  string                        `json:"root_identity_hash"`
	SandboxHash                       string                        `json:"sandbox_hash"`
	DescriptorClosureHash             string                        `json:"descriptor_closure_hash"`
	BootEpochID                       string                        `json:"boot_epoch_id"`
	BootEpochHash                     string                        `json:"boot_epoch_hash"`
}

// AdapterSandboxVerifierAuthority is the release-pinned verifier identity.
// It binds the verifier to the compiled seatbelt profile, the frozen UID
// pool, the frozen release pins, and the ordered closed verification kinds.
type AdapterSandboxVerifierAuthority struct {
	SchemaVersion         string                           `json:"schema_version"`
	VerifierAuthorityHash string                           `json:"verifier_authority_hash"`
	VerifierID            string                           `json:"verifier_id"`
	SeatbeltProfileID     string                           `json:"seatbelt_profile_id"`
	SeatbeltProfileHash   string                           `json:"seatbelt_profile_hash"`
	UIDPoolHash           string                           `json:"uid_pool_hash"`
	ReleasePinsHash       string                           `json:"release_pins_hash"`
	VerificationKinds     []AdapterSandboxVerificationKind `json:"verification_kinds"`
}

// AdapterSandboxVerificationSeal is a self-hashed provenance record for one
// verification kind. It binds the frozen verifier authority, the observation
// and canonical hashes, and kind-specific evidence.
type AdapterSandboxVerificationSeal struct {
	SchemaVersion         string                         `json:"schema_version"`
	SealHash              string                         `json:"seal_hash"`
	SealKind              AdapterSandboxVerificationKind `json:"seal_kind"`
	VerifierAuthorityHash string                         `json:"verifier_authority_hash"`
	ObservationHash       string                         `json:"observation_hash"`
	CanonicalHash         string                         `json:"canonical_hash"`
	EvidenceHash          string                         `json:"evidence_hash"`
}

// VerifiedAdapterSandboxSnapshot is opaque evidence from a future trusted
// sandbox/terminal verifier. No production constructor or decoder from caller
// bytes exists in this slice.
type VerifiedAdapterSandboxSnapshot struct {
	valid                             bool
	uidLeaseVerified                  bool
	seatbeltProfileVerified           bool
	terminalProofVerified             bool
	sandboxBoundaryVerified           bool
	descriptorClosureVerified         bool
	rootIdentityVerified              bool
	uidEmptyVerified                  bool
	observation                       AdapterSandboxObservation
	canonical                         []byte
	canonicalHash                     string
	verifierAuthorityHash             string
	uidLeaseVerificationSeal          string
	seatbeltProfileVerificationSeal   string
	terminalProofVerificationSeal     string
	sandboxBoundaryVerificationSeal   string
	descriptorClosureVerificationSeal string
	rootIdentityVerificationSeal      string
	uidEmptyVerificationSeal          string
	integrityHash                     string
}

type adapterSandboxSnapshotIntegrity struct {
	IntegrityHash                     string `json:"integrity_hash"`
	Valid                             bool   `json:"valid"`
	UIDLeaseVerified                  bool   `json:"uid_lease_verified"`
	SeatbeltProfileVerified           bool   `json:"seatbelt_profile_verified"`
	TerminalProofVerified             bool   `json:"terminal_proof_verified"`
	SandboxBoundaryVerified           bool   `json:"sandbox_boundary_verified"`
	DescriptorClosureVerified         bool   `json:"descriptor_closure_verified"`
	RootIdentityVerified              bool   `json:"root_identity_verified"`
	UIDEmptyVerified                  bool   `json:"uid_empty_verified"`
	ObservationHash                   string `json:"observation_hash"`
	CanonicalHash                     string `json:"canonical_hash"`
	VerifierAuthorityHash             string `json:"verifier_authority_hash"`
	UIDLeaseVerificationSeal          string `json:"uid_lease_verification_seal"`
	SeatbeltProfileVerificationSeal   string `json:"seatbelt_profile_verification_seal"`
	TerminalProofVerificationSeal     string `json:"terminal_proof_verification_seal"`
	SandboxBoundaryVerificationSeal   string `json:"sandbox_boundary_verification_seal"`
	DescriptorClosureVerificationSeal string `json:"descriptor_closure_verification_seal"`
	RootIdentityVerificationSeal      string `json:"root_identity_verification_seal"`
	UIDEmptyVerificationSeal          string `json:"uid_empty_verification_seal"`
}

// AdapterSandboxAssessment is classification only. EffectAllowed is always
// false and is never accepted as cleanup, launch, commit, or process authority.
type AdapterSandboxAssessment struct {
	Disposition     AdapterSandboxDisposition
	EffectAllowed   bool
	NextRequirement AdapterSandboxRequirement
}

// VerifiedAdapterSandbox is an opaque predecessor capability for the next
// (test) phase. It grants no filesystem, process, cleanup, or launch effect.
type VerifiedAdapterSandbox struct {
	valid                  bool
	observationHash        string
	snapshotIntegrityHash  string
	verifierAuthorityHash  string
	verificationSealsHash  string
	authorizationHash      string
	claimHash              string
	attemptHash            string
	predecessorClaimHash   string
	worktreeCapabilityHash string
	uidLeaseHash           string
	terminalProofHash      string
	canonical              []byte
	canonicalHash          string
	integrityHash          string
}

type verifiedAdapterSandboxIntegrity struct {
	IntegrityHash          string `json:"integrity_hash"`
	Valid                  bool   `json:"valid"`
	ObservationHash        string `json:"observation_hash"`
	SnapshotIntegrityHash  string `json:"snapshot_integrity_hash"`
	VerifierAuthorityHash  string `json:"verifier_authority_hash"`
	VerificationSealsHash  string `json:"verification_seals_hash"`
	AuthorizationHash      string `json:"authorization_hash"`
	ClaimHash              string `json:"claim_hash"`
	AttemptHash            string `json:"attempt_hash"`
	PredecessorClaimHash   string `json:"predecessor_claim_hash"`
	WorktreeCapabilityHash string `json:"worktree_capability_hash"`
	UIDLeaseHash           string `json:"uid_lease_hash"`
	TerminalProofHash      string `json:"terminal_proof_hash"`
	CanonicalHash          string `json:"canonical_hash"`
}

// --- Frozen compiled values ---

var compiledAdapterUIDPool = mustDeriveAdapterUIDPool()
var compiledAdapterSeatbeltProfile = mustDeriveAdapterSeatbeltProfile()
var compiledAdapterSandboxVerifierAuthority = mustDeriveAdapterSandboxVerifierAuthority()

// FrozenAdapterUIDPool returns the only accepted release-provisioned UID pool.
func FrozenAdapterUIDPool() AdapterUIDPool {
	return compiledAdapterUIDPool
}

// FrozenAdapterSeatbeltProfile returns the only accepted seatbelt profile.
func FrozenAdapterSeatbeltProfile() AdapterSeatbeltProfile {
	return compiledAdapterSeatbeltProfile
}

// FrozenAdapterSandboxVerifierAuthority returns the release-pinned verifier
// authority derived only from compiled material and the frozen release pins.
func FrozenAdapterSandboxVerifierAuthority() AdapterSandboxVerifierAuthority {
	return compiledAdapterSandboxVerifierAuthority
}

func mustDeriveAdapterUIDPool() AdapterUIDPool {
	pool, err := deriveAdapterUIDPool()
	if err != nil || !recordHashMatches(pool, "pool_hash", pool.PoolHash) {
		panic("invalid compiled adapter UID pool")
	}
	return pool
}

func deriveAdapterUIDPool() (AdapterUIDPool, error) {
	entries := make([]AdapterUIDPoolEntry, 0, adapterUIDPoolSize)
	for i := range adapterUIDPoolSize {
		entry := AdapterUIDPoolEntry{
			SchemaVersion: AdapterUIDPoolEntrySchemaVersion,
			Sequence:      i + 1,
			UID:           adapterUIDPoolBaseUID + uint32(i),
			UserName:      adapterUIDPoolGroupName + "_" + strconv.Itoa(i+1),
			GroupID:       adapterUIDPoolGroupID,
		}
		entry.EntryHash = mustHashRecord(entry, "entry_hash")
		entries = append(entries, entry)
	}
	pool := AdapterUIDPool{
		SchemaVersion: AdapterUIDPoolSchemaVersion,
		PoolID:        adapterUIDPoolID,
		GroupID:       adapterUIDPoolGroupID,
		GroupName:     adapterUIDPoolGroupName,
		PoolSize:      adapterUIDPoolSize,
		Entries:       entries,
	}
	pool.PoolHash = mustHashRecord(pool, "pool_hash")
	if !recordHashMatches(pool, "pool_hash", pool.PoolHash) {
		return AdapterUIDPool{}, ErrInvalidAdapterSandbox
	}
	return pool, nil
}

func deriveFrozenAdapterUIDPool() (AdapterUIDPool, error) {
	derived, err := deriveAdapterUIDPool()
	if err != nil || !reflect.DeepEqual(derived, FrozenAdapterUIDPool()) {
		return AdapterUIDPool{}, ErrInvalidAdapterSandbox
	}
	return derived, nil
}

func mustDeriveAdapterSeatbeltProfile() AdapterSeatbeltProfile {
	profile, err := deriveAdapterSeatbeltProfile()
	if err != nil || !recordHashMatches(profile, "profile_hash", profile.ProfileHash) {
		panic("invalid compiled adapter seatbelt profile")
	}
	return profile
}

func deriveAdapterSeatbeltProfile() (AdapterSeatbeltProfile, error) {
	profile := AdapterSeatbeltProfile{
		SchemaVersion:                     AdapterSeatbeltProfileSchemaVersion,
		ProfileID:                         adapterSeatbeltProfileID,
		AdapterChildProcessOnly:           true,
		NoInProcessInterface:              true,
		WriteDisposableRootsOnly:          true,
		ReadPinnedRootsOnly:               true,
		NetworkBrokerOnly:                 true,
		ExecPinnedAdapterOnly:             true,
		NoCredentialsInArgv:               true,
		NoCredentialsInPolicyOrEvidence:   true,
		DescriptorClosureRequired:         true,
		UIDWideProcessEnumerationRequired: true,
		RootFreezeBeforeContinue:          true,
	}
	profile.ProfileHash = mustHashRecord(profile, "profile_hash")
	if !recordHashMatches(profile, "profile_hash", profile.ProfileHash) {
		return AdapterSeatbeltProfile{}, ErrInvalidAdapterSandbox
	}
	return profile, nil
}

func deriveFrozenAdapterSeatbeltProfile() (AdapterSeatbeltProfile, error) {
	derived, err := deriveAdapterSeatbeltProfile()
	if err != nil || !reflect.DeepEqual(derived, FrozenAdapterSeatbeltProfile()) {
		return AdapterSeatbeltProfile{}, ErrInvalidAdapterSandbox
	}
	return derived, nil
}

func mustDeriveAdapterSandboxVerifierAuthority() AdapterSandboxVerifierAuthority {
	authority, err := deriveAdapterSandboxVerifierAuthority()
	if err != nil || !recordHashMatches(authority, "verifier_authority_hash", authority.VerifierAuthorityHash) {
		panic("invalid compiled adapter sandbox verifier authority")
	}
	return authority
}

func deriveAdapterSandboxVerifierAuthority() (AdapterSandboxVerifierAuthority, error) {
	profile, err := deriveAdapterSeatbeltProfile()
	if err != nil || profile != FrozenAdapterSeatbeltProfile() {
		return AdapterSandboxVerifierAuthority{}, ErrInvalidAdapterSandbox
	}
	pool, err := deriveAdapterUIDPool()
	if err != nil || !reflect.DeepEqual(pool, FrozenAdapterUIDPool()) {
		return AdapterSandboxVerifierAuthority{}, ErrInvalidAdapterSandbox
	}
	authority := AdapterSandboxVerifierAuthority{
		SchemaVersion:       AdapterSandboxVerifierAuthoritySchemaVersion,
		VerifierID:          adapterSandboxVerifierID,
		SeatbeltProfileID:   profile.ProfileID,
		SeatbeltProfileHash: profile.ProfileHash,
		UIDPoolHash:         pool.PoolHash,
		ReleasePinsHash:     FrozenReleasePins().ReleasePinsHash,
		VerificationKinds:   adapterSandboxVerificationKinds(),
	}
	authority.VerifierAuthorityHash = mustHashRecord(authority, "verifier_authority_hash")
	if !recordHashMatches(authority, "verifier_authority_hash", authority.VerifierAuthorityHash) {
		return AdapterSandboxVerifierAuthority{}, ErrInvalidAdapterSandbox
	}
	return authority, nil
}

func deriveFrozenAdapterSandboxVerifierAuthority() (AdapterSandboxVerifierAuthority, error) {
	derived, err := deriveAdapterSandboxVerifierAuthority()
	if err != nil || !reflect.DeepEqual(derived, FrozenAdapterSandboxVerifierAuthority()) {
		return AdapterSandboxVerifierAuthority{}, ErrInvalidAdapterSandbox
	}
	return derived, nil
}

func adapterSandboxVerificationKinds() []AdapterSandboxVerificationKind {
	return []AdapterSandboxVerificationKind{
		AdapterUIDLeaseVerification,
		AdapterSeatbeltProfileVerification,
		AdapterTerminalProofVerification,
		AdapterSandboxBoundaryVerification,
		AdapterDescriptorClosureVerification,
		AdapterRootIdentityVerification,
		AdapterUIDEmptyVerification,
	}
}

func validAdapterSandboxVerificationKind(value AdapterSandboxVerificationKind) bool {
	switch value {
	case AdapterUIDLeaseVerification, AdapterSeatbeltProfileVerification,
		AdapterTerminalProofVerification, AdapterSandboxBoundaryVerification,
		AdapterDescriptorClosureVerification, AdapterRootIdentityVerification,
		AdapterUIDEmptyVerification:
		return true
	default:
		return false
	}
}

// --- UID lease derivation ---

// deriveAdapterUIDLeaseID is the frozen authorization-derived lease ID grammar.
func deriveAdapterUIDLeaseID(attemptNumber int) string {
	return adapterUIDLeaseSlotPrefix + strconv.Itoa(attemptNumber) + adapterUIDLeaseSlotSuffix
}

// --- Verification seals ---

type adapterSandboxUIDLeaseSealEvidence struct {
	UIDLeaseHash string `json:"uid_lease_hash"`
	UID          uint32 `json:"uid"`
	GroupID      uint32 `json:"group_id"`
	PoolID       string `json:"pool_id"`
}

type adapterSandboxSeatbeltSealEvidence struct {
	SeatbeltProfileHash string `json:"seatbelt_profile_hash"`
	SandboxHash         string `json:"sandbox_hash"`
}

type adapterSandboxTerminalProofSealEvidence struct {
	TerminalProofHash string                       `json:"terminal_proof_hash"`
	CleanupResult     AdapterTerminalCleanupResult `json:"cleanup_result"`
	UIDEmptyVerified  bool                         `json:"uid_empty_verified"`
}

type adapterSandboxBoundarySealEvidence struct {
	SandboxHash           string `json:"sandbox_hash"`
	DescriptorClosureHash string `json:"descriptor_closure_hash"`
}

type adapterSandboxDescriptorClosureSealEvidence struct {
	DescriptorClosureHash string `json:"descriptor_closure_hash"`
	DescriptorsClosed     bool   `json:"descriptors_closed"`
}

type adapterSandboxRootIdentitySealEvidence struct {
	RootIdentityHash string `json:"root_identity_hash"`
	RootsFrozen      bool   `json:"roots_frozen"`
}

type adapterSandboxUIDEmptySealEvidence struct {
	UIDEmptyObservationHash string `json:"uid_empty_observation_hash"`
	UIDEmptyVerified        bool   `json:"uid_empty_verified"`
}

type adapterSandboxVerificationSealSet struct {
	uidLease          string
	seatbeltProfile   string
	terminalProof     string
	sandboxBoundary   string
	descriptorClosure string
	rootIdentity      string
	uidEmpty          string
}

func (seals adapterSandboxVerificationSealSet) ordered() []string {
	return []string{seals.uidLease, seals.seatbeltProfile, seals.terminalProof,
		seals.sandboxBoundary, seals.descriptorClosure, seals.rootIdentity, seals.uidEmpty}
}

func adapterSandboxSealEvidenceHash(kind AdapterSandboxVerificationKind, observation AdapterSandboxObservation) string {
	var evidence any
	switch kind {
	case AdapterUIDLeaseVerification:
		evidence = adapterSandboxUIDLeaseSealEvidence{
			UIDLeaseHash: observation.UIDLeaseHash,
			UID:          observation.UID,
			GroupID:      observation.GroupID,
			PoolID:       FrozenAdapterUIDPool().PoolID,
		}
	case AdapterSeatbeltProfileVerification:
		evidence = adapterSandboxSeatbeltSealEvidence{
			SeatbeltProfileHash: observation.SeatbeltProfileHash,
			SandboxHash:         observation.SandboxHash,
		}
	case AdapterTerminalProofVerification:
		evidence = adapterSandboxTerminalProofSealEvidence{
			TerminalProofHash: observation.TerminalProof.ProofHash,
			CleanupResult:     observation.TerminalProof.CleanupResult,
			UIDEmptyVerified:  observation.TerminalProof.UIDEmptyVerified,
		}
	case AdapterSandboxBoundaryVerification:
		evidence = adapterSandboxBoundarySealEvidence{
			SandboxHash:           observation.SandboxHash,
			DescriptorClosureHash: observation.DescriptorClosureHash,
		}
	case AdapterDescriptorClosureVerification:
		evidence = adapterSandboxDescriptorClosureSealEvidence{
			DescriptorClosureHash: observation.DescriptorClosureHash,
			DescriptorsClosed:     observation.TerminalProof.DescriptorsClosed,
		}
	case AdapterRootIdentityVerification:
		evidence = adapterSandboxRootIdentitySealEvidence{
			RootIdentityHash: observation.RootIdentityHash,
			RootsFrozen:      observation.TerminalProof.RootsFrozen,
		}
	case AdapterUIDEmptyVerification:
		evidence = adapterSandboxUIDEmptySealEvidence{
			UIDEmptyObservationHash: observation.TerminalProof.UIDEmptyObservationHash,
			UIDEmptyVerified:        observation.TerminalProof.UIDEmptyVerified,
		}
	default:
		return ""
	}
	raw, err := canonicalBytes(evidence)
	if err != nil {
		return ""
	}
	return sha256Digest(raw)
}

func deriveAdapterSandboxVerificationSeal(kind AdapterSandboxVerificationKind, verifier AdapterSandboxVerifierAuthority, observation AdapterSandboxObservation, canonicalHash string) string {
	evidenceHash := adapterSandboxSealEvidenceHash(kind, observation)
	if !validHash(evidenceHash) {
		return ""
	}
	seal := AdapterSandboxVerificationSeal{
		SchemaVersion:         AdapterSandboxVerificationSealSchemaVersion,
		SealKind:              kind,
		VerifierAuthorityHash: verifier.VerifierAuthorityHash,
		ObservationHash:       observation.ObservationHash,
		CanonicalHash:         canonicalHash,
		EvidenceHash:          evidenceHash,
	}
	seal.SealHash = mustHashRecord(seal, "seal_hash")
	return seal.SealHash
}

func deriveAdapterSandboxVerificationSeals(verifier AdapterSandboxVerifierAuthority, observation AdapterSandboxObservation, canonicalHash string) adapterSandboxVerificationSealSet {
	return adapterSandboxVerificationSealSet{
		uidLease:          deriveAdapterSandboxVerificationSeal(AdapterUIDLeaseVerification, verifier, observation, canonicalHash),
		seatbeltProfile:   deriveAdapterSandboxVerificationSeal(AdapterSeatbeltProfileVerification, verifier, observation, canonicalHash),
		terminalProof:     deriveAdapterSandboxVerificationSeal(AdapterTerminalProofVerification, verifier, observation, canonicalHash),
		sandboxBoundary:   deriveAdapterSandboxVerificationSeal(AdapterSandboxBoundaryVerification, verifier, observation, canonicalHash),
		descriptorClosure: deriveAdapterSandboxVerificationSeal(AdapterDescriptorClosureVerification, verifier, observation, canonicalHash),
		rootIdentity:      deriveAdapterSandboxVerificationSeal(AdapterRootIdentityVerification, verifier, observation, canonicalHash),
		uidEmpty:          deriveAdapterSandboxVerificationSeal(AdapterUIDEmptyVerification, verifier, observation, canonicalHash),
	}
}

func adapterSandboxVerificationSealsHash(seals adapterSandboxVerificationSealSet) string {
	ordered := seals.ordered()
	if len(ordered) != len(adapterSandboxVerificationKinds()) {
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

// --- Decoding ---

// DecodeAdapterSandboxObservation accepts canonical data only. Success is
// never process, sandbox, or terminal authority.
func DecodeAdapterSandboxObservation(raw []byte) (AdapterSandboxObservation, error) {
	value, err := decodeCanonicalRecord[AdapterSandboxObservation](raw)
	if err != nil || validateAdapterSandboxObservation(value) != nil {
		return AdapterSandboxObservation{}, ErrInvalidAdapterSandbox
	}
	return value, nil
}

// --- Evaluation ---

// EvaluateAdapterSandbox first re-establishes fresh release trust and the
// release-pinned verifier authority, then rechecks all opaque evidence and
// complete effect-time freshness. Its only capability result is for a terminal-
// proven snapshot and that result remains non-effect authority.
func EvaluateAdapterSandbox(expected SupervisorIntentAuthority, authorization *VerifiedAuthorization, claim *VerifiedSupervisorIntentClaim, predecessor *VerifiedSupervisorIntentClaim, predecessorTerminal *VerifiedSupervisorTerminalEvent, worktree *VerifiedRepositoryWorktree, snapshot *VerifiedAdapterSandboxSnapshot, action AdapterSandboxAction, now time.Time) (AdapterSandboxAssessment, *VerifiedAdapterSandbox, error) {
	invalid := AdapterSandboxAssessment{}
	now = now.UTC()
	verifier, err := deriveFrozenAdapterSandboxVerifierAuthority()
	if err != nil || VerifyReleaseTrust(FrozenReleasePins(), FrozenTrustBundle(), frozenRotation(), now) != nil {
		return invalid, nil, ErrInvalidAdapterSandbox
	}
	if expected.Phase != AdapterClaimPhase || expected.Sequence != 2 ||
		validateSupervisorIntentAuthority(expected, authorization) != nil ||
		validateSupervisorIntentFreshness(expected, authorization, now) != nil ||
		!verifiedSupervisorIntentClaimIntact(claim) ||
		validateSupervisorIntentClaim(expected, authorization, predecessor, predecessorTerminal, claim.claim) != nil ||
		!verifiedRepositoryWorktreeIntact(worktree) ||
		!verifiedAdapterSandboxSnapshotIntact(snapshot, verifier) ||
		!adapterSandboxSnapshotMatchesAuthority(snapshot, expected, authorization, claim, worktree) {
		return invalid, nil, ErrInvalidAdapterSandbox
	}

	observation := snapshot.observation
	switch observation.State {
	case AdapterSandboxTerminalProven:
		if action != AdapterSandboxAdmitTerminal ||
			!snapshot.uidLeaseVerified || !snapshot.seatbeltProfileVerified || !snapshot.terminalProofVerified ||
			!snapshot.sandboxBoundaryVerified || !snapshot.descriptorClosureVerified || !snapshot.rootIdentityVerified || !snapshot.uidEmptyVerified {
			return invalid, nil, ErrInvalidAdapterSandbox
		}
		if !adapterSandboxTerminalProvenClosure(observation) {
			return invalid, nil, ErrInvalidAdapterSandbox
		}
		seals := deriveAdapterSandboxVerificationSeals(verifier, observation, snapshot.canonicalHash)
		capability := &VerifiedAdapterSandbox{
			valid:                  true,
			observationHash:        observation.ObservationHash,
			snapshotIntegrityHash:  snapshot.integrityHash,
			verifierAuthorityHash:  verifier.VerifierAuthorityHash,
			verificationSealsHash:  adapterSandboxVerificationSealsHash(seals),
			authorizationHash:      observation.AuthorizationHash,
			claimHash:              observation.ClaimHash,
			attemptHash:            observation.AttemptHash,
			predecessorClaimHash:   observation.PredecessorClaimHash,
			worktreeCapabilityHash: observation.WorktreeCapabilityHash,
			uidLeaseHash:           observation.UIDLeaseHash,
			terminalProofHash:      observation.TerminalProof.ProofHash,
			canonical:              append([]byte(nil), snapshot.canonical...),
			canonicalHash:          snapshot.canonicalHash,
		}
		capability.integrityHash = verifiedAdapterSandboxIntegrityHash(capability)
		if !verifiedAdapterSandboxIntact(capability) {
			return invalid, nil, ErrInvalidAdapterSandbox
		}
		return AdapterSandboxAssessment{
			Disposition:     AdapterSandboxCapabilityReady,
			NextRequirement: AdapterSandboxNextTestPhase,
		}, capability, nil
	case AdapterSandboxRetainedReplay:
		if action != AdapterSandboxStatusOnly ||
			!snapshot.uidLeaseVerified || !snapshot.seatbeltProfileVerified || !snapshot.terminalProofVerified ||
			!snapshot.sandboxBoundaryVerified || !snapshot.descriptorClosureVerified || !snapshot.rootIdentityVerified || !snapshot.uidEmptyVerified {
			return invalid, nil, ErrInvalidAdapterSandbox
		}
		if !adapterSandboxTerminalProvenClosure(observation) {
			return invalid, nil, ErrInvalidAdapterSandbox
		}
		return AdapterSandboxAssessment{
			Disposition:     AdapterSandboxRetainedStatus,
			NextRequirement: AdapterSandboxNoFurtherEffect,
		}, nil, nil
	case AdapterSandboxRetainForHuman:
		if action != AdapterSandboxStatusOnly || !validAdapterSandboxWaitingReason(observation.AmbiguityReason) {
			return invalid, nil, ErrInvalidAdapterSandbox
		}
		return AdapterSandboxAssessment{
			Disposition:     AdapterSandboxWaitingForHuman,
			NextRequirement: AdapterSandboxHumanReviewRequired,
		}, nil, nil
	default:
		return invalid, nil, ErrInvalidAdapterSandbox
	}
}

// --- Validators ---

func validateAdapterUIDPoolEntry(value AdapterUIDPoolEntry) error {
	if value.SchemaVersion != AdapterUIDPoolEntrySchemaVersion ||
		!recordHashMatches(value, "entry_hash", value.EntryHash) ||
		value.Sequence < 1 || value.UID == 0 || value.GroupID == 0 || value.UserName == "" {
		return ErrInvalidAdapterSandbox
	}
	return nil
}

func validateAdapterUIDPool(value AdapterUIDPool) error {
	if value.SchemaVersion != AdapterUIDPoolSchemaVersion ||
		!validClosedIdentifier(value.PoolID, maxAdapterSandboxIDBytes) ||
		value.GroupID == 0 || value.GroupName == "" ||
		value.PoolSize <= 0 || len(value.Entries) != value.PoolSize ||
		!recordHashMatches(value, "pool_hash", value.PoolHash) {
		return ErrInvalidAdapterSandbox
	}
	seen := make(map[uint32]struct{}, len(value.Entries))
	for i, entry := range value.Entries {
		if validateAdapterUIDPoolEntry(entry) != nil || entry.Sequence != i+1 || entry.GroupID != value.GroupID {
			return ErrInvalidAdapterSandbox
		}
		if _, dup := seen[entry.UID]; dup {
			return ErrInvalidAdapterSandbox
		}
		seen[entry.UID] = struct{}{}
	}
	return nil
}

func validateAdapterUIDLease(value AdapterUIDLease) error {
	if value.SchemaVersion != AdapterUIDLeaseSchemaVersion ||
		!validClosedIdentifier(value.LeaseID, maxAdapterSandboxIDBytes) ||
		!validHash(value.AttemptHash) || !validHash(value.LeaseHash) ||
		value.AttemptNumber < 1 || value.AttemptNumber > AttemptCap ||
		value.UID == 0 || value.GroupID == 0 || !validClosedIdentifier(value.PoolID, maxAdapterSandboxIDBytes) ||
		!value.Exclusive || !recordHashMatches(value, "lease_hash", value.LeaseHash) {
		return ErrInvalidAdapterSandbox
	}
	if _, err := parseUTC(value.AcquiredAt); err != nil {
		return ErrInvalidAdapterSandbox
	}
	pool := FrozenAdapterUIDPool()
	if value.PoolID != pool.PoolID || value.GroupID != pool.GroupID {
		return ErrInvalidAdapterSandbox
	}
	found := false
	for _, entry := range pool.Entries {
		if entry.UID == value.UID {
			found = true
			break
		}
	}
	if !found {
		return ErrInvalidAdapterSandbox
	}
	if value.LeaseID != deriveAdapterUIDLeaseID(value.AttemptNumber) {
		return ErrInvalidAdapterSandbox
	}
	return nil
}

func validateAdapterSeatbeltProfile(value AdapterSeatbeltProfile) error {
	if value.SchemaVersion != AdapterSeatbeltProfileSchemaVersion ||
		!validClosedIdentifier(value.ProfileID, maxAdapterSandboxIDBytes) ||
		!validHash(value.ProfileHash) || !recordHashMatches(value, "profile_hash", value.ProfileHash) {
		return ErrInvalidAdapterSandbox
	}
	if !value.AdapterChildProcessOnly || !value.NoInProcessInterface ||
		!value.WriteDisposableRootsOnly || !value.ReadPinnedRootsOnly ||
		!value.NetworkBrokerOnly || !value.ExecPinnedAdapterOnly ||
		!value.NoCredentialsInArgv || !value.NoCredentialsInPolicyOrEvidence ||
		!value.DescriptorClosureRequired || !value.UIDWideProcessEnumerationRequired ||
		!value.RootFreezeBeforeContinue {
		return ErrInvalidAdapterSandbox
	}
	return nil
}

func validateAdapterTerminalProof(value AdapterTerminalProof) error {
	if value.SchemaVersion != AdapterTerminalProofSchemaVersion ||
		!validClosedIdentifier(value.ProofID, maxAdapterSandboxIDBytes) ||
		!validHash(value.ProofHash) || !validHash(value.UIDLeaseHash) ||
		!validHash(value.LeaderIdentityHash) || !validHash(value.ProcessGroupIdentityHash) ||
		!validHash(value.UIDEmptyObservationHash) || !validHash(value.SandboxHash) ||
		!validHash(value.RootIdentityHash) || !validHash(value.DescriptorClosureHash) ||
		!validHash(value.BootEpochHash) ||
		!validAdapterTerminalCleanupResult(value.CleanupResult) ||
		!recordHashMatches(value, "proof_hash", value.ProofHash) {
		return ErrInvalidAdapterSandbox
	}
	if _, err := parseUTC(value.ObservedAt); err != nil {
		return ErrInvalidAdapterSandbox
	}
	return nil
}

func validateAdapterSandboxObservation(value AdapterSandboxObservation) error {
	if value.SchemaVersion != AdapterSandboxObservationSchemaVersion ||
		!validClosedIdentifier(value.ObservationID, maxAdapterSandboxIDBytes) ||
		!recordHashMatches(value, "observation_hash", value.ObservationHash) ||
		!validHash(value.AuthorizationHash) || !validHash(value.ApprovalHash) || !validHash(value.RequestHash) ||
		!validHash(value.DispatchHash) || !validHash(value.AttemptHash) || !validHash(value.ClaimHash) ||
		!validHash(value.PredecessorClaimHash) || !validHash(value.PredecessorTerminalEventHash) ||
		!validHash(value.RepositoryBindingHash) || !validHash(value.RepositoryIdentityHash) ||
		!validClosedIdentifier(value.WorktreeSlotID, maxAdapterSandboxIDBytes) || !validHash(value.WorktreeSlotPathHash) ||
		!validHash(value.WorktreeCapabilityHash) || !validHash(value.InstalledWorktreeRootIdentityHash) ||
		!validHash(value.UIDPoolHash) || !validHash(value.UIDLeaseHash) ||
		!validClosedIdentifier(value.SeatbeltProfileID, maxAdapterSandboxIDBytes) || !validHash(value.SeatbeltProfileHash) ||
		!validHash(value.RootIdentityHash) || !validHash(value.SandboxHash) || !validHash(value.DescriptorClosureHash) ||
		!validClosedIdentifier(value.BootEpochID, maxAdapterSandboxIDBytes) || !validHash(value.BootEpochHash) ||
		value.AttemptNumber < 1 || value.AttemptNumber > AttemptCap || value.AttemptCap != AttemptCap ||
		value.UID == 0 || value.GroupID == 0 {
		return ErrInvalidAdapterSandbox
	}
	if !validAdapterSandboxStateReason(value.State, value.AmbiguityReason) {
		return ErrInvalidAdapterSandbox
	}
	if validateAdapterTerminalProof(value.TerminalProof) != nil {
		return ErrInvalidAdapterSandbox
	}
	pool := FrozenAdapterUIDPool()
	if value.UIDPoolHash != pool.PoolHash || value.GroupID != pool.GroupID {
		return ErrInvalidAdapterSandbox
	}
	profile := FrozenAdapterSeatbeltProfile()
	if value.SeatbeltProfileID != profile.ProfileID || value.SeatbeltProfileHash != profile.ProfileHash {
		return ErrInvalidAdapterSandbox
	}
	found := false
	for _, entry := range pool.Entries {
		if entry.UID == value.UID {
			found = true
			break
		}
	}
	if !found {
		return ErrInvalidAdapterSandbox
	}
	return nil
}

func validAdapterSandboxStateReason(state AdapterSandboxState, reason AdapterSandboxAmbiguityReason) bool {
	switch state {
	case AdapterSandboxTerminalProven, AdapterSandboxRetainedReplay:
		return reason == ""
	case AdapterSandboxRetainForHuman:
		return validAdapterSandboxWaitingReason(reason)
	default:
		return false
	}
}

func validAdapterSandboxWaitingReason(value AdapterSandboxAmbiguityReason) bool {
	switch value {
	case AdapterAmbiguityUIDNotEmpty, AdapterAmbiguityDescriptorsOpen,
		AdapterAmbiguityRootsNotFrozen, AdapterAmbiguityStalePID,
		AdapterAmbiguityUIDReuse, AdapterAmbiguityChildAlive,
		AdapterAmbiguityBrokerEscape, AdapterAmbiguityIgnoredContext,
		AdapterAmbiguityDoubleFork, AdapterAmbiguitySetsidEscape,
		AdapterAmbiguityClosedStdio, AdapterAmbiguityDelayedMutation:
		return true
	default:
		return false
	}
}

func validAdapterTerminalCleanupResult(value AdapterTerminalCleanupResult) bool {
	switch value {
	case AdapterCleanupUIDEmptyRootsFrozen, AdapterCleanupUIDNonemptyRetained, AdapterCleanupPartialRetained:
		return true
	default:
		return false
	}
}

func adapterSandboxTerminalProvenClosure(observation AdapterSandboxObservation) bool {
	proof := observation.TerminalProof
	return proof.UIDLeaseHash == observation.UIDLeaseHash &&
		proof.SandboxHash == observation.SandboxHash &&
		proof.RootIdentityHash == observation.RootIdentityHash &&
		proof.DescriptorClosureHash == observation.DescriptorClosureHash &&
		proof.BootEpochHash == observation.BootEpochHash &&
		proof.CleanupResult == AdapterCleanupUIDEmptyRootsFrozen &&
		proof.UIDEmptyVerified && proof.DescriptorsClosed && proof.RootsFrozen
}

// --- Snapshot integrity ---

func adapterSandboxSnapshotIntegrityHash(value *VerifiedAdapterSandboxSnapshot) string {
	if value == nil {
		return ""
	}
	record := adapterSandboxSnapshotIntegrity{
		Valid: value.valid, UIDLeaseVerified: value.uidLeaseVerified,
		SeatbeltProfileVerified: value.seatbeltProfileVerified, TerminalProofVerified: value.terminalProofVerified,
		SandboxBoundaryVerified: value.sandboxBoundaryVerified, DescriptorClosureVerified: value.descriptorClosureVerified,
		RootIdentityVerified: value.rootIdentityVerified, UIDEmptyVerified: value.uidEmptyVerified,
		ObservationHash: value.observation.ObservationHash, CanonicalHash: value.canonicalHash,
		VerifierAuthorityHash:             value.verifierAuthorityHash,
		UIDLeaseVerificationSeal:          value.uidLeaseVerificationSeal,
		SeatbeltProfileVerificationSeal:   value.seatbeltProfileVerificationSeal,
		TerminalProofVerificationSeal:     value.terminalProofVerificationSeal,
		SandboxBoundaryVerificationSeal:   value.sandboxBoundaryVerificationSeal,
		DescriptorClosureVerificationSeal: value.descriptorClosureVerificationSeal,
		RootIdentityVerificationSeal:      value.rootIdentityVerificationSeal,
		UIDEmptyVerificationSeal:          value.uidEmptyVerificationSeal,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

// verifiedAdapterSandboxSnapshotIntact re-establishes verifier provenance:
// every seal is recomputed from the decoded observation under the frozen
// verifier authority and must match the stored seal exactly.
func verifiedAdapterSandboxSnapshotIntact(value *VerifiedAdapterSandboxSnapshot, verifier AdapterSandboxVerifierAuthority) bool {
	if value == nil || !recordHashMatches(verifier, "verifier_authority_hash", verifier.VerifierAuthorityHash) ||
		!value.valid || !value.uidLeaseVerified || !value.seatbeltProfileVerified || !value.terminalProofVerified ||
		!value.sandboxBoundaryVerified || !value.descriptorClosureVerified || !value.rootIdentityVerified || !value.uidEmptyVerified ||
		!validHash(value.integrityHash) || value.integrityHash != adapterSandboxSnapshotIntegrityHash(value) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		value.verifierAuthorityHash != verifier.VerifierAuthorityHash {
		return false
	}
	decoded, err := DecodeAdapterSandboxObservation(value.canonical)
	if err != nil || !reflect.DeepEqual(decoded, value.observation) {
		return false
	}
	seals := deriveAdapterSandboxVerificationSeals(verifier, decoded, value.canonicalHash)
	return value.uidLeaseVerificationSeal == seals.uidLease &&
		value.seatbeltProfileVerificationSeal == seals.seatbeltProfile &&
		value.terminalProofVerificationSeal == seals.terminalProof &&
		value.sandboxBoundaryVerificationSeal == seals.sandboxBoundary &&
		value.descriptorClosureVerificationSeal == seals.descriptorClosure &&
		value.rootIdentityVerificationSeal == seals.rootIdentity &&
		value.uidEmptyVerificationSeal == seals.uidEmpty
}

func adapterSandboxSnapshotMatchesAuthority(value *VerifiedAdapterSandboxSnapshot, expected SupervisorIntentAuthority, authorization *VerifiedAuthorization, claim *VerifiedSupervisorIntentClaim, worktree *VerifiedRepositoryWorktree) bool {
	observation := value.observation
	return observation.AuthorizationHash == authorization.authorization.AuthorizationHash &&
		observation.ApprovalHash == authorization.authorization.ApprovalHash &&
		observation.RequestHash == expected.AcceptedDispatch.Request.RequestHash &&
		observation.DispatchHash == expected.AcceptedDispatch.DispatchHash &&
		observation.AttemptHash == expected.AttemptHash && observation.AttemptNumber == expected.AttemptNumber &&
		observation.AttemptCap == expected.AttemptCap && observation.ClaimHash == claim.claim.ClaimHash &&
		observation.PredecessorClaimHash == expected.PredecessorClaimHash &&
		observation.RepositoryBindingHash == expected.Repository.RepositoryBindingHash &&
		observation.RepositoryIdentityHash == expected.Repository.RepositoryIdentityHash &&
		observation.BootEpochID == expected.BootEpochID && observation.BootEpochHash == expected.BootEpochHash &&
		observation.WorktreeSlotID == worktree.worktreeSlotID &&
		observation.WorktreeSlotPathHash == worktree.writablePathSetHash &&
		observation.WorktreeCapabilityHash == worktree.snapshotIntegrityHash &&
		observation.InstalledWorktreeRootIdentityHash == worktree.candidateRootIdentityHash
}

// --- Capability integrity ---

func verifiedAdapterSandboxIntegrityHash(value *VerifiedAdapterSandbox) string {
	if value == nil {
		return ""
	}
	record := verifiedAdapterSandboxIntegrity{
		Valid: value.valid, ObservationHash: value.observationHash,
		SnapshotIntegrityHash: value.snapshotIntegrityHash,
		VerifierAuthorityHash: value.verifierAuthorityHash, VerificationSealsHash: value.verificationSealsHash,
		AuthorizationHash: value.authorizationHash, ClaimHash: value.claimHash, AttemptHash: value.attemptHash,
		PredecessorClaimHash: value.predecessorClaimHash, WorktreeCapabilityHash: value.worktreeCapabilityHash,
		UIDLeaseHash: value.uidLeaseHash, TerminalProofHash: value.terminalProofHash, CanonicalHash: value.canonicalHash,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

// verifiedAdapterSandboxIntact re-establishes provenance: the seven seals
// are recomputed from the decoded observation under the frozen verifier
// authority and the terminal-proven mint invariants, and must reproduce the
// aggregate verification-seals hash exactly.
func verifiedAdapterSandboxIntact(value *VerifiedAdapterSandbox) bool {
	if value == nil || !value.valid || !validHash(value.integrityHash) ||
		value.integrityHash != verifiedAdapterSandboxIntegrityHash(value) ||
		!validHash(value.snapshotIntegrityHash) || !validHash(value.authorizationHash) || !validHash(value.claimHash) ||
		!validHash(value.attemptHash) || !validHash(value.predecessorClaimHash) || !validHash(value.worktreeCapabilityHash) ||
		!validHash(value.uidLeaseHash) || !validHash(value.terminalProofHash) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		!validHash(value.verifierAuthorityHash) || !validHash(value.verificationSealsHash) {
		return false
	}
	verifier := FrozenAdapterSandboxVerifierAuthority()
	if value.verifierAuthorityHash != verifier.VerifierAuthorityHash {
		return false
	}
	observation, err := DecodeAdapterSandboxObservation(value.canonical)
	if err != nil || observation.State != AdapterSandboxTerminalProven ||
		observation.ObservationHash != value.observationHash ||
		observation.AuthorizationHash != value.authorizationHash ||
		observation.ClaimHash != value.claimHash || observation.AttemptHash != value.attemptHash ||
		observation.PredecessorClaimHash != value.predecessorClaimHash ||
		observation.WorktreeCapabilityHash != value.worktreeCapabilityHash ||
		observation.UIDLeaseHash != value.uidLeaseHash ||
		observation.TerminalProof.ProofHash != value.terminalProofHash {
		return false
	}
	seals := deriveAdapterSandboxVerificationSeals(verifier, observation, value.canonicalHash)
	return value.verificationSealsHash == adapterSandboxVerificationSealsHash(seals)
}
