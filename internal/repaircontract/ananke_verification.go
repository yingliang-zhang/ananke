package repaircontract

import (
	"errors"
	"reflect"
	"time"
)

const (
	AnankeVerificationSchemaVersion      = "ananke.controlled-repair-ananke-verification.v1"
	AnankeVerifierAuthoritySchemaVersion = "ananke.controlled-repair-ananke-verifier-authority.v1"
	AnankeVerificationSealSchemaVersion  = "ananke.controlled-repair-ananke-verification-seal.v1"

	anankeVerifierID              = "controlled_repair_ananke_verifier_v1"
	anankeVerificationStateReview = "waiting_for_review"

	maxAnankeVerificationIDBytes = 128
)

// --- Closed string enums ---

type AnankeVerificationState string

const (
	AnankeVerificationWaitingForReview AnankeVerificationState = "waiting_for_review"
)

type AnankeVerificationAction string

const (
	AnankeVerificationAdmitReview AnankeVerificationAction = "admit_for_review"
	AnankeVerificationStatusOnly  AnankeVerificationAction = "status_only"
)

type AnankeVerificationDisposition string

const (
	AnankeVerificationRecordReady    AnankeVerificationDisposition = "record_ready"
	AnankeVerificationRetainedStatus AnankeVerificationDisposition = "retained_status"
)

type AnankeVerificationRequirement string

const (
	AnankeVerificationNextHuman AnankeVerificationRequirement = "human_review_required"
	AnankeVerificationNoEffect  AnankeVerificationRequirement = "no_further_effect_permitted"
)

type AnankeVerificationKind string

const (
	SignatureVerification           AnankeVerificationKind = "signature_verification"
	SignerRoleVerification          AnankeVerificationKind = "signer_role_verification"
	CertificateValidityVerification AnankeVerificationKind = "certificate_validity_verification"
	FreshnessVerification           AnankeVerificationKind = "freshness_verification"
	ChannelVerification             AnankeVerificationKind = "channel_verification"
	RequestBindingVerification      AnankeVerificationKind = "request_binding_verification"
	HeadConsistencyVerification     AnankeVerificationKind = "head_consistency_verification"
)

func validAnankeVerificationState(value AnankeVerificationState) bool {
	switch value {
	case AnankeVerificationWaitingForReview:
		return true
	default:
		return false
	}
}

func validAnankeVerificationAction(value AnankeVerificationAction) bool {
	switch value {
	case AnankeVerificationAdmitReview, AnankeVerificationStatusOnly:
		return true
	default:
		return false
	}
}

func validAnankeVerificationKind(value AnankeVerificationKind) bool {
	switch value {
	case SignatureVerification, SignerRoleVerification, CertificateValidityVerification,
		FreshnessVerification, ChannelVerification, RequestBindingVerification,
		HeadConsistencyVerification:
		return true
	default:
		return false
	}
}

// --- Canonical record types ---

// AnankeAttestationRecord is the canonical, closed, self-hashed record that
// Ananke persists after verifying a signed repair-review attestation from the
// supervisor. It binds the attestation hash, the verification state (always
// waiting_for_review), the verifier authority, and all verification seals.
// State is always exactly waiting_for_review. Raw DB insertion cannot produce
// this record — it can only be minted by the evaluator after full verification.
type AnankeAttestationRecord struct {
	SchemaVersion    string                  `json:"schema_version"`
	VerificationHash string                  `json:"verification_hash"`
	VerificationID   string                  `json:"verification_id"`
	AttestationHash  string                  `json:"attestation_hash"`
	VerifiedAt       string                  `json:"verified_at"`
	State            AnankeVerificationState `json:"state"`
	// Release pins binding
	ReleasePinsHash       string `json:"release_pins_hash"`
	VerifierAuthorityHash string `json:"verifier_authority_hash"`
	// Signer binding (from attestation)
	RepairAttestorCertificateHash string `json:"repair_attestor_certificate_hash"`
	RepairAttestorRootID          string `json:"repair_attestor_root_id"`
	RepairAttestorLeafSPKI        string `json:"repair_attestor_leaf_spki"`
	// Freshness
	AttestationIssuedAt string `json:"attestation_issued_at"`
	FreshnessCheckedAt  string `json:"freshness_checked_at"`
	// Channel
	RequestNonceHash  string `json:"request_nonce_hash"`
	ResponseNonceHash string `json:"response_nonce_hash"`
	ChannelHash       string `json:"channel_hash"`
	// Authorization binding
	AuthorizationHash string `json:"authorization_hash"`
	AttemptHash       string `json:"attempt_hash"`
	AttemptNumber     int    `json:"attempt_number"`
	// Head consistency
	SupervisorJournalHeadHash string `json:"supervisor_journal_head_hash"`
}

// AnankeVerifierAuthority is the frozen, release-pinned verifier for Ananke's
// attestation verification. It is derived once at init and must match exactly.
type AnankeVerifierAuthority struct {
	SchemaVersion         string                   `json:"schema_version"`
	VerifierAuthorityHash string                   `json:"verifier_authority_hash"`
	VerifierID            string                   `json:"verifier_id"`
	ReleasePinsHash       string                   `json:"release_pins_hash"`
	SignatureDomain       string                   `json:"signature_domain"`
	VerificationKinds     []AnankeVerificationKind `json:"verification_kinds"`
}

// AnankeVerificationSeal is a self-hashed provenance record.
type AnankeVerificationSeal struct {
	SchemaVersion         string                 `json:"schema_version"`
	SealHash              string                 `json:"seal_hash"`
	SealKind              AnankeVerificationKind `json:"seal_kind"`
	VerifierAuthorityHash string                 `json:"verifier_authority_hash"`
	VerificationHash      string                 `json:"verification_hash"`
	EvidenceHash          string                 `json:"evidence_hash"`
}

// VerifiedAnankeRecord is opaque evidence. Its private fields bind the
// verification record, all verification seals, and the canonical bytes.
type VerifiedAnankeRecord struct {
	valid                               bool
	signatureVerified                   bool
	signerRoleVerified                  bool
	certificateValidityVerified         bool
	freshnessVerified                   bool
	channelVerified                     bool
	requestBindingVerified              bool
	headConsistencyVerified             bool
	record                              AnankeAttestationRecord
	canonical                           []byte
	canonicalHash                       string
	verifierAuthorityHash               string
	signatureVerificationSeal           string
	signerRoleVerificationSeal          string
	certificateValidityVerificationSeal string
	freshnessVerificationSeal           string
	channelVerificationSeal             string
	requestBindingVerificationSeal      string
	headConsistencyVerificationSeal     string
	integrityHash                       string
}

type verifiedAnankeRecordIntegrity struct {
	IntegrityHash                       string `json:"integrity_hash"`
	Valid                               bool   `json:"valid"`
	SignatureVerified                   bool   `json:"signature_verified"`
	SignerRoleVerified                  bool   `json:"signer_role_verified"`
	CertificateValidityVerified         bool   `json:"certificate_validity_verified"`
	FreshnessVerified                   bool   `json:"freshness_verified"`
	ChannelVerified                     bool   `json:"channel_verified"`
	RequestBindingVerified              bool   `json:"request_binding_verified"`
	HeadConsistencyVerified             bool   `json:"head_consistency_verified"`
	VerificationHash                    string `json:"verification_hash"`
	CanonicalHash                       string `json:"canonical_hash"`
	VerifierAuthorityHash               string `json:"verifier_authority_hash"`
	SignatureVerificationSeal           string `json:"signature_verification_seal"`
	SignerRoleVerificationSeal          string `json:"signer_role_verification_seal"`
	CertificateValidityVerificationSeal string `json:"certificate_validity_verification_seal"`
	FreshnessVerificationSeal           string `json:"freshness_verification_seal"`
	ChannelVerificationSeal             string `json:"channel_verification_seal"`
	RequestBindingVerificationSeal      string `json:"request_binding_verification_seal"`
	HeadConsistencyVerificationSeal     string `json:"head_consistency_verification_seal"`
}

// AnankeVerificationAssessment is classification only. EffectAllowed is always false.
type AnankeVerificationAssessment struct {
	Disposition     AnankeVerificationDisposition
	EffectAllowed   bool
	NextRequirement AnankeVerificationRequirement
}

// VerifiedAnankeCapability is an opaque terminal capability. It grants no
// filesystem, process, or effect authority. It represents the durable,
// re-verifiable record that Ananke persists.
type VerifiedAnankeCapability struct {
	valid                 bool
	verificationHash      string
	recordIntegrityHash   string
	verifierAuthorityHash string
	verificationSealsHash string
	attestationHash       string
	authorizationHash     string
	canonical             []byte
	canonicalHash         string
	integrityHash         string
}

type verifiedAnankeCapabilityIntegrity struct {
	IntegrityHash         string `json:"integrity_hash"`
	Valid                 bool   `json:"valid"`
	VerificationHash      string `json:"verification_hash"`
	RecordIntegrityHash   string `json:"record_integrity_hash"`
	VerifierAuthorityHash string `json:"verifier_authority_hash"`
	VerificationSealsHash string `json:"verification_seals_hash"`
	AttestationHash       string `json:"attestation_hash"`
	AuthorizationHash     string `json:"authorization_hash"`
	CanonicalHash         string `json:"canonical_hash"`
}

// --- Errors ---

var ErrInvalidAnankeVerification = errors.New("invalid ananke verification")

// --- Frozen compiled values ---

var frozenAnankeVerifierAuthority = mustDeriveAnankeVerifierAuthority()

func mustDeriveAnankeVerifierAuthority() AnankeVerifierAuthority {
	pins := FrozenReleasePins()
	verifier := AnankeVerifierAuthority{
		SchemaVersion:   AnankeVerifierAuthoritySchemaVersion,
		VerifierID:      anankeVerifierID,
		ReleasePinsHash: pins.ReleasePinsHash,
		SignatureDomain: SignatureDomain,
		VerificationKinds: []AnankeVerificationKind{
			SignatureVerification,
			SignerRoleVerification,
			CertificateValidityVerification,
			FreshnessVerification,
			ChannelVerification,
			RequestBindingVerification,
			HeadConsistencyVerification,
		},
	}
	hash, err := hashRecord(verifier, "verifier_authority_hash")
	if err != nil {
		panic(err)
	}
	verifier.VerifierAuthorityHash = hash
	return verifier
}

func FrozenAnankeVerifierAuthority() AnankeVerifierAuthority {
	return frozenAnankeVerifierAuthority
}

func deriveFrozenAnankeVerifierAuthority() (AnankeVerifierAuthority, error) {
	derived := mustDeriveAnankeVerifierAuthority()
	frozen := FrozenAnankeVerifierAuthority()
	if !reflect.DeepEqual(derived, frozen) {
		return AnankeVerifierAuthority{}, ErrInvalidAnankeVerification
	}
	return frozen, nil
}

// --- Seal derivation ---

type anankeVerificationSealSet struct {
	signature           string
	signerRole          string
	certificateValidity string
	freshness           string
	channel             string
	requestBinding      string
	headConsistency     string
}

func (seals anankeVerificationSealSet) ordered() []string {
	return []string{
		seals.signature, seals.signerRole, seals.certificateValidity,
		seals.freshness, seals.channel, seals.requestBinding, seals.headConsistency,
	}
}

type anankeSignatureSealEvidence struct {
	AttestationHash string `json:"attestation_hash"`
	SignatureDomain string `json:"signature_domain"`
}

type anankeSignerRoleSealEvidence struct {
	RepairAttestorCertificateHash string `json:"repair_attestor_certificate_hash"`
	RepairAttestorRootID          string `json:"repair_attestor_root_id"`
	RepairAttestorLeafSPKI        string `json:"repair_attestor_leaf_spki"`
}

type anankeCertificateValiditySealEvidence struct {
	RepairAttestorCertificateHash string `json:"repair_attestor_certificate_hash"`
	VerifiedAt                    string `json:"verified_at"`
}

type anankeFreshnessSealEvidence struct {
	AttestationIssuedAt string `json:"attestation_issued_at"`
	FreshnessCheckedAt  string `json:"freshness_checked_at"`
}

type anankeChannelSealEvidence struct {
	RequestNonceHash  string `json:"request_nonce_hash"`
	ResponseNonceHash string `json:"response_nonce_hash"`
	ChannelHash       string `json:"channel_hash"`
}

type anankeRequestBindingSealEvidence struct {
	AuthorizationHash string `json:"authorization_hash"`
	AttemptHash       string `json:"attempt_hash"`
	AttemptNumber     int    `json:"attempt_number"`
}

type anankeHeadConsistencySealEvidence struct {
	SupervisorJournalHeadHash string `json:"supervisor_journal_head_hash"`
}

func anankeSealEvidenceHash(kind AnankeVerificationKind, record AnankeAttestationRecord) string {
	var evidence any
	switch kind {
	case SignatureVerification:
		evidence = anankeSignatureSealEvidence{
			AttestationHash: record.AttestationHash,
			SignatureDomain: SignatureDomain,
		}
	case SignerRoleVerification:
		evidence = anankeSignerRoleSealEvidence{
			RepairAttestorCertificateHash: record.RepairAttestorCertificateHash,
			RepairAttestorRootID:          record.RepairAttestorRootID,
			RepairAttestorLeafSPKI:        record.RepairAttestorLeafSPKI,
		}
	case CertificateValidityVerification:
		evidence = anankeCertificateValiditySealEvidence{
			RepairAttestorCertificateHash: record.RepairAttestorCertificateHash,
			VerifiedAt:                    record.VerifiedAt,
		}
	case FreshnessVerification:
		evidence = anankeFreshnessSealEvidence{
			AttestationIssuedAt: record.AttestationIssuedAt,
			FreshnessCheckedAt:  record.FreshnessCheckedAt,
		}
	case ChannelVerification:
		evidence = anankeChannelSealEvidence{
			RequestNonceHash:  record.RequestNonceHash,
			ResponseNonceHash: record.ResponseNonceHash,
			ChannelHash:       record.ChannelHash,
		}
	case RequestBindingVerification:
		evidence = anankeRequestBindingSealEvidence{
			AuthorizationHash: record.AuthorizationHash,
			AttemptHash:       record.AttemptHash,
			AttemptNumber:     record.AttemptNumber,
		}
	case HeadConsistencyVerification:
		evidence = anankeHeadConsistencySealEvidence{
			SupervisorJournalHeadHash: record.SupervisorJournalHeadHash,
		}
	default:
		return ""
	}
	canonical, err := canonicalBytes(evidence)
	if err != nil {
		return ""
	}
	return sha256Digest(canonical)
}

func deriveAnankeVerificationSeal(kind AnankeVerificationKind, verifier AnankeVerifierAuthority, record AnankeAttestationRecord) string {
	seal := AnankeVerificationSeal{
		SchemaVersion:         AnankeVerificationSealSchemaVersion,
		SealKind:              kind,
		VerifierAuthorityHash: verifier.VerifierAuthorityHash,
		VerificationHash:      record.VerificationHash,
		EvidenceHash:          anankeSealEvidenceHash(kind, record),
	}
	hash, err := hashRecord(seal, "seal_hash")
	if err != nil {
		return ""
	}
	seal.SealHash = hash
	raw, _ := canonicalBytes(seal)
	return sha256Digest(raw)
}

func deriveAnankeVerificationSeals(verifier AnankeVerifierAuthority, record AnankeAttestationRecord) anankeVerificationSealSet {
	return anankeVerificationSealSet{
		signature:           deriveAnankeVerificationSeal(SignatureVerification, verifier, record),
		signerRole:          deriveAnankeVerificationSeal(SignerRoleVerification, verifier, record),
		certificateValidity: deriveAnankeVerificationSeal(CertificateValidityVerification, verifier, record),
		freshness:           deriveAnankeVerificationSeal(FreshnessVerification, verifier, record),
		channel:             deriveAnankeVerificationSeal(ChannelVerification, verifier, record),
		requestBinding:      deriveAnankeVerificationSeal(RequestBindingVerification, verifier, record),
		headConsistency:     deriveAnankeVerificationSeal(HeadConsistencyVerification, verifier, record),
	}
}

func anankeVerificationSealsHash(seals anankeVerificationSealSet) string {
	ordered := seals.ordered()
	if len(ordered) != 7 {
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

// --- Decode ---

func DecodeAnankeAttestationRecord(raw []byte) (AnankeAttestationRecord, error) {
	return decodeCanonicalRecord[AnankeAttestationRecord](raw)
}

// --- Validation ---

func validateAnankeRecord(value AnankeAttestationRecord) error {
	if value.SchemaVersion != AnankeVerificationSchemaVersion ||
		!validAnankeVerificationState(value.State) ||
		value.State != AnankeVerificationWaitingForReview ||
		!validClosedIdentifier(value.VerificationID, maxAnankeVerificationIDBytes) ||
		!validHash(value.VerificationHash) ||
		!validHash(value.AttestationHash) ||
		!validHash(value.ReleasePinsHash) || !validHash(value.VerifierAuthorityHash) ||
		!validHash(value.RepairAttestorCertificateHash) ||
		!validClosedIdentifier(value.RepairAttestorRootID, maxAnankeVerificationIDBytes) ||
		!validHash(value.RepairAttestorLeafSPKI) ||
		!validHash(value.RequestNonceHash) || !validHash(value.ResponseNonceHash) || !validHash(value.ChannelHash) ||
		!validHash(value.AuthorizationHash) || !validHash(value.AttemptHash) ||
		value.AttemptNumber < 1 || value.AttemptNumber > AttemptCap ||
		!validHash(value.SupervisorJournalHeadHash) {
		return ErrInvalidAnankeVerification
	}
	if _, err := parseUTC(value.VerifiedAt); err != nil {
		return ErrInvalidAnankeVerification
	}
	if _, err := parseUTC(value.AttestationIssuedAt); err != nil {
		return ErrInvalidAnankeVerification
	}
	if _, err := parseUTC(value.FreshnessCheckedAt); err != nil {
		return ErrInvalidAnankeVerification
	}
	if !recordHashMatches(value, "verification_hash", value.VerificationHash) {
		return ErrInvalidAnankeVerification
	}
	return nil
}

// --- Snapshot integrity ---

func verifiedAnankeRecordIntegrityHash(value *VerifiedAnankeRecord) string {
	if value == nil {
		return ""
	}
	record := verifiedAnankeRecordIntegrity{
		IntegrityHash:                       value.integrityHash,
		Valid:                               value.valid,
		SignatureVerified:                   value.signatureVerified,
		SignerRoleVerified:                  value.signerRoleVerified,
		CertificateValidityVerified:         value.certificateValidityVerified,
		FreshnessVerified:                   value.freshnessVerified,
		ChannelVerified:                     value.channelVerified,
		RequestBindingVerified:              value.requestBindingVerified,
		HeadConsistencyVerified:             value.headConsistencyVerified,
		VerificationHash:                    value.record.VerificationHash,
		CanonicalHash:                       value.canonicalHash,
		VerifierAuthorityHash:               value.verifierAuthorityHash,
		SignatureVerificationSeal:           value.signatureVerificationSeal,
		SignerRoleVerificationSeal:          value.signerRoleVerificationSeal,
		CertificateValidityVerificationSeal: value.certificateValidityVerificationSeal,
		FreshnessVerificationSeal:           value.freshnessVerificationSeal,
		ChannelVerificationSeal:             value.channelVerificationSeal,
		RequestBindingVerificationSeal:      value.requestBindingVerificationSeal,
		HeadConsistencyVerificationSeal:     value.headConsistencyVerificationSeal,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedAnankeRecordIntact(value *VerifiedAnankeRecord, verifier AnankeVerifierAuthority) bool {
	if value == nil || !value.valid || !value.signatureVerified || !value.signerRoleVerified ||
		!value.certificateValidityVerified || !value.freshnessVerified || !value.channelVerified ||
		!value.requestBindingVerified || !value.headConsistencyVerified ||
		!validHash(value.integrityHash) || value.integrityHash != verifiedAnankeRecordIntegrityHash(value) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		value.verifierAuthorityHash != verifier.VerifierAuthorityHash ||
		!recordHashMatches(verifier, "verifier_authority_hash", verifier.VerifierAuthorityHash) {
		return false
	}
	decoded, err := DecodeAnankeAttestationRecord(value.canonical)
	if err != nil || !reflect.DeepEqual(decoded, value.record) {
		return false
	}
	if validateAnankeRecord(decoded) != nil {
		return false
	}
	seals := deriveAnankeVerificationSeals(verifier, decoded)
	return value.signatureVerificationSeal == seals.signature &&
		value.signerRoleVerificationSeal == seals.signerRole &&
		value.certificateValidityVerificationSeal == seals.certificateValidity &&
		value.freshnessVerificationSeal == seals.freshness &&
		value.channelVerificationSeal == seals.channel &&
		value.requestBindingVerificationSeal == seals.requestBinding &&
		value.headConsistencyVerificationSeal == seals.headConsistency
}

// --- Capability integrity ---

func verifiedAnankeCapabilityIntegrityHash(value *VerifiedAnankeCapability) string {
	if value == nil {
		return ""
	}
	record := verifiedAnankeCapabilityIntegrity{
		IntegrityHash:         value.integrityHash,
		Valid:                 value.valid,
		VerificationHash:      value.verificationHash,
		RecordIntegrityHash:   value.recordIntegrityHash,
		VerifierAuthorityHash: value.verifierAuthorityHash,
		VerificationSealsHash: value.verificationSealsHash,
		AttestationHash:       value.attestationHash,
		AuthorizationHash:     value.authorizationHash,
		CanonicalHash:         value.canonicalHash,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedAnankeCapabilityIntact(value *VerifiedAnankeCapability) bool {
	if value == nil || !value.valid || !validHash(value.integrityHash) ||
		value.integrityHash != verifiedAnankeCapabilityIntegrityHash(value) ||
		!validHash(value.verificationHash) || !validHash(value.recordIntegrityHash) ||
		!validHash(value.verifierAuthorityHash) || !validHash(value.verificationSealsHash) ||
		!validHash(value.attestationHash) || !validHash(value.authorizationHash) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) {
		return false
	}
	verifier, err := deriveFrozenAnankeVerifierAuthority()
	if err != nil || value.verifierAuthorityHash != verifier.VerifierAuthorityHash {
		return false
	}
	decoded, err := DecodeAnankeAttestationRecord(value.canonical)
	if err != nil || validateAnankeRecord(decoded) != nil {
		return false
	}
	if decoded.VerificationHash != value.verificationHash {
		return false
	}
	// Defense-in-depth: recompute all 7 verification seals.
	seals := deriveAnankeVerificationSeals(verifier, decoded)
	return value.verificationSealsHash == anankeVerificationSealsHash(seals)
}

// --- Evaluator ---

// EvaluateAnankeVerification validates a signed repair-review attestation from
// the supervisor and produces a durable, re-verifiable Ananke-side record. It
// first re-establishes fresh release trust, derives the frozen Ananke verifier
// authority, checks the attestation capability intact, verifies signer role
// and certificate validity against release pins, checks freshness, channel,
// request binding, and head consistency, and mints an opaque
// VerifiedAnankeCapability only if state is exactly waiting_for_review and all
// bindings match. EffectAllowed is always false; no production minter exists.
// Every read re-verifies the signature against release pins; raw DB insertion
// cannot produce an accepted state.
func EvaluateAnankeVerification(
	attestation *VerifiedAttestation,
	snapshot *VerifiedAnankeRecord,
	expectedPins ReleasePins,
	expectedBundle TrustBundle,
	now time.Time,
	action AnankeVerificationAction,
) (AnankeVerificationAssessment, *VerifiedAnankeCapability, error) {
	invalid := AnankeVerificationAssessment{}
	now = now.UTC()
	verifier, err := deriveFrozenAnankeVerifierAuthority()
	if err != nil || VerifyReleaseTrust(expectedPins, expectedBundle, frozenRotation(), now) != nil {
		return invalid, nil, ErrInvalidAnankeVerification
	}
	if !verifiedAttestationIntact(attestation) ||
		!verifiedAnankeRecordIntact(snapshot, verifier) {
		return invalid, nil, ErrInvalidAnankeVerification
	}

	// Verify the attestation's signer matches the release-pinned repair attestor.
	decoded, err := DecodeRepairReviewAttestation(attestation.canonical)
	if err != nil {
		return invalid, nil, ErrInvalidAnankeVerification
	}
	if decoded.SignatureDomain != SignatureDomain ||
		decoded.RepairAttestorCertificateHash != expectedBundle.RepairAttestor.CertificateHash ||
		decoded.RepairAttestorRootID != expectedBundle.RepairAttestor.IssuerRootID ||
		decoded.RepairAttestorLeafSPKI != expectedBundle.RepairAttestor.SubjectSPKISHA256 ||
		decoded.ReleasePinsHash != expectedPins.ReleasePinsHash ||
		decoded.TrustBundleHash != expectedBundle.TrustBundleHash {
		return invalid, nil, ErrInvalidAnankeVerification
	}

	// Verify the snapshot's record matches the attestation's binding hashes.
	// Cross-bind release pins hash and verifier authority hash to the frozen
	// values, not just form-validate them (K3 audit P4 fix, matching Slice 7
	// attestationSnapshotMatchesAuthority cross-binding pattern).
	record := snapshot.record
	if record.AttestationHash != attestation.attestationHash ||
		record.AuthorizationHash != attestation.authorizationHash ||
		record.ReleasePinsHash != expectedPins.ReleasePinsHash ||
		record.VerifierAuthorityHash != verifier.VerifierAuthorityHash ||
		record.RepairAttestorCertificateHash != decoded.RepairAttestorCertificateHash ||
		record.RepairAttestorRootID != decoded.RepairAttestorRootID ||
		record.RepairAttestorLeafSPKI != decoded.RepairAttestorLeafSPKI ||
		record.RequestNonceHash != decoded.RequestNonceHash ||
		record.ResponseNonceHash != decoded.ResponseNonceHash ||
		record.ChannelHash != decoded.ChannelHash ||
		record.AttestationIssuedAt != decoded.IssuedAt ||
		record.AuthorizationHash != decoded.AuthorizationHash ||
		record.AttemptHash != decoded.AttemptHash ||
		record.AttemptNumber != decoded.AttemptNumber ||
		record.SupervisorJournalHeadHash != decoded.SupervisorJournalHeadHash {
		return invalid, nil, ErrInvalidAnankeVerification
	}

	switch record.State {
	case AnankeVerificationWaitingForReview:
		if action == AnankeVerificationStatusOnly {
			return AnankeVerificationAssessment{
				Disposition:     AnankeVerificationRetainedStatus,
				NextRequirement: AnankeVerificationNoEffect,
			}, nil, nil
		}
		if action != AnankeVerificationAdmitReview ||
			!snapshot.signatureVerified || !snapshot.signerRoleVerified ||
			!snapshot.certificateValidityVerified || !snapshot.freshnessVerified ||
			!snapshot.channelVerified || !snapshot.requestBindingVerified ||
			!snapshot.headConsistencyVerified {
			return invalid, nil, ErrInvalidAnankeVerification
		}
		seals := deriveAnankeVerificationSeals(verifier, record)
		capability := &VerifiedAnankeCapability{
			valid:                 true,
			verificationHash:      record.VerificationHash,
			recordIntegrityHash:   snapshot.integrityHash,
			verifierAuthorityHash: verifier.VerifierAuthorityHash,
			verificationSealsHash: anankeVerificationSealsHash(seals),
			attestationHash:       record.AttestationHash,
			authorizationHash:     record.AuthorizationHash,
			canonical:             append([]byte(nil), snapshot.canonical...),
			canonicalHash:         snapshot.canonicalHash,
		}
		capability.integrityHash = verifiedAnankeCapabilityIntegrityHash(capability)
		if !verifiedAnankeCapabilityIntact(capability) {
			return invalid, nil, ErrInvalidAnankeVerification
		}
		return AnankeVerificationAssessment{
			Disposition:     AnankeVerificationRecordReady,
			NextRequirement: AnankeVerificationNextHuman,
		}, capability, nil
	default:
		return invalid, nil, ErrInvalidAnankeVerification
	}
}
