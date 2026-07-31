package repaircontract

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

const (
	SchemaCutoverRecordSchemaVersion    = "ananke.controlled-repair-schema-cutover.v1"
	SchemaCutoverAuthoritySchemaVersion = "ananke.controlled-repair-schema-cutover-authority.v1"
	SchemaCutoverSealSchemaVersion      = "ananke.controlled-repair-schema-cutover-seal.v1"

	schemaCutoverID            = "controlled_repair_schema_cutover_v1"
	acceptedStoreSchemaVersion = 14

	maxSchemaCutoverIDBytes = 128
)

// --- Closed string enums ---

type SchemaCutoverState string

const (
	SchemaCutoverCutoverAccepted SchemaCutoverState = "cutover_accepted"
)

type SchemaCutoverAction string

const (
	SchemaCutoverAdmitCutover SchemaCutoverAction = "admit_cutover"
	SchemaCutoverStatusOnly   SchemaCutoverAction = "status_only"
)

type SchemaCutoverDisposition string

const (
	SchemaCutoverRecordReady    SchemaCutoverDisposition = "record_ready"
	SchemaCutoverRetainedStatus SchemaCutoverDisposition = "retained_status"
)

type SchemaCutoverRequirement string

const (
	SchemaCutoverNextCutover SchemaCutoverRequirement = "cutover_accepted_no_effect"
	SchemaCutoverNoEffect    SchemaCutoverRequirement = "no_further_effect_permitted"
)

type SchemaCutoverSealKind string

const (
	StoreSchemaBindingVerification    SchemaCutoverSealKind = "store_schema_binding_verification"
	ContractSchemaBindingVerification SchemaCutoverSealKind = "contract_schema_binding_verification"
	RejectedSchemaForeignVerification SchemaCutoverSealKind = "rejected_schema_foreign_verification"
	RejectedAPIAbsenceVerification    SchemaCutoverSealKind = "rejected_api_absence_verification"
	BinaryPurityVerification          SchemaCutoverSealKind = "binary_purity_verification"
	ProtocolKeySeparationVerification SchemaCutoverSealKind = "protocol_key_separation_verification"
)

func validSchemaCutoverState(value SchemaCutoverState) bool {
	switch value {
	case SchemaCutoverCutoverAccepted:
		return true
	default:
		return false
	}
}

func validSchemaCutoverAction(value SchemaCutoverAction) bool {
	switch value {
	case SchemaCutoverAdmitCutover, SchemaCutoverStatusOnly:
		return true
	default:
		return false
	}
}

func validSchemaCutoverSealKind(value SchemaCutoverSealKind) bool {
	switch value {
	case StoreSchemaBindingVerification, ContractSchemaBindingVerification,
		RejectedSchemaForeignVerification, RejectedAPIAbsenceVerification,
		BinaryPurityVerification, ProtocolKeySeparationVerification:
		return true
	default:
		return false
	}
}

// --- Accepted/rejected/forbidden inventory ---

// acceptedContractSchemaVersions is the ordered list of canonical record schema
// versions that the accepted P6 contract (Slices 1–8) freezes. Each entry is a
// compile-time exported constant from the repaircontract package.
func acceptedContractSchemaVersions() []string {
	return []string{
		// Slices 1–2: trust bootstrap, authorization, dispatch
		ContractFixtureSchemaVersion,
		ReleasePinsSchemaVersion,
		TrustBundleSchemaVersion,
		AuthorizationSchemaVersion,
		ImmutableDispatchSchemaVersion,
		// Slice 3: supervisor intent
		SupervisorIntentClaimSchemaVersion,
		SupervisorAttemptIdentitySchemaVersion,
		// Slice 4: repository worktree
		RepositoryWorktreeObservationSchemaVersion,
		RepositoryWorktreeVerifierAuthoritySchemaVersion,
		// Slice 5: adapter sandbox
		AdapterSandboxObservationSchemaVersion,
		AdapterSandboxVerifierAuthoritySchemaVersion,
		// Slice 6: closed offline Go test profile
		GoTestProfileSchemaVersion,
		TestSandboxVerifierAuthoritySchemaVersion,
		// Slice 7: canonical repair-review attestation
		AttestationSchemaVersion,
		AttestationVerifierAuthoritySchemaVersion,
		// Slice 8: Ananke verification and persistence
		AnankeVerificationSchemaVersion,
		AnankeVerifierAuthoritySchemaVersion,
	}
}

// rejectedStoreSchemaVersions lists the unreleased P6 store migrations that
// were squashed. A populated local DB at any of these versions is foreign and
// must be rejected during migration instead of interpreted.
func rejectedStoreSchemaVersions() []int {
	return []int{15, 16}
}

// rejectedAPIMarkerList names the exported effect/evidence APIs from the
// rejected first candidate that have been removed from production binaries.
func rejectedAPIMarkerList() []string {
	return []string{
		"repairrunner_effect_dispatch",
		"repairrunner_evidence_persist",
		"unsigned_review_evidence_persist",
		"in_process_adapter_launch",
		"arbitrary_test_runner_launch",
	}
}

// forbiddenBinaryMarkerList names the markers that must not exist in production
// binaries. Their absence is proven by contract simulation.
func forbiddenBinaryMarkerList() []string {
	return []string{
		"in_process_adapter",
		"arbitrary_test_runner",
		"rejected_api_marker",
		"reused_p5_protocol_key",
	}
}

func storeSchemaVersionHash(version int) string {
	return fixedHash(fmt.Sprintf("controlled_repair_store_schema_v%d", version))
}

func canonicalStringArrayHash(values []string) string {
	if len(values) == 0 {
		return ""
	}
	raw, err := canonicalBytes(values)
	if err != nil {
		return ""
	}
	return sha256Digest(raw)
}

func canonicalIntArrayHash(values []int) string {
	if len(values) == 0 {
		return ""
	}
	raw, err := canonicalBytes(values)
	if err != nil {
		return ""
	}
	return sha256Digest(raw)
}

// --- Canonical record types ---

// SchemaCutoverRecord is the canonical, closed, self-hashed record that binds
// the accepted store schema version (14) to the accepted P6 contract, rejects
// unreleased v15/v16 schemas as foreign, proves rejected API markers are absent,
// and proves production binaries contain no forbidden markers. State is always
// exactly cutover_accepted. Raw DB insertion cannot produce this record — it can
// only be minted by the evaluator after full verification.
type SchemaCutoverRecord struct {
	SchemaVersion string             `json:"schema_version"`
	CutoverHash   string             `json:"cutover_hash"`
	CutoverID     string             `json:"cutover_id"`
	State         SchemaCutoverState `json:"state"`

	// Store schema binding
	AcceptedStoreSchemaVersion int    `json:"accepted_store_schema_version"`
	StoreSchemaVersionHash     string `json:"store_schema_version_hash"`

	// Accepted contract schema versions (Slices 1–8)
	AcceptedContractSchemas     []string `json:"accepted_contract_schemas"`
	AcceptedContractSchemasHash string   `json:"accepted_contract_schemas_hash"`

	// Rejected store schema versions (v15, v16 — unreleased)
	RejectedSchemaVersions     []int  `json:"rejected_schema_versions"`
	RejectedSchemaVersionsHash string `json:"rejected_schema_versions_hash"`

	// Rejected API markers (exported effect/evidence APIs that were removed)
	RejectedAPIMarkers     []string `json:"rejected_api_markers"`
	RejectedAPIMarkersHash string   `json:"rejected_api_markers_hash"`

	// Forbidden binary markers (must not exist in production binaries)
	ForbiddenBinaryMarkers     []string `json:"forbidden_binary_markers"`
	ForbiddenBinaryMarkersHash string   `json:"forbidden_binary_markers_hash"`

	// Protocol key separation: P5 protocol adapter SPKI hash that must NOT be
	// reused as the repair attestor key.
	ForbiddenProtocolAdapterSPKIHash string `json:"forbidden_protocol_adapter_spki_hash"`
	AcceptedRepairAttestorLeafSPKI   string `json:"accepted_repair_attestor_leaf_spki"`

	// Release pins binding
	ReleasePinsHash       string `json:"release_pins_hash"`
	VerifierAuthorityHash string `json:"verifier_authority_hash"`

	VerifiedAt string `json:"verified_at"`
}

// SchemaCutoverAuthority is the frozen, release-pinned verifier for the schema
// cutover. It is derived once at init and must match exactly.
type SchemaCutoverAuthority struct {
	SchemaVersion                    string                  `json:"schema_version"`
	CutoverAuthorityHash             string                  `json:"cutover_authority_hash"`
	CutoverID                        string                  `json:"cutover_id"`
	ReleasePinsHash                  string                  `json:"release_pins_hash"`
	AcceptedStoreSchemaVersion       int                     `json:"accepted_store_schema_version"`
	AcceptedContractSchemas          []string                `json:"accepted_contract_schemas"`
	RejectedSchemaVersions           []int                   `json:"rejected_schema_versions"`
	RejectedAPIMarkers               []string                `json:"rejected_api_markers"`
	ForbiddenBinaryMarkers           []string                `json:"forbidden_binary_markers"`
	ForbiddenProtocolAdapterSPKIHash string                  `json:"forbidden_protocol_adapter_spki_hash"`
	AcceptedRepairAttestorLeafSPKI   string                  `json:"accepted_repair_attestor_leaf_spki"`
	VerificationKinds                []SchemaCutoverSealKind `json:"verification_kinds"`
}

// SchemaCutoverSeal is a self-hashed provenance record.
type SchemaCutoverSeal struct {
	SchemaVersion        string                `json:"schema_version"`
	SealHash             string                `json:"seal_hash"`
	SealKind             SchemaCutoverSealKind `json:"seal_kind"`
	CutoverAuthorityHash string                `json:"cutover_authority_hash"`
	CutoverHash          string                `json:"cutover_hash"`
	EvidenceHash         string                `json:"evidence_hash"`
}

// VerifiedSchemaCutoverRecord is opaque evidence. Its private fields bind the
// cutover record, all verification seals, and the canonical bytes.
type VerifiedSchemaCutoverRecord struct {
	valid                         bool
	storeSchemaBindingVerified    bool
	contractSchemaBindingVerified bool
	rejectedSchemaForeignVerified bool
	rejectedAPIAbsenceVerified    bool
	binaryPurityVerified          bool
	protocolKeySeparationVerified bool
	record                        SchemaCutoverRecord
	canonical                     []byte
	canonicalHash                 string
	cutoverAuthorityHash          string
	storeSchemaBindingSeal        string
	contractSchemaBindingSeal     string
	rejectedSchemaForeignSeal     string
	rejectedAPIAbsenceSeal        string
	binaryPuritySeal              string
	protocolKeySeparationSeal     string
	integrityHash                 string
}

type verifiedSchemaCutoverRecordIntegrity struct {
	IntegrityHash                 string `json:"integrity_hash"`
	Valid                         bool   `json:"valid"`
	StoreSchemaBindingVerified    bool   `json:"store_schema_binding_verified"`
	ContractSchemaBindingVerified bool   `json:"contract_schema_binding_verified"`
	RejectedSchemaForeignVerified bool   `json:"rejected_schema_foreign_verified"`
	RejectedAPIAbsenceVerified    bool   `json:"rejected_api_absence_verified"`
	BinaryPurityVerified          bool   `json:"binary_purity_verified"`
	ProtocolKeySeparationVerified bool   `json:"protocol_key_separation_verified"`
	CutoverHash                   string `json:"cutover_hash"`
	CanonicalHash                 string `json:"canonical_hash"`
	CutoverAuthorityHash          string `json:"cutover_authority_hash"`
	StoreSchemaBindingSeal        string `json:"store_schema_binding_seal"`
	ContractSchemaBindingSeal     string `json:"contract_schema_binding_seal"`
	RejectedSchemaForeignSeal     string `json:"rejected_schema_foreign_seal"`
	RejectedAPIAbsenceSeal        string `json:"rejected_api_absence_seal"`
	BinaryPuritySeal              string `json:"binary_purity_seal"`
	ProtocolKeySeparationSeal     string `json:"protocol_key_separation_seal"`
}

// SchemaCutoverAssessment is classification only. EffectAllowed is always false.
type SchemaCutoverAssessment struct {
	Disposition     SchemaCutoverDisposition
	EffectAllowed   bool
	NextRequirement SchemaCutoverRequirement
}

// VerifiedSchemaCutoverCapability is an opaque terminal capability. It grants
// no filesystem, process, or effect authority. It represents the durable,
// re-verifiable record that the schema cutover is accepted.
type VerifiedSchemaCutoverCapability struct {
	valid                      bool
	cutoverHash                string
	recordIntegrityHash        string
	cutoverAuthorityHash       string
	cutoverSealsHash           string
	acceptedStoreSchemaVersion int
	releasePinsHash            string
	canonical                  []byte
	canonicalHash              string
	integrityHash              string
}

type verifiedSchemaCutoverCapabilityIntegrity struct {
	IntegrityHash              string `json:"integrity_hash"`
	Valid                      bool   `json:"valid"`
	CutoverHash                string `json:"cutover_hash"`
	RecordIntegrityHash        string `json:"record_integrity_hash"`
	CutoverAuthorityHash       string `json:"cutover_authority_hash"`
	CutoverSealsHash           string `json:"cutover_seals_hash"`
	AcceptedStoreSchemaVersion int    `json:"accepted_store_schema_version"`
	ReleasePinsHash            string `json:"release_pins_hash"`
	CanonicalHash              string `json:"canonical_hash"`
}

// --- Errors ---

var ErrInvalidSchemaCutover = errors.New("invalid schema cutover")

// --- Frozen compiled values ---

var frozenSchemaCutoverAuthority = mustDeriveSchemaCutoverAuthority()

func mustDeriveSchemaCutoverAuthority() SchemaCutoverAuthority {
	pins := FrozenReleasePins()
	authority := SchemaCutoverAuthority{
		SchemaVersion:                    SchemaCutoverAuthoritySchemaVersion,
		CutoverID:                        schemaCutoverID,
		ReleasePinsHash:                  pins.ReleasePinsHash,
		AcceptedStoreSchemaVersion:       acceptedStoreSchemaVersion,
		AcceptedContractSchemas:          acceptedContractSchemaVersions(),
		RejectedSchemaVersions:           rejectedStoreSchemaVersions(),
		RejectedAPIMarkers:               rejectedAPIMarkerList(),
		ForbiddenBinaryMarkers:           forbiddenBinaryMarkerList(),
		ForbiddenProtocolAdapterSPKIHash: ProtocolAdapterLeafSPKIHash,
		AcceptedRepairAttestorLeafSPKI:   pins.RepairAttestorLeafSPKI,
		VerificationKinds: []SchemaCutoverSealKind{
			StoreSchemaBindingVerification,
			ContractSchemaBindingVerification,
			RejectedSchemaForeignVerification,
			RejectedAPIAbsenceVerification,
			BinaryPurityVerification,
			ProtocolKeySeparationVerification,
		},
	}
	hash, err := hashRecord(authority, "cutover_authority_hash")
	if err != nil {
		panic(err)
	}
	authority.CutoverAuthorityHash = hash
	return authority
}

func FrozenSchemaCutoverAuthority() SchemaCutoverAuthority {
	return frozenSchemaCutoverAuthority
}

func deriveFrozenSchemaCutoverAuthority() (SchemaCutoverAuthority, error) {
	derived := mustDeriveSchemaCutoverAuthority()
	frozen := FrozenSchemaCutoverAuthority()
	if !reflect.DeepEqual(derived, frozen) {
		return SchemaCutoverAuthority{}, ErrInvalidSchemaCutover
	}
	return frozen, nil
}

// --- Seal derivation ---

type schemaCutoverSealSet struct {
	storeSchemaBinding    string
	contractSchemaBinding string
	rejectedSchemaForeign string
	rejectedAPIAbsence    string
	binaryPurity          string
	protocolKeySeparation string
}

func (seals schemaCutoverSealSet) ordered() []string {
	return []string{
		seals.storeSchemaBinding, seals.contractSchemaBinding,
		seals.rejectedSchemaForeign, seals.rejectedAPIAbsence,
		seals.binaryPurity, seals.protocolKeySeparation,
	}
}

type schemaCutoverStoreSchemaBindingSealEvidence struct {
	AcceptedStoreSchemaVersion int    `json:"accepted_store_schema_version"`
	StoreSchemaVersionHash     string `json:"store_schema_version_hash"`
}

type schemaCutoverContractSchemaBindingSealEvidence struct {
	AcceptedContractSchemasHash string `json:"accepted_contract_schemas_hash"`
}

type schemaCutoverRejectedSchemaForeignSealEvidence struct {
	RejectedSchemaVersionsHash string `json:"rejected_schema_versions_hash"`
}

type schemaCutoverRejectedAPIAbsenceSealEvidence struct {
	RejectedAPIMarkersHash string `json:"rejected_api_markers_hash"`
}

type schemaCutoverBinaryPuritySealEvidence struct {
	ForbiddenBinaryMarkersHash string `json:"forbidden_binary_markers_hash"`
}

type schemaCutoverProtocolKeySeparationSealEvidence struct {
	ForbiddenProtocolAdapterSPKIHash string `json:"forbidden_protocol_adapter_spki_hash"`
	AcceptedRepairAttestorLeafSPKI   string `json:"accepted_repair_attestor_leaf_spki"`
}

func schemaCutoverSealEvidenceHash(kind SchemaCutoverSealKind, record SchemaCutoverRecord) string {
	var evidence any
	switch kind {
	case StoreSchemaBindingVerification:
		evidence = schemaCutoverStoreSchemaBindingSealEvidence{
			AcceptedStoreSchemaVersion: record.AcceptedStoreSchemaVersion,
			StoreSchemaVersionHash:     record.StoreSchemaVersionHash,
		}
	case ContractSchemaBindingVerification:
		evidence = schemaCutoverContractSchemaBindingSealEvidence{
			AcceptedContractSchemasHash: record.AcceptedContractSchemasHash,
		}
	case RejectedSchemaForeignVerification:
		evidence = schemaCutoverRejectedSchemaForeignSealEvidence{
			RejectedSchemaVersionsHash: record.RejectedSchemaVersionsHash,
		}
	case RejectedAPIAbsenceVerification:
		evidence = schemaCutoverRejectedAPIAbsenceSealEvidence{
			RejectedAPIMarkersHash: record.RejectedAPIMarkersHash,
		}
	case BinaryPurityVerification:
		evidence = schemaCutoverBinaryPuritySealEvidence{
			ForbiddenBinaryMarkersHash: record.ForbiddenBinaryMarkersHash,
		}
	case ProtocolKeySeparationVerification:
		evidence = schemaCutoverProtocolKeySeparationSealEvidence{
			ForbiddenProtocolAdapterSPKIHash: record.ForbiddenProtocolAdapterSPKIHash,
			AcceptedRepairAttestorLeafSPKI:   record.AcceptedRepairAttestorLeafSPKI,
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

func deriveSchemaCutoverSeal(kind SchemaCutoverSealKind, authority SchemaCutoverAuthority, record SchemaCutoverRecord) string {
	seal := SchemaCutoverSeal{
		SchemaVersion:        SchemaCutoverSealSchemaVersion,
		SealKind:             kind,
		CutoverAuthorityHash: authority.CutoverAuthorityHash,
		CutoverHash:          record.CutoverHash,
		EvidenceHash:         schemaCutoverSealEvidenceHash(kind, record),
	}
	hash, err := hashRecord(seal, "seal_hash")
	if err != nil {
		return ""
	}
	seal.SealHash = hash
	raw, _ := canonicalBytes(seal)
	return sha256Digest(raw)
}

func deriveSchemaCutoverSeals(authority SchemaCutoverAuthority, record SchemaCutoverRecord) schemaCutoverSealSet {
	return schemaCutoverSealSet{
		storeSchemaBinding:    deriveSchemaCutoverSeal(StoreSchemaBindingVerification, authority, record),
		contractSchemaBinding: deriveSchemaCutoverSeal(ContractSchemaBindingVerification, authority, record),
		rejectedSchemaForeign: deriveSchemaCutoverSeal(RejectedSchemaForeignVerification, authority, record),
		rejectedAPIAbsence:    deriveSchemaCutoverSeal(RejectedAPIAbsenceVerification, authority, record),
		binaryPurity:          deriveSchemaCutoverSeal(BinaryPurityVerification, authority, record),
		protocolKeySeparation: deriveSchemaCutoverSeal(ProtocolKeySeparationVerification, authority, record),
	}
}

func schemaCutoverSealsHash(seals schemaCutoverSealSet) string {
	ordered := seals.ordered()
	if len(ordered) != 6 {
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

func DecodeSchemaCutoverRecord(raw []byte) (SchemaCutoverRecord, error) {
	return decodeCanonicalRecord[SchemaCutoverRecord](raw)
}

// --- Validation ---

func validateSchemaCutoverRecord(value SchemaCutoverRecord) error {
	if value.SchemaVersion != SchemaCutoverRecordSchemaVersion ||
		!validSchemaCutoverState(value.State) ||
		value.State != SchemaCutoverCutoverAccepted ||
		!validClosedIdentifier(value.CutoverID, maxSchemaCutoverIDBytes) ||
		!validHash(value.CutoverHash) ||
		value.AcceptedStoreSchemaVersion < 1 ||
		!validHash(value.StoreSchemaVersionHash) ||
		len(value.AcceptedContractSchemas) == 0 ||
		!validHash(value.AcceptedContractSchemasHash) ||
		len(value.RejectedSchemaVersions) == 0 ||
		!validHash(value.RejectedSchemaVersionsHash) ||
		len(value.RejectedAPIMarkers) == 0 ||
		!validHash(value.RejectedAPIMarkersHash) ||
		len(value.ForbiddenBinaryMarkers) == 0 ||
		!validHash(value.ForbiddenBinaryMarkersHash) ||
		!validHash(value.ForbiddenProtocolAdapterSPKIHash) ||
		!validHash(value.AcceptedRepairAttestorLeafSPKI) ||
		!validHash(value.ReleasePinsHash) || !validHash(value.VerifierAuthorityHash) {
		return ErrInvalidSchemaCutover
	}
	for _, schema := range value.AcceptedContractSchemas {
		if schema == "" {
			return ErrInvalidSchemaCutover
		}
	}
	for _, version := range value.RejectedSchemaVersions {
		if version < 1 {
			return ErrInvalidSchemaCutover
		}
	}
	for _, marker := range value.RejectedAPIMarkers {
		if marker == "" {
			return ErrInvalidSchemaCutover
		}
	}
	for _, marker := range value.ForbiddenBinaryMarkers {
		if marker == "" {
			return ErrInvalidSchemaCutover
		}
	}
	if _, err := parseUTC(value.VerifiedAt); err != nil {
		return ErrInvalidSchemaCutover
	}
	// Protocol key separation: the forbidden P5 protocol adapter SPKI hash
	// must differ from the accepted repair attestor leaf SPKI.
	if value.ForbiddenProtocolAdapterSPKIHash == value.AcceptedRepairAttestorLeafSPKI {
		return ErrInvalidSchemaCutover
	}
	if !recordHashMatches(value, "cutover_hash", value.CutoverHash) {
		return ErrInvalidSchemaCutover
	}
	return nil
}

// --- Snapshot integrity ---

func verifiedSchemaCutoverRecordIntegrityHash(value *VerifiedSchemaCutoverRecord) string {
	if value == nil {
		return ""
	}
	record := verifiedSchemaCutoverRecordIntegrity{
		IntegrityHash:                 value.integrityHash,
		Valid:                         value.valid,
		StoreSchemaBindingVerified:    value.storeSchemaBindingVerified,
		ContractSchemaBindingVerified: value.contractSchemaBindingVerified,
		RejectedSchemaForeignVerified: value.rejectedSchemaForeignVerified,
		RejectedAPIAbsenceVerified:    value.rejectedAPIAbsenceVerified,
		BinaryPurityVerified:          value.binaryPurityVerified,
		ProtocolKeySeparationVerified: value.protocolKeySeparationVerified,
		CutoverHash:                   value.record.CutoverHash,
		CanonicalHash:                 value.canonicalHash,
		CutoverAuthorityHash:          value.cutoverAuthorityHash,
		StoreSchemaBindingSeal:        value.storeSchemaBindingSeal,
		ContractSchemaBindingSeal:     value.contractSchemaBindingSeal,
		RejectedSchemaForeignSeal:     value.rejectedSchemaForeignSeal,
		RejectedAPIAbsenceSeal:        value.rejectedAPIAbsenceSeal,
		BinaryPuritySeal:              value.binaryPuritySeal,
		ProtocolKeySeparationSeal:     value.protocolKeySeparationSeal,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedSchemaCutoverRecordIntact(value *VerifiedSchemaCutoverRecord, authority SchemaCutoverAuthority) bool {
	if value == nil || !value.valid || !value.storeSchemaBindingVerified ||
		!value.contractSchemaBindingVerified || !value.rejectedSchemaForeignVerified ||
		!value.rejectedAPIAbsenceVerified || !value.binaryPurityVerified ||
		!value.protocolKeySeparationVerified ||
		!validHash(value.integrityHash) || value.integrityHash != verifiedSchemaCutoverRecordIntegrityHash(value) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) ||
		value.cutoverAuthorityHash != authority.CutoverAuthorityHash ||
		!recordHashMatches(authority, "cutover_authority_hash", authority.CutoverAuthorityHash) {
		return false
	}
	decoded, err := DecodeSchemaCutoverRecord(value.canonical)
	if err != nil || !reflect.DeepEqual(decoded, value.record) {
		return false
	}
	if validateSchemaCutoverRecord(decoded) != nil {
		return false
	}
	seals := deriveSchemaCutoverSeals(authority, decoded)
	return value.storeSchemaBindingSeal == seals.storeSchemaBinding &&
		value.contractSchemaBindingSeal == seals.contractSchemaBinding &&
		value.rejectedSchemaForeignSeal == seals.rejectedSchemaForeign &&
		value.rejectedAPIAbsenceSeal == seals.rejectedAPIAbsence &&
		value.binaryPuritySeal == seals.binaryPurity &&
		value.protocolKeySeparationSeal == seals.protocolKeySeparation
}

// --- Capability integrity ---

func verifiedSchemaCutoverCapabilityIntegrityHash(value *VerifiedSchemaCutoverCapability) string {
	if value == nil {
		return ""
	}
	record := verifiedSchemaCutoverCapabilityIntegrity{
		IntegrityHash:              value.integrityHash,
		Valid:                      value.valid,
		CutoverHash:                value.cutoverHash,
		RecordIntegrityHash:        value.recordIntegrityHash,
		CutoverAuthorityHash:       value.cutoverAuthorityHash,
		CutoverSealsHash:           value.cutoverSealsHash,
		AcceptedStoreSchemaVersion: value.acceptedStoreSchemaVersion,
		ReleasePinsHash:            value.releasePinsHash,
		CanonicalHash:              value.canonicalHash,
	}
	hash, err := hashRecord(record, "integrity_hash")
	if err != nil {
		return ""
	}
	return hash
}

func verifiedSchemaCutoverCapabilityIntact(value *VerifiedSchemaCutoverCapability) bool {
	if value == nil || !value.valid || !validHash(value.integrityHash) ||
		value.integrityHash != verifiedSchemaCutoverCapabilityIntegrityHash(value) ||
		!validHash(value.cutoverHash) || !validHash(value.recordIntegrityHash) ||
		!validHash(value.cutoverAuthorityHash) || !validHash(value.cutoverSealsHash) ||
		value.acceptedStoreSchemaVersion < 1 ||
		!validHash(value.releasePinsHash) ||
		!validHash(value.canonicalHash) || value.canonicalHash != sha256Digest(value.canonical) {
		return false
	}
	authority, err := deriveFrozenSchemaCutoverAuthority()
	if err != nil || value.cutoverAuthorityHash != authority.CutoverAuthorityHash {
		return false
	}
	decoded, err := DecodeSchemaCutoverRecord(value.canonical)
	if err != nil || validateSchemaCutoverRecord(decoded) != nil {
		return false
	}
	if decoded.CutoverHash != value.cutoverHash ||
		decoded.AcceptedStoreSchemaVersion != value.acceptedStoreSchemaVersion ||
		decoded.ReleasePinsHash != value.releasePinsHash {
		return false
	}
	// Defense-in-depth: recompute all 6 cutover seals.
	seals := deriveSchemaCutoverSeals(authority, decoded)
	return value.cutoverSealsHash == schemaCutoverSealsHash(seals)
}

// --- Evaluator ---

// EvaluateSchemaCutover validates a schema cutover record and produces a
// durable, re-verifiable capability. It first re-establishes fresh release
// trust, derives the frozen schema cutover authority, checks the snapshot
// intact, cross-binds the record to the frozen authority values, verifies
// protocol key separation, and mints an opaque VerifiedSchemaCutoverCapability
// only if state is exactly cutover_accepted and all bindings match.
// EffectAllowed is always false; no production minter exists.
func EvaluateSchemaCutover(
	snapshot *VerifiedSchemaCutoverRecord,
	expectedPins ReleasePins,
	expectedBundle TrustBundle,
	now time.Time,
	action SchemaCutoverAction,
) (SchemaCutoverAssessment, *VerifiedSchemaCutoverCapability, error) {
	invalid := SchemaCutoverAssessment{}
	now = now.UTC()
	authority, err := deriveFrozenSchemaCutoverAuthority()
	if err != nil || VerifyReleaseTrust(expectedPins, expectedBundle, frozenRotation(), now) != nil {
		return invalid, nil, ErrInvalidSchemaCutover
	}
	if !verifiedSchemaCutoverRecordIntact(snapshot, authority) {
		return invalid, nil, ErrInvalidSchemaCutover
	}

	// Cross-bind the record's values to the frozen authority and release pins.
	record := snapshot.record
	if record.CutoverID != authority.CutoverID ||
		record.AcceptedStoreSchemaVersion != authority.AcceptedStoreSchemaVersion ||
		record.StoreSchemaVersionHash != storeSchemaVersionHash(authority.AcceptedStoreSchemaVersion) ||
		record.AcceptedContractSchemasHash != canonicalStringArrayHash(authority.AcceptedContractSchemas) ||
		!reflect.DeepEqual(record.AcceptedContractSchemas, authority.AcceptedContractSchemas) ||
		record.RejectedSchemaVersionsHash != canonicalIntArrayHash(authority.RejectedSchemaVersions) ||
		!reflect.DeepEqual(record.RejectedSchemaVersions, authority.RejectedSchemaVersions) ||
		record.RejectedAPIMarkersHash != canonicalStringArrayHash(authority.RejectedAPIMarkers) ||
		!reflect.DeepEqual(record.RejectedAPIMarkers, authority.RejectedAPIMarkers) ||
		record.ForbiddenBinaryMarkersHash != canonicalStringArrayHash(authority.ForbiddenBinaryMarkers) ||
		!reflect.DeepEqual(record.ForbiddenBinaryMarkers, authority.ForbiddenBinaryMarkers) ||
		record.ForbiddenProtocolAdapterSPKIHash != authority.ForbiddenProtocolAdapterSPKIHash ||
		record.AcceptedRepairAttestorLeafSPKI != authority.AcceptedRepairAttestorLeafSPKI ||
		record.AcceptedRepairAttestorLeafSPKI != expectedPins.RepairAttestorLeafSPKI ||
		record.ReleasePinsHash != expectedPins.ReleasePinsHash ||
		record.ReleasePinsHash != authority.ReleasePinsHash ||
		record.VerifierAuthorityHash != authority.CutoverAuthorityHash {
		return invalid, nil, ErrInvalidSchemaCutover
	}

	switch record.State {
	case SchemaCutoverCutoverAccepted:
		if action == SchemaCutoverStatusOnly {
			return SchemaCutoverAssessment{
				Disposition:     SchemaCutoverRetainedStatus,
				NextRequirement: SchemaCutoverNoEffect,
			}, nil, nil
		}
		if action != SchemaCutoverAdmitCutover ||
			!snapshot.storeSchemaBindingVerified || !snapshot.contractSchemaBindingVerified ||
			!snapshot.rejectedSchemaForeignVerified || !snapshot.rejectedAPIAbsenceVerified ||
			!snapshot.binaryPurityVerified || !snapshot.protocolKeySeparationVerified {
			return invalid, nil, ErrInvalidSchemaCutover
		}
		seals := deriveSchemaCutoverSeals(authority, record)
		capability := &VerifiedSchemaCutoverCapability{
			valid:                      true,
			cutoverHash:                record.CutoverHash,
			recordIntegrityHash:        snapshot.integrityHash,
			cutoverAuthorityHash:       authority.CutoverAuthorityHash,
			cutoverSealsHash:           schemaCutoverSealsHash(seals),
			acceptedStoreSchemaVersion: record.AcceptedStoreSchemaVersion,
			releasePinsHash:            record.ReleasePinsHash,
			canonical:                  append([]byte(nil), snapshot.canonical...),
			canonicalHash:              snapshot.canonicalHash,
		}
		capability.integrityHash = verifiedSchemaCutoverCapabilityIntegrityHash(capability)
		if !verifiedSchemaCutoverCapabilityIntact(capability) {
			return invalid, nil, ErrInvalidSchemaCutover
		}
		return SchemaCutoverAssessment{
			Disposition:     SchemaCutoverRecordReady,
			NextRequirement: SchemaCutoverNextCutover,
		}, capability, nil
	default:
		return invalid, nil, ErrInvalidSchemaCutover
	}
}
