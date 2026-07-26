package repaircontract

import (
	"bytes"
	"errors"
	"time"
)

const (
	SupervisorIntentClaimSchemaVersion     = "ananke.controlled-repair-supervisor-intent-claim.v1"
	SupervisorAttemptIdentitySchemaVersion = "ananke.controlled-repair-supervisor-attempt-identity.v1"
	SupervisorRecoveryRecordSchemaVersion  = "ananke.controlled-repair-supervisor-recovery-record.v1"
	DurabilityPolicyFullFullFSync          = "FULL+fullfsync"

	MaterializationClaimPhase SupervisorIntentPhase = "materialization_claim"
	AdapterClaimPhase         SupervisorIntentPhase = "adapter_claim"
	TestClaimPhase            SupervisorIntentPhase = "test_claim"

	SupervisorClaimAwaitingCommit SupervisorClaimDisposition = "awaiting_journal_commit"
	SupervisorClaimExactReplay    SupervisorClaimDisposition = "exact_replay"
	SupervisorClaimConflict       SupervisorClaimDisposition = "conflict"

	BeforeClaimCommit                     SupervisorCrashCutPoint = "before_claim_commit"
	AfterClaimCommit                      SupervisorCrashCutPoint = "after_claim_commit"
	BeforePhaseLaunch                     SupervisorCrashCutPoint = "before_phase_launch"
	AfterPhaseLaunch                      SupervisorCrashCutPoint = "after_phase_launch"
	BeforeTerminalProofPersistence        SupervisorCrashCutPoint = "before_terminal_proof_persistence"
	AfterTerminalProofPersistence         SupervisorCrashCutPoint = "after_terminal_proof_persistence"
	BeforeAttestationSignaturePersistence SupervisorCrashCutPoint = "before_attestation_signature_persistence"
	AfterAttestationSignaturePersistence  SupervisorCrashCutPoint = "after_attestation_signature_persistence"
	BeforeResponsePersistence             SupervisorCrashCutPoint = "before_response_persistence"
	AfterResponsePersistence              SupervisorCrashCutPoint = "after_response_persistence"

	SupervisorRecoveryClaimCommitRecord          SupervisorRecoveryRecordKind = "claim_commit"
	SupervisorRecoveryPhaseLaunchRecord          SupervisorRecoveryRecordKind = "phase_launch"
	SupervisorRecoveryTerminalProofRecord        SupervisorRecoveryRecordKind = "terminal_proof"
	SupervisorRecoveryAttestationSignatureRecord SupervisorRecoveryRecordKind = "attestation_signature"
	SupervisorRecoveryResponseRecord             SupervisorRecoveryRecordKind = "response"

	RecoveryStatusOnly     SupervisorRecoveryAction = "status_only"
	RecoveryReplayResponse SupervisorRecoveryAction = "replay_response"

	NoDurableSupervisorClaim             SupervisorDurableObservation = "no_durable_claim"
	DurableClaimCommitted                SupervisorDurableObservation = "claim_committed"
	DurablePhaseLaunched                 SupervisorDurableObservation = "phase_launched"
	DurableTerminalProofPersisted        SupervisorDurableObservation = "terminal_proof_persisted"
	DurableAttestationSignaturePersisted SupervisorDurableObservation = "attestation_signature_persisted"
	DurableResponsePersisted             SupervisorDurableObservation = "response_persisted"

	RecoveryNoDurableClaim         SupervisorRecoveryDisposition = "no_durable_claim"
	RecoveryWaitingForHuman        SupervisorRecoveryDisposition = "waiting_for_human"
	RecoveryCurrentEpochStatusOnly SupervisorRecoveryDisposition = "current_epoch_status_only"
	RecoveryTerminalStatus         SupervisorRecoveryDisposition = "terminal_status"
	RecoveryResponseReplay         SupervisorRecoveryDisposition = "response_replay"

	FreshLiveIntentRequired        SupervisorRecoveryRequirement = "fresh_live_intent_required"
	HumanReviewRequired            SupervisorRecoveryRequirement = "human_review_required"
	LiveCommitConfirmationRequired SupervisorRecoveryRequirement = "live_commit_confirmation_required"
	AttestationStatusRequired      SupervisorRecoveryRequirement = "attestation_status_required"
	ResponseStatusRequired         SupervisorRecoveryRequirement = "response_status_required"
	NoFurtherEffectPermitted       SupervisorRecoveryRequirement = "no_further_effect_permitted"

	MaxSupervisorIntentLifetime = time.Minute

	maxSupervisorIntentIDBytes = 192
	maxRecoveryRecordIDBytes   = 192
)

var (
	ErrInvalidSupervisorIntent   = errors.New("controlled repair supervisor intent is invalid")
	ErrSupervisorClaimConflict   = errors.New("controlled repair supervisor claim conflicts with committed slot")
	ErrInvalidSupervisorRecovery = errors.New("controlled repair supervisor recovery observation is invalid")
)

type SupervisorIntentPhase string
type SupervisorClaimDisposition string
type SupervisorCrashCutPoint string
type SupervisorRecoveryRecordKind string
type SupervisorRecoveryAction string
type SupervisorDurableObservation string
type SupervisorRecoveryDisposition string
type SupervisorRecoveryRequirement string

type supervisorAttemptIdentity struct {
	SchemaVersion     string `json:"schema_version"`
	AttemptHash       string `json:"attempt_hash"`
	AttemptNumber     int    `json:"attempt_number"`
	AttemptCap        int    `json:"attempt_cap"`
	AuthorizationHash string `json:"authorization_hash"`
	ApprovalHash      string `json:"approval_hash"`
	RequestHash       string `json:"request_hash"`
	DispatchHash      string `json:"dispatch_hash"`
}
type supervisorClaimSlotCommitIntegrity struct {
	IntegrityHash          string                `json:"integrity_hash"`
	Valid                  bool                  `json:"valid"`
	CommitVerified         bool                  `json:"commit_verified"`
	TupleUnique            bool                  `json:"tuple_unique"`
	SlotUnique             bool                  `json:"slot_unique"`
	AttemptHash            string                `json:"attempt_hash"`
	Phase                  SupervisorIntentPhase `json:"phase"`
	Sequence               int                   `json:"sequence"`
	SlotID                 string                `json:"slot_id"`
	JournalHeadHash        string                `json:"journal_head_hash"`
	BootEpochID            string                `json:"boot_epoch_id"`
	BootEpochHash          string                `json:"boot_epoch_hash"`
	ClaimID                string                `json:"claim_id"`
	ClaimHash              string                `json:"claim_hash"`
	AuthorizationHash      string                `json:"authorization_hash"`
	ApprovalHash           string                `json:"approval_hash"`
	RequestHash            string                `json:"request_hash"`
	DispatchHash           string                `json:"dispatch_hash"`
	CommittedCanonicalHash string                `json:"committed_canonical_hash"`
}

type supervisorTerminalEventIntegrity struct {
	IntegrityHash           string                `json:"integrity_hash"`
	Valid                   bool                  `json:"valid"`
	TerminalVerified        bool                  `json:"terminal_verified"`
	AttemptHash             string                `json:"attempt_hash"`
	Phase                   SupervisorIntentPhase `json:"phase"`
	Sequence                int                   `json:"sequence"`
	ClaimHash               string                `json:"claim_hash"`
	TerminalEventHash       string                `json:"terminal_event_hash"`
	BootEpochHash           string                `json:"boot_epoch_hash"`
	SlotID                  string                `json:"slot_id"`
	ClaimJournalHeadHash    string                `json:"claim_journal_head_hash"`
	TerminalJournalHeadHash string                `json:"terminal_journal_head_hash"`
	TerminalStatus          string                `json:"terminal_status"`
}

// SupervisorIntentClaim is an immutable journal value. It describes one phase
// intent and cannot itself authorize a process or filesystem effect.
type SupervisorIntentClaim struct {
	SchemaVersion                string                `json:"schema_version"`
	ClaimHash                    string                `json:"claim_hash"`
	ClaimID                      string                `json:"claim_id"`
	Phase                        SupervisorIntentPhase `json:"phase"`
	Sequence                     int                   `json:"sequence"`
	AttemptHash                  string                `json:"attempt_hash"`
	AttemptNumber                int                   `json:"attempt_number"`
	AttemptCap                   int                   `json:"attempt_cap"`
	AuthorizationHash            string                `json:"authorization_hash"`
	ApprovalHash                 string                `json:"approval_hash"`
	PolicyHash                   string                `json:"policy_hash"`
	P4FactHash                   string                `json:"p4_fact_hash"`
	P4ProposalHash               string                `json:"p4_proposal_hash"`
	P4InputHash                  string                `json:"p4_input_hash"`
	P4EvidenceBundleHash         string                `json:"p4_evidence_bundle_hash"`
	P4AdmissionHash              string                `json:"p4_admission_hash"`
	FenceHash                    string                `json:"fence_hash"`
	FenceClaimTokenHash          string                `json:"fence_claim_token_hash"`
	RequestHash                  string                `json:"request_hash"`
	DispatchHash                 string                `json:"dispatch_hash"`
	ChannelBindingHash           string                `json:"channel_binding_hash"`
	PeerIdentityHash             string                `json:"peer_identity_hash"`
	PredecessorClaimHash         string                `json:"predecessor_claim_hash"`
	PredecessorTerminalEventHash string                `json:"predecessor_terminal_event_hash"`
	SupervisorBootEpochID        string                `json:"supervisor_boot_epoch_id"`
	SupervisorBootEpochHash      string                `json:"supervisor_boot_epoch_hash"`
	JournalHeadHash              string                `json:"journal_head_hash"`
	JournalSlotID                string                `json:"journal_slot_id"`
	RepositoryBindingHash        string                `json:"repository_binding_hash"`
	RepositoryIdentityHash       string                `json:"repository_identity_hash"`
	BaseCommit                   string                `json:"base_commit"`
	BaseTree                     string                `json:"base_tree"`
	ExecutableIdentityHash       string                `json:"executable_identity_hash"`
	SandboxProfileID             string                `json:"sandbox_profile_id"`
	SandboxProfileHash           string                `json:"sandbox_profile_hash"`
	NamespaceID                  string                `json:"namespace_id"`
	NamespaceIdentityHash        string                `json:"namespace_identity_hash"`
	RootIdentityHash             string                `json:"root_identity_hash"`
	DurabilityPolicyID           string                `json:"durability_policy_id"`
	CreatedAt                    string                `json:"created_at"`
	NotAfter                     string                `json:"not_after"`
}

// SupervisorIntentAuthority contains expected immutable identifiers and
// independently accepted authorization/installation state. It contains no
// caller-settable journal status, committed bytes, uniqueness assertion, or
// terminal-event verification state.
type SupervisorIntentAuthority struct {
	BootEpochID            string
	BootEpochHash          string
	JournalHeadHash        string
	JournalSlotID          string
	AttemptHash            string
	AttemptNumber          int
	AttemptCap             int
	Phase                  SupervisorIntentPhase
	Sequence               int
	ClaimID                string
	AcceptedDispatch       ImmutableDispatch
	Repository             RepositoryBinding
	ExecutableIdentityHash string
	SandboxProfileID       string
	SandboxProfileHash     string
	NamespaceID            string
	NamespaceIdentityHash  string
	RootIdentityHash       string
	DurabilityPolicyID     string
	CreatedAt              string
	NotAfter               string
	PredecessorClaimHash   string
}

// VerifiedSupervisorClaimSlotCommit is opaque journal evidence. No exported
// constructor or decoder exists; future minting belongs to trusted in-package
// journal verification or a separately reviewed seam.
type VerifiedSupervisorClaimSlotCommit struct {
	valid                  bool
	commitVerified         bool
	tupleUnique            bool
	slotUnique             bool
	attemptHash            string
	phase                  SupervisorIntentPhase
	sequence               int
	slotID                 string
	journalHeadHash        string
	bootEpochID            string
	bootEpochHash          string
	claimID                string
	claimHash              string
	authorizationHash      string
	approvalHash           string
	requestHash            string
	dispatchHash           string
	committedCanonical     []byte
	committedCanonicalHash string
	integrityHash          string
}

// VerifiedSupervisorTerminalEvent is opaque predecessor terminal evidence.
// Its private integrity binds the preceding claim, slot/journal lineage, boot
// epoch, terminal event, and successful terminal status.
type VerifiedSupervisorTerminalEvent struct {
	valid                   bool
	terminalVerified        bool
	attemptHash             string
	phase                   SupervisorIntentPhase
	sequence                int
	claimHash               string
	terminalEventHash       string
	bootEpochHash           string
	slotID                  string
	claimJournalHeadHash    string
	terminalJournalHeadHash string
	terminalStatus          string
	integrityHash           string
}

// SupervisorClaimAssessment is classification only. EffectAllowed is always
// false; no API in this package accepts it as launch authority.
type SupervisorClaimAssessment struct {
	Disposition   SupervisorClaimDisposition
	EffectAllowed bool
}

// VerifiedSupervisorIntentClaim is opaque predecessor evidence minted only
// from a fresh, exact replay backed by an intact opaque slot-commit proof.
type VerifiedSupervisorIntentClaim struct {
	valid      bool
	claim      SupervisorIntentClaim
	canonical  []byte
	slotCommit VerifiedSupervisorClaimSlotCommit
}

// EvaluateSupervisorIntentClaim validates canonical claim bytes against opaque
// authorization, optional journal-commit, predecessor-claim, and predecessor
// terminal-event evidence. A nil commit proof can only yield an awaiting-commit
// classification. Historical exact replay never reconstructs a fresh
// predecessor capability.
func EvaluateSupervisorIntentClaim(expected SupervisorIntentAuthority, authorization *VerifiedAuthorization, journalCommit *VerifiedSupervisorClaimSlotCommit, predecessor *VerifiedSupervisorIntentClaim, predecessorTerminal *VerifiedSupervisorTerminalEvent, raw []byte, now time.Time) (SupervisorClaimAssessment, *VerifiedSupervisorIntentClaim, error) {
	invalid := SupervisorClaimAssessment{}
	if validateSupervisorIntentAuthority(expected, authorization) != nil {
		return invalid, nil, ErrInvalidSupervisorIntent
	}

	if journalCommit == nil {
		claim, err := decodeCanonicalRecord[SupervisorIntentClaim](raw)
		if err != nil || validateSupervisorIntentClaim(expected, authorization, predecessor, predecessorTerminal, claim) != nil ||
			validateSupervisorIntentFreshness(expected, authorization, now.UTC()) != nil {
			return invalid, nil, ErrInvalidSupervisorIntent
		}
		return SupervisorClaimAssessment{Disposition: SupervisorClaimAwaitingCommit}, nil, nil
	}

	if !verifiedSupervisorClaimSlotCommitIntact(journalCommit) || !slotCommitMatchesAuthority(journalCommit, expected, authorization) {
		return invalid, nil, ErrInvalidSupervisorIntent
	}
	if !bytes.Equal(journalCommit.committedCanonical, raw) {
		return SupervisorClaimAssessment{Disposition: SupervisorClaimConflict}, nil, ErrSupervisorClaimConflict
	}
	claim, err := decodeCanonicalRecord[SupervisorIntentClaim](journalCommit.committedCanonical)
	if err != nil || validateSupervisorIntentClaim(expected, authorization, predecessor, predecessorTerminal, claim) != nil {
		return invalid, nil, ErrInvalidSupervisorIntent
	}
	assessment := SupervisorClaimAssessment{Disposition: SupervisorClaimExactReplay}
	if validateSupervisorIntentFreshness(expected, authorization, now.UTC()) != nil {
		return assessment, nil, nil
	}
	canonical := append([]byte(nil), journalCommit.committedCanonical...)
	commitCopy := cloneSupervisorClaimSlotCommit(*journalCommit)
	return assessment, &VerifiedSupervisorIntentClaim{
		valid: true, claim: claim, canonical: canonical, slotCommit: commitCopy,
	}, nil
}

func validateSupervisorIntentAuthority(expected SupervisorIntentAuthority, verified *VerifiedAuthorization) error {
	if !verifiedAuthorizationIntact(verified) {
		return ErrInvalidSupervisorIntent
	}
	wantAttemptHash := supervisorAttemptIdentityHash(verified.authorization, expected.AcceptedDispatch)
	if wantAttemptHash == "" || expected.AttemptHash != wantAttemptHash || expected.AttemptNumber != verified.authorization.Scope.Attempt.AttemptNumber ||
		expected.AttemptCap != AttemptCap || expected.AttemptCap != verified.authorization.Scope.Attempt.AttemptCap ||
		expected.Repository != verified.authorization.Scope.Repository || expected.AcceptedDispatch.AttemptNumber != expected.AttemptNumber ||
		expected.AcceptedDispatch.AttemptCap != expected.AttemptCap || expected.AcceptedDispatch.AuthorizationHash != verified.authorization.AuthorizationHash ||
		expected.AcceptedDispatch.ApprovalHash != verified.authorization.ApprovalHash || expected.AcceptedDispatch.PolicyHash != verified.authorization.PolicyHash ||
		expected.AcceptedDispatch.Request.AuthorizationHash != verified.authorization.AuthorizationHash ||
		expected.AcceptedDispatch.Request.AttemptNumber != expected.AttemptNumber || expected.AcceptedDispatch.Request.AttemptCap != expected.AttemptCap ||
		phaseSequence(expected.Phase) != expected.Sequence || !validClosedIdentifier(expected.ClaimID, maxSupervisorIntentIDBytes) ||
		!validClosedIdentifier(expected.BootEpochID, maxSupervisorIntentIDBytes) || !validClosedIdentifier(expected.JournalSlotID, maxSupervisorIntentIDBytes) ||
		!validClosedIdentifier(expected.SandboxProfileID, maxSupervisorIntentIDBytes) || !validClosedIdentifier(expected.NamespaceID, maxSupervisorIntentIDBytes) ||
		expected.DurabilityPolicyID != DurabilityPolicyFullFullFSync {
		return ErrInvalidSupervisorIntent
	}
	if !validHash(expected.BootEpochHash) || !validHash(expected.JournalHeadHash) || !validHash(expected.AttemptHash) ||
		!validHash(expected.ExecutableIdentityHash) || !validHash(expected.SandboxProfileHash) ||
		!validHash(expected.NamespaceIdentityHash) || !validHash(expected.RootIdentityHash) ||
		!recordHashMatches(expected.Repository, "repository_binding_hash", expected.Repository.RepositoryBindingHash) {
		return ErrInvalidSupervisorIntent
	}
	if expected.Sequence == 1 {
		if expected.PredecessorClaimHash != "" {
			return ErrInvalidSupervisorIntent
		}
	} else if !validHash(expected.PredecessorClaimHash) {
		return ErrInvalidSupervisorIntent
	}
	createdAt, err := parseUTC(expected.CreatedAt)
	if err != nil {
		return ErrInvalidSupervisorIntent
	}
	notAfter, err := parseUTC(expected.NotAfter)
	if err != nil || !notAfter.After(createdAt) || notAfter.Sub(createdAt) > MaxSupervisorIntentLifetime {
		return ErrInvalidSupervisorIntent
	}
	dispatchCreatedAt, err := parseUTC(expected.AcceptedDispatch.CreatedAt)
	if err != nil {
		return ErrInvalidSupervisorIntent
	}
	dispatchNotAfter, err := parseUTC(expected.AcceptedDispatch.DispatchNotAfter)
	lastLiveInstant := notAfter.Add(-time.Nanosecond)
	if err != nil || createdAt.Before(dispatchCreatedAt) || notAfter.After(dispatchNotAfter) || lastLiveInstant.Before(createdAt) ||
		validateDispatchRecord(verified.authority, verified, expected.AcceptedDispatch, time.Time{}, ValidationAdmission, false) != nil ||
		validateDispatchRecord(verified.authority, verified, expected.AcceptedDispatch, lastLiveInstant, ValidationEffect, true) != nil {
		return ErrInvalidSupervisorIntent
	}
	return nil
}

func validateSupervisorIntentFreshness(expected SupervisorIntentAuthority, verified *VerifiedAuthorization, now time.Time) error {
	createdAt, err := parseUTC(expected.CreatedAt)
	if err != nil {
		return ErrInvalidSupervisorIntent
	}
	notAfter, err := parseUTC(expected.NotAfter)
	if err != nil || now.Before(createdAt) || !now.Before(notAfter) ||
		validateDispatchRecord(verified.authority, verified, expected.AcceptedDispatch, now, ValidationEffect, true) != nil {
		return ErrInvalidSupervisorIntent
	}
	return nil
}

func validateSupervisorIntentClaim(expected SupervisorIntentAuthority, verified *VerifiedAuthorization, predecessor *VerifiedSupervisorIntentClaim, predecessorTerminal *VerifiedSupervisorTerminalEvent, claim SupervisorIntentClaim) error {
	p4 := verified.authorization.Scope.P4
	dispatch := expected.AcceptedDispatch
	if claim.SchemaVersion != SupervisorIntentClaimSchemaVersion || claim.ClaimID != expected.ClaimID || claim.Phase != expected.Phase ||
		claim.Sequence != expected.Sequence || claim.AttemptHash != expected.AttemptHash || claim.AttemptNumber != expected.AttemptNumber ||
		claim.AttemptCap != expected.AttemptCap || claim.AuthorizationHash != verified.authorization.AuthorizationHash ||
		claim.ApprovalHash != verified.authorization.ApprovalHash || claim.PolicyHash != verified.authorization.PolicyHash ||
		claim.P4FactHash != p4.P4FactHash || claim.P4ProposalHash != p4.P4ProposalHash || claim.P4InputHash != p4.P4InputHash ||
		claim.P4EvidenceBundleHash != p4.P4EvidenceBundleHash || claim.P4AdmissionHash != p4.P4AdmissionHash ||
		claim.FenceHash != p4.FullFence.FenceHash || claim.FenceClaimTokenHash != p4.FullFence.ClaimTokenHash ||
		claim.RequestHash != dispatch.Request.RequestHash || claim.DispatchHash != dispatch.DispatchHash ||
		claim.ChannelBindingHash != dispatch.ChannelBindingHash || claim.PeerIdentityHash != dispatch.ExpectedPeer.PeerIdentityHash ||
		claim.PredecessorClaimHash != expected.PredecessorClaimHash ||
		claim.SupervisorBootEpochID != expected.BootEpochID || claim.SupervisorBootEpochHash != expected.BootEpochHash ||
		claim.JournalHeadHash != expected.JournalHeadHash || claim.JournalSlotID != expected.JournalSlotID ||
		claim.RepositoryBindingHash != expected.Repository.RepositoryBindingHash || claim.RepositoryIdentityHash != expected.Repository.RepositoryIdentityHash ||
		claim.BaseCommit != expected.Repository.BaseCommit || claim.BaseTree != expected.Repository.BaseTree ||
		claim.ExecutableIdentityHash != expected.ExecutableIdentityHash || claim.SandboxProfileID != expected.SandboxProfileID ||
		claim.SandboxProfileHash != expected.SandboxProfileHash || claim.NamespaceID != expected.NamespaceID ||
		claim.NamespaceIdentityHash != expected.NamespaceIdentityHash || claim.RootIdentityHash != expected.RootIdentityHash ||
		claim.DurabilityPolicyID != DurabilityPolicyFullFullFSync || claim.CreatedAt != expected.CreatedAt || claim.NotAfter != expected.NotAfter ||
		!recordHashMatches(claim, "claim_hash", claim.ClaimHash) || !validSupervisorIntentClaimHashes(claim) {
		return ErrInvalidSupervisorIntent
	}
	if expected.Sequence == 1 {
		if predecessor != nil || predecessorTerminal != nil {
			return ErrInvalidSupervisorIntent
		}
		return nil
	}
	if !verifiedSupervisorIntentClaimIntact(predecessor) || !verifiedSupervisorTerminalEventIntact(predecessorTerminal) ||
		predecessor.claim.Sequence != expected.Sequence-1 || phaseSequence(predecessor.claim.Phase) != expected.Sequence-1 ||
		predecessor.claim.ClaimHash != expected.PredecessorClaimHash || predecessor.claim.AttemptHash != claim.AttemptHash ||
		predecessor.claim.AttemptNumber != claim.AttemptNumber || predecessor.claim.AttemptCap != claim.AttemptCap ||
		predecessor.claim.AuthorizationHash != claim.AuthorizationHash || predecessor.claim.ApprovalHash != claim.ApprovalHash ||
		predecessor.claim.SupervisorBootEpochHash != claim.SupervisorBootEpochHash ||
		predecessorTerminal.attemptHash != claim.AttemptHash || predecessorTerminal.phase != predecessor.claim.Phase ||
		predecessorTerminal.sequence != predecessor.claim.Sequence || predecessorTerminal.claimHash != predecessor.claim.ClaimHash ||
		predecessorTerminal.terminalEventHash != claim.PredecessorTerminalEventHash ||
		predecessorTerminal.bootEpochHash != claim.SupervisorBootEpochHash || predecessorTerminal.slotID != predecessor.claim.JournalSlotID ||
		predecessorTerminal.claimJournalHeadHash != predecessor.claim.JournalHeadHash ||
		predecessorTerminal.terminalJournalHeadHash != claim.JournalHeadHash {
		return ErrInvalidSupervisorIntent
	}
	return nil
}

func validSupervisorIntentClaimHashes(claim SupervisorIntentClaim) bool {
	required := []string{
		claim.ClaimHash, claim.AttemptHash, claim.AuthorizationHash, claim.ApprovalHash, claim.PolicyHash,
		claim.P4FactHash, claim.P4ProposalHash, claim.P4InputHash, claim.P4EvidenceBundleHash, claim.P4AdmissionHash,
		claim.FenceHash, claim.FenceClaimTokenHash, claim.RequestHash, claim.DispatchHash, claim.ChannelBindingHash,
		claim.PeerIdentityHash, claim.SupervisorBootEpochHash, claim.JournalHeadHash, claim.RepositoryBindingHash,
		claim.RepositoryIdentityHash, claim.ExecutableIdentityHash, claim.SandboxProfileHash, claim.NamespaceIdentityHash,
		claim.RootIdentityHash,
	}
	for _, value := range required {
		if !validHash(value) {
			return false
		}
	}
	if claim.Sequence == 1 {
		return claim.PredecessorClaimHash == "" && claim.PredecessorTerminalEventHash == ""
	}
	return validHash(claim.PredecessorClaimHash) && validHash(claim.PredecessorTerminalEventHash)
}

func supervisorAttemptIdentityHash(authorization Authorization, dispatch ImmutableDispatch) string {
	value := supervisorAttemptIdentity{
		SchemaVersion:     SupervisorAttemptIdentitySchemaVersion,
		AttemptNumber:     authorization.Scope.Attempt.AttemptNumber,
		AttemptCap:        authorization.Scope.Attempt.AttemptCap,
		AuthorizationHash: authorization.AuthorizationHash,
		ApprovalHash:      authorization.ApprovalHash,
		RequestHash:       dispatch.Request.RequestHash,
		DispatchHash:      dispatch.DispatchHash,
	}
	hash, err := hashRecord(value, "attempt_hash")
	if err != nil {
		return ""
	}
	return hash
}

func phaseSequence(phase SupervisorIntentPhase) int {
	switch phase {
	case MaterializationClaimPhase:
		return 1
	case AdapterClaimPhase:
		return 2
	case TestClaimPhase:
		return 3
	default:
		return 0
	}
}

func supervisorClaimSlotCommitIntegrityHash(value *VerifiedSupervisorClaimSlotCommit) string {
	if value == nil {
		return ""
	}
	record := supervisorClaimSlotCommitIntegrity{
		Valid: value.valid, CommitVerified: value.commitVerified, TupleUnique: value.tupleUnique, SlotUnique: value.slotUnique,
		AttemptHash: value.attemptHash, Phase: value.phase, Sequence: value.sequence, SlotID: value.slotID,
		JournalHeadHash: value.journalHeadHash, BootEpochID: value.bootEpochID, BootEpochHash: value.bootEpochHash,
		ClaimID: value.claimID, ClaimHash: value.claimHash, AuthorizationHash: value.authorizationHash,
		ApprovalHash: value.approvalHash, RequestHash: value.requestHash, DispatchHash: value.dispatchHash,
		CommittedCanonicalHash: value.committedCanonicalHash,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedSupervisorClaimSlotCommitIntact(value *VerifiedSupervisorClaimSlotCommit) bool {
	if value == nil || !value.valid || !value.commitVerified || !value.tupleUnique || !value.slotUnique ||
		!validHash(value.integrityHash) || value.integrityHash != supervisorClaimSlotCommitIntegrityHash(value) ||
		!validHash(value.committedCanonicalHash) || value.committedCanonicalHash != sha256Digest(value.committedCanonical) ||
		!validHash(value.attemptHash) || !validHash(value.journalHeadHash) || !validHash(value.bootEpochHash) ||
		!validHash(value.claimHash) || !validHash(value.authorizationHash) || !validHash(value.approvalHash) ||
		!validHash(value.requestHash) || !validHash(value.dispatchHash) ||
		!validClosedIdentifier(value.slotID, maxSupervisorIntentIDBytes) || !validClosedIdentifier(value.bootEpochID, maxSupervisorIntentIDBytes) ||
		phaseSequence(value.phase) != value.sequence {
		return false
	}
	claim, err := decodeCanonicalRecord[SupervisorIntentClaim](value.committedCanonical)
	return err == nil && slotCommitMatchesClaim(value, claim) && recordHashMatches(claim, "claim_hash", claim.ClaimHash)
}

func slotCommitMatchesAuthority(value *VerifiedSupervisorClaimSlotCommit, expected SupervisorIntentAuthority, verified *VerifiedAuthorization) bool {
	return value.attemptHash == expected.AttemptHash && value.phase == expected.Phase && value.sequence == expected.Sequence &&
		value.slotID == expected.JournalSlotID && value.journalHeadHash == expected.JournalHeadHash &&
		value.bootEpochID == expected.BootEpochID && value.bootEpochHash == expected.BootEpochHash &&
		value.claimID == expected.ClaimID && value.authorizationHash == verified.authorization.AuthorizationHash &&
		value.approvalHash == verified.authorization.ApprovalHash && value.requestHash == expected.AcceptedDispatch.Request.RequestHash &&
		value.dispatchHash == expected.AcceptedDispatch.DispatchHash
}

func slotCommitMatchesClaim(value *VerifiedSupervisorClaimSlotCommit, claim SupervisorIntentClaim) bool {
	return value.attemptHash == claim.AttemptHash && value.phase == claim.Phase && value.sequence == claim.Sequence &&
		value.slotID == claim.JournalSlotID && value.journalHeadHash == claim.JournalHeadHash &&
		value.bootEpochID == claim.SupervisorBootEpochID && value.bootEpochHash == claim.SupervisorBootEpochHash &&
		value.claimID == claim.ClaimID && value.claimHash == claim.ClaimHash &&
		value.authorizationHash == claim.AuthorizationHash && value.approvalHash == claim.ApprovalHash &&
		value.requestHash == claim.RequestHash && value.dispatchHash == claim.DispatchHash
}

func cloneSupervisorClaimSlotCommit(value VerifiedSupervisorClaimSlotCommit) VerifiedSupervisorClaimSlotCommit {
	value.committedCanonical = append([]byte(nil), value.committedCanonical...)
	return value
}

func supervisorTerminalEventIntegrityHash(value *VerifiedSupervisorTerminalEvent) string {
	if value == nil {
		return ""
	}
	record := supervisorTerminalEventIntegrity{
		Valid: value.valid, TerminalVerified: value.terminalVerified, AttemptHash: value.attemptHash,
		Phase: value.phase, Sequence: value.sequence, ClaimHash: value.claimHash, TerminalEventHash: value.terminalEventHash,
		BootEpochHash: value.bootEpochHash, SlotID: value.slotID, ClaimJournalHeadHash: value.claimJournalHeadHash,
		TerminalJournalHeadHash: value.terminalJournalHeadHash, TerminalStatus: value.terminalStatus,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedSupervisorTerminalEventIntact(value *VerifiedSupervisorTerminalEvent) bool {
	return value != nil && value.valid && value.terminalVerified && value.terminalStatus == "succeeded" &&
		value.sequence >= 1 && value.sequence < 3 && phaseSequence(value.phase) == value.sequence &&
		validHash(value.integrityHash) && value.integrityHash == supervisorTerminalEventIntegrityHash(value) &&
		validHash(value.attemptHash) && validHash(value.claimHash) && validHash(value.terminalEventHash) &&
		validHash(value.bootEpochHash) && validHash(value.claimJournalHeadHash) && validHash(value.terminalJournalHeadHash) &&
		validClosedIdentifier(value.slotID, maxSupervisorIntentIDBytes)
}

func verifiedSupervisorIntentClaimIntact(value *VerifiedSupervisorIntentClaim) bool {
	if value == nil || !value.valid || !verifiedSupervisorClaimSlotCommitIntact(&value.slotCommit) {
		return false
	}
	canonical, err := canonicalBytes(value.claim)
	return err == nil && bytes.Equal(canonical, value.canonical) &&
		bytes.Equal(value.slotCommit.committedCanonical, value.canonical) && slotCommitMatchesClaim(&value.slotCommit, value.claim) &&
		recordHashMatches(value.claim, "claim_hash", value.claim.ClaimHash)
}

// SupervisorRecoveryRecord is canonical data, not durability authority. Only
// an intact opaque VerifiedSupervisorRecoverySnapshot can establish that a
// canonical record was completely and unambiguously observed in the journal.
type SupervisorRecoveryRecord struct {
	SchemaVersion           string                       `json:"schema_version"`
	RecordHash              string                       `json:"record_hash"`
	RecordID                string                       `json:"record_id"`
	Sequence                int                          `json:"sequence"`
	Kind                    SupervisorRecoveryRecordKind `json:"record_kind"`
	AttemptHash             string                       `json:"attempt_hash"`
	AttemptNumber           int                          `json:"attempt_number"`
	AttemptCap              int                          `json:"attempt_cap"`
	Phase                   SupervisorIntentPhase        `json:"phase"`
	ClaimHash               string                       `json:"claim_hash"`
	SlotID                  string                       `json:"slot_id"`
	SupervisorBootEpochHash string                       `json:"supervisor_boot_epoch_hash"`
	JournalHeadHash         string                       `json:"journal_head_hash"`
	PredecessorRecordHash   string                       `json:"predecessor_record_hash"`
	DurabilityPolicyID      string                       `json:"durability_policy_id"`
	OccurredAt              string                       `json:"occurred_at"`
	SemanticPayloadHash     string                       `json:"semantic_payload_hash"`
}

type supervisorRecoverySnapshotIntegrity struct {
	IntegrityHash         string                  `json:"integrity_hash"`
	Valid                 bool                    `json:"valid"`
	JournalVerified       bool                    `json:"journal_verified"`
	DurabilityVerified    bool                    `json:"durability_verified"`
	CompletenessVerified  bool                    `json:"completeness_verified"`
	AmbiguityChecked      bool                    `json:"ambiguity_checked"`
	Unambiguous           bool                    `json:"unambiguous"`
	CurrentBootEpochHash  string                  `json:"current_boot_epoch_hash"`
	RecordedBootEpochHash string                  `json:"recorded_boot_epoch_hash"`
	AttemptHash           string                  `json:"attempt_hash"`
	AttemptNumber         int                     `json:"attempt_number"`
	AttemptCap            int                     `json:"attempt_cap"`
	Phase                 SupervisorIntentPhase   `json:"phase"`
	ClaimHash             string                  `json:"claim_hash"`
	SlotID                string                  `json:"slot_id"`
	JournalHeadHash       string                  `json:"journal_head_hash"`
	CutPoint              SupervisorCrashCutPoint `json:"cut_point"`
	RecordHashes          []string                `json:"record_hashes"`
	CanonicalRecordHashes []string                `json:"canonical_record_hashes"`
}

// VerifiedSupervisorRecoverySnapshot is opaque verified journal evidence. All
// fields are private. No production constructor or decoder exists; future
// minting belongs to trusted in-package journal verification or a separately
// reviewed seam.
type VerifiedSupervisorRecoverySnapshot struct {
	valid                 bool
	journalVerified       bool
	durabilityVerified    bool
	completenessVerified  bool
	ambiguityChecked      bool
	unambiguous           bool
	currentBootEpochHash  string
	recordedBootEpochHash string
	attemptHash           string
	attemptNumber         int
	attemptCap            int
	phase                 SupervisorIntentPhase
	claimHash             string
	slotID                string
	journalHeadHash       string
	cutPoint              SupervisorCrashCutPoint
	records               []SupervisorRecoveryRecord
	canonicalRecords      [][]byte
	canonicalRecordHashes []string
	integrityHash         string
}

// SupervisorRecoveryResult has no launch disposition. EffectAllowed is always
// false, including verified empty-journal status, current-epoch status, exact
// response replay, and every prior-epoch nonterminal state.
type SupervisorRecoveryResult struct {
	DurableObservation SupervisorDurableObservation
	Disposition        SupervisorRecoveryDisposition
	EffectAllowed      bool
	NextRequirement    SupervisorRecoveryRequirement
}

// DecodeSupervisorRecoveryRecord accepts only one closed RFC 8785/JCS record
// with a complete self-hash. Success does not establish journal durability.
func DecodeSupervisorRecoveryRecord(raw []byte) (SupervisorRecoveryRecord, error) {
	value, err := decodeCanonicalRecord[SupervisorRecoveryRecord](raw)
	if err != nil || validateSupervisorRecoveryRecord(value) != nil {
		return SupervisorRecoveryRecord{}, ErrInvalidSupervisorRecovery
	}
	return value, nil
}

func validateSupervisorRecoveryRecord(value SupervisorRecoveryRecord) error {
	if value.SchemaVersion != SupervisorRecoveryRecordSchemaVersion ||
		!validClosedIdentifier(value.RecordID, maxRecoveryRecordIDBytes) ||
		value.Sequence < 1 || value.Sequence > 5 || recoveryRecordKindForSequence(value.Sequence) != value.Kind ||
		value.AttemptNumber < 1 || value.AttemptNumber > AttemptCap || value.AttemptCap != AttemptCap ||
		phaseSequence(value.Phase) == 0 || !validClosedIdentifier(value.SlotID, maxSupervisorIntentIDBytes) ||
		value.DurabilityPolicyID != DurabilityPolicyFullFullFSync ||
		!validHash(value.RecordHash) || !validHash(value.AttemptHash) || !validHash(value.ClaimHash) ||
		!validHash(value.SupervisorBootEpochHash) || !validHash(value.JournalHeadHash) || !validHash(value.SemanticPayloadHash) ||
		!recordHashMatches(value, "record_hash", value.RecordHash) {
		return ErrInvalidSupervisorRecovery
	}
	if value.Sequence == 1 {
		if value.PredecessorRecordHash != "" {
			return ErrInvalidSupervisorRecovery
		}
	} else if !validHash(value.PredecessorRecordHash) {
		return ErrInvalidSupervisorRecovery
	}
	if _, err := parseUTC(value.OccurredAt); err != nil {
		return ErrInvalidSupervisorRecovery
	}
	return nil
}

func recoveryRecordKindForSequence(sequence int) SupervisorRecoveryRecordKind {
	switch sequence {
	case 1:
		return SupervisorRecoveryClaimCommitRecord
	case 2:
		return SupervisorRecoveryPhaseLaunchRecord
	case 3:
		return SupervisorRecoveryTerminalProofRecord
	case 4:
		return SupervisorRecoveryAttestationSignatureRecord
	case 5:
		return SupervisorRecoveryResponseRecord
	default:
		return ""
	}
}

func supervisorRecoverySnapshotIntegrityHash(value *VerifiedSupervisorRecoverySnapshot) string {
	if value == nil {
		return ""
	}
	recordHashes := make([]string, len(value.records))
	for index := range value.records {
		recordHashes[index] = value.records[index].RecordHash
	}
	record := supervisorRecoverySnapshotIntegrity{
		Valid: value.valid, JournalVerified: value.journalVerified, DurabilityVerified: value.durabilityVerified,
		CompletenessVerified: value.completenessVerified, AmbiguityChecked: value.ambiguityChecked, Unambiguous: value.unambiguous,
		CurrentBootEpochHash: value.currentBootEpochHash, RecordedBootEpochHash: value.recordedBootEpochHash,
		AttemptHash: value.attemptHash, AttemptNumber: value.attemptNumber, AttemptCap: value.attemptCap,
		Phase: value.phase, ClaimHash: value.claimHash, SlotID: value.slotID, JournalHeadHash: value.journalHeadHash,
		CutPoint: value.cutPoint, RecordHashes: recordHashes,
		CanonicalRecordHashes: append([]string(nil), value.canonicalRecordHashes...),
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedSupervisorRecoverySnapshotIntact(value *VerifiedSupervisorRecoverySnapshot) bool {
	if value == nil || !value.valid || !value.journalVerified || !value.durabilityVerified ||
		!value.completenessVerified || !value.ambiguityChecked || !value.unambiguous ||
		!validHash(value.integrityHash) || value.integrityHash != supervisorRecoverySnapshotIntegrityHash(value) ||
		!validHash(value.currentBootEpochHash) || !validHash(value.recordedBootEpochHash) ||
		!validHash(value.attemptHash) || !validHash(value.claimHash) || !validHash(value.journalHeadHash) ||
		value.attemptNumber < 1 || value.attemptNumber > AttemptCap || value.attemptCap != AttemptCap ||
		phaseSequence(value.phase) == 0 || !validClosedIdentifier(value.slotID, maxSupervisorIntentIDBytes) {
		return false
	}
	count, _, valid := recoveryPrefixForCutPoint(value.cutPoint)
	if !valid || len(value.records) != count || len(value.canonicalRecords) != count || len(value.canonicalRecordHashes) != count {
		return false
	}
	seenIDs := make(map[string]struct{}, count)
	seenRecordHashes := make(map[string]struct{}, count)
	seenCanonicalHashes := make(map[string]struct{}, count)
	seenJournalHeads := make(map[string]struct{}, count)
	seenPayloads := make(map[string]struct{}, count)
	var previousHash string
	var previousTime time.Time
	for index := range count {
		raw := value.canonicalRecords[index]
		if value.canonicalRecordHashes[index] != sha256Digest(raw) || !validHash(value.canonicalRecordHashes[index]) {
			return false
		}
		record, err := DecodeSupervisorRecoveryRecord(raw)
		if err != nil || record != value.records[index] || record.Sequence != index+1 ||
			record.AttemptHash != value.attemptHash || record.AttemptNumber != value.attemptNumber || record.AttemptCap != value.attemptCap ||
			record.Phase != value.phase || record.ClaimHash != value.claimHash || record.SlotID != value.slotID ||
			record.SupervisorBootEpochHash != value.recordedBootEpochHash || record.DurabilityPolicyID != DurabilityPolicyFullFullFSync ||
			record.PredecessorRecordHash != previousHash {
			return false
		}
		occurredAt, err := parseUTC(record.OccurredAt)
		if err != nil || index > 0 && !occurredAt.After(previousTime) {
			return false
		}
		if _, duplicate := seenIDs[record.RecordID]; duplicate {
			return false
		}
		if _, duplicate := seenRecordHashes[record.RecordHash]; duplicate {
			return false
		}
		if _, duplicate := seenCanonicalHashes[value.canonicalRecordHashes[index]]; duplicate {
			return false
		}
		if _, duplicate := seenJournalHeads[record.JournalHeadHash]; duplicate {
			return false
		}
		if _, duplicate := seenPayloads[record.SemanticPayloadHash]; duplicate {
			return false
		}
		seenIDs[record.RecordID] = struct{}{}
		seenRecordHashes[record.RecordHash] = struct{}{}
		seenCanonicalHashes[value.canonicalRecordHashes[index]] = struct{}{}
		seenJournalHeads[record.JournalHeadHash] = struct{}{}
		seenPayloads[record.SemanticPayloadHash] = struct{}{}
		previousHash = record.RecordHash
		previousTime = occurredAt
	}
	if count > 0 && value.records[count-1].JournalHeadHash != value.journalHeadHash {
		return false
	}
	return true
}

// ClassifySupervisorRecovery accepts only opaque verified journal evidence and
// a closed status/replay action. It performs no repair, persistence, signing,
// response write, launch, or replayed effect.
func ClassifySupervisorRecovery(snapshot *VerifiedSupervisorRecoverySnapshot, action SupervisorRecoveryAction) (SupervisorRecoveryResult, error) {
	if !verifiedSupervisorRecoverySnapshotIntact(snapshot) {
		return SupervisorRecoveryResult{}, ErrInvalidSupervisorRecovery
	}
	count, durable, valid := recoveryPrefixForCutPoint(snapshot.cutPoint)
	if !valid {
		return SupervisorRecoveryResult{}, ErrInvalidSupervisorRecovery
	}
	if count == 5 {
		if action != RecoveryReplayResponse {
			return SupervisorRecoveryResult{}, ErrInvalidSupervisorRecovery
		}
	} else if action != RecoveryStatusOnly {
		return SupervisorRecoveryResult{}, ErrInvalidSupervisorRecovery
	}

	result := SupervisorRecoveryResult{DurableObservation: durable}
	switch {
	case count == 0:
		result.Disposition = RecoveryNoDurableClaim
		result.NextRequirement = FreshLiveIntentRequired
	case count <= 2 && snapshot.recordedBootEpochHash != snapshot.currentBootEpochHash:
		result.Disposition = RecoveryWaitingForHuman
		result.NextRequirement = HumanReviewRequired
	case count <= 2:
		result.Disposition = RecoveryCurrentEpochStatusOnly
		result.NextRequirement = LiveCommitConfirmationRequired
	case count <= 3:
		result.Disposition = RecoveryTerminalStatus
		result.NextRequirement = AttestationStatusRequired
	case count == 4:
		result.Disposition = RecoveryTerminalStatus
		result.NextRequirement = ResponseStatusRequired
	case count == 5:
		result.Disposition = RecoveryResponseReplay
		result.NextRequirement = NoFurtherEffectPermitted
	default:
		return SupervisorRecoveryResult{}, ErrInvalidSupervisorRecovery
	}
	return result, nil
}

func recoveryPrefixForCutPoint(cutPoint SupervisorCrashCutPoint) (int, SupervisorDurableObservation, bool) {
	switch cutPoint {
	case BeforeClaimCommit:
		return 0, NoDurableSupervisorClaim, true
	case AfterClaimCommit, BeforePhaseLaunch:
		return 1, DurableClaimCommitted, true
	case AfterPhaseLaunch, BeforeTerminalProofPersistence:
		return 2, DurablePhaseLaunched, true
	case AfterTerminalProofPersistence, BeforeAttestationSignaturePersistence:
		return 3, DurableTerminalProofPersisted, true
	case AfterAttestationSignaturePersistence, BeforeResponsePersistence:
		return 4, DurableAttestationSignaturePersisted, true
	case AfterResponsePersistence:
		return 5, DurableResponsePersisted, true
	default:
		return 0, "", false
	}
}
