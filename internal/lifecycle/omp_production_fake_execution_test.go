package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

const (
	ompProductionOutputSchemaVersion          = "ananke.omp-production-output.v1"
	ompProductionSeatbeltExecutable           = "/usr/bin/sandbox-exec"
	ompProductionSeatbeltProfile              = "(version 1) (allow default) (deny file-write*)"
	ompProductionFakeWrapperArtifactSHA256    = "sha256:844afd703c0709afe8d5861c214fd2e0fd04abc498d47e655e6d41472fe73ec1"
	ompProductionFakeWrapperChildArgument     = "omp-production-fake-wrapper-child-v1"
	ompProductionFakeWrapperArtifactTestdata  = "omp-production-fake-wrapper-v1"
	ompProductionFakeWrapperArtifactDirectory = "testdata"
)

var (
	errOMPProductionExecutionDenied    = errors.New("OMP production wrapper execution denied")
	errOMPProductionSandboxUnsupported = errors.New("OMP production OS sandbox capability unavailable")
)

type ompProductionExecutionStage string

const (
	ompProductionExecutionStageRequest    ompProductionExecutionStage = "request_validation"
	ompProductionExecutionStageDescriptor ompProductionExecutionStage = "descriptor_validation"
	ompProductionExecutionStageAdmission  ompProductionExecutionStage = "fence_admission"
	ompProductionExecutionStageSandbox    ompProductionExecutionStage = "sandbox"
	ompProductionExecutionStageCleanup    ompProductionExecutionStage = "cleanup"
)

// ompProductionExecutionFailure retains a private test-only cause while its
// Error and Unwrap methods preserve the stable production-shaped denial.
type ompProductionExecutionFailure struct {
	stage ompProductionExecutionStage
	cause error
}

func (failure *ompProductionExecutionFailure) Error() string {
	return errOMPProductionExecutionDenied.Error()
}

func (failure *ompProductionExecutionFailure) Unwrap() error {
	return errOMPProductionExecutionDenied
}

func ompDenyExecution(stage ompProductionExecutionStage, cause error) error {
	return &ompProductionExecutionFailure{stage: stage, cause: cause}
}

// ompProductionExecutionOutput is a test-only closed projection. It never
// returns fake-wrapper output, descriptor identity, or fence state.
type ompProductionExecutionOutput struct {
	Events            []ompProductionExecutionEvent `json:"events"`
	Result            *ompProductionExecutionResult `json:"result"`
	SchemaVersion     string                        `json:"schema_version"`
	State             string                        `json:"state"`
	VerificationState string                        `json:"verification_state"`
}

type ompProductionExecutionEvent struct{}
type ompProductionExecutionResult struct{}

func ompProductionFailClosedOutput() ompProductionExecutionOutput {
	return ompProductionExecutionOutput{
		Events:            []ompProductionExecutionEvent{},
		Result:            nil,
		SchemaVersion:     ompProductionOutputSchemaVersion,
		State:             "waiting_for_human",
		VerificationState: "not_run",
	}
}

type ompProductionExecutionFence interface {
	GetLaunchRecoveryBoundary(context.Context, string) (store.LaunchRecoveryBoundary, error)
	WithLaunchFenceAdmission(context.Context, string, store.LaunchFence, func(store.LaunchRecoveryBoundary) error) error
}

// ompProductionSandboxFiles contains only the inherited descriptors used by
// the fixed fake test child. It deliberately has no wrapper path or program.
type ompProductionSandboxFiles struct {
	source   *os.File
	manifest *os.File
	evidence *os.File
}

type ompProductionSandbox interface {
	start(context.Context, ompProductionSandboxFiles) error
}

// ompProductionFakeWrapperExecutor is compiled only into package tests. Its
// constructor accepts no executable, artifact, approval, path, argv, or
// environment input. The only child it can start is this package's test binary.
type ompProductionFakeWrapperExecutor struct {
	fence   ompProductionExecutionFence
	wrapper ompProductionSealedWrapper
	now     func() time.Time
	sandbox ompProductionSandbox

	mu     sync.Mutex
	closed bool
}

// ompProductionOwnedDescriptor is a test-owned duplicate with the device and
// inode identity it must retain through fake execution and cleanup.
type ompProductionOwnedDescriptor struct {
	file   *os.File
	device uint64
	inode  uint64
	kind   uint16
}

type ompProductionOwnedDescriptors struct {
	source   ompProductionOwnedDescriptor
	manifest ompProductionOwnedDescriptor
	evidence ompProductionOwnedDescriptor
}

// ompProductionSealedWrapper owns a copy of one fixed testdata artifact. The
// fake sandbox never executes this pathname; it re-execs the Go test binary.
type ompProductionSealedWrapper struct {
	descriptor ompProductionOwnedDescriptor
	parent     *os.File
	parentDev  uint64
	parentIno  uint64
	name       string
}

func newOMPProductionFakeWrapperExecutor(fence ompProductionExecutionFence, now func() time.Time) (*ompProductionFakeWrapperExecutor, error) {
	if fence == nil || now == nil {
		return nil, errOMPProductionExecutionDenied
	}
	wrapper, err := ompStageFixedFakeWrapperArtifact()
	if err != nil {
		return nil, errOMPProductionExecutionDenied
	}
	return &ompProductionFakeWrapperExecutor{
		fence: fence, wrapper: wrapper, now: now, sandbox: ompProductionFakeTestBinarySandbox{},
	}, nil
}

// execute admits only a prepared inert request and runs the fixed fake child
// inside the test-only sandbox. No production type has a method that calls it.
func (executor *ompProductionFakeWrapperExecutor) execute(ctx context.Context, prepared ompPreparedFDActivationRequest) (output ompProductionExecutionOutput, returned error) {
	output = ompProductionFailClosedOutput()
	if executor == nil || ctx == nil {
		return output, ompDenyExecution(ompProductionExecutionStageRequest, errOMPProductionExecutionDenied)
	}

	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.closed || !executor.validPrepared(ctx, prepared) {
		return output, ompDenyExecution(ompProductionExecutionStageRequest, errOMPProductionExecutionDenied)
	}

	owned, err := ompDuplicateOwnedDescriptors(prepared)
	if err != nil {
		return output, ompDenyExecution(ompProductionExecutionStageDescriptor, err)
	}
	defer func() {
		if closeErr := owned.close(); closeErr != nil && returned == nil {
			returned = ompDenyExecution(ompProductionExecutionStageCleanup, closeErr)
		}
	}()

	if err := executor.fence.WithLaunchFenceAdmission(ctx, prepared.launchSpecHash, prepared.fence, func(boundary store.LaunchRecoveryBoundary) error {
		if !executor.validPreparedIdentity(prepared) || !owned.validate() || !executor.validWrapperAtBoundary() ||
			!validOMPProductionFenceBoundary(prepared.launchSpecHash, prepared.fence, boundary) {
			return ompDenyExecution(ompProductionExecutionStageAdmission, errOMPProductionExecutionDenied)
		}
		if !executor.now().UTC().Before(prepared.deadline) {
			return ompDenyExecution(ompProductionExecutionStageAdmission, errOMPProductionExecutionDenied)
		}
		if executor.sandbox == nil {
			return ompDenyExecution(ompProductionExecutionStageSandbox, errOMPProductionExecutionDenied)
		}
		if err := executor.sandbox.start(ctx, ompProductionSandboxFiles{
			source: owned.source.file, manifest: owned.manifest.file, evidence: owned.evidence.file,
		}); err != nil {
			return ompDenyExecution(ompProductionExecutionStageSandbox, err)
		}
		return nil
	}); err != nil {
		if failure := new(ompProductionExecutionFailure); errors.As(err, &failure) {
			return output, err
		}
		return output, ompDenyExecution(ompProductionExecutionStageAdmission, err)
	}
	return output, nil
}

func (executor *ompProductionFakeWrapperExecutor) close() error {
	if executor == nil {
		return nil
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	if executor.closed {
		return nil
	}
	executor.closed = true
	return executor.wrapper.close()
}

func (executor *ompProductionFakeWrapperExecutor) validPrepared(ctx context.Context, prepared ompPreparedFDActivationRequest) bool {
	if !executor.validPreparedIdentity(prepared) || !validOMPProductionActivationDescriptors(ompProductionActivationDescriptors{
		source: prepared.source, manifest: prepared.manifest, evidence: prepared.evidence,
	}) {
		return false
	}
	boundary, err := executor.fence.GetLaunchRecoveryBoundary(ctx, prepared.launchSpecHash)
	return err == nil && validOMPProductionFenceBoundary(prepared.launchSpecHash, prepared.fence, boundary)
}

func (executor *ompProductionFakeWrapperExecutor) validPreparedIdentity(prepared ompPreparedFDActivationRequest) bool {
	return prepared.wrapper == ompProductionApprovedWrapperIdentity() &&
		prepared.deadline.Equal(ompProductionDeadline) &&
		prepared.p3cAction == ompProductionP3cAction &&
		prepared.p3dHostSpecHash == ompProductionP3dHostSpecHash &&
		prepared.p3dSourceSnapshotHash == ompProductionP3dSourceSnapshotHash &&
		prepared.sourceManifestHash == ompProductionSourceManifestHash &&
		validOMPSHA256(prepared.launchSpecHash)
}

func (executor *ompProductionFakeWrapperExecutor) validWrapperAtBoundary() bool {
	return executor.wrapper.validate() &&
		ompHashOwnedDescriptor(executor.wrapper.descriptor) == ompProductionFakeWrapperArtifactSHA256
}

func validOMPProductionFenceBoundary(launchSpecHash string, fence store.LaunchFence, boundary store.LaunchRecoveryBoundary) bool {
	return validateFencedLaunchBoundary(boundary) == nil &&
		boundary.LaunchSpecHash == launchSpecHash &&
		boundary.Action == store.LaunchRecoveryRetryProcessAdmission &&
		boundary.Claim.Fence == fence &&
		boundary.Outbox.LaunchFence == fence
}

type ompProductionDescriptorKind uint8

const (
	ompProductionDescriptorRegular ompProductionDescriptorKind = iota
	ompProductionDescriptorSource
	ompProductionDescriptorData
)

func ompDuplicateOwnedDescriptors(prepared ompPreparedFDActivationRequest) (ompProductionOwnedDescriptors, error) {
	source, err := ompDuplicateOwnedDescriptor(prepared.source, ompProductionDescriptorSource)
	if err != nil {
		return ompProductionOwnedDescriptors{}, err
	}
	manifest, err := ompDuplicateOwnedDescriptor(prepared.manifest, ompProductionDescriptorData)
	if err != nil {
		_ = source.file.Close()
		return ompProductionOwnedDescriptors{}, err
	}
	evidence, err := ompDuplicateOwnedDescriptor(prepared.evidence, ompProductionDescriptorData)
	if err != nil {
		_ = source.file.Close()
		_ = manifest.file.Close()
		return ompProductionOwnedDescriptors{}, err
	}
	return ompProductionOwnedDescriptors{source: source, manifest: manifest, evidence: evidence}, nil
}

func ompDuplicateOwnedDescriptor(source *os.File, expected ompProductionDescriptorKind) (ompProductionOwnedDescriptor, error) {
	if source == nil || source.Fd() == ^uintptr(0) {
		return ompProductionOwnedDescriptor{}, errOMPProductionExecutionDenied
	}
	var initial unix.Stat_t
	if err := unix.Fstat(int(source.Fd()), &initial); err != nil || !validOMPProductionDescriptorKind(initial.Mode, expected) {
		return ompProductionOwnedDescriptor{}, errOMPProductionExecutionDenied
	}
	fd, err := unix.Dup(int(source.Fd()))
	if err != nil {
		return ompProductionOwnedDescriptor{}, err
	}
	duplicate := os.NewFile(uintptr(fd), "")
	if duplicate == nil {
		_ = unix.Close(fd)
		return ompProductionOwnedDescriptor{}, errOMPProductionExecutionDenied
	}
	descriptor := ompProductionOwnedDescriptor{
		file: duplicate, device: uint64(initial.Dev), inode: uint64(initial.Ino), kind: initial.Mode & unix.S_IFMT,
	}
	if !descriptor.validate(expected) {
		_ = duplicate.Close()
		return ompProductionOwnedDescriptor{}, errOMPProductionExecutionDenied
	}
	return descriptor, nil
}

func ompOwnedDescriptorFromFile(file *os.File, expected ompProductionDescriptorKind) (ompProductionOwnedDescriptor, error) {
	if file == nil || file.Fd() == ^uintptr(0) {
		return ompProductionOwnedDescriptor{}, errOMPProductionExecutionDenied
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil || !validOMPProductionDescriptorKind(stat.Mode, expected) {
		return ompProductionOwnedDescriptor{}, errOMPProductionExecutionDenied
	}
	return ompProductionOwnedDescriptor{file: file, device: uint64(stat.Dev), inode: uint64(stat.Ino), kind: stat.Mode & unix.S_IFMT}, nil
}

func (descriptor ompProductionOwnedDescriptor) validate(expected ompProductionDescriptorKind) bool {
	if descriptor.file == nil || descriptor.file.Fd() == ^uintptr(0) {
		return false
	}
	var stat unix.Stat_t
	return unix.Fstat(int(descriptor.file.Fd()), &stat) == nil &&
		uint64(stat.Dev) == descriptor.device && uint64(stat.Ino) == descriptor.inode &&
		stat.Mode&unix.S_IFMT == descriptor.kind && validOMPProductionDescriptorKind(stat.Mode, expected)
}

func (descriptors ompProductionOwnedDescriptors) validate() bool {
	return descriptors.source.validate(ompProductionDescriptorSource) &&
		descriptors.manifest.validate(ompProductionDescriptorData) &&
		descriptors.evidence.validate(ompProductionDescriptorData)
}

func (descriptors ompProductionOwnedDescriptors) close() error {
	var first error
	for _, descriptor := range []ompProductionOwnedDescriptor{descriptors.source, descriptors.manifest, descriptors.evidence} {
		if descriptor.file != nil {
			if err := descriptor.file.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

func validOMPProductionDescriptorKind(mode uint16, expected ompProductionDescriptorKind) bool {
	switch expected {
	case ompProductionDescriptorRegular:
		return mode&unix.S_IFMT == unix.S_IFREG
	case ompProductionDescriptorSource:
		kind := mode & unix.S_IFMT
		return kind == unix.S_IFREG || kind == unix.S_IFDIR
	case ompProductionDescriptorData:
		kind := mode & unix.S_IFMT
		return kind == unix.S_IFREG || kind == unix.S_IFIFO
	default:
		return false
	}
}

func ompHashOwnedDescriptor(descriptor ompProductionOwnedDescriptor) string {
	if !descriptor.validate(ompProductionDescriptorRegular) {
		return ""
	}
	if _, err := descriptor.file.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, descriptor.file); err != nil {
		return ""
	}
	if _, err := descriptor.file.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func ompStageFixedFakeWrapperArtifact() (ompProductionSealedWrapper, error) {
	artifact, err := os.ReadFile(filepath.Join(ompProductionFakeWrapperArtifactDirectory, ompProductionFakeWrapperArtifactTestdata))
	if err != nil || ompHashBytes(artifact) != ompProductionFakeWrapperArtifactSHA256 {
		return ompProductionSealedWrapper{}, errOMPProductionExecutionDenied
	}

	directoryName, err := os.MkdirTemp("", "ananke-omp-test-wrapper-")
	if err != nil {
		return ompProductionSealedWrapper{}, err
	}
	cleanupDirectory := true
	defer func() {
		if cleanupDirectory {
			_ = os.RemoveAll(directoryName)
		}
	}()

	parent, err := os.Open(directoryName)
	if err != nil {
		return ompProductionSealedWrapper{}, err
	}
	var parentStat unix.Stat_t
	if err := unix.Fstat(int(parent.Fd()), &parentStat); err != nil || parentStat.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = parent.Close()
		return ompProductionSealedWrapper{}, errOMPProductionExecutionDenied
	}

	name := "fixed-test-artifact"
	target, err := os.OpenFile(filepath.Join(directoryName, name), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		_ = parent.Close()
		return ompProductionSealedWrapper{}, err
	}
	written, writeErr := target.Write(artifact)
	syncErr := target.Sync()
	chmodErr := target.Chmod(0o400)
	if writeErr != nil || syncErr != nil || chmodErr != nil || written != len(artifact) {
		_ = target.Close()
		_ = parent.Close()
		return ompProductionSealedWrapper{}, errOMPProductionExecutionDenied
	}
	descriptor, err := ompOwnedDescriptorFromFile(target, ompProductionDescriptorRegular)
	if err != nil || ompHashOwnedDescriptor(descriptor) != ompProductionFakeWrapperArtifactSHA256 {
		_ = target.Close()
		_ = parent.Close()
		return ompProductionSealedWrapper{}, errOMPProductionExecutionDenied
	}
	sealed := ompProductionSealedWrapper{
		descriptor: descriptor, parent: parent, parentDev: uint64(parentStat.Dev), parentIno: uint64(parentStat.Ino), name: name,
	}
	if !sealed.validate() {
		_ = target.Close()
		_ = parent.Close()
		return ompProductionSealedWrapper{}, errOMPProductionExecutionDenied
	}
	cleanupDirectory = false
	return sealed, nil
}

func ompHashBytes(bytes []byte) string {
	sum := sha256.Sum256(bytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (sealed *ompProductionSealedWrapper) validate() bool {
	if sealed == nil || sealed.parent == nil || sealed.parent.Fd() == ^uintptr(0) || sealed.name != "fixed-test-artifact" ||
		!sealed.descriptor.validate(ompProductionDescriptorRegular) {
		return false
	}
	var parent unix.Stat_t
	if unix.Fstat(int(sealed.parent.Fd()), &parent) != nil || parent.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(parent.Dev) != sealed.parentDev || uint64(parent.Ino) != sealed.parentIno {
		return false
	}
	fd, err := unix.Openat(int(sealed.parent.Fd()), sealed.name, unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer func() { _ = unix.Close(fd) }()
	var child unix.Stat_t
	return unix.Fstat(fd, &child) == nil && child.Mode&unix.S_IFMT == unix.S_IFREG &&
		uint64(child.Dev) == sealed.descriptor.device && uint64(child.Ino) == sealed.descriptor.inode
}

// close always releases the descriptor and directory handles. If the bound
// path has been replaced, it reports the ownership denial without touching the
// replacement, after closing the now-unlinkable owned inode handles.
func (sealed *ompProductionSealedWrapper) close() error {
	if sealed == nil {
		return nil
	}

	var first error
	if !sealed.validate() {
		first = errOMPProductionExecutionDenied
	} else if err := unix.Unlinkat(int(sealed.parent.Fd()), sealed.name, 0); err != nil {
		first = err
	} else if !ompNamedDirectoryMatches(sealed.parent.Name(), sealed.parentDev, sealed.parentIno) {
		first = errOMPProductionExecutionDenied
	} else if err := os.Remove(sealed.parent.Name()); err != nil {
		first = err
	}

	if sealed.descriptor.file != nil {
		if err := sealed.descriptor.file.Close(); err != nil && first == nil {
			first = err
		}
		sealed.descriptor.file = nil
	}
	if sealed.parent != nil {
		if err := sealed.parent.Close(); err != nil && first == nil {
			first = err
		}
		sealed.parent = nil
	}
	return first
}

func ompNamedDirectoryMatches(directory string, device, inode uint64) bool {
	var stat unix.Stat_t
	return unix.Stat(directory, &stat) == nil && stat.Mode&unix.S_IFMT == unix.S_IFDIR &&
		uint64(stat.Dev) == device && uint64(stat.Ino) == inode
}

type ompProductionFakeTestBinarySandbox struct{}

func (ompProductionFakeTestBinarySandbox) start(ctx context.Context, files ompProductionSandboxFiles) error {
	if runtime.GOOS != "darwin" {
		return errOMPProductionSandboxUnsupported
	}
	info, err := os.Stat(ompProductionSeatbeltExecutable)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return errOMPProductionSandboxUnsupported
	}
	if ctx == nil || files.source == nil || files.manifest == nil || files.evidence == nil ||
		files.source.Fd() == ^uintptr(0) || files.manifest.Fd() == ^uintptr(0) || files.evidence.Fd() == ^uintptr(0) {
		return errOMPProductionExecutionDenied
	}
	command := exec.CommandContext(ctx, ompProductionSeatbeltExecutable, "-p", ompProductionSeatbeltProfile,
		os.Args[0], "-test.run=^TestOMPProductionFakeWrapperChild$", ompProductionFakeWrapperChildArgument)
	command.Dir = "/"
	command.Env = []string{}
	command.ExtraFiles = []*os.File{files.source, files.manifest, files.evidence}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command.Run()
}

type ompProductionUnsupportedSandbox struct{}

func (ompProductionUnsupportedSandbox) start(context.Context, ompProductionSandboxFiles) error {
	return errOMPProductionSandboxUnsupported
}
