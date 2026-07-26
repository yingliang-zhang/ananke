// Package repaircontract freezes the pure P6 controlled-repair trust,
// authorization, and immutable-dispatch contracts. It performs no storage,
// process, filesystem, socket, network, or private-key operation.
package repaircontract

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

const (
	ContractFixtureSchemaVersion             = "ananke.controlled-repair-contract-fixture.v1"
	ReleasePinsSchemaVersion                 = "ananke.controlled-repair-release-pins.v1"
	BundleDescriptorObservationSchemaVersion = "ananke.controlled-repair-bundle-descriptor-observation.v1"
	TrustRootSchemaVersion                   = "ananke.controlled-repair-trust-root.v1"
	RepairAttestorCertificateSchemaVersion   = "ananke.controlled-repair-attestor-certificate.v1"
	RotationApproverCertificateSchemaVersion = "ananke.controlled-repair-rotation-approver-certificate.v1"
	TrustBundleSchemaVersion                 = "ananke.controlled-repair-trust-bundle.v1"
	RotationSchemaVersion                    = "ananke.controlled-repair-trust-rotation.v1"
	AuthorizationScopeSchemaVersion          = "ananke.controlled-repair-authorization-scope.v1"
	OperatorApprovalSchemaVersion            = "ananke.controlled-repair-operator-approval.v1"
	AuthorizationSchemaVersion               = "ananke.controlled-repair-authorization.v1"
	P4BindingSchemaVersion                   = "ananke.controlled-repair-p4-binding.v1"
	FullFenceSchemaVersion                   = "ananke.controlled-repair-full-fence.v1"
	RepositoryBindingSchemaVersion           = "ananke.controlled-repair-repository-binding.v1"
	WritablePathBindingSchemaVersion         = "ananke.controlled-repair-writable-path-binding.v1"
	TestProfileBindingSchemaVersion          = "ananke.controlled-repair-test-profile-binding.v1"
	RouteBindingSchemaVersion                = "ananke.controlled-repair-route-binding.v1"
	AttemptBindingSchemaVersion              = "ananke.controlled-repair-attempt-binding.v1"
	UnixPeerIdentitySchemaVersion            = "ananke.controlled-repair-unix-peer-identity.v1"
	DispatchRequestSchemaVersion             = "ananke.controlled-repair-dispatch-request.v1"
	ImmutableDispatchSchemaVersion           = "ananke.controlled-repair-immutable-dispatch.v1"

	SignatureDomain     = "ananke.controlled-repair.review-attestation.v1"
	RepairAttestorRole  = "controlled_repair_review_attestor"
	ProtocolAdapterRole = "independent_supervisor_protocol_adapter"

	ProtocolAdapterLeafSPKIHash     = "sha256:992b7a6da10ae5bdbffca27986dc3e7d29b05472e417bf12e8218d8751b25ab7"
	RotationApproverRole            = "controlled_repair_rotation_release_approver"
	RotationApproverSignatureDomain = "ananke.controlled-repair.root-rotation-release-approval.v1"
	RotationApproverKeyID           = "ananke_controlled_repair_rotation_release_approver_v1"
	AttemptCap                      = 2

	ValidationAdmission ValidationMoment = "admission"
	ValidationEffect    ValidationMoment = "effect"

	DispatchExactReplay DispatchReplayDisposition = "exact_replay"
	DispatchConflict    DispatchReplayDisposition = "conflict"
)

// These deliberately small limits are suitable for a local GUI gesture. Both
// boundaries are inclusive; not_after itself remains an exclusive instant.
const (
	MaxApprovalAge           = 5 * time.Minute
	MaxAuthorizationLifetime = 10 * time.Minute
	MaxDispatchLifetime      = 4 * time.Minute
)

// Closed dynamic identifier limits are inclusive byte counts. The accepted
// grammars below are ASCII-only, so byte and character counts are identical.
const (
	maxClaimIDBytes             = 128
	maxRepositoryIdentityBytes  = 256
	maxRouteIDBytes             = 128
	maxSupervisorProfileIDBytes = 128
	maxPeerRoleBytes            = 128
)

var (
	ErrInvalidContract  = errors.New("controlled repair contract is invalid")
	ErrDispatchConflict = errors.New("controlled repair dispatch conflicts with immutable identity")

	gitObjectPattern          = regexp.MustCompile(`^[0-9a-f]{40}$`)
	closedIdentifierPattern   = regexp.MustCompile(`^[a-z0-9]+(?:_[a-z0-9]+)*$`)
	repositoryIdentityPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*(?:/[a-z0-9]+(?:[-._][a-z0-9]+)*)+$`)
)

type ValidationMoment string
type DispatchReplayDisposition string

// ReleasePins is compiled release material. It is intentionally independent of
// any database and offers no caller-selected verifier or runtime installation.
type ReleasePins struct {
	SchemaVersion                       string `json:"schema_version"`
	ReleasePinsHash                     string `json:"release_pins_hash"`
	TrustBundleHash                     string `json:"trust_bundle_hash"`
	RepairRootCertificateHash           string `json:"repair_root_certificate_hash"`
	RepairRootSPKISHA256                string `json:"repair_root_spki_sha256"`
	RepairAttestorCertificateHash       string `json:"repair_attestor_certificate_hash"`
	RepairAttestorRootID                string `json:"repair_attestor_root_id"`
	RepairAttestorLeafSPKI              string `json:"repair_attestor_leaf_spki"`
	RotationApproverRootCertificateHash string `json:"rotation_approver_root_certificate_hash"`
	RotationApproverRootSPKISHA256      string `json:"rotation_approver_root_spki_sha256"`
	RotationApproverCertificateHash     string `json:"rotation_approver_certificate_hash"`
	RotationApproverRootID              string `json:"rotation_approver_root_id"`
	RotationApproverKeyID               string `json:"rotation_approver_key_id"`
	RotationApproverLeafSPKI            string `json:"rotation_approver_leaf_spki"`
	RotationApproverRole                string `json:"rotation_approver_role"`
	RotationApproverDomain              string `json:"rotation_approver_domain"`
	ReleaseManifestHash                 string `json:"release_manifest_hash"`
	BuildIdentityHash                   string `json:"build_identity_hash"`
	SupervisorPolicyHash                string `json:"supervisor_policy_hash"`
	SupervisorProfileHash               string `json:"supervisor_profile_hash"`
	RotationPolicyHash                  string `json:"rotation_policy_hash"`
	SignatureDomain                     string `json:"signature_domain"`
	TrustBootstrapMode                  string `json:"trust_bootstrap_mode"`
	DatabaseTrustMode                   string `json:"database_trust_mode"`
	RuntimeInstallMode                  string `json:"runtime_install_mode"`
	VerifierSelection                   string `json:"verifier_selection"`
	AnankeKeyCustody                    string `json:"ananke_key_custody"`
}

// BundleDescriptorObservation is a future no-follow descriptor observation
// schema. No portable release value instantiates or pins these machine-local
// facts, and the schema deliberately has no pathname field.
type BundleDescriptorObservation struct {
	SchemaVersion string `json:"schema_version"`
	FileID        string `json:"file_id"`
	ContentSHA256 string `json:"content_sha256"`
	Device        uint64 `json:"device"`
	Inode         uint64 `json:"inode"`
	OwnerUserID   uint32 `json:"owner_user_id"`
	OwnerGroupID  uint32 `json:"owner_group_id"`
	Mode          uint32 `json:"mode"`
	Size          int64  `json:"size"`
	OpenMode      string `json:"open_mode"`
}

type TrustRoot struct {
	SchemaVersion   string `json:"schema_version"`
	RootHash        string `json:"root_hash"`
	RootID          string `json:"root_id"`
	RootSPKISHA256  string `json:"root_spki_sha256"`
	ValidFrom       string `json:"valid_from"`
	NotAfter        string `json:"not_after"`
	RevocationState string `json:"revocation_state"`
}

type RepairAttestorCertificate struct {
	SchemaVersion     string `json:"schema_version"`
	CertificateHash   string `json:"certificate_hash"`
	Role              string `json:"role"`
	SubjectSPKISHA256 string `json:"subject_spki_sha256"`
	IssuerRootID      string `json:"issuer_root_id"`
	ValidFrom         string `json:"valid_from"`
	NotAfter          string `json:"not_after"`
	RevocationState   string `json:"revocation_state"`
}

type RotationApproverCertificate struct {
	SchemaVersion     string `json:"schema_version"`
	CertificateHash   string `json:"certificate_hash"`
	KeyID             string `json:"key_id"`
	Role              string `json:"role"`
	SignatureDomain   string `json:"signature_domain"`
	SubjectSPKISHA256 string `json:"subject_spki_sha256"`
	IssuerRootID      string `json:"issuer_root_id"`
	ValidFrom         string `json:"valid_from"`
	NotAfter          string `json:"not_after"`
	RevocationState   string `json:"revocation_state"`
}

type TrustBundle struct {
	SchemaVersion        string                      `json:"schema_version"`
	TrustBundleHash      string                      `json:"trust_bundle_hash"`
	BundleID             string                      `json:"bundle_id"`
	Root                 TrustRoot                   `json:"root"`
	RepairAttestor       RepairAttestorCertificate   `json:"repair_attestor"`
	RotationApproverRoot TrustRoot                   `json:"rotation_approver_root"`
	RotationApprover     RotationApproverCertificate `json:"rotation_approver"`
}

// TrustRotation records only the materialized V1 state. The embedded rotation
// policy defines future signed schemas and activation rules; no successor or
// signature reference exists until a later release actually supplies bytes.
type TrustRotation struct {
	SchemaVersion       string `json:"schema_version"`
	RotationHash        string `json:"rotation_hash"`
	State               string `json:"state"`
	CurrentRootID       string `json:"current_root_id"`
	RotationPolicyHash  string `json:"rotation_policy_hash"`
	ReleaseManifestHash string `json:"release_manifest_hash"`
}

type FullFence struct {
	SchemaVersion   string `json:"schema_version"`
	FenceHash       string `json:"fence_hash"`
	ClaimID         string `json:"claim_id"`
	ClaimTokenHash  string `json:"claim_token_hash"`
	FenceGeneration int    `json:"fence_generation"`
}

type P4Binding struct {
	SchemaVersion        string    `json:"schema_version"`
	P4FactHash           string    `json:"p4_fact_hash"`
	P4ProposalHash       string    `json:"p4_proposal_hash"`
	P4InputHash          string    `json:"p4_input_hash"`
	P4EvidenceBundleHash string    `json:"p4_evidence_bundle_hash"`
	P4AdmissionHash      string    `json:"p4_admission_hash"`
	FullFence            FullFence `json:"full_fence"`
	AttemptNumber        int       `json:"attempt_number"`
	AttemptCap           int       `json:"attempt_cap"`
}

type RepositoryBinding struct {
	SchemaVersion          string `json:"schema_version"`
	RepositoryBindingHash  string `json:"repository_binding_hash"`
	RepositoryIdentity     string `json:"repository_identity"`
	RepositoryIdentityHash string `json:"repository_identity_hash"`
	BaseCommit             string `json:"base_commit"`
	BaseTree               string `json:"base_tree"`
}

// WritablePathBinding carries only a release-defined ID and the hash of the
// repository-relative path. Raw path strings are deliberately unrepresentable.
type WritablePathBinding struct {
	SchemaVersion              string `json:"schema_version"`
	PathBindingHash            string `json:"path_binding_hash"`
	Sequence                   int    `json:"sequence"`
	PathID                     string `json:"path_id"`
	RepositoryRelativePathHash string `json:"repository_relative_path_hash"`
}

// TestProfileBinding has no executable, argv, environment, cwd, or cache field.
type TestProfileBinding struct {
	SchemaVersion        string `json:"schema_version"`
	ProfileBindingHash   string `json:"profile_binding_hash"`
	Sequence             int    `json:"sequence"`
	ProfileID            string `json:"profile_id"`
	InstalledProfileHash string `json:"installed_profile_hash"`
}

type RouteBinding struct {
	SchemaVersion         string `json:"schema_version"`
	RouteBindingHash      string `json:"route_binding_hash"`
	RouteID               string `json:"route_id"`
	RouteIdentityHash     string `json:"route_identity_hash"`
	SupervisorProfileID   string `json:"supervisor_profile_id"`
	SupervisorProfileHash string `json:"supervisor_profile_hash"`
}

type AttemptBinding struct {
	SchemaVersion             string `json:"schema_version"`
	AttemptNumber             int    `json:"attempt_number"`
	AttemptCap                int    `json:"attempt_cap"`
	PreviousAuthorizationHash string `json:"previous_authorization_hash"`
}

// UnixPeerIdentity excludes a mutable PID and socket pathname. It freezes the
// expected peer credential and release identities inspected by a future local
// authenticated Unix transport.
type UnixPeerIdentity struct {
	SchemaVersion           string `json:"schema_version"`
	PeerIdentityHash        string `json:"peer_identity_hash"`
	PeerRole                string `json:"peer_role"`
	UserID                  uint32 `json:"user_id"`
	GroupID                 uint32 `json:"group_id"`
	CodeSigningIdentityHash string `json:"code_signing_identity_hash"`
	ExecutableIdentityHash  string `json:"executable_identity_hash"`
	RuntimeIdentityHash     string `json:"runtime_identity_hash"`
}

type AuthorizationScope struct {
	SchemaVersion      string                `json:"schema_version"`
	ScopeHash          string                `json:"scope_hash"`
	RepairLineageHash  string                `json:"repair_lineage_hash"`
	P4                 P4Binding             `json:"p4"`
	Repository         RepositoryBinding     `json:"repository"`
	WritablePaths      []WritablePathBinding `json:"writable_paths"`
	TestProfiles       []TestProfileBinding  `json:"test_profiles"`
	Route              RouteBinding          `json:"route"`
	Attempt            AttemptBinding        `json:"attempt"`
	ChannelBindingHash string                `json:"channel_binding_hash"`
	ExpectedPeer       UnixPeerIdentity      `json:"expected_peer"`
	PolicyHash         string                `json:"policy_hash"`
	RotationMode       string                `json:"rotation_mode"`
}

type OperatorApproval struct {
	SchemaVersion            string `json:"schema_version"`
	ApprovalHash             string `json:"approval_hash"`
	ApprovalID               string `json:"approval_id"`
	Decision                 string `json:"decision"`
	OperatorIdentity         string `json:"operator_identity"`
	OperatorIdentityHash     string `json:"operator_identity_hash"`
	AuthenticationProvenance string `json:"authentication_provenance"`
	GUIProvenanceHash        string `json:"gui_provenance_hash"`
	ApprovedScopeHash        string `json:"approved_scope_hash"`
	ApprovedAt               string `json:"approved_at"`
	NotAfter                 string `json:"not_after"`
}

type Authorization struct {
	SchemaVersion     string             `json:"schema_version"`
	AuthorizationHash string             `json:"authorization_hash"`
	ApprovalHash      string             `json:"approval_hash"`
	PolicyHash        string             `json:"policy_hash"`
	Scope             AuthorizationScope `json:"scope"`
	Approval          OperatorApproval   `json:"approval"`
}

type DispatchRequest struct {
	SchemaVersion     string `json:"schema_version"`
	RequestHash       string `json:"request_hash"`
	RequestID         string `json:"request_id"`
	AuthorizationHash string `json:"authorization_hash"`
	AttemptNumber     int    `json:"attempt_number"`
	AttemptCap        int    `json:"attempt_cap"`
}

// ImmutableDispatch is an outbox value, not an operation. No field can name a
// process, executable, command, environment, private key, or socket pathname.
type ImmutableDispatch struct {
	SchemaVersion                 string           `json:"schema_version"`
	DispatchHash                  string           `json:"dispatch_hash"`
	AuthorizationHash             string           `json:"authorization_hash"`
	ApprovalHash                  string           `json:"approval_hash"`
	PolicyHash                    string           `json:"policy_hash"`
	AttemptNumber                 int              `json:"attempt_number"`
	AttemptCap                    int              `json:"attempt_cap"`
	Request                       DispatchRequest  `json:"request"`
	ChannelBindingHash            string           `json:"channel_binding_hash"`
	ExpectedPeer                  UnixPeerIdentity `json:"expected_peer"`
	ReleasePinsHash               string           `json:"release_pins_hash"`
	SelectedSupervisorPolicyHash  string           `json:"selected_supervisor_policy_hash"`
	SelectedSupervisorProfileID   string           `json:"selected_supervisor_profile_id"`
	SelectedSupervisorProfileHash string           `json:"selected_supervisor_profile_hash"`
	CreatedAt                     string           `json:"created_at"`
	DispatchNotAfter              string           `json:"dispatch_not_after"`
}

type ContractFixture struct {
	SchemaVersion string            `json:"schema_version"`
	FixtureHash   string            `json:"fixture_hash"`
	ReleasePins   ReleasePins       `json:"release_pins"`
	TrustBundle   TrustBundle       `json:"trust_bundle"`
	Rotation      TrustRotation     `json:"rotation"`
	Authorization Authorization     `json:"authorization"`
	Dispatch      ImmutableDispatch `json:"dispatch"`
}

func frozenFullFence() FullFence {
	value := FullFence{
		SchemaVersion:   FullFenceSchemaVersion,
		ClaimID:         "p4_repair_fence_claim_001",
		ClaimTokenHash:  "sha256:7506737a97ecf137840f1f6ec0c2c9c210733fc35751fcda967a75dfe084eacd",
		FenceGeneration: 8,
	}
	value.FenceHash = mustHashRecord(value, "fence_hash")
	return value
}

func frozenP4(attempt int) P4Binding {
	value := P4Binding{
		SchemaVersion:        P4BindingSchemaVersion,
		P4ProposalHash:       fixedHash("p4-controlled-repair-proposal-001"),
		P4InputHash:          "sha256:c7d9a26636b16df70d77d443a37df7c91d640731c1dbbb9ad339990cd9b77eb8",
		P4EvidenceBundleHash: "sha256:12ec67830ffa00eb637ed0594b46b89be79c28cce3854574f540f9dc2b6a5c0d",
		P4AdmissionHash:      "sha256:54446404a8e615d1abf63abd396b303ae86047be14a1eeeaabb6176c2d9deedb",
		FullFence:            frozenFullFence(),
		AttemptNumber:        attempt,
		AttemptCap:           AttemptCap,
	}
	value.P4FactHash = mustHashRecord(value, "p4_fact_hash")
	return value
}

func frozenRepository() RepositoryBinding {
	value := RepositoryBinding{
		SchemaVersion:          RepositoryBindingSchemaVersion,
		RepositoryIdentity:     "github.com/yingliang-zhang/ananke",
		RepositoryIdentityHash: fixedHash("github.com/yingliang-zhang/ananke"),
		BaseCommit:             "7a1f7ce102f6611a6f4ddbd6ee45263f211e9588",
		BaseTree:               "9b5f88f170846bf4b5fc7595f53344f993bfde12",
	}
	value.RepositoryBindingHash = mustHashRecord(value, "repository_binding_hash")
	return value
}

func frozenWritablePaths() []WritablePathBinding {
	values := []WritablePathBinding{
		{SchemaVersion: WritablePathBindingSchemaVersion, Sequence: 1, PathID: "authorized_source_member_1", RepositoryRelativePathHash: fixedHash("internal/lifecycle/backend.go")},
		{SchemaVersion: WritablePathBindingSchemaVersion, Sequence: 2, PathID: "authorized_source_member_2", RepositoryRelativePathHash: fixedHash("internal/lifecycle/engine.go")},
	}
	for index := range values {
		values[index].PathBindingHash = mustHashRecord(values[index], "path_binding_hash")
	}
	return values
}

func frozenTestProfiles() []TestProfileBinding {
	values := []TestProfileBinding{
		{SchemaVersion: TestProfileBindingSchemaVersion, Sequence: 1, ProfileID: "go_unit_offline_v1", InstalledProfileHash: fixedHash("controlled-repair-go-unit-offline-profile-v1")},
		{SchemaVersion: TestProfileBindingSchemaVersion, Sequence: 2, ProfileID: "go_race_offline_v1", InstalledProfileHash: fixedHash("controlled-repair-go-race-offline-profile-v1")},
	}
	for index := range values {
		values[index].ProfileBindingHash = mustHashRecord(values[index], "profile_binding_hash")
	}
	return values
}

func frozenRoute() RouteBinding {
	pins := FrozenReleasePins()
	value := RouteBinding{
		SchemaVersion:         RouteBindingSchemaVersion,
		RouteID:               "controlled_repair_local_supervisor_v1",
		RouteIdentityHash:     fixedHash("controlled-repair-local-supervisor-route-v1"),
		SupervisorProfileID:   "controlled_repair_supervisor_local_v1",
		SupervisorProfileHash: pins.SupervisorProfileHash,
	}
	value.RouteBindingHash = mustHashRecord(value, "route_binding_hash")
	return value
}

func frozenPeer() UnixPeerIdentity {
	value := UnixPeerIdentity{
		SchemaVersion:           UnixPeerIdentitySchemaVersion,
		PeerRole:                "controlled_repair_supervisor",
		UserID:                  62001,
		GroupID:                 62001,
		CodeSigningIdentityHash: fixedHash("controlled-repair-supervisor-code-signing-identity-v1"),
		ExecutableIdentityHash:  fixedHash("controlled-repair-supervisor-executable-identity-v1"),
		RuntimeIdentityHash:     fixedHash("controlled-repair-supervisor-runtime-identity-v1"),
	}
	value.PeerIdentityHash = mustHashRecord(value, "peer_identity_hash")
	return value
}

func frozenScope(attempt int, previousAuthorizationHash string) AuthorizationScope {
	pins := FrozenReleasePins()
	value := AuthorizationScope{
		SchemaVersion:     AuthorizationScopeSchemaVersion,
		RepairLineageHash: fixedHash("controlled-repair-lineage-001"),
		P4:                frozenP4(attempt),
		Repository:        frozenRepository(),
		WritablePaths:     frozenWritablePaths(),
		TestProfiles:      frozenTestProfiles(),
		Route:             frozenRoute(),
		Attempt: AttemptBinding{
			SchemaVersion:             AttemptBindingSchemaVersion,
			AttemptNumber:             attempt,
			AttemptCap:                AttemptCap,
			PreviousAuthorizationHash: previousAuthorizationHash,
		},
		ChannelBindingHash: fixedHash("controlled-repair-local-channel-binding-v1"),
		ExpectedPeer:       frozenPeer(),
		PolicyHash:         pins.SupervisorPolicyHash,
		RotationMode:       "forbidden_v1",
	}
	value.ScopeHash = mustHashRecord(value, "scope_hash")
	return value
}

// CanonicalFixture returns the attempt-1 fixture oracle. It performs no I/O.
func CanonicalFixture() ContractFixture {
	return canonicalFixtureForAttempt(1, "")
}

// CanonicalAttemptTwoFixture returns a permanent attempt-2 oracle whose
// predecessor hash is the exact canonical attempt-1 authorization hash.
func CanonicalAttemptTwoFixture() ContractFixture {
	first := CanonicalFixture()
	return canonicalFixtureForAttempt(2, first.Authorization.AuthorizationHash)
}

func canonicalFixtureForAttempt(attempt int, previousAuthorizationHash string) ContractFixture {
	pins := FrozenReleasePins()
	authorization := canonicalAuthorization(attempt, previousAuthorizationHash)
	scope := authorization.Scope
	requestID := "controlled_repair_request_001"
	createdAt := "2026-07-26T12:04:00Z"
	dispatchNotAfter := "2026-07-26T12:08:00Z"
	if attempt == 2 {
		requestID = "controlled_repair_request_002"
		createdAt = "2026-07-26T12:06:00Z"
		dispatchNotAfter = "2026-07-26T12:10:00Z"
	}
	request := DispatchRequest{
		SchemaVersion:     DispatchRequestSchemaVersion,
		RequestID:         requestID,
		AuthorizationHash: authorization.AuthorizationHash,
		AttemptNumber:     attempt,
		AttemptCap:        AttemptCap,
	}
	request.RequestHash = mustHashRecord(request, "request_hash")
	dispatch := ImmutableDispatch{
		SchemaVersion:                 ImmutableDispatchSchemaVersion,
		AuthorizationHash:             authorization.AuthorizationHash,
		ApprovalHash:                  authorization.ApprovalHash,
		PolicyHash:                    authorization.PolicyHash,
		AttemptNumber:                 attempt,
		AttemptCap:                    AttemptCap,
		Request:                       request,
		ChannelBindingHash:            scope.ChannelBindingHash,
		ExpectedPeer:                  scope.ExpectedPeer,
		ReleasePinsHash:               pins.ReleasePinsHash,
		SelectedSupervisorPolicyHash:  pins.SupervisorPolicyHash,
		SelectedSupervisorProfileID:   scope.Route.SupervisorProfileID,
		SelectedSupervisorProfileHash: pins.SupervisorProfileHash,
		CreatedAt:                     createdAt,
		DispatchNotAfter:              dispatchNotAfter,
	}
	dispatch.DispatchHash = mustHashRecord(dispatch, "dispatch_hash")
	fixture := ContractFixture{
		SchemaVersion: ContractFixtureSchemaVersion,
		ReleasePins:   pins,
		TrustBundle:   FrozenTrustBundle(),
		Rotation:      frozenRotation(),
		Authorization: authorization,
		Dispatch:      dispatch,
	}
	fixture.FixtureHash = mustHashRecord(fixture, "fixture_hash")
	return fixture
}

func canonicalAuthorization(attempt int, previousAuthorizationHash string) Authorization {
	scope := frozenScope(attempt, previousAuthorizationHash)
	approvalID := "controlled_repair_gui_approval_001"
	provenanceHash := fixedHash("authenticated-local-gui-provenance-event-001")
	approvedAt := "2026-07-26T12:00:00Z"
	notAfter := "2026-07-26T12:10:00Z"
	if attempt == 2 {
		approvalID = "controlled_repair_gui_approval_002"
		provenanceHash = fixedHash("authenticated-local-gui-provenance-event-002")
		approvedAt = "2026-07-26T12:05:00Z"
		notAfter = "2026-07-26T12:15:00Z"
	}
	approval := OperatorApproval{
		SchemaVersion:            OperatorApprovalSchemaVersion,
		ApprovalID:               approvalID,
		Decision:                 "approved",
		OperatorIdentity:         "local_gui_operator",
		OperatorIdentityHash:     fixedHash("local-gui-operator-identity-v1"),
		AuthenticationProvenance: "authenticated_local_gui_session",
		GUIProvenanceHash:        provenanceHash,
		ApprovedScopeHash:        scope.ScopeHash,
		ApprovedAt:               approvedAt,
		NotAfter:                 notAfter,
	}
	approval.ApprovalHash = mustHashRecord(approval, "approval_hash")
	authorization := Authorization{
		SchemaVersion: AuthorizationSchemaVersion,
		ApprovalHash:  approval.ApprovalHash,
		PolicyHash:    scope.PolicyHash,
		Scope:         scope,
		Approval:      approval,
	}
	authorization.AuthorizationHash = mustHashRecord(authorization, "authorization_hash")
	return authorization
}

// VerifyFixture verifies only the canonical attempt-1 oracle. Reusable runtime
// verification must call VerifyReleaseTrust, VerifyAuthorization, and
// DecodeDispatch with independently constructed AuthorityContext values.
func VerifyFixture(fixture ContractFixture, now time.Time, moment ValidationMoment) error {
	now = now.UTC()
	if fixture.SchemaVersion != ContractFixtureSchemaVersion || validateAllHashStrings(reflect.ValueOf(fixture)) != nil ||
		!recordHashMatches(fixture, "fixture_hash", fixture.FixtureHash) ||
		VerifyReleaseTrust(fixture.ReleasePins, fixture.TrustBundle, fixture.Rotation, now) != nil {
		return ErrInvalidContract
	}
	expected := authorityFromAuthorization(CanonicalFixture().Authorization)
	verified, err := VerifyAuthorization(expected, fixture.Authorization, nil, now, moment)
	if err != nil {
		return ErrInvalidContract
	}
	dispatchBytes, err := canonicalBytes(fixture.Dispatch)
	if err != nil {
		return ErrInvalidContract
	}
	if _, err := DecodeDispatch(expected, verified, dispatchBytes, now, moment); err != nil {
		return ErrInvalidContract
	}
	return nil
}

func validateTrustTimes(fixture ContractFixture, now time.Time) error {
	rootFrom, err := parseUTC(fixture.TrustBundle.Root.ValidFrom)
	if err != nil {
		return err
	}
	rootUntil, err := parseUTC(fixture.TrustBundle.Root.NotAfter)
	if err != nil || now.Before(rootFrom) || !now.Before(rootUntil) || fixture.TrustBundle.Root.RevocationState != "not_revoked" {
		return ErrInvalidContract
	}
	certificateFrom, err := parseUTC(fixture.TrustBundle.RepairAttestor.ValidFrom)
	if err != nil {
		return err
	}
	certificateUntil, err := parseUTC(fixture.TrustBundle.RepairAttestor.NotAfter)
	if err != nil || now.Before(certificateFrom) || !now.Before(certificateUntil) || fixture.TrustBundle.RepairAttestor.RevocationState != "not_revoked" {
		return ErrInvalidContract
	}
	approverRootFrom, err := parseUTC(fixture.TrustBundle.RotationApproverRoot.ValidFrom)
	if err != nil {
		return err
	}
	approverRootUntil, err := parseUTC(fixture.TrustBundle.RotationApproverRoot.NotAfter)
	if err != nil || now.Before(approverRootFrom) || !now.Before(approverRootUntil) || fixture.TrustBundle.RotationApproverRoot.RevocationState != "not_revoked" {
		return ErrInvalidContract
	}
	approverFrom, err := parseUTC(fixture.TrustBundle.RotationApprover.ValidFrom)
	if err != nil {
		return err
	}
	approverUntil, err := parseUTC(fixture.TrustBundle.RotationApprover.NotAfter)
	if err != nil || now.Before(approverFrom) || !now.Before(approverUntil) || fixture.TrustBundle.RotationApprover.RevocationState != "not_revoked" {
		return ErrInvalidContract
	}
	return nil
}

func hasDuplicatePaths(values []WritablePathBinding) bool {
	ids := make(map[string]struct{}, len(values))
	hashes := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, found := ids[value.PathID]; found {
			return true
		}
		if _, found := hashes[value.RepositoryRelativePathHash]; found {
			return true
		}
		ids[value.PathID] = struct{}{}
		hashes[value.RepositoryRelativePathHash] = struct{}{}
	}
	return false
}

func hasDuplicateProfiles(values []TestProfileBinding) bool {
	ids := make(map[string]struct{}, len(values))
	hashes := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, found := ids[value.ProfileID]; found {
			return true
		}
		if _, found := hashes[value.InstalledProfileHash]; found {
			return true
		}
		ids[value.ProfileID] = struct{}{}
		hashes[value.InstalledProfileHash] = struct{}{}
	}
	return false
}

func parseUTC(value string) (time.Time, error) {
	if !strings.HasSuffix(value, "Z") {
		return time.Time{}, ErrInvalidContract
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.Location() != time.UTC || parsed.Format(time.RFC3339Nano) != value {
		return time.Time{}, ErrInvalidContract
	}
	return parsed, nil
}

func validHash(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func validClosedIdentifier(value string, maxBytes int) bool {
	return len(value) > 0 && len(value) <= maxBytes && closedIdentifierPattern.MatchString(value)
}

func validRepositoryIdentity(value string) bool {
	return len(value) > 0 && len(value) <= maxRepositoryIdentityBytes && repositoryIdentityPattern.MatchString(value)
}

func validFullFence(value FullFence) bool {
	return value.SchemaVersion == FullFenceSchemaVersion && validHash(value.FenceHash) &&
		validClosedIdentifier(value.ClaimID, maxClaimIDBytes) && validHash(value.ClaimTokenHash) &&
		value.FenceGeneration > 0
}

func validateAllHashStrings(value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return validateAllHashStrings(value.Elem())
	}
	switch value.Kind() {
	case reflect.Struct:
		typeOfValue := value.Type()
		for index := range value.NumField() {
			field := value.Field(index)
			tag := strings.Split(typeOfValue.Field(index).Tag.Get("json"), ",")[0]
			if field.Kind() == reflect.String && (strings.HasSuffix(tag, "_hash") || strings.HasSuffix(tag, "_sha256") || strings.HasSuffix(tag, "_spki")) {
				if tag == "previous_authorization_hash" && field.String() == "" {
					continue
				}
				if !validHash(field.String()) {
					return ErrInvalidContract
				}
			}
			if err := validateAllHashStrings(field); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			if err := validateAllHashStrings(value.Index(index)); err != nil {
				return err
			}
		}
	}
	return nil
}

func distinctHashes(values ...string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !validHash(value) {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func recordHashMatches(value any, ownField, expected string) bool {
	computed, err := hashRecord(value, ownField)
	return err == nil && computed == expected
}

func mustHashRecord(value any, ownField string) string {
	hash, err := hashRecord(value, ownField)
	if err != nil {
		panic(fmt.Sprintf("repair contract programmer error: %v", err))
	}
	return hash
}

func fixedHash(label string) string {
	return sha256Digest([]byte(label))
}
