package repaircontract

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"io"
	"reflect"
	"time"
	"unicode/utf8"
)

const (
	publicTrustBundleSchemaVersion = "ananke.controlled-repair-public-trust-bundle.v1"
	supervisorPolicySchemaVersion  = "ananke.controlled-repair-supervisor-policy.v1"
	supervisorProfileSchemaVersion = "ananke.controlled-repair-supervisor-profile.v1"
	contractReleaseSchemaVersion   = "ananke.controlled-repair-contract-release.v1"
	releaseManifestSchemaVersion   = "ananke.controlled-repair-release-manifest.v1"
	rotationPolicySchemaVersion    = "ananke.controlled-repair-trust-rotation-policy.v1"

	repairRootID                 = "ananke_controlled_repair_attestation_root_v1"
	repairTrustBundleID          = "ananke_controlled_repair_public_trust_bundle_v1"
	releaseID                    = "p6_contract_slices_1_2_repair_batch_a1_v1"
	rotationPolicyID             = "controlled_repair_trust_rotation_policy_v1"
	rotationApproverRootID       = "ananke_controlled_repair_rotation_approver_root_v1"
	rotationApproverLeafSPKIHash = "sha256:95b82df9b281943f2229a0ab6d830c4b0133eda9bcec9eee8cffdd4f82db1d1d"
)

// These explicit embeds are the complete public release oracle. No wildcard can
// silently add a key or another installation-local file to the compiled trust
// boundary.

//go:embed testdata/release-v1/repair-root-cert.der
var embeddedRepairRootCertificateDER string

//go:embed testdata/release-v1/repair-root-spki.der
var embeddedRepairRootSPKIDER string

//go:embed testdata/release-v1/repair-attestor-cert.der
var embeddedRepairAttestorCertificateDER string

//go:embed testdata/release-v1/repair-attestor-spki.der
var embeddedRepairAttestorSPKIDER string

//go:embed testdata/release-v1/rotation-approver-root-cert.der
var embeddedRotationApproverRootCertificateDER string

//go:embed testdata/release-v1/rotation-approver-root-spki.der
var embeddedRotationApproverRootSPKIDER string

//go:embed testdata/release-v1/rotation-approver-cert.der
var embeddedRotationApproverCertificateDER string

//go:embed testdata/release-v1/rotation-approver-spki.der
var embeddedRotationApproverSPKIDER string

//go:embed testdata/release-v1/public-trust-bundle.json
var embeddedPublicTrustBundle string

//go:embed testdata/release-v1/supervisor-policy.json
var embeddedSupervisorPolicy string

//go:embed testdata/release-v1/supervisor-profile.json
var embeddedSupervisorProfile string

//go:embed testdata/release-v1/contract-release.json
var embeddedContractRelease string

//go:embed testdata/release-v1/rotation-policy.json
var embeddedRotationPolicy string

//go:embed testdata/release-v1/release-manifest.json
var embeddedReleaseManifest string

type releaseArtifactSet struct {
	contractRelease                    string
	publicTrustBundle                  string
	repairAttestorCertificateDER       string
	repairAttestorSPKIDER              string
	repairRootCertificateDER           string
	repairRootSPKIDER                  string
	rotationApproverCertificateDER     string
	rotationApproverSPKIDER            string
	rotationApproverRootCertificateDER string
	rotationApproverRootSPKIDER        string
	rotationPolicy                     string
	supervisorPolicy                   string
	supervisorProfile                  string
	releaseManifest                    string
}

func embeddedReleaseArtifactSet() releaseArtifactSet {
	return releaseArtifactSet{
		contractRelease:                    embeddedContractRelease,
		publicTrustBundle:                  embeddedPublicTrustBundle,
		repairAttestorCertificateDER:       embeddedRepairAttestorCertificateDER,
		repairAttestorSPKIDER:              embeddedRepairAttestorSPKIDER,
		repairRootCertificateDER:           embeddedRepairRootCertificateDER,
		repairRootSPKIDER:                  embeddedRepairRootSPKIDER,
		rotationApproverCertificateDER:     embeddedRotationApproverCertificateDER,
		rotationApproverSPKIDER:            embeddedRotationApproverSPKIDER,
		rotationApproverRootCertificateDER: embeddedRotationApproverRootCertificateDER,
		rotationApproverRootSPKIDER:        embeddedRotationApproverRootSPKIDER,
		rotationPolicy:                     embeddedRotationPolicy,
		supervisorPolicy:                   embeddedSupervisorPolicy,
		supervisorProfile:                  embeddedSupervisorProfile,
		releaseManifest:                    embeddedReleaseManifest,
	}
}

func (artifacts releaseArtifactSet) byID() map[string]string {
	return map[string]string{
		"contract_release":                       artifacts.contractRelease,
		"public_trust_bundle":                    artifacts.publicTrustBundle,
		"repair_attestor_certificate_der":        artifacts.repairAttestorCertificateDER,
		"repair_attestor_spki_der":               artifacts.repairAttestorSPKIDER,
		"repair_root_certificate_der":            artifacts.repairRootCertificateDER,
		"repair_root_spki_der":                   artifacts.repairRootSPKIDER,
		"rotation_approver_certificate_der":      artifacts.rotationApproverCertificateDER,
		"rotation_approver_spki_der":             artifacts.rotationApproverSPKIDER,
		"rotation_approver_root_certificate_der": artifacts.rotationApproverRootCertificateDER,
		"rotation_approver_root_spki_der":        artifacts.rotationApproverRootSPKIDER,
		"rotation_policy":                        artifacts.rotationPolicy,
		"supervisor_policy":                      artifacts.supervisorPolicy,
		"supervisor_profile":                     artifacts.supervisorProfile,
		"release_manifest":                       artifacts.releaseManifest,
	}
}

func (artifacts releaseArtifactSet) withArtifact(id, value string) (releaseArtifactSet, bool) {
	switch id {
	case "contract_release":
		artifacts.contractRelease = value
	case "public_trust_bundle":
		artifacts.publicTrustBundle = value
	case "repair_attestor_certificate_der":
		artifacts.repairAttestorCertificateDER = value
	case "repair_attestor_spki_der":
		artifacts.repairAttestorSPKIDER = value
	case "repair_root_certificate_der":
		artifacts.repairRootCertificateDER = value
	case "repair_root_spki_der":
		artifacts.repairRootSPKIDER = value
	case "rotation_approver_certificate_der":
		artifacts.rotationApproverCertificateDER = value
	case "rotation_approver_spki_der":
		artifacts.rotationApproverSPKIDER = value
	case "rotation_approver_root_certificate_der":
		artifacts.rotationApproverRootCertificateDER = value
	case "rotation_approver_root_spki_der":
		artifacts.rotationApproverRootSPKIDER = value
	case "rotation_policy":
		artifacts.rotationPolicy = value
	case "supervisor_policy":
		artifacts.supervisorPolicy = value
	case "supervisor_profile":
		artifacts.supervisorProfile = value
	case "release_manifest":
		artifacts.releaseManifest = value
	default:
		return artifacts, false
	}
	return artifacts, true
}

type publicTrustBundleDeclaration struct {
	SchemaVersion                      string `json:"schema_version"`
	BundleID                           string `json:"bundle_id"`
	RepairRootCertificateDER           string `json:"repair_root_certificate_der"`
	RepairRootSPKIDER                  string `json:"repair_root_spki_der"`
	RepairAttestorCertificateDER       string `json:"repair_attestor_certificate_der"`
	RepairAttestorSPKIDER              string `json:"repair_attestor_spki_der"`
	RotationApproverRootCertificateDER string `json:"rotation_approver_root_certificate_der"`
	RotationApproverRootSPKIDER        string `json:"rotation_approver_root_spki_der"`
	RotationApproverCertificateDER     string `json:"rotation_approver_certificate_der"`
	RotationApproverSPKIDER            string `json:"rotation_approver_spki_der"`
}

type descriptorObservationRequirements struct {
	SchemaVersion                string `json:"schema_version"`
	NoFollowRequired             bool   `json:"nofollow_required"`
	RegularFileRequired          bool   `json:"regular_file_required"`
	OwnerModeCheckRequired       bool   `json:"owner_mode_check_required"`
	DeviceInodeStabilityRequired bool   `json:"device_inode_stability_required"`
	ContentHashRequired          bool   `json:"content_hash_required"`
}

type supervisorPolicyDeclaration struct {
	SchemaVersion         string                            `json:"schema_version"`
	PolicyID              string                            `json:"policy_id"`
	TrustBootstrapMode    string                            `json:"trust_bootstrap_mode"`
	DatabaseTrustMode     string                            `json:"database_trust_mode"`
	RuntimeInstallMode    string                            `json:"runtime_install_mode"`
	VerifierSelection     string                            `json:"verifier_selection"`
	AnankeKeyCustody      string                            `json:"ananke_key_custody"`
	RotationMode          string                            `json:"rotation_mode"`
	DescriptorObservation descriptorObservationRequirements `json:"descriptor_observation"`
}

type supervisorProfileDeclaration struct {
	SchemaVersion      string `json:"schema_version"`
	ProfileID          string `json:"profile_id"`
	PolicyID           string `json:"policy_id"`
	TrustBundleID      string `json:"trust_bundle_id"`
	RepairAttestorRole string `json:"repair_attestor_role"`
	SignatureDomain    string `json:"signature_domain"`
	RotationPolicyID   string `json:"rotation_policy_id"`
}

type contractReleaseDeclaration struct {
	SchemaVersion      string `json:"schema_version"`
	ReleaseID          string `json:"release_id"`
	ContractID         string `json:"contract_id"`
	BuildMode          string `json:"build_mode"`
	PublicMaterialOnly bool   `json:"public_material_only"`
}

type releaseManifestEntry struct {
	ArtifactID    string `json:"artifact_id"`
	ContentSHA256 string `json:"content_sha256"`
}

type releaseManifestDeclaration struct {
	SchemaVersion string                 `json:"schema_version"`
	ReleaseID     string                 `json:"release_id"`
	Artifacts     []releaseManifestEntry `json:"artifacts"`
}

type rotationActivationRules struct {
	ActivationRequires            []string `json:"activation_requires"`
	CurrentRootNotAfter           string   `json:"current_root_not_after"`
	MinimumActivationDelaySeconds int      `json:"minimum_activation_delay_seconds"`
	MinimumOverlapSeconds         int      `json:"minimum_overlap_seconds"`
	NotBeforeRule                 string   `json:"not_before_rule"`
	OverlapRule                   string   `json:"overlap_rule"`
	SuccessorNotAfter             string   `json:"successor_not_after"`
}

type rotationSignatureDeclaration struct {
	RecordFields              []string `json:"record_fields"`
	SchemaVersion             string   `json:"schema_version"`
	SignatureDomain           string   `json:"signature_domain"`
	SignerRole                string   `json:"signer_role"`
	Algorithm                 string   `json:"algorithm"`
	SignatureEncoding         string   `json:"signature_encoding"`
	SignedObjectSchemaVersion string   `json:"signed_object_schema_version"`
	SignedObjectMembers       []string `json:"signed_object_members"`
}

type rotationReleaseApprovalDeclaration struct {
	RecordFields              []string `json:"record_fields"`
	SchemaVersion             string   `json:"schema_version"`
	SignatureDomain           string   `json:"signature_domain"`
	SignerRole                string   `json:"signer_role"`
	Algorithm                 string   `json:"algorithm"`
	SignerKeyID               string   `json:"signer_key_id"`
	SignerSPKISHA256          string   `json:"signer_spki_sha256"`
	SignatureEncoding         string   `json:"signature_encoding"`
	SignedObjectSchemaVersion string   `json:"signed_object_schema_version"`
	SignedObjectMembers       []string `json:"signed_object_members"`
	SignerSPKIMustDifferFrom  string   `json:"signer_spki_must_differ_from"`
}

type rotationPolicyDeclaration struct {
	SchemaVersion              string                             `json:"schema_version"`
	PolicyID                   string                             `json:"policy_id"`
	Canonicalization           string                             `json:"canonicalization"`
	ProposalSchemaVersion      string                             `json:"proposal_schema_version"`
	ProposalFields             []string                           `json:"proposal_fields"`
	CurrentRootCrossSignature  rotationSignatureDeclaration       `json:"current_root_cross_signature"`
	IndependentReleaseApproval rotationReleaseApprovalDeclaration `json:"independent_release_approval"`
	ActivationRules            rotationActivationRules            `json:"activation_rules"`
	V1FixtureState             string                             `json:"v1_fixture_state"`
}

type releaseMaterial struct {
	pins                 ReleasePins
	bundle               TrustBundle
	rotation             TrustRotation
	root                 *x509.Certificate
	leaf                 *x509.Certificate
	rotationApproverRoot *x509.Certificate
	rotationApprover     *x509.Certificate
}

var compiledRelease = mustDeriveReleaseMaterial(embeddedReleaseArtifactSet())

func mustDeriveReleaseMaterial(artifacts releaseArtifactSet) releaseMaterial {
	material, err := deriveReleaseMaterial(artifacts)
	if err != nil {
		panic("invalid embedded controlled-repair public release artifacts")
	}
	return material
}

// FrozenReleasePins returns pins derived only from exact embedded public bytes.
func FrozenReleasePins() ReleasePins {
	return compiledRelease.pins
}

// FrozenTrustBundle returns the certificate projection verified from embedded
// DER and SPKI bytes. The bundle hash is the exact public bundle content hash.
func FrozenTrustBundle() TrustBundle {
	return compiledRelease.bundle
}

func frozenRotation() TrustRotation {
	return compiledRelease.rotation
}

func verifyEmbeddedReleaseTrust(now time.Time) error {
	if verifyCertificateTime(compiledRelease.root, compiledRelease.leaf, now) != nil ||
		verifyCertificateTime(compiledRelease.rotationApproverRoot, compiledRelease.rotationApprover, now) != nil {
		return ErrInvalidContract
	}
	return nil
}

// VerifyReleaseTrust verifies only the compiled public release boundary. It
// accepts no dynamic authorization, request, fixture, or installation value.
func VerifyReleaseTrust(pins ReleasePins, bundle TrustBundle, rotation TrustRotation, now time.Time) error {
	now = now.UTC()
	if pins != FrozenReleasePins() || bundle != FrozenTrustBundle() || rotation != frozenRotation() ||
		verifyEmbeddedReleaseTrust(now) != nil || validateAllHashStrings(reflect.ValueOf(pins)) != nil ||
		!recordHashMatches(pins, "release_pins_hash", pins.ReleasePinsHash) ||
		!recordHashMatches(rotation, "rotation_hash", rotation.RotationHash) {
		return ErrInvalidContract
	}
	root := bundle.Root
	attestor := bundle.RepairAttestor
	approverRoot := bundle.RotationApproverRoot
	approver := bundle.RotationApprover
	if attestor.Role != RepairAttestorRole || attestor.Role == ProtocolAdapterRole ||
		attestor.SubjectSPKISHA256 == ProtocolAdapterLeafSPKIHash || attestor.IssuerRootID != root.RootID ||
		approver.KeyID != RotationApproverKeyID || approver.Role != RotationApproverRole ||
		approver.SignatureDomain != RotationApproverSignatureDomain || approver.IssuerRootID != approverRoot.RootID ||
		approver.Role == RepairAttestorRole || approver.Role == ProtocolAdapterRole ||
		approver.SubjectSPKISHA256 == ProtocolAdapterLeafSPKIHash ||
		pins.TrustBundleHash != bundle.TrustBundleHash || pins.RepairRootCertificateHash != root.RootHash ||
		pins.RepairRootSPKISHA256 != root.RootSPKISHA256 || pins.RepairAttestorCertificateHash != attestor.CertificateHash ||
		pins.RepairAttestorRootID != root.RootID || pins.RepairAttestorLeafSPKI != attestor.SubjectSPKISHA256 ||
		pins.RotationApproverRootCertificateHash != approverRoot.RootHash ||
		pins.RotationApproverRootSPKISHA256 != approverRoot.RootSPKISHA256 ||
		pins.RotationApproverCertificateHash != approver.CertificateHash ||
		pins.RotationApproverRootID != approverRoot.RootID || pins.RotationApproverKeyID != approver.KeyID ||
		pins.RotationApproverLeafSPKI != approver.SubjectSPKISHA256 || pins.RotationApproverRole != approver.Role ||
		pins.RotationApproverDomain != approver.SignatureDomain ||
		!distinctHashes(pins.TrustBundleHash, pins.RepairRootCertificateHash, pins.RepairRootSPKISHA256,
			pins.RepairAttestorCertificateHash, pins.RepairAttestorLeafSPKI,
			pins.RotationApproverRootCertificateHash, pins.RotationApproverRootSPKISHA256,
			pins.RotationApproverCertificateHash, pins.RotationApproverLeafSPKI, pins.ReleaseManifestHash,
			pins.BuildIdentityHash, pins.SupervisorPolicyHash, pins.SupervisorProfileHash, pins.RotationPolicyHash) {
		return ErrInvalidContract
	}
	if approverRoot.RootHash == root.RootHash || approverRoot.RootSPKISHA256 == root.RootSPKISHA256 ||
		approver.CertificateHash == attestor.CertificateHash || approver.SubjectSPKISHA256 == attestor.SubjectSPKISHA256 {
		return ErrInvalidContract
	}
	fixture := ContractFixture{TrustBundle: bundle}
	if err := validateTrustTimes(fixture, now); err != nil {
		return ErrInvalidContract
	}
	return nil
}

func verifyReleaseArtifactSet(artifacts releaseArtifactSet, expected ReleasePins, now time.Time) error {
	material, err := deriveReleaseMaterial(artifacts)
	if err != nil || material.pins != expected {
		return ErrInvalidContract
	}
	if verifyCertificateTime(material.root, material.leaf, now) != nil ||
		verifyCertificateTime(material.rotationApproverRoot, material.rotationApprover, now) != nil {
		return ErrInvalidContract
	}
	return nil
}

func deriveReleaseMaterial(artifacts releaseArtifactSet) (releaseMaterial, error) {
	var zero releaseMaterial
	bundleDeclaration, err := decodeCanonicalReleaseArtifact[publicTrustBundleDeclaration]([]byte(artifacts.publicTrustBundle))
	if err != nil || bundleDeclaration.SchemaVersion != publicTrustBundleSchemaVersion || bundleDeclaration.BundleID != repairTrustBundleID {
		return zero, ErrInvalidContract
	}
	policy, err := decodeCanonicalReleaseArtifact[supervisorPolicyDeclaration]([]byte(artifacts.supervisorPolicy))
	if err != nil || !reflect.DeepEqual(policy, expectedSupervisorPolicy()) {
		return zero, ErrInvalidContract
	}
	profile, err := decodeCanonicalReleaseArtifact[supervisorProfileDeclaration]([]byte(artifacts.supervisorProfile))
	if err != nil || profile != expectedSupervisorProfile() {
		return zero, ErrInvalidContract
	}
	release, err := decodeCanonicalReleaseArtifact[contractReleaseDeclaration]([]byte(artifacts.contractRelease))
	if err != nil || release != expectedContractRelease() {
		return zero, ErrInvalidContract
	}
	rotationPolicy, err := decodeCanonicalReleaseArtifact[rotationPolicyDeclaration]([]byte(artifacts.rotationPolicy))
	if err != nil || !reflect.DeepEqual(rotationPolicy, expectedRotationPolicy()) {
		return zero, ErrInvalidContract
	}
	manifest, err := decodeCanonicalReleaseArtifact[releaseManifestDeclaration]([]byte(artifacts.releaseManifest))
	if err != nil || !validReleaseManifest(manifest, artifacts) {
		return zero, ErrInvalidContract
	}

	rootDER, err := base64.StdEncoding.Strict().DecodeString(bundleDeclaration.RepairRootCertificateDER)
	if err != nil || !bytes.Equal(rootDER, []byte(artifacts.repairRootCertificateDER)) {
		return zero, ErrInvalidContract
	}
	rootSPKI, err := base64.StdEncoding.Strict().DecodeString(bundleDeclaration.RepairRootSPKIDER)
	if err != nil || !bytes.Equal(rootSPKI, []byte(artifacts.repairRootSPKIDER)) {
		return zero, ErrInvalidContract
	}
	leafDER, err := base64.StdEncoding.Strict().DecodeString(bundleDeclaration.RepairAttestorCertificateDER)
	if err != nil || !bytes.Equal(leafDER, []byte(artifacts.repairAttestorCertificateDER)) {
		return zero, ErrInvalidContract
	}
	leafSPKI, err := base64.StdEncoding.Strict().DecodeString(bundleDeclaration.RepairAttestorSPKIDER)
	if err != nil || !bytes.Equal(leafSPKI, []byte(artifacts.repairAttestorSPKIDER)) {
		return zero, ErrInvalidContract
	}
	approverRootDER, err := base64.StdEncoding.Strict().DecodeString(bundleDeclaration.RotationApproverRootCertificateDER)
	if err != nil || !bytes.Equal(approverRootDER, []byte(artifacts.rotationApproverRootCertificateDER)) {
		return zero, ErrInvalidContract
	}
	approverRootSPKI, err := base64.StdEncoding.Strict().DecodeString(bundleDeclaration.RotationApproverRootSPKIDER)
	if err != nil || !bytes.Equal(approverRootSPKI, []byte(artifacts.rotationApproverRootSPKIDER)) {
		return zero, ErrInvalidContract
	}
	approverDER, err := base64.StdEncoding.Strict().DecodeString(bundleDeclaration.RotationApproverCertificateDER)
	if err != nil || !bytes.Equal(approverDER, []byte(artifacts.rotationApproverCertificateDER)) {
		return zero, ErrInvalidContract
	}
	approverSPKI, err := base64.StdEncoding.Strict().DecodeString(bundleDeclaration.RotationApproverSPKIDER)
	if err != nil || !bytes.Equal(approverSPKI, []byte(artifacts.rotationApproverSPKIDER)) {
		return zero, ErrInvalidContract
	}

	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		return zero, ErrInvalidContract
	}
	leaf, err := x509.ParseCertificate(leafDER)
	if err != nil || validateCertificateSemantics(root, leaf, rootSPKI, leafSPKI) != nil {
		return zero, ErrInvalidContract
	}
	approverRoot, err := x509.ParseCertificate(approverRootDER)
	if err != nil {
		return zero, ErrInvalidContract
	}
	approver, err := x509.ParseCertificate(approverDER)
	if err != nil || validateRotationApproverCertificateSemantics(approverRoot, approver, root, leaf, approverRootSPKI, approverSPKI) != nil ||
		rotationPolicy.IndependentReleaseApproval.SignerKeyID != RotationApproverKeyID ||
		rotationPolicy.IndependentReleaseApproval.SignerSPKISHA256 != sha256Digest(approver.RawSubjectPublicKeyInfo) ||
		rotationPolicy.IndependentReleaseApproval.SignerRole != RotationApproverRole ||
		rotationPolicy.IndependentReleaseApproval.SignatureDomain != RotationApproverSignatureDomain {
		return zero, ErrInvalidContract
	}

	bundle := TrustBundle{
		SchemaVersion:   TrustBundleSchemaVersion,
		TrustBundleHash: sha256Digest([]byte(artifacts.publicTrustBundle)),
		BundleID:        bundleDeclaration.BundleID,
		Root: TrustRoot{
			SchemaVersion:   TrustRootSchemaVersion,
			RootHash:        sha256Digest(root.Raw),
			RootID:          repairRootID,
			RootSPKISHA256:  sha256Digest(root.RawSubjectPublicKeyInfo),
			ValidFrom:       root.NotBefore.UTC().Format(time.RFC3339Nano),
			NotAfter:        root.NotAfter.UTC().Format(time.RFC3339Nano),
			RevocationState: "not_revoked",
		},
		RepairAttestor: RepairAttestorCertificate{
			SchemaVersion:     RepairAttestorCertificateSchemaVersion,
			CertificateHash:   sha256Digest(leaf.Raw),
			Role:              RepairAttestorRole,
			SubjectSPKISHA256: sha256Digest(leaf.RawSubjectPublicKeyInfo),
			IssuerRootID:      repairRootID,
			ValidFrom:         leaf.NotBefore.UTC().Format(time.RFC3339Nano),
			NotAfter:          leaf.NotAfter.UTC().Format(time.RFC3339Nano),
			RevocationState:   "not_revoked",
		},
		RotationApproverRoot: TrustRoot{
			SchemaVersion:   TrustRootSchemaVersion,
			RootHash:        sha256Digest(approverRoot.Raw),
			RootID:          rotationApproverRootID,
			RootSPKISHA256:  sha256Digest(approverRoot.RawSubjectPublicKeyInfo),
			ValidFrom:       approverRoot.NotBefore.UTC().Format(time.RFC3339Nano),
			NotAfter:        approverRoot.NotAfter.UTC().Format(time.RFC3339Nano),
			RevocationState: "not_revoked",
		},
		RotationApprover: RotationApproverCertificate{
			SchemaVersion:     RotationApproverCertificateSchemaVersion,
			CertificateHash:   sha256Digest(approver.Raw),
			KeyID:             RotationApproverKeyID,
			Role:              RotationApproverRole,
			SignatureDomain:   RotationApproverSignatureDomain,
			SubjectSPKISHA256: sha256Digest(approver.RawSubjectPublicKeyInfo),
			IssuerRootID:      rotationApproverRootID,
			ValidFrom:         approver.NotBefore.UTC().Format(time.RFC3339Nano),
			NotAfter:          approver.NotAfter.UTC().Format(time.RFC3339Nano),
			RevocationState:   "not_revoked",
		},
	}
	pins := ReleasePins{
		SchemaVersion:                       ReleasePinsSchemaVersion,
		TrustBundleHash:                     bundle.TrustBundleHash,
		RepairRootCertificateHash:           bundle.Root.RootHash,
		RepairRootSPKISHA256:                bundle.Root.RootSPKISHA256,
		RepairAttestorCertificateHash:       bundle.RepairAttestor.CertificateHash,
		RepairAttestorRootID:                repairRootID,
		RepairAttestorLeafSPKI:              bundle.RepairAttestor.SubjectSPKISHA256,
		RotationApproverRootCertificateHash: bundle.RotationApproverRoot.RootHash,
		RotationApproverRootSPKISHA256:      bundle.RotationApproverRoot.RootSPKISHA256,
		RotationApproverCertificateHash:     bundle.RotationApprover.CertificateHash,
		RotationApproverRootID:              rotationApproverRootID,
		RotationApproverKeyID:               RotationApproverKeyID,
		RotationApproverLeafSPKI:            bundle.RotationApprover.SubjectSPKISHA256,
		RotationApproverRole:                RotationApproverRole,
		RotationApproverDomain:              RotationApproverSignatureDomain,
		ReleaseManifestHash:                 sha256Digest([]byte(artifacts.releaseManifest)),
		BuildIdentityHash:                   sha256Digest([]byte(artifacts.contractRelease)),
		SupervisorPolicyHash:                sha256Digest([]byte(artifacts.supervisorPolicy)),
		SupervisorProfileHash:               sha256Digest([]byte(artifacts.supervisorProfile)),
		RotationPolicyHash:                  sha256Digest([]byte(artifacts.rotationPolicy)),
		SignatureDomain:                     SignatureDomain,
		TrustBootstrapMode:                  policy.TrustBootstrapMode,
		DatabaseTrustMode:                   policy.DatabaseTrustMode,
		RuntimeInstallMode:                  policy.RuntimeInstallMode,
		VerifierSelection:                   policy.VerifierSelection,
		AnankeKeyCustody:                    policy.AnankeKeyCustody,
	}
	pins.ReleasePinsHash = mustHashRecord(pins, "release_pins_hash")
	rotation := TrustRotation{
		SchemaVersion:       RotationSchemaVersion,
		State:               rotationPolicy.V1FixtureState,
		CurrentRootID:       repairRootID,
		RotationPolicyHash:  pins.RotationPolicyHash,
		ReleaseManifestHash: pins.ReleaseManifestHash,
	}
	rotation.RotationHash = mustHashRecord(rotation, "rotation_hash")
	return releaseMaterial{
		pins: pins, bundle: bundle, rotation: rotation, root: root, leaf: leaf,
		rotationApproverRoot: approverRoot, rotationApprover: approver,
	}, nil
}

func decodeCanonicalReleaseArtifact[T any](raw []byte) (T, error) {
	var zero T
	if len(raw) == 0 || !utf8.Valid(raw) || bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) || validateRawJSONStringScalars(raw) != nil {
		return zero, ErrInvalidContract
	}
	normalized, err := decodeUniqueJSON(raw)
	if err != nil {
		return zero, ErrInvalidContract
	}
	canonical, err := canonicalBytes(normalized)
	if err != nil || !bytes.Equal(raw, canonical) {
		return zero, ErrInvalidContract
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	var value T
	if err := decoder.Decode(&value); err != nil {
		return zero, ErrInvalidContract
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return zero, ErrInvalidContract
	}
	return value, nil
}

var manifestArtifactIDs = []string{
	"contract_release",
	"public_trust_bundle",
	"repair_attestor_certificate_der",
	"repair_attestor_spki_der",
	"repair_root_certificate_der",
	"repair_root_spki_der",
	"rotation_approver_certificate_der",
	"rotation_approver_spki_der",
	"rotation_approver_root_certificate_der",
	"rotation_approver_root_spki_der",
	"rotation_policy",
	"supervisor_policy",
	"supervisor_profile",
}

func validReleaseManifest(manifest releaseManifestDeclaration, artifacts releaseArtifactSet) bool {
	if manifest.SchemaVersion != releaseManifestSchemaVersion || manifest.ReleaseID != releaseID || len(manifest.Artifacts) != len(manifestArtifactIDs) {
		return false
	}
	byID := artifacts.byID()
	for index, id := range manifestArtifactIDs {
		entry := manifest.Artifacts[index]
		if entry.ArtifactID != id || entry.ContentSHA256 != sha256Digest([]byte(byID[id])) {
			return false
		}
	}
	return true
}

func expectedSupervisorPolicy() supervisorPolicyDeclaration {
	return supervisorPolicyDeclaration{
		SchemaVersion:      supervisorPolicySchemaVersion,
		PolicyID:           "controlled_repair_supervisor_policy_v1",
		TrustBootstrapMode: "release_pinned_only",
		DatabaseTrustMode:  "mirror_only_no_replacement",
		RuntimeInstallMode: "forbidden",
		VerifierSelection:  "compiled_embedded_release_artifacts_only",
		AnankeKeyCustody:   "public_material_only_no_private_key",
		RotationMode:       "forbidden_v1",
		DescriptorObservation: descriptorObservationRequirements{
			SchemaVersion:                "ananke.controlled-repair-descriptor-observation-requirements.v1",
			NoFollowRequired:             true,
			RegularFileRequired:          true,
			OwnerModeCheckRequired:       true,
			DeviceInodeStabilityRequired: true,
			ContentHashRequired:          true,
		},
	}
}

func expectedSupervisorProfile() supervisorProfileDeclaration {
	return supervisorProfileDeclaration{
		SchemaVersion:      supervisorProfileSchemaVersion,
		ProfileID:          "controlled_repair_supervisor_local_v1",
		PolicyID:           "controlled_repair_supervisor_policy_v1",
		TrustBundleID:      repairTrustBundleID,
		RepairAttestorRole: RepairAttestorRole,
		SignatureDomain:    SignatureDomain,
		RotationPolicyID:   rotationPolicyID,
	}
}

func expectedContractRelease() contractReleaseDeclaration {
	return contractReleaseDeclaration{
		SchemaVersion:      contractReleaseSchemaVersion,
		ReleaseID:          releaseID,
		ContractID:         "p6_controlled_repair_supervisor_contract_slices_1_2",
		BuildMode:          "source_embedded_public_artifacts",
		PublicMaterialOnly: true,
	}
}

func expectedRotationPolicy() rotationPolicyDeclaration {
	return rotationPolicyDeclaration{
		SchemaVersion:         rotationPolicySchemaVersion,
		PolicyID:              rotationPolicyID,
		Canonicalization:      "RFC8785_JCS",
		ProposalSchemaVersion: "ananke.controlled-repair-trust-rotation-proposal.v1",
		ProposalFields: []string{
			"schema_version", "proposal_hash", "current_root_id", "current_root_certificate_hash",
			"successor_root_id", "successor_root_certificate_hash", "successor_root_spki_sha256",
			"successor_not_before", "successor_not_after", "current_root_not_after", "release_manifest_hash",
		},
		CurrentRootCrossSignature: rotationSignatureDeclaration{
			RecordFields: []string{
				"schema_version", "cross_signature_hash", "signature_domain", "signer_role", "signer_root_id",
				"signer_spki_sha256", "proposal_hash", "signature_base64",
			},
			SchemaVersion:             "ananke.controlled-repair-current-root-cross-signature.v1",
			SignatureDomain:           "ananke.controlled-repair.root-rotation-cross-signature.v1",
			SignerRole:                "controlled_repair_current_root",
			Algorithm:                 "Ed25519",
			SignatureEncoding:         "base64_raw_64_bytes",
			SignedObjectSchemaVersion: "ananke.controlled-repair-trust-rotation-proposal.v1",
			SignedObjectMembers:       []string{"signature_domain", "proposal_hash"},
		},
		IndependentReleaseApproval: rotationReleaseApprovalDeclaration{
			RecordFields: []string{
				"schema_version", "release_approval_hash", "signature_domain", "signer_role", "signer_key_id",
				"signer_spki_sha256", "proposal_hash", "current_root_cross_signature_hash", "approved_at", "signature_base64",
			},
			SchemaVersion:             "ananke.controlled-repair-independent-release-approval.v1",
			SignatureDomain:           RotationApproverSignatureDomain,
			SignerRole:                RotationApproverRole,
			Algorithm:                 "Ed25519",
			SignerKeyID:               RotationApproverKeyID,
			SignerSPKISHA256:          rotationApproverLeafSPKIHash,
			SignatureEncoding:         "base64_raw_64_bytes",
			SignedObjectSchemaVersion: "ananke.controlled-repair-trust-rotation-proposal.v1",
			SignedObjectMembers:       []string{"signature_domain", "proposal_hash", "current_root_cross_signature_hash", "approved_at"},
			SignerSPKIMustDifferFrom:  "current_root_and_successor_root",
		},
		ActivationRules: rotationActivationRules{
			ActivationRequires: []string{
				"valid_current_root_cross_signature", "valid_independent_release_approval",
				"release_manifest_hash_match", "successor_certificate_valid_at_activation",
			},
			CurrentRootNotAfter:           "exclusive",
			MinimumActivationDelaySeconds: 86400,
			MinimumOverlapSeconds:         86400,
			NotBeforeRule:                 "successor_not_before_at_or_after_release_approved_at_plus_minimum_activation_delay",
			OverlapRule:                   "current_root_not_after_at_or_after_successor_not_before_plus_minimum_overlap",
			SuccessorNotAfter:             "exclusive",
		},
		V1FixtureState: "no_successor_authorized",
	}
}

type rotationApprovalSignerIdentity struct {
	SignatureDomain  string
	SignerRole       string
	SignerKeyID      string
	SignerSPKISHA256 string
}

func releasedRotationApprovalSignerIdentity() rotationApprovalSignerIdentity {
	pins := FrozenReleasePins()
	return rotationApprovalSignerIdentity{
		SignatureDomain:  pins.RotationApproverDomain,
		SignerRole:       pins.RotationApproverRole,
		SignerKeyID:      pins.RotationApproverKeyID,
		SignerSPKISHA256: pins.RotationApproverLeafSPKI,
	}
}

func validateRotationApprovalSignerIdentity(value rotationApprovalSignerIdentity) error {
	if value != releasedRotationApprovalSignerIdentity() {
		return ErrInvalidContract
	}
	return nil
}

func validateCertificateSemantics(root, leaf *x509.Certificate, rootSPKI, leafSPKI []byte) error {
	if root.SignatureAlgorithm != x509.PureEd25519 || leaf.SignatureAlgorithm != x509.PureEd25519 ||
		root.PublicKeyAlgorithm != x509.Ed25519 || leaf.PublicKeyAlgorithm != x509.Ed25519 {
		return ErrInvalidContract
	}
	if _, ok := root.PublicKey.(ed25519.PublicKey); !ok {
		return ErrInvalidContract
	}
	if _, ok := leaf.PublicKey.(ed25519.PublicKey); !ok {
		return ErrInvalidContract
	}
	if !bytes.Equal(root.RawSubjectPublicKeyInfo, rootSPKI) || !bytes.Equal(leaf.RawSubjectPublicKeyInfo, leafSPKI) ||
		!bytes.Equal(root.RawIssuer, root.RawSubject) || !bytes.Equal(leaf.RawIssuer, root.RawSubject) {
		return ErrInvalidContract
	}
	if root.Subject.CommonName != "Ananke Controlled Repair Release Root v1" ||
		leaf.Subject.CommonName != "Ananke Controlled Repair Review Attestor v1" {
		return ErrInvalidContract
	}
	if !root.IsCA || !root.BasicConstraintsValid || root.MaxPathLen != 1 ||
		root.KeyUsage != x509.KeyUsageDigitalSignature|x509.KeyUsageCertSign ||
		leaf.IsCA || leaf.KeyUsage != x509.KeyUsageDigitalSignature {
		return ErrInvalidContract
	}
	if !root.NotBefore.Before(root.NotAfter) || !leaf.NotBefore.Before(leaf.NotAfter) ||
		root.NotBefore.After(leaf.NotBefore) || root.NotAfter.Before(leaf.NotAfter) {
		return ErrInvalidContract
	}
	if err := root.CheckSignatureFrom(root); err != nil {
		return ErrInvalidContract
	}
	if err := leaf.CheckSignatureFrom(root); err != nil {
		return ErrInvalidContract
	}
	if !exactCriticalExtension(leaf, "1.3.6.1.4.1.57264.1.6", RepairAttestorRole) ||
		!exactCriticalExtension(leaf, "1.3.6.1.4.1.57264.1.7", SignatureDomain) ||
		exactExtensionPresent(root, "1.3.6.1.4.1.57264.1.6") || exactExtensionPresent(root, "1.3.6.1.4.1.57264.1.7") ||
		RepairAttestorRole == ProtocolAdapterRole || sha256Digest(leaf.RawSubjectPublicKeyInfo) == ProtocolAdapterLeafSPKIHash {
		return ErrInvalidContract
	}
	return nil
}
func validateRotationApproverCertificateSemantics(root, leaf, repairRoot, repairLeaf *x509.Certificate, rootSPKI, leafSPKI []byte) error {
	if root.SignatureAlgorithm != x509.PureEd25519 || leaf.SignatureAlgorithm != x509.PureEd25519 ||
		root.PublicKeyAlgorithm != x509.Ed25519 || leaf.PublicKeyAlgorithm != x509.Ed25519 {
		return ErrInvalidContract
	}
	if _, ok := root.PublicKey.(ed25519.PublicKey); !ok {
		return ErrInvalidContract
	}
	if _, ok := leaf.PublicKey.(ed25519.PublicKey); !ok {
		return ErrInvalidContract
	}
	if !bytes.Equal(root.RawSubjectPublicKeyInfo, rootSPKI) || !bytes.Equal(leaf.RawSubjectPublicKeyInfo, leafSPKI) ||
		!bytes.Equal(root.RawIssuer, root.RawSubject) || !bytes.Equal(leaf.RawIssuer, root.RawSubject) {
		return ErrInvalidContract
	}
	if root.Subject.CommonName != "Ananke Controlled Repair Rotation Approver Root v1" ||
		leaf.Subject.CommonName != "Ananke Controlled Repair Rotation Release Approver v1" {
		return ErrInvalidContract
	}
	if !root.IsCA || !root.BasicConstraintsValid || root.MaxPathLen != 1 ||
		root.KeyUsage != x509.KeyUsageDigitalSignature|x509.KeyUsageCertSign ||
		leaf.IsCA || leaf.KeyUsage != x509.KeyUsageDigitalSignature {
		return ErrInvalidContract
	}
	if !root.NotBefore.Before(root.NotAfter) || !leaf.NotBefore.Before(leaf.NotAfter) ||
		root.NotBefore.After(leaf.NotBefore) || root.NotAfter.Before(leaf.NotAfter) {
		return ErrInvalidContract
	}
	if err := root.CheckSignatureFrom(root); err != nil {
		return ErrInvalidContract
	}
	if err := leaf.CheckSignatureFrom(root); err != nil {
		return ErrInvalidContract
	}
	if !exactCriticalExtension(leaf, "1.3.6.1.4.1.57264.1.6", RotationApproverRole) ||
		!exactCriticalExtension(leaf, "1.3.6.1.4.1.57264.1.7", RotationApproverSignatureDomain) ||
		exactExtensionPresent(root, "1.3.6.1.4.1.57264.1.6") || exactExtensionPresent(root, "1.3.6.1.4.1.57264.1.7") ||
		RotationApproverRole == RepairAttestorRole || RotationApproverRole == ProtocolAdapterRole ||
		sha256Digest(leaf.RawSubjectPublicKeyInfo) == ProtocolAdapterLeafSPKIHash ||
		bytes.Equal(root.Raw, repairRoot.Raw) || bytes.Equal(root.RawSubjectPublicKeyInfo, repairRoot.RawSubjectPublicKeyInfo) ||
		bytes.Equal(leaf.Raw, repairLeaf.Raw) || bytes.Equal(leaf.RawSubjectPublicKeyInfo, repairLeaf.RawSubjectPublicKeyInfo) {
		return ErrInvalidContract
	}
	return nil
}

func exactCriticalExtension(certificate *x509.Certificate, oid, value string) bool {
	matches := 0
	for _, extension := range certificate.Extensions {
		if extension.Id.String() == oid {
			matches++
			if !extension.Critical || string(extension.Value) != value {
				return false
			}
		}
	}
	return matches == 1
}

func exactExtensionPresent(certificate *x509.Certificate, oid string) bool {
	for _, extension := range certificate.Extensions {
		if extension.Id.String() == oid {
			return true
		}
	}
	return false
}

func verifyCertificateTime(root, leaf *x509.Certificate, now time.Time) error {
	now = now.UTC()
	if now.Before(root.NotBefore) || !now.Before(root.NotAfter) || now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return ErrInvalidContract
	}
	leafForVerify := *leaf
	unhandled := leaf.UnhandledCriticalExtensions
	leafForVerify.UnhandledCriticalExtensions = nil
	for _, oid := range unhandled {
		if oid.String() != "1.3.6.1.4.1.57264.1.6" && oid.String() != "1.3.6.1.4.1.57264.1.7" {
			leafForVerify.UnhandledCriticalExtensions = append(leafForVerify.UnhandledCriticalExtensions, oid)
		}
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	if _, err := leafForVerify.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return ErrInvalidContract
	}
	return nil
}
