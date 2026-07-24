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
	externalSupervisorDeadline               = "2026-07-30T12:00:00Z"
	externalSupervisorAttemptCap             = 3
	externalSupervisorEvidenceSchemaVersion  = "ananke.remote-supervisor-evidence.v1"
	externalSupervisorOutputSchemaVersion    = "ananke.omp-production-output.v1"
)

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

// externalSupervisorHandoffTarget is intentionally an in-process boundary.
// This package supplies no concrete target; package tests supply the one fake.
type externalSupervisorHandoffTarget interface {
	Deliver(context.Context, store.ExternalSupervisorEnvelope) (store.ExternalSupervisorAcceptanceReceipt, error)
	Reconcile(context.Context, store.ExternalSupervisorAcceptanceReceipt) (*store.ExternalSupervisorCallback, error)
	Cancel(context.Context, store.ExternalSupervisorCancellation) error
}

// externalSupervisorHandoffRuntime retains no route, endpoint, command,
// credential, artifact, source, evidence, process, or OMP capability. It only
// stages sealed identities and delegates to an injected in-process test target.
type externalSupervisorHandoffRuntime struct {
	journal       *store.Store
	target        externalSupervisorHandoffTarget
	authenticator store.ExternalSupervisorAuthenticator
	currentRoot   func() store.ExternalSupervisorTrustRoot
}

func newExternalSupervisorHandoffRuntime(journal *store.Store, target externalSupervisorHandoffTarget, authenticator store.ExternalSupervisorAuthenticator, currentRoot func() store.ExternalSupervisorTrustRoot) (*externalSupervisorHandoffRuntime, error) {
	if journal == nil || target == nil || authenticator == nil || currentRoot == nil {
		return nil, errExternalSupervisorRuntimeDenied
	}
	return &externalSupervisorHandoffRuntime{
		journal: journal, target: target, authenticator: authenticator, currentRoot: currentRoot,
	}, nil
}

// submit persists before in-process fake delivery. It returns the same closed
// output on success and every failure; a receipt is not a terminal outcome.
func (runtime *externalSupervisorHandoffRuntime) submit(ctx context.Context, envelope store.ExternalSupervisorEnvelope, fence store.LaunchFence) externalSupervisorPublicOutput {
	output := externalSupervisorFailClosedOutput()
	if runtime == nil || ctx == nil || !validExternalSupervisorEnvelope(envelope) {
		return output
	}
	handoff, err := runtime.journal.StageExternalSupervisorHandoff(ctx, envelope, fence)
	if err != nil {
		return output
	}
	runtime.deliver(ctx, handoff.Envelope.HandoffID)
	return output
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

	var callback *store.ExternalSupervisorCallback
	if err := runtime.journal.WithExternalSupervisorDeliveryAdmission(ctx, handoffID, func(store.ExternalSupervisorEnvelope) error {
		var reconcileErr error
		callback, reconcileErr = runtime.target.Reconcile(ctx, *boundary.Receipt)
		return reconcileErr
	}); err != nil || callback == nil {
		return output
	}
	root := runtime.currentRoot()
	_, _ = runtime.journal.AcceptExternalSupervisorCallback(ctx, *callback, root, runtime.authenticator)
	return output
}

// cancel persists only a receipt-bound cancellation intent after the journal
// reauthenticates the full private fence. A fake cancellation acknowledgement
// never becomes a terminal result.
func (runtime *externalSupervisorHandoffRuntime) cancel(ctx context.Context, cancellation store.ExternalSupervisorCancellation, fence store.LaunchFence) externalSupervisorPublicOutput {
	output := externalSupervisorFailClosedOutput()
	if runtime == nil || ctx == nil {
		return output
	}
	handoff, err := runtime.journal.GetExternalSupervisorHandoff(ctx, cancellation.HandoffID)
	if err != nil || !validExternalSupervisorEnvelope(handoff.Envelope) {
		return output
	}
	accepted, err := runtime.journal.RecordExternalSupervisorCancellation(ctx, cancellation, fence)
	if err != nil {
		return output
	}
	_ = runtime.target.Cancel(ctx, accepted)
	return output
}

func (runtime *externalSupervisorHandoffRuntime) deliver(ctx context.Context, handoffID string) {
	boundary, err := runtime.journal.GetExternalSupervisorRecoveryBoundary(ctx, handoffID)
	if err != nil || boundary.Receipt != nil || !validExternalSupervisorEnvelope(boundary.Handoff.Envelope) {
		return
	}
	var receipt store.ExternalSupervisorAcceptanceReceipt
	if err := runtime.journal.WithExternalSupervisorDeliveryAdmission(ctx, handoffID, func(envelope store.ExternalSupervisorEnvelope) error {
		var deliveryErr error
		receipt, deliveryErr = runtime.target.Deliver(ctx, envelope)
		return deliveryErr
	}); err != nil {
		return
	}
	root := runtime.currentRoot()
	_, _ = runtime.journal.AcceptExternalSupervisorReceipt(ctx, receipt, root, runtime.authenticator)
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
