package lifecycle

import (
	"context"
	"errors"

	"github.com/yingliang-zhang/ananke/internal/store"
)

const (
	externalSupervisorRouteMappingHash       = "sha256:a468e940e5dd5752285b8aba2533109cfde2d8b259a007647ca6f431e0736603"
	externalSupervisorP3dSourceSnapshotHash  = "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4"
	externalSupervisorSourceManifestHash     = "sha256:842188d5ce1e461839bf33fb50a4040a3bf9f2e44d94c31be640058f5765cc15"
	externalSupervisorRepositoryIdentity     = "github.com/yingliang-zhang/ananke"
	externalSupervisorArtifactSHA256         = "sha256:fe7ce7ab9cb07d010a0a02526674efeb486fecc50ce07699acac5a305179588d"
	externalSupervisorBuildIdentityHash      = "sha256:771b9391bb1445c5186b7033c1eed137eafbc4afeca5a7dc712ea8993d57e0df"
	externalSupervisorReleaseAttestationHash = "sha256:2ac3954f26baa6a33f87f455f6081beeb9ed27725ad4d56961be2fda86662475"
	externalSupervisorReleaseApprovalHash    = "sha256:65509b813e0563c23b6f871e9005f4db76d5790cc325267e3963d3672cd60fe6"
	externalSupervisorEvidenceContractHash   = "sha256:9309381f36076c263c60d6ef3db5e93b52694d645ffbbef25a4d87dce6459a05"
	externalSupervisorDeadline               = "2031-07-31T12:00:00Z"
	externalSupervisorAttemptCap             = 3
	externalSupervisorEvidenceSchemaVersion  = "ananke.remote-supervisor-evidence.v1"
	externalSupervisorOutputSchemaVersion    = "ananke.omp-production-output.v1"
)

// ExternalSupervisorPredecessorReleaseIdentity returns the frozen local P3f
// predecessor pins used by both lifecycle admission and transport authentication.
func ExternalSupervisorPredecessorReleaseIdentity() store.ExternalSupervisorPredecessorReleaseIdentity {
	return store.ExternalSupervisorPredecessorReleaseIdentity{
		SupervisorArtifactSHA256: externalSupervisorArtifactSHA256,
		BuildIdentityHash:        externalSupervisorBuildIdentityHash,
		ReleaseAttestationHash:   externalSupervisorReleaseAttestationHash,
		ReleaseApprovalHash:      externalSupervisorReleaseApprovalHash,
	}
}

var errExternalSupervisorRuntimeDenied = errors.New("external supervisor handoff runtime denied")

// externalSupervisorPublicOutput is deliberately a normalized no-authority
// projection. Receipt, callback, cancellation, target, and trust-root facts
// remain private durable identities and never appear here.
type externalSupervisorPublicOutput struct {
	Events            []externalSupervisorPublicEvent `json:"events"`
	Result            *externalSupervisorPublicResult `json:"result"`
	SchemaVersion     string                          `json:"schema_version"`
	State             string                          `json:"state"`
	VerificationState string                          `json:"verification_state"`
}

type externalSupervisorPublicEvent struct{}
type externalSupervisorPublicResult struct{}

func externalSupervisorFailClosedOutput() externalSupervisorPublicOutput {
	return externalSupervisorPublicOutput{
		Events:            []externalSupervisorPublicEvent{},
		Result:            nil,
		SchemaVersion:     externalSupervisorOutputSchemaVersion,
		State:             "waiting_for_human",
		VerificationState: "not_run",
	}
}

// externalSupervisorHandoffTransport carries only exact authenticated P3f
// records. Reconcile and cancel must receive the durable envelope and receipt.
type externalSupervisorHandoffTransport interface {
	Deliver(context.Context, store.ExternalSupervisorEnvelope) (store.ExternalSupervisorAuthenticatedReceipt, error)
	Reconcile(context.Context, store.ExternalSupervisorEnvelope, store.ExternalSupervisorAuthenticatedReceipt) (*store.ExternalSupervisorAuthenticatedCallback, error)
	Cancel(context.Context, store.ExternalSupervisorEnvelope, store.ExternalSupervisorAuthenticatedReceipt, store.ExternalSupervisorCancellation) (store.ExternalSupervisorAuthenticatedCancellation, error)
}

// ExternalSupervisorHandoffTransport exposes the existing identity-only seam
// for production composition roots. Implementations remain outside lifecycle.
type ExternalSupervisorHandoffTransport = externalSupervisorHandoffTransport

// externalSupervisorHandoffRuntime retains no route, endpoint, command,
// credential, artifact, source, evidence, process, or OMP capability. It only
// stages sealed identities and delegates to an injected transport.
type externalSupervisorHandoffRuntime struct {
	journal       *store.Store
	transport     externalSupervisorHandoffTransport
	authenticator store.ExternalSupervisorAuthenticator
}

// ExternalSupervisorHandoffRuntime and ExternalSupervisorPublicOutput expose
// the existing fail-closed runtime without widening its authority or fields.
type ExternalSupervisorHandoffRuntime = externalSupervisorHandoffRuntime
type ExternalSupervisorPublicOutput = externalSupervisorPublicOutput

func newExternalSupervisorHandoffRuntime(journal *store.Store, transport externalSupervisorHandoffTransport, authenticator store.ExternalSupervisorAuthenticator) (*externalSupervisorHandoffRuntime, error) {
	if journal == nil || transport == nil || authenticator == nil {
		return nil, errExternalSupervisorRuntimeDenied
	}
	return &externalSupervisorHandoffRuntime{journal: journal, transport: transport, authenticator: authenticator}, nil
}

// NewExternalSupervisorHandoffRuntime injects a production transport and its
// independent receipt/callback authenticator into the existing private seam.
func NewExternalSupervisorHandoffRuntime(journal *store.Store, transport ExternalSupervisorHandoffTransport, authenticator store.ExternalSupervisorAuthenticator) (*ExternalSupervisorHandoffRuntime, error) {
	return newExternalSupervisorHandoffRuntime(journal, transport, authenticator)
}

// submit persists before delivery through the injected seam. It returns the same closed
// output on success and every failure; a receipt is not a terminal outcome.
func (runtime *externalSupervisorHandoffRuntime) submit(ctx context.Context, envelope store.ExternalSupervisorEnvelope, fence store.LaunchFence) externalSupervisorPublicOutput {
	output := externalSupervisorFailClosedOutput()
	if runtime == nil || ctx == nil || !validExternalSupervisorEnvelope(envelope) {
		return output
	}
	if runtime.authenticator.VerifyExternalSupervisorEnvelope(ctx, envelope) != nil {
		return output
	}
	handoff, err := runtime.journal.StageExternalSupervisorHandoff(ctx, envelope, fence)
	if err != nil {
		return output
	}
	runtime.deliver(ctx, handoff.Envelope.HandoffID)
	return output
}

// Submit stages and delivers an exact sealed handoff while retaining the
// normalized waiting_for_human projection.
func (runtime *externalSupervisorHandoffRuntime) Submit(ctx context.Context, envelope store.ExternalSupervisorEnvelope, fence store.LaunchFence) ExternalSupervisorPublicOutput {
	return runtime.submit(ctx, envelope, fence)
}

// recover replays only an immutable delivery obligation or authenticated
// receipt reconciliation. An absent response, target failure, stale fence, or
// invalid callback produces no inferred outcome.
func (runtime *externalSupervisorHandoffRuntime) recover(ctx context.Context, handoffID string) externalSupervisorPublicOutput {
	output := externalSupervisorFailClosedOutput()
	if runtime == nil || ctx == nil {
		return output
	}
	boundary, err := runtime.journal.GetExternalSupervisorRecoveryBoundary(ctx, handoffID)
	if err != nil || !validExternalSupervisorEnvelope(boundary.Handoff.Envelope) {
		return output
	}
	if boundary.Receipt == nil {
		runtime.deliver(ctx, handoffID)
		return output
	}
	if boundary.Callback != nil {
		return output
	}

	_, _ = runtime.journal.ReconcileAndPersistExternalSupervisorCallback(ctx, handoffID, runtime.authenticator, func(envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt) (*store.ExternalSupervisorAuthenticatedCallback, error) {
		return runtime.transport.Reconcile(ctx, envelope, receipt)
	})
	return output
}

// Recover reconciles only the exact durable handoff boundary.
func (runtime *externalSupervisorHandoffRuntime) Recover(ctx context.Context, handoffID string) ExternalSupervisorPublicOutput {
	return runtime.recover(ctx, handoffID)
}

// cancel persists only a receipt-bound cancellation intent after the journal
// reauthenticates the full private fence. A cancellation acknowledgement never
// becomes a terminal result.
func (runtime *externalSupervisorHandoffRuntime) cancel(ctx context.Context, cancellation store.ExternalSupervisorCancellation, fence store.LaunchFence) externalSupervisorPublicOutput {
	output := externalSupervisorFailClosedOutput()
	if runtime == nil || ctx == nil {
		return output
	}
	_, _ = runtime.journal.CancelAndPersistExternalSupervisorCancellation(ctx, cancellation, fence, runtime.authenticator, func(envelope store.ExternalSupervisorEnvelope, receipt store.ExternalSupervisorAuthenticatedReceipt, exact store.ExternalSupervisorCancellation) (store.ExternalSupervisorAuthenticatedCancellation, error) {
		return runtime.transport.Cancel(ctx, envelope, receipt, exact)
	})
	return output
}

// Cancel records and transports only a receipt-bound cancellation intent.
func (runtime *externalSupervisorHandoffRuntime) Cancel(ctx context.Context, cancellation store.ExternalSupervisorCancellation, fence store.LaunchFence) ExternalSupervisorPublicOutput {
	return runtime.cancel(ctx, cancellation, fence)
}

func (runtime *externalSupervisorHandoffRuntime) deliver(ctx context.Context, handoffID string) {
	boundary, err := runtime.journal.GetExternalSupervisorRecoveryBoundary(ctx, handoffID)
	if err != nil || boundary.Receipt != nil || !validExternalSupervisorEnvelope(boundary.Handoff.Envelope) {
		return
	}
	_, _ = runtime.journal.DeliverAndPersistExternalSupervisorReceipt(ctx, handoffID, runtime.authenticator, func(envelope store.ExternalSupervisorEnvelope) (store.ExternalSupervisorAuthenticatedReceipt, error) {
		return runtime.transport.Deliver(ctx, envelope)
	})
}

func validExternalSupervisorEnvelope(envelope store.ExternalSupervisorEnvelope) bool {
	return store.ValidateExternalSupervisorEnvelope(envelope) == nil &&
		envelope.Deadline == externalSupervisorDeadline &&
		envelope.AttemptCap == externalSupervisorAttemptCap &&
		envelope.RouteMappingHash == externalSupervisorRouteMappingHash &&
		envelope.SourceSnapshotHash == externalSupervisorP3dSourceSnapshotHash &&
		envelope.SourceManifestHash == externalSupervisorSourceManifestHash &&
		envelope.RepositoryIdentity == externalSupervisorRepositoryIdentity &&
		envelope.SupervisorArtifactSHA256 == externalSupervisorArtifactSHA256 &&
		envelope.BuildIdentityHash == externalSupervisorBuildIdentityHash &&
		envelope.ReleaseAttestationHash == externalSupervisorReleaseAttestationHash &&
		envelope.ReleaseApprovalHash == externalSupervisorReleaseApprovalHash &&
		envelope.EvidenceContractHash == externalSupervisorEvidenceContractHash &&
		envelope.EvidenceSchemaVersion == externalSupervisorEvidenceSchemaVersion
}
