package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

const (
	ExternalSupervisorTrustBundleSchemaVersion                 = "ananke.independent-supervisor-trust-bundle.v1"
	ExternalSupervisorRootRotationSchemaVersion                = "ananke.independent-supervisor-trust-root-rotation.v1"
	ExternalSupervisorRootRevocationSchemaVersion              = "ananke.independent-supervisor-trust-root-revocation.v1"
	ExternalSupervisorSigningCertificateSchemaVersion          = "ananke.independent-supervisor-signing-certificate.v1"
	ExternalSupervisorDetachedSignatureSchemaVersion           = "ananke.independent-supervisor-detached-signature.v1"
	ExternalSupervisorReleaseAttestationSchemaVersion          = "ananke.independent-supervisor-release-attestation.v1"
	ExternalSupervisorReleaseApprovalSchemaVersion             = "ananke.independent-supervisor-release-approval.v1"
	ExternalSupervisorMoARoleGrantSchemaVersion                = "ananke.moa-typed-role-grant.v1"
	ExternalSupervisorSealedDeliverySchemaVersion              = "ananke.independent-supervisor-sealed-handoff-delivery.v1"
	ExternalSupervisorProtocolReceiptSchemaVersion             = "ananke.independent-supervisor-acceptance-receipt.v1"
	ExternalSupervisorProtocolCallbackSchemaVersion            = "ananke.independent-supervisor-callback.v1"
	ExternalSupervisorMessageAuthenticationSchemaVersion       = "ananke.local-trusted-supervisor-message-authentication.v1"
	ExternalSupervisorAuthenticatedReceiptSchemaVersion        = "ananke.local-trusted-supervisor-authenticated-receipt.v1"
	ExternalSupervisorAuthenticatedCallbackSchemaVersion       = "ananke.local-trusted-supervisor-authenticated-callback.v1"
	ExternalSupervisorCancellationAcknowledgementSchemaVersion = "ananke.local-trusted-supervisor-cancellation-acknowledgement.v1"
	ExternalSupervisorAuthenticatedCancellationSchemaVersion   = "ananke.local-trusted-supervisor-authenticated-cancellation.v1"
	ExternalSupervisorAuditNotRunResultSchemaVersion           = "ananke.independent-supervisor-audit-not-run.v1"
	ExternalSupervisorWaitingForHumanState                     = "waiting_for_human"
)

var (
	externalSupervisorPublicKeyPattern = regexp.MustCompile(`^ed25519:[0-9a-f]{64}$`)
	externalSupervisorSignaturePattern = regexp.MustCompile(`^ed25519:[0-9a-f]{128}$`)
)

var externalSupervisorForbiddenOpaqueIdentifierMarkers = [...]string{
	"argument", "argv", "command", "credential", "environment", "error", "exec", "instruction", "password", "path", "pid", "prompt", "prose", "raw", "secret", "socket", "token",
	"address", "admin", "authority", "certificate", "delegate", "endpoint", "host", "http", "https", "issuer", "key", "port", "principal", "private", "public", "root", "signer", "spki", "uri", "url",
	"source", "artifact", "evidence",
}

// ExternalSupervisorDetachedSignature carries public detached-signature
// evidence only. It can never contain a private key.
type ExternalSupervisorDetachedSignature struct {
	SchemaVersion       string `json:"schema_version"`
	Algorithm           string `json:"algorithm"`
	SignerKeySPKISHA256 string `json:"signer_key_spki_sha256"`
	Signature           string `json:"signature"`
	SignatureHash       string `json:"signature_hash"`
}

type ExternalSupervisorTrustRootKey struct {
	RootID     string `json:"root_id"`
	PublicKey  string `json:"public_key"`
	SPKISHA256 string `json:"spki_sha256"`
	ValidFrom  string `json:"valid_from"`
	NotAfter   string `json:"not_after"`
}

type ExternalSupervisorRootRotation struct {
	SchemaVersion               string `json:"schema_version"`
	CrossSignatureReferenceHash string `json:"cross_signature_reference_hash"`
	OldRootID                   string `json:"old_root_id"`
	NewRootID                   string `json:"new_root_id"`
	NewRootSPKISHA256           string `json:"new_root_spki_sha256"`
	NewRootValidFrom            string `json:"new_root_valid_from"`
	OldRootNotAfter             string `json:"old_root_not_after"`
	RotationHash                string `json:"rotation_hash"`
}

type ExternalSupervisorRootRevocation struct {
	SchemaVersion         string `json:"schema_version"`
	RevokedRootID         string `json:"revoked_root_id"`
	IssuerRootID          string `json:"issuer_root_id"`
	EffectiveAt           string `json:"effective_at"`
	RevocationReasonClass string `json:"revocation_reason_class"`
	RevocationHash        string `json:"revocation_hash"`
}

type ExternalSupervisorTrustRootLifecycle struct {
	Active              ExternalSupervisorTrustRootKey      `json:"active"`
	Successor           ExternalSupervisorTrustRootKey      `json:"successor"`
	Rotation            ExternalSupervisorRootRotation      `json:"rotation"`
	RotationSignature   ExternalSupervisorDetachedSignature `json:"rotation_signature"`
	Revocation          ExternalSupervisorRootRevocation    `json:"revocation"`
	RevocationSignature ExternalSupervisorDetachedSignature `json:"revocation_signature"`
}

type ExternalSupervisorSigningCertificate struct {
	SchemaVersion        string `json:"schema_version"`
	Role                 string `json:"role"`
	SubjectKeySPKISHA256 string `json:"subject_key_spki_sha256"`
	SubjectPublicKey     string `json:"subject_public_key"`
	IssuerRootID         string `json:"issuer_root_id"`
	IssuedAt             string `json:"issued_at"`
	NotAfter             string `json:"not_after"`
	CertificateHash      string `json:"certificate_hash"`
}

type ExternalSupervisorSignedCertificate struct {
	Certificate ExternalSupervisorSigningCertificate `json:"certificate"`
	Signature   ExternalSupervisorDetachedSignature  `json:"signature"`
}

type ExternalSupervisorReleaseAttestation struct {
	SchemaVersion         string `json:"schema_version"`
	ArtifactSHA256        string `json:"artifact_sha256"`
	AttestationHash       string `json:"attestation_hash"`
	AttestorKeySPKISHA256 string `json:"attestor_key_spki_sha256"`
	BuildIdentityHash     string `json:"build_identity_hash"`
	IssuedAt              string `json:"issued_at"`
	NotAfter              string `json:"not_after"`
	ReleaseRootID         string `json:"release_root_id"`
	RouteMappingHash      string `json:"route_mapping_hash"`
}

type ExternalSupervisorReleaseApproval struct {
	SchemaVersion         string `json:"schema_version"`
	ApprovalHash          string `json:"approval_hash"`
	ApprovalID            string `json:"approval_id"`
	ApproverKeySPKISHA256 string `json:"approver_key_spki_sha256"`
	ApproverRootID        string `json:"approver_root_id"`
	AttestationHash       string `json:"attestation_hash"`
	Decision              string `json:"decision"`
	IssuedAt              string `json:"issued_at"`
	NotAfter              string `json:"not_after"`
	RouteMappingHash      string `json:"route_mapping_hash"`
}

type ExternalSupervisorMoARoleGrant struct {
	SchemaVersion          string `json:"schema_version"`
	GrantHash              string `json:"grant_hash"`
	GrantID                string `json:"grant_id"`
	GranteeRole            string `json:"grantee_role"`
	GrantorKeySPKISHA256   string `json:"grantor_key_spki_sha256"`
	GrantorRootID          string `json:"grantor_root_id"`
	IssuedAt               string `json:"issued_at"`
	NotAfter               string `json:"not_after"`
	ReleaseApprovalHash    string `json:"release_approval_hash"`
	ReleaseAttestationHash string `json:"release_attestation_hash"`
	RouteMappingHash       string `json:"route_mapping_hash"`
}

type ExternalSupervisorAuthorizationChain struct {
	ReleaseAttestation          ExternalSupervisorReleaseAttestation `json:"release_attestation"`
	ReleaseAttestationSignature ExternalSupervisorDetachedSignature  `json:"release_attestation_signature"`
	ReleaseApproval             ExternalSupervisorReleaseApproval    `json:"release_approval"`
	ReleaseApprovalSignature    ExternalSupervisorDetachedSignature  `json:"release_approval_signature"`
	MoARoleGrant                ExternalSupervisorMoARoleGrant       `json:"moa_typed_role_grant"`
	MoARoleGrantSignature       ExternalSupervisorDetachedSignature  `json:"moa_typed_role_grant_signature"`
}

type ExternalSupervisorTrustBundle struct {
	SchemaVersion   string                               `json:"schema_version"`
	TrustBundleHash string                               `json:"trust_bundle_hash"`
	ReleaseRoots    ExternalSupervisorTrustRootLifecycle `json:"release_roots"`
	ApprovalRoots   ExternalSupervisorTrustRootLifecycle `json:"approval_roots"`
	MoARoots        ExternalSupervisorTrustRootLifecycle `json:"moa_roots"`
	ReleaseAttestor ExternalSupervisorSignedCertificate  `json:"release_attestor"`
	ReleaseApprover ExternalSupervisorSignedCertificate  `json:"release_approver"`
	MoAGrantor      ExternalSupervisorSignedCertificate  `json:"moa_grantor"`
	SupervisorPeer  ExternalSupervisorSignedCertificate  `json:"supervisor_peer"`
	Authorization   ExternalSupervisorAuthorizationChain `json:"authorization"`
}

// The delivery/receipt/callback records below preserve the exact closed field
// inventories frozen by the P3f protocol-adapter contract.
type ExternalSupervisorSealedDelivery struct {
	SchemaVersion                 string `json:"schema_version"`
	AttemptCap                    int    `json:"attempt_cap"`
	AttemptNumber                 int    `json:"attempt_number"`
	ChannelBindingHash            string `json:"channel_binding_hash"`
	Deadline                      string `json:"deadline"`
	DeliveryExpiresAt             string `json:"delivery_expires_at"`
	DeliveryHash                  string `json:"delivery_hash"`
	DeliveryID                    string `json:"delivery_id"`
	IssuedAt                      string `json:"issued_at"`
	MoARoleGrantHash              string `json:"moa_role_grant_hash"`
	NonceHash                     string `json:"nonce_hash"`
	PredecessorEnvelopeHash       string `json:"predecessor_envelope_hash"`
	PredecessorIdempotencyKeyHash string `json:"predecessor_idempotency_key_hash"`
	ReleaseApprovalHash           string `json:"release_approval_hash"`
	ReleaseAttestationHash        string `json:"release_attestation_hash"`
	RouteMappingHash              string `json:"route_mapping_hash"`
	TrustBundleHash               string `json:"trust_bundle_hash"`
}

type ExternalSupervisorProtocolReceipt struct {
	SchemaVersion       string `json:"schema_version"`
	AttemptNumber       int    `json:"attempt_number"`
	ChannelBindingHash  string `json:"channel_binding_hash"`
	DeliveryHash        string `json:"delivery_hash"`
	EnvelopeHash        string `json:"envelope_hash"`
	IssuedAt            string `json:"issued_at"`
	NonceHash           string `json:"nonce_hash"`
	ReceiptHash         string `json:"receipt_hash"`
	ReceiptID           string `json:"receipt_id"`
	ReleaseApprovalHash string `json:"release_approval_hash"`
	RouteMappingHash    string `json:"route_mapping_hash"`
	SignerKeySPKISHA256 string `json:"signer_key_spki_sha256"`
	TrustRootID         string `json:"trust_root_id"`
}

type ExternalSupervisorProtocolCallback struct {
	SchemaVersion              string `json:"schema_version"`
	AttemptNumber              int    `json:"attempt_number"`
	CallbackChannelBindingHash string `json:"callback_channel_binding_hash"`
	CallbackHash               string `json:"callback_hash"`
	CallbackID                 string `json:"callback_id"`
	DeliveryHash               string `json:"delivery_hash"`
	EnvelopeHash               string `json:"envelope_hash"`
	EvidenceHash               string `json:"evidence_hash"`
	IssuedAt                   string `json:"issued_at"`
	NonceHash                  string `json:"nonce_hash"`
	ReceiptHash                string `json:"receipt_hash"`
	ResultSchemaVersion        string `json:"result_schema_version"`
	RouteMappingHash           string `json:"route_mapping_hash"`
	SignerKeySPKISHA256        string `json:"signer_key_spki_sha256"`
	TerminalState              string `json:"terminal_state"`
	TrustRootID                string `json:"trust_root_id"`
}

type ExternalSupervisorMessageAuthentication struct {
	SchemaVersion       string `json:"schema_version"`
	MessageType         string `json:"message_type"`
	MessageHash         string `json:"message_hash"`
	NonceHash           string `json:"nonce_hash"`
	ChannelBindingHash  string `json:"channel_binding_hash"`
	RequestHash         string `json:"request_hash"`
	IssuedAt            string `json:"issued_at"`
	SignerKeySPKISHA256 string `json:"signer_key_spki_sha256"`
	Signature           string `json:"signature"`
	SignatureHash       string `json:"signature_hash"`
}

type ExternalSupervisorAuthenticatedReceipt struct {
	SchemaVersion          string                                  `json:"schema_version"`
	Authorization          ExternalSupervisorAuthorizationChain    `json:"authorization"`
	Delivery               ExternalSupervisorSealedDelivery        `json:"delivery"`
	DeliveryAuthentication ExternalSupervisorMessageAuthentication `json:"delivery_authentication"`
	Receipt                ExternalSupervisorProtocolReceipt       `json:"receipt"`
	ReceiptAuthentication  ExternalSupervisorMessageAuthentication `json:"receipt_authentication"`
}

type ExternalSupervisorAuthenticatedCallback struct {
	SchemaVersion          string                                  `json:"schema_version"`
	Callback               ExternalSupervisorProtocolCallback      `json:"callback"`
	CallbackAuthentication ExternalSupervisorMessageAuthentication `json:"callback_authentication"`
}

type ExternalSupervisorCancellationAcknowledgement struct {
	SchemaVersion       string `json:"schema_version"`
	AcknowledgementID   string `json:"acknowledgement_id"`
	AcknowledgementHash string `json:"acknowledgement_hash"`
	CancellationHash    string `json:"cancellation_hash"`
	DeliveryHash        string `json:"delivery_hash"`
	EnvelopeHash        string `json:"envelope_hash"`
	ReceiptHash         string `json:"receipt_hash"`
	AttemptNumber       int    `json:"attempt_number"`
	ChannelBindingHash  string `json:"channel_binding_hash"`
	NonceHash           string `json:"nonce_hash"`
	IssuedAt            string `json:"issued_at"`
	SignerKeySPKISHA256 string `json:"signer_key_spki_sha256"`
	TrustRootID         string `json:"trust_root_id"`
}

type ExternalSupervisorAuthenticatedCancellation struct {
	SchemaVersion                 string                                        `json:"schema_version"`
	Cancellation                  ExternalSupervisorCancellation                `json:"cancellation"`
	Acknowledgement               ExternalSupervisorCancellationAcknowledgement `json:"acknowledgement"`
	AcknowledgementAuthentication ExternalSupervisorMessageAuthentication       `json:"acknowledgement_authentication"`
}

func SealExternalSupervisorDetachedSignature(value ExternalSupervisorDetachedSignature) (ExternalSupervisorDetachedSignature, error) {
	value.SignatureHash = ""
	if value.SchemaVersion != ExternalSupervisorDetachedSignatureSchemaVersion || value.Algorithm != "ed25519" ||
		!launchHashPattern.MatchString(value.SignerKeySPKISHA256) || !externalSupervisorSignaturePattern.MatchString(value.Signature) {
		return ExternalSupervisorDetachedSignature{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "signature_hash")
	if err != nil {
		return ExternalSupervisorDetachedSignature{}, err
	}
	value.SignatureHash = hash
	return value, nil
}

func SealExternalSupervisorRootRotation(value ExternalSupervisorRootRotation) (ExternalSupervisorRootRotation, error) {
	value.RotationHash = ""
	if value.SchemaVersion != ExternalSupervisorRootRotationSchemaVersion || !validExternalSupervisorIdentifier(value.OldRootID) ||
		!validExternalSupervisorIdentifier(value.NewRootID) || value.OldRootID == value.NewRootID ||
		!externalSupervisorHashes(value.CrossSignatureReferenceHash, value.NewRootSPKISHA256) ||
		!externalSupervisorOrderedTimes(value.NewRootValidFrom, value.OldRootNotAfter) {
		return ExternalSupervisorRootRotation{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "rotation_hash")
	if err != nil {
		return ExternalSupervisorRootRotation{}, err
	}
	value.RotationHash = hash
	return value, nil
}

func SealExternalSupervisorRootRevocation(value ExternalSupervisorRootRevocation) (ExternalSupervisorRootRevocation, error) {
	value.RevocationHash = ""
	if value.SchemaVersion != ExternalSupervisorRootRevocationSchemaVersion || !validExternalSupervisorIdentifier(value.RevokedRootID) ||
		!validExternalSupervisorIdentifier(value.IssuerRootID) || value.RevocationReasonClass != "key_compromise_or_policy_withdrawal" || !externalSupervisorValidTime(value.EffectiveAt) {
		return ExternalSupervisorRootRevocation{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "revocation_hash")
	if err != nil {
		return ExternalSupervisorRootRevocation{}, err
	}
	value.RevocationHash = hash
	return value, nil
}

func SealExternalSupervisorSigningCertificate(value ExternalSupervisorSigningCertificate) (ExternalSupervisorSigningCertificate, error) {
	value.CertificateHash = ""
	if value.SchemaVersion != ExternalSupervisorSigningCertificateSchemaVersion || !validExternalSupervisorIdentifier(value.Role) ||
		!validExternalSupervisorIdentifier(value.IssuerRootID) || !launchHashPattern.MatchString(value.SubjectKeySPKISHA256) ||
		!externalSupervisorPublicKeyPattern.MatchString(value.SubjectPublicKey) || !externalSupervisorOrderedTimes(value.IssuedAt, value.NotAfter) {
		return ExternalSupervisorSigningCertificate{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "certificate_hash")
	if err != nil {
		return ExternalSupervisorSigningCertificate{}, err
	}
	value.CertificateHash = hash
	return value, nil
}

func SealExternalSupervisorReleaseAttestation(value ExternalSupervisorReleaseAttestation) (ExternalSupervisorReleaseAttestation, error) {
	value.AttestationHash = ""
	if value.SchemaVersion != ExternalSupervisorReleaseAttestationSchemaVersion || !validExternalSupervisorIdentifier(value.ReleaseRootID) ||
		!externalSupervisorOrderedTimes(value.IssuedAt, value.NotAfter) || !externalSupervisorHashes(value.ArtifactSHA256, value.AttestorKeySPKISHA256, value.BuildIdentityHash, value.RouteMappingHash) {
		return ExternalSupervisorReleaseAttestation{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "attestation_hash")
	if err != nil {
		return ExternalSupervisorReleaseAttestation{}, err
	}
	value.AttestationHash = hash
	return value, nil
}

func SealExternalSupervisorReleaseApproval(value ExternalSupervisorReleaseApproval) (ExternalSupervisorReleaseApproval, error) {
	value.ApprovalHash = ""
	if value.SchemaVersion != ExternalSupervisorReleaseApprovalSchemaVersion || !validExternalSupervisorSafeOpaqueIdentifier(value.ApprovalID) ||
		!validExternalSupervisorIdentifier(value.ApproverRootID) || value.Decision != "approved" || !externalSupervisorOrderedTimes(value.IssuedAt, value.NotAfter) ||
		!externalSupervisorHashes(value.ApproverKeySPKISHA256, value.AttestationHash, value.RouteMappingHash) {
		return ExternalSupervisorReleaseApproval{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "approval_hash")
	if err != nil {
		return ExternalSupervisorReleaseApproval{}, err
	}
	value.ApprovalHash = hash
	return value, nil
}

func SealExternalSupervisorMoARoleGrant(value ExternalSupervisorMoARoleGrant) (ExternalSupervisorMoARoleGrant, error) {
	value.GrantHash = ""
	if value.SchemaVersion != ExternalSupervisorMoARoleGrantSchemaVersion || !validExternalSupervisorSafeOpaqueIdentifier(value.GrantID) ||
		value.GranteeRole != "remote_supervisor_runner" || !validExternalSupervisorIdentifier(value.GrantorRootID) ||
		!externalSupervisorOrderedTimes(value.IssuedAt, value.NotAfter) || !externalSupervisorHashes(value.GrantorKeySPKISHA256, value.ReleaseApprovalHash, value.ReleaseAttestationHash, value.RouteMappingHash) {
		return ExternalSupervisorMoARoleGrant{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "grant_hash")
	if err != nil {
		return ExternalSupervisorMoARoleGrant{}, err
	}
	value.GrantHash = hash
	return value, nil
}

func SealExternalSupervisorTrustBundle(value ExternalSupervisorTrustBundle) (ExternalSupervisorTrustBundle, error) {
	value.TrustBundleHash = ""
	if value.SchemaVersion != ExternalSupervisorTrustBundleSchemaVersion {
		return ExternalSupervisorTrustBundle{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "trust_bundle_hash")
	if err != nil {
		return ExternalSupervisorTrustBundle{}, err
	}
	value.TrustBundleHash = hash
	return value, nil
}

func SealExternalSupervisorSealedDelivery(value ExternalSupervisorSealedDelivery) (ExternalSupervisorSealedDelivery, error) {
	value.DeliveryHash = ""
	issuedAt, issuedErr := time.Parse(time.RFC3339Nano, value.IssuedAt)
	expiresAt, expiryErr := time.Parse(time.RFC3339Nano, value.DeliveryExpiresAt)
	deadline, deadlineErr := time.Parse(time.RFC3339Nano, value.Deadline)
	if value.SchemaVersion != ExternalSupervisorSealedDeliverySchemaVersion || !validExternalSupervisorIdentifier(value.DeliveryID) ||
		value.AttemptNumber < 1 || value.AttemptCap < value.AttemptNumber || issuedErr != nil || expiryErr != nil || deadlineErr != nil ||
		!issuedAt.Before(expiresAt) || expiresAt.After(deadline) || !externalSupervisorHashes(value.ChannelBindingHash, value.MoARoleGrantHash,
		value.NonceHash, value.PredecessorEnvelopeHash, value.PredecessorIdempotencyKeyHash, value.ReleaseApprovalHash,
		value.ReleaseAttestationHash, value.RouteMappingHash, value.TrustBundleHash) {
		return ExternalSupervisorSealedDelivery{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "delivery_hash")
	if err != nil {
		return ExternalSupervisorSealedDelivery{}, err
	}
	value.DeliveryHash = hash
	return value, nil
}

func SealExternalSupervisorProtocolReceipt(value ExternalSupervisorProtocolReceipt) (ExternalSupervisorProtocolReceipt, error) {
	value.ReceiptHash = ""
	if value.SchemaVersion != ExternalSupervisorProtocolReceiptSchemaVersion || !validExternalSupervisorIdentifier(value.ReceiptID) ||
		!validExternalSupervisorIdentifier(value.TrustRootID) || value.AttemptNumber < 1 || !externalSupervisorValidTime(value.IssuedAt) ||
		!externalSupervisorHashes(value.ChannelBindingHash, value.DeliveryHash, value.EnvelopeHash, value.NonceHash,
			value.ReleaseApprovalHash, value.RouteMappingHash, value.SignerKeySPKISHA256) {
		return ExternalSupervisorProtocolReceipt{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "receipt_hash")
	if err != nil {
		return ExternalSupervisorProtocolReceipt{}, err
	}
	value.ReceiptHash = hash
	return value, nil
}

func SealExternalSupervisorProtocolCallback(value ExternalSupervisorProtocolCallback) (ExternalSupervisorProtocolCallback, error) {
	value.CallbackHash = ""
	legacyTerminal := value.ResultSchemaVersion == "ananke.independent-supervisor-result.v1" &&
		(value.TerminalState == "completed" || value.TerminalState == "failed" || value.TerminalState == "cancelled")
	auditNotRun := value.ResultSchemaVersion == ExternalSupervisorAuditNotRunResultSchemaVersion &&
		value.TerminalState == ExternalSupervisorWaitingForHumanState
	if value.SchemaVersion != ExternalSupervisorProtocolCallbackSchemaVersion || !validExternalSupervisorIdentifier(value.CallbackID) ||
		!validExternalSupervisorIdentifier(value.TrustRootID) || value.AttemptNumber < 1 || !externalSupervisorValidTime(value.IssuedAt) ||
		(!legacyTerminal && !auditNotRun) ||
		!externalSupervisorHashes(value.CallbackChannelBindingHash, value.DeliveryHash, value.EnvelopeHash, value.EvidenceHash,
			value.NonceHash, value.ReceiptHash, value.RouteMappingHash, value.SignerKeySPKISHA256) {
		return ExternalSupervisorProtocolCallback{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "callback_hash")
	if err != nil {
		return ExternalSupervisorProtocolCallback{}, err
	}
	value.CallbackHash = hash
	return value, nil
}

func SealExternalSupervisorMessageAuthentication(value ExternalSupervisorMessageAuthentication) (ExternalSupervisorMessageAuthentication, error) {
	value.SignatureHash = ""
	if value.SchemaVersion != ExternalSupervisorMessageAuthenticationSchemaVersion ||
		(value.MessageType != "delivery" && value.MessageType != "receipt" && value.MessageType != "callback" && value.MessageType != "cancellation") ||
		!externalSupervisorValidTime(value.IssuedAt) || !externalSupervisorSignaturePattern.MatchString(value.Signature) ||
		!externalSupervisorHashes(value.MessageHash, value.NonceHash, value.ChannelBindingHash, value.RequestHash, value.SignerKeySPKISHA256) {
		return ExternalSupervisorMessageAuthentication{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "signature_hash")
	if err != nil {
		return ExternalSupervisorMessageAuthentication{}, err
	}
	value.SignatureHash = hash
	return value, nil
}

func SealExternalSupervisorCancellation(value ExternalSupervisorCancellation) (ExternalSupervisorCancellation, error) {
	value.CancellationIdentityHash = ""
	if value.SchemaVersion != ExternalSupervisorCancellationSchemaVersion || !validExternalSupervisorIdentifier(value.HandoffID) || value.AttemptNumber < 1 ||
		!externalSupervisorHashes(value.EnvelopeHash, value.ReceiptIdentityHash) {
		return ExternalSupervisorCancellation{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "cancellation_identity_hash")
	if err != nil {
		return ExternalSupervisorCancellation{}, err
	}
	value.CancellationIdentityHash = hash
	return value, nil
}

func SealExternalSupervisorCancellationAcknowledgement(value ExternalSupervisorCancellationAcknowledgement) (ExternalSupervisorCancellationAcknowledgement, error) {
	value.AcknowledgementHash = ""
	if value.SchemaVersion != ExternalSupervisorCancellationAcknowledgementSchemaVersion || !validExternalSupervisorIdentifier(value.AcknowledgementID) ||
		!validExternalSupervisorIdentifier(value.TrustRootID) || value.AttemptNumber < 1 || !externalSupervisorValidTime(value.IssuedAt) ||
		!externalSupervisorHashes(value.CancellationHash, value.DeliveryHash, value.EnvelopeHash, value.ReceiptHash,
			value.ChannelBindingHash, value.NonceHash, value.SignerKeySPKISHA256) {
		return ExternalSupervisorCancellationAcknowledgement{}, ErrExternalSupervisorInvalid
	}
	hash, err := externalSupervisorHashWithoutField(value, "acknowledgement_hash")
	if err != nil {
		return ExternalSupervisorCancellationAcknowledgement{}, err
	}
	value.AcknowledgementHash = hash
	return value, nil
}

func validExternalSupervisorSafeOpaqueIdentifier(value string) bool {
	if !validExternalSupervisorIdentifier(value) {
		return false
	}
	for _, marker := range externalSupervisorForbiddenOpaqueIdentifierMarkers {
		if externalSupervisorIdentifierContainsMarker(value, marker) {
			return false
		}
	}
	return true
}

func externalSupervisorIdentifierContainsMarker(value, marker string) bool {
	for start := 0; start < len(value); start++ {
		valueIndex := start
		markerIndex := 0
		for valueIndex < len(value) && markerIndex < len(marker) {
			if value[valueIndex] == '_' {
				valueIndex++
				continue
			}
			if value[valueIndex] != marker[markerIndex] {
				break
			}
			valueIndex++
			markerIndex++
		}
		if markerIndex == len(marker) {
			return true
		}
	}
	return false
}

func externalSupervisorHashWithoutField(value any, field string) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return "", err
	}
	delete(object, field)
	return canonicalJSONHash(object)
}

func externalSupervisorHashes(values ...string) bool {
	for _, value := range values {
		if !launchHashPattern.MatchString(value) {
			return false
		}
	}
	return true
}

func externalSupervisorValidTime(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC
}

func externalSupervisorOrderedTimes(first, second string) bool {
	left, leftErr := time.Parse(time.RFC3339Nano, first)
	right, rightErr := time.Parse(time.RFC3339Nano, second)
	return leftErr == nil && rightErr == nil && left.Before(right)
}

func ValidateExternalSupervisorTrustBundle(value ExternalSupervisorTrustBundle) error {
	sealed, err := SealExternalSupervisorTrustBundle(value)
	if err != nil || sealed.TrustBundleHash != value.TrustBundleHash || !launchHashPattern.MatchString(value.TrustBundleHash) {
		return fmt.Errorf("%w: trust bundle", ErrExternalSupervisorInvalid)
	}
	return nil
}
