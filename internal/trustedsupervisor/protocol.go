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
	requestSchemaVersion               = "ananke.local-trusted-supervisor-request.v3"
	responseSchemaVersion              = "ananke.local-trusted-supervisor-response.v2"
	wireEnvelopeReferenceSchemaVersion = "ananke.local-trusted-supervisor-envelope-reference.v1"

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
type wireEnvelopeReference struct {
	SchemaVersion         string `json:"schema_version"`
	DurableEnvelopeHash   string `json:"durable_envelope_hash"`
	EnvelopeReferenceHash string `json:"envelope_reference_hash"`
}

func sealWireEnvelopeReference(envelope store.ExternalSupervisorEnvelope) (wireEnvelopeReference, error) {
	if store.ValidateExternalSupervisorEnvelope(envelope) != nil {
		return wireEnvelopeReference{}, ErrProtocol
	}
	reference := wireEnvelopeReference{
		SchemaVersion:       wireEnvelopeReferenceSchemaVersion,
		DurableEnvelopeHash: envelope.EnvelopeHash,
	}
	hash, err := canonicalHash(map[string]any{
		"schema_version":        reference.SchemaVersion,
		"durable_envelope_hash": reference.DurableEnvelopeHash,
	})
	if err != nil {
		return wireEnvelopeReference{}, err
	}
	reference.EnvelopeReferenceHash = hash
	return reference, nil
}

func validateWireEnvelopeReference(reference wireEnvelopeReference) error {
	if reference.SchemaVersion != wireEnvelopeReferenceSchemaVersion ||
		!protocolHashPattern.MatchString(reference.DurableEnvelopeHash) ||
		!protocolHashPattern.MatchString(reference.EnvelopeReferenceHash) {
		return ErrProtocol
	}
	hash, err := canonicalHash(map[string]any{
		"schema_version":        reference.SchemaVersion,
		"durable_envelope_hash": reference.DurableEnvelopeHash,
	})
	if err != nil || hash != reference.EnvelopeReferenceHash {
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
