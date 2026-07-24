package lifecycle

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

const (
	ompProductionWrapperIdentitySchemaVersion = "ananke.omp-production-wrapper-identity.v1"
	ompProductionP3cAction                    = "retry_process_admission"
	ompProductionP3dHostSpecHash              = "sha256:f176da10c1c055b930b17137d790580400c9a0e6963e967290a19cb608512e8b"
	ompProductionP3dSourceSnapshotHash        = "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4"
	ompProductionSourceManifestHash           = "sha256:842188d5ce1e461839bf33fb50a4040a3bf9f2e44d94c31be640058f5765cc15"
	ompProductionWrapperBinarySHA256          = "sha256:ac36f5816b1a6caaf4e4bed488e90d94c426cf9f126678c4c0f1eb50dc231a91"
	ompProductionWrapperKind                  = "ananke_omp_readonly_wrapper_v1"
	ompProductionWrapperRoute                 = "ananke_omp_read_only_audit_v1"
)

var (
	ompProductionDeadline = time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	errOMPProductionActivationDenied = errors.New("OMP production activation preparation denied")
)

// ompApprovedWrapperIdentityManifest is an immutable identity declaration. It
// deliberately has no path, command, argv, environment, or executable handle.
type ompApprovedWrapperIdentityManifest struct {
	schemaVersion string
	binarySHA256  string
	kind          string
	route         string
}

// ompProductionActivationDescriptors carries the only launch surfaces that
// this core may prepare. The descriptors remain caller-owned and this core
// neither opens, closes, reads, writes, nor duplicates them.
type ompProductionActivationDescriptors struct {
	source   *os.File
	manifest *os.File
	evidence *os.File
}

// ompProductionActivationInput contains the P3f identity facts that must be
// verified together before a descriptor-only request can be prepared.
type ompProductionActivationInput struct {
	launchSpecHash        string
	fence                 store.LaunchFence
	deadline              time.Time
	p3cAction             string
	p3dHostSpecHash       string
	p3dSourceSnapshotHash string
	sourceManifestHash    string
	wrapper               ompApprovedWrapperIdentityManifest
	descriptors           ompProductionActivationDescriptors
}

// ompPreparedFDActivationRequest is intentionally inert. It is not a launch
// capability: no method on this type can start a process or resolve a program.
type ompPreparedFDActivationRequest struct {
	launchSpecHash        string
	fence                 store.LaunchFence
	deadline              time.Time
	p3cAction             string
	p3dHostSpecHash       string
	p3dSourceSnapshotHash string
	sourceManifestHash    string
	wrapper               ompApprovedWrapperIdentityManifest
	source                *os.File
	manifest              *os.File
	evidence              *os.File
}

type ompProductionFenceReader interface {
	GetLaunchRecoveryBoundary(context.Context, string) (store.LaunchRecoveryBoundary, error)
}

// ompProductionActivationPreparer validates one accepted wrapper identity and
// prepares only typed descriptors. It never obtains execution authority.
type ompProductionActivationPreparer struct {
	fence    ompProductionFenceReader
	approved ompApprovedWrapperIdentityManifest
	now      func() time.Time
}

func newOMPProductionActivationPreparer(fence ompProductionFenceReader, approved ompApprovedWrapperIdentityManifest, now func() time.Time) (*ompProductionActivationPreparer, error) {
	if fence == nil || now == nil || !validOMPApprovedWrapperIdentity(approved) {
		return nil, errOMPProductionActivationDenied
	}
	return &ompProductionActivationPreparer{fence: fence, approved: approved, now: now}, nil
}

func (preparer *ompProductionActivationPreparer) prepare(ctx context.Context, input ompProductionActivationInput) (ompPreparedFDActivationRequest, error) {
	if preparer == nil || ctx == nil || !preparer.validInput(ctx, input) {
		return ompPreparedFDActivationRequest{}, errOMPProductionActivationDenied
	}
	return ompPreparedFDActivationRequest{
		launchSpecHash:        input.launchSpecHash,
		fence:                 input.fence,
		deadline:              input.deadline,
		p3cAction:             input.p3cAction,
		p3dHostSpecHash:       input.p3dHostSpecHash,
		p3dSourceSnapshotHash: input.p3dSourceSnapshotHash,
		sourceManifestHash:    input.sourceManifestHash,
		wrapper:               input.wrapper,
		source:                input.descriptors.source,
		manifest:              input.descriptors.manifest,
		evidence:              input.descriptors.evidence,
	}, nil
}

func (preparer *ompProductionActivationPreparer) validInput(ctx context.Context, input ompProductionActivationInput) bool {
	return validOMPProductionActivationDescriptors(input.descriptors) &&
		input.wrapper == preparer.approved &&
		input.deadline.Equal(ompProductionDeadline) &&
		preparer.now().UTC().Before(input.deadline) &&
		input.p3cAction == ompProductionP3cAction &&
		input.p3dHostSpecHash == ompProductionP3dHostSpecHash &&
		validOMPSHA256(input.p3dSourceSnapshotHash) &&
		input.p3dSourceSnapshotHash == ompProductionP3dSourceSnapshotHash &&
		input.sourceManifestHash == ompProductionSourceManifestHash &&
		preparer.validFence(ctx, input.launchSpecHash, input.fence)
}

func (preparer *ompProductionActivationPreparer) validFence(ctx context.Context, launchSpecHash string, fence store.LaunchFence) bool {
	if !validOMPSHA256(launchSpecHash) {
		return false
	}
	boundary, err := preparer.fence.GetLaunchRecoveryBoundary(ctx, launchSpecHash)
	if err != nil || validateFencedLaunchBoundary(boundary) != nil ||
		boundary.Action != store.LaunchRecoveryRetryProcessAdmission ||
		boundary.Claim.Fence != fence || boundary.Outbox.LaunchFence != fence {
		return false
	}
	return true
}

func ompProductionApprovedWrapperIdentity() ompApprovedWrapperIdentityManifest {
	return ompApprovedWrapperIdentityManifest{
		schemaVersion: ompProductionWrapperIdentitySchemaVersion,
		binarySHA256:  ompProductionWrapperBinarySHA256,
		kind:          ompProductionWrapperKind,
		route:         ompProductionWrapperRoute,
	}
}

func validOMPApprovedWrapperIdentity(identity ompApprovedWrapperIdentityManifest) bool {
	return identity == ompProductionApprovedWrapperIdentity()
}

func validOMPProductionActivationDescriptors(descriptors ompProductionActivationDescriptors) bool {
	if descriptors.source == nil || descriptors.manifest == nil || descriptors.evidence == nil {
		return false
	}
	sourceFD := descriptors.source.Fd()
	manifestFD := descriptors.manifest.Fd()
	evidenceFD := descriptors.evidence.Fd()
	invalid := ^uintptr(0)
	return sourceFD != invalid && manifestFD != invalid && evidenceFD != invalid &&
		sourceFD != manifestFD && sourceFD != evidenceFD && manifestFD != evidenceFD
}

func validOMPSHA256(value string) bool {
	if len(value) != len("sha256:")+64 || value[:len("sha256:")] != "sha256:" {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
