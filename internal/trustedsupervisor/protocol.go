// Package trustedsupervisor implements the bounded, identity-only local Unix
// transport for the external-supervisor handoff seam. Operator endpoint and
// process credentials remain local configuration and are never serialized.
package trustedsupervisor

import (
	"context"
	"errors"
	"net"
	"regexp"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

const (
	requestSchemaVersion                   = "ananke.local-trusted-supervisor-request.v4"
	responseSchemaVersion                  = "ananke.local-trusted-supervisor-response.v2"
	wireEnvelopeReferenceSchemaVersion     = "ananke.local-trusted-supervisor-envelope-reference.v2"
	wirePredecessorProjectionSchemaVersion = "ananke.local-trusted-supervisor-predecessor-projection.v1"

	operationDeliver   = "deliver"
	operationReconcile = "reconcile"
	operationCancel    = "cancel"

	minFrameBytes uint32 = 1024
	maxFrameBytes uint32 = 64 * 1024
	maxTimeout           = 10 * time.Second
)

var (
	ErrAuthentication = errors.New("local trusted supervisor authentication failed")
	ErrDeadline       = errors.New("local trusted supervisor deadline exceeded")
	ErrLimit          = errors.New("local trusted supervisor limit exceeded")
	ErrProtocol       = errors.New("local trusted supervisor protocol rejected")
	ErrReplay         = errors.New("local trusted supervisor replay conflict")

	protocolHashPattern       = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	protocolIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,63}$`)
)

// AuthenticatedChannel records only the connected Unix peer metadata used to
// derive a per-connection channel hash. UID/PID are defense in depth and are
// deliberately absent from every wire and durable authentication record.
type AuthenticatedChannel struct {
	BindingHash   string
	PeerUserID    uint32
	PeerProcessID int32
	RequestHash   string
}

// AuthenticationBoundary is supplied to optional post-verification hooks.
// Hooks cannot replace mandatory Ed25519 verification and run inside the same
// exchange deadline.
type AuthenticationBoundary struct {
	MessageType string
	MessageHash string
	IssuedAt    time.Time
}

type AuthenticationHooks interface {
	Authenticate(context.Context, AuthenticationBoundary) error
}

// ChannelBinder validates credentials on the connected socket and derives a
// local channel hash. Peer key possession is established separately by an
// Ed25519 signature over that hash and the exact request.
type ChannelBinder interface {
	Bind(context.Context, net.Conn, uint32, int32, string) (AuthenticatedChannel, error)
}

type DialContextFunc func(context.Context, string, string) (net.Conn, error)

type Config struct {
	Authentication                     AuthenticationHooks
	Binder                             ChannelBinder
	DialContext                        DialContextFunc
	ExpectedProcessID                  int32
	ExpectedPredecessorReleaseIdentity store.ExternalSupervisorPredecessorReleaseIdentity
	ExpectedUserID                     uint32
	MaxFrameBytes                      uint32
	Now                                func() time.Time
	SocketPath                         string
	Timeout                            time.Duration
	TrustBundle                        store.ExternalSupervisorTrustBundle
}
type wirePredecessorProjection struct {
	SchemaVersion             string `json:"schema_version"`
	EnvelopeSchemaVersion     string `json:"envelope_schema_version"`
	HandoffID                 string `json:"handoff_id"`
	IdempotencyKeyHash        string `json:"idempotency_key_hash"`
	LaunchSpecHash            string `json:"launch_spec_hash"`
	FenceBindingHash          string `json:"fence_binding_hash"`
	Deadline                  string `json:"deadline"`
	AttemptNumber             int    `json:"attempt_number"`
	AttemptCap                int    `json:"attempt_cap"`
	RouteMappingHash          string `json:"route_mapping_hash"`
	SourceSnapshotHash        string `json:"source_snapshot_hash"`
	SourceManifestHash        string `json:"source_manifest_hash"`
	RepositoryIdentityHash    string `json:"repository_identity_hash"`
	SupervisorArtifactSHA256  string `json:"supervisor_artifact_sha256"`
	BuildIdentityHash         string `json:"build_identity_hash"`
	ReleaseAttestationHash    string `json:"release_attestation_hash"`
	ReleaseApprovalHash       string `json:"release_approval_hash"`
	EvidenceContractHash      string `json:"evidence_contract_hash"`
	EvidenceSchemaVersion     string `json:"evidence_schema_version"`
	EnvelopeHash              string `json:"envelope_hash"`
	PredecessorProjectionHash string `json:"predecessor_projection_hash,omitempty"`
}

type wireEnvelopeReference struct {
	SchemaVersion         string                    `json:"schema_version"`
	DurableEnvelopeHash   string                    `json:"durable_envelope_hash"`
	PredecessorProjection wirePredecessorProjection `json:"predecessor_projection"`
	EnvelopeReferenceHash string                    `json:"envelope_reference_hash,omitempty"`
}

func sealWirePredecessorProjection(envelope store.ExternalSupervisorEnvelope) (wirePredecessorProjection, error) {
	if store.ValidateExternalSupervisorEnvelope(envelope) != nil {
		return wirePredecessorProjection{}, ErrProtocol
	}
	projection := wirePredecessorProjection{
		SchemaVersion: wirePredecessorProjectionSchemaVersion, EnvelopeSchemaVersion: envelope.SchemaVersion,
		HandoffID: envelope.HandoffID, IdempotencyKeyHash: envelope.IdempotencyKeyHash,
		LaunchSpecHash: envelope.LaunchSpecHash, FenceBindingHash: envelope.FenceBindingHash,
		Deadline: envelope.Deadline, AttemptNumber: envelope.AttemptNumber, AttemptCap: envelope.AttemptCap,
		RouteMappingHash: envelope.RouteMappingHash, SourceSnapshotHash: envelope.SourceSnapshotHash,
		SourceManifestHash: envelope.SourceManifestHash, RepositoryIdentityHash: repositoryIdentityHash(envelope.RepositoryIdentity),
		SupervisorArtifactSHA256: envelope.SupervisorArtifactSHA256, BuildIdentityHash: envelope.BuildIdentityHash,
		ReleaseAttestationHash: envelope.ReleaseAttestationHash, ReleaseApprovalHash: envelope.ReleaseApprovalHash,
		EvidenceContractHash: envelope.EvidenceContractHash, EvidenceSchemaVersion: envelope.EvidenceSchemaVersion,
		EnvelopeHash: envelope.EnvelopeHash,
	}
	hash, err := canonicalHash(projection)
	if err != nil {
		return wirePredecessorProjection{}, err
	}
	projection.PredecessorProjectionHash = hash
	return projection, nil
}

func validateWirePredecessorProjection(projection wirePredecessorProjection) error {
	if projection.SchemaVersion != wirePredecessorProjectionSchemaVersion ||
		projection.EnvelopeSchemaVersion != store.ExternalSupervisorEnvelopeSchemaVersion ||
		!protocolIdentifierPattern.MatchString(projection.HandoffID) || projection.AttemptNumber < 1 ||
		projection.AttemptCap < 1 || projection.AttemptNumber > projection.AttemptCap ||
		projection.EvidenceSchemaVersion != "ananke.remote-supervisor-evidence.v1" {
		return ErrProtocol
	}
	if _, err := time.Parse(time.RFC3339Nano, projection.Deadline); err != nil {
		return ErrProtocol
	}
	for _, hash := range []string{
		projection.IdempotencyKeyHash, projection.LaunchSpecHash, projection.FenceBindingHash,
		projection.RouteMappingHash, projection.SourceSnapshotHash, projection.SourceManifestHash,
		projection.RepositoryIdentityHash, projection.SupervisorArtifactSHA256, projection.BuildIdentityHash,
		projection.ReleaseAttestationHash, projection.ReleaseApprovalHash, projection.EvidenceContractHash,
		projection.EnvelopeHash, projection.PredecessorProjectionHash,
	} {
		if !protocolHashPattern.MatchString(hash) {
			return ErrProtocol
		}
	}
	claimedHash := projection.PredecessorProjectionHash
	projection.PredecessorProjectionHash = ""
	computed, err := canonicalHash(projection)
	if err != nil || computed != claimedHash {
		return ErrProtocol
	}
	return nil
}

func sealWireEnvelopeReference(envelope store.ExternalSupervisorEnvelope) (wireEnvelopeReference, error) {
	projection, err := sealWirePredecessorProjection(envelope)
	if err != nil {
		return wireEnvelopeReference{}, err
	}
	reference := wireEnvelopeReference{
		SchemaVersion: wireEnvelopeReferenceSchemaVersion, DurableEnvelopeHash: envelope.EnvelopeHash,
		PredecessorProjection: projection,
	}
	hash, err := canonicalHash(reference)
	if err != nil {
		return wireEnvelopeReference{}, err
	}
	reference.EnvelopeReferenceHash = hash
	return reference, nil
}

func validateWireEnvelopeReference(reference wireEnvelopeReference) error {
	if reference.SchemaVersion != wireEnvelopeReferenceSchemaVersion ||
		!protocolHashPattern.MatchString(reference.DurableEnvelopeHash) ||
		!protocolHashPattern.MatchString(reference.EnvelopeReferenceHash) ||
		validateWirePredecessorProjection(reference.PredecessorProjection) != nil ||
		reference.DurableEnvelopeHash != reference.PredecessorProjection.EnvelopeHash {
		return ErrProtocol
	}
	claimedHash := reference.EnvelopeReferenceHash
	reference.EnvelopeReferenceHash = ""
	computed, err := canonicalHash(reference)
	if err != nil || computed != claimedHash {
		return ErrProtocol
	}
	return nil
}

type wireRequest struct {
	SchemaVersion      string                                        `json:"schema_version"`
	Operation          string                                        `json:"operation"`
	EnvelopeReference  *wireEnvelopeReference                        `json:"envelope_reference,omitempty"`
	Authorization      *store.ExternalSupervisorAuthorizationChain   `json:"authorization,omitempty"`
	Delivery           *store.ExternalSupervisorSealedDelivery       `json:"delivery,omitempty"`
	Receipt            *store.ExternalSupervisorAuthenticatedReceipt `json:"receipt,omitempty"`
	Cancellation       *store.ExternalSupervisorCancellation         `json:"cancellation,omitempty"`
	ChannelBindingHash string                                        `json:"channel_binding_hash"`
	RequestNonceHash   string                                        `json:"request_nonce_hash"`
	ResponseNonceHash  string                                        `json:"response_nonce_hash"`
	RequestHash        string                                        `json:"request_hash,omitempty"`
}

type wireResponse struct {
	SchemaVersion               string                                               `json:"schema_version"`
	Operation                   string                                               `json:"operation"`
	RequestHash                 string                                               `json:"request_hash"`
	PeerSignerSPKISHA256        string                                               `json:"peer_signer_spki_sha256"`
	Status                      string                                               `json:"status"`
	DeliveryAuthentication      *store.ExternalSupervisorMessageAuthentication       `json:"delivery_authentication,omitempty"`
	Receipt                     *store.ExternalSupervisorProtocolReceipt             `json:"receipt,omitempty"`
	ReceiptAuthentication       *store.ExternalSupervisorMessageAuthentication       `json:"receipt_authentication,omitempty"`
	Callback                    *store.ExternalSupervisorProtocolCallback            `json:"callback,omitempty"`
	CallbackAuthentication      *store.ExternalSupervisorMessageAuthentication       `json:"callback_authentication,omitempty"`
	CancellationAcknowledgement *store.ExternalSupervisorCancellationAcknowledgement `json:"cancellation_acknowledgement,omitempty"`
	CancellationAuthentication  *store.ExternalSupervisorMessageAuthentication       `json:"cancellation_authentication,omitempty"`
}
