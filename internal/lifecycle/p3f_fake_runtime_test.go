package lifecycle

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

const (
	p3fP3dHostSpecHash       = "sha256:f176da10c1c055b930b17137d790580400c9a0e6963e967290a19cb608512e8b"
	p3fP3dSourceSnapshotHash = "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4"
	p3fRepositoryIdentity    = "github.com/yingliang-zhang/ananke"
	p3fP3cAction             = "retry_process_admission"
	p3fWrapperBinarySHA256   = "sha256:ac36f5816b1a6caaf4e4bed488e90d94c426cf9f126678c4c0f1eb50dc231a91"
	p3fWrapperKind           = "ananke_omp_readonly_wrapper_v1"
	p3fWrapperRoute          = "ananke_omp_read_only_audit_v1"

	p3fManifestDescriptorLimit   = 64 * 1024
	p3fArchiveEntryLimit         = 1 << 20
	p3fFakeChildArgument         = "p3f-fake-child-v1"
	p3fFakeChildReadOnlyEvidence = "p3f-fake-child-os-readonly-v1"
	p3fTarBlockSize              = 512
	p3fSeatbeltExecutable        = "/usr/bin/sandbox-exec"

	p3fSourceDescriptor   = 3
	p3fManifestDescriptor = 4
	p3fEvidenceDescriptor = 5
)

var (
	p3fFixtureNow = time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)
	p3fDeadline   = time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

	errP3fDenied             = errors.New("P3f sandboxed fake-child activation denied")
	errP3fSandboxUnsupported = errors.New("P3f OS sandbox capability unavailable")
)

type p3fStartStage string

const (
	p3fStartStageRequest    p3fStartStage = "request_validation"
	p3fStartStageArchive    p3fStartStage = "tracked_archive"
	p3fStartStageDescriptor p3fStartStage = "descriptor_validation"
	p3fStartStageFence      p3fStartStage = "fence_boundary_validation"
	p3fStartStageSandbox    p3fStartStage = "sandbox"
	p3fStartStageEvidence   p3fStartStage = "fake_child_evidence"
	p3fStartStageCleanup    p3fStartStage = "cleanup"
)

// p3fStartFailure keeps the private failure cause available to same-package
// deterministic tests while every caller gets only the stable denial string.
type p3fStartFailure struct {
	stage p3fStartStage
	cause error
}

func (failure *p3fStartFailure) Error() string { return errP3fDenied.Error() }
func (failure *p3fStartFailure) Unwrap() error { return errP3fDenied }

func p3fDeny(stage p3fStartStage, cause error) error {
	return &p3fStartFailure{stage: stage, cause: cause}
}

// p3fTrackedSourceManifest binds immutable archive bytes to opaque Git archive
// entries. The PAX commit is only consistency metadata; it never authenticates
// the archive.
type p3fTrackedSourceManifest struct {
	ArchiveSHA256                 string
	SchemaVersion                 string
	GitCommit                     string
	P3dRequiredSourceSnapshotHash string
	RepositoryIdentity            string
	Tracked                       bool
	Entries                       []p3fTrackedSourceEntry
	SourceManifestHash            string
}

type p3fTrackedSourceEntry struct {
	EntryID    string
	BlobSHA256 string
}

type p3fWrapperIdentity struct {
	BinarySHA256 string
	Kind         string
	Route        string
}

type p3fActivation struct {
	ArchiveSHA256         string
	Manifest              p3fTrackedSourceManifest
	Deadline              time.Time
	P3cAction             string
	P3dHostSpecHash       string
	P3dSourceSnapshotHash string
	Wrapper               p3fWrapperIdentity
}

func newP3fActivation(manifest p3fTrackedSourceManifest) p3fActivation {
	return p3fActivation{
		ArchiveSHA256:         manifest.ArchiveSHA256,
		Manifest:              p3fCopyManifest(manifest),
		Deadline:              p3fDeadline,
		P3cAction:             p3fP3cAction,
		P3dHostSpecHash:       p3fP3dHostSpecHash,
		P3dSourceSnapshotHash: p3fP3dSourceSnapshotHash,
		Wrapper: p3fWrapperIdentity{
			BinarySHA256: p3fWrapperBinarySHA256,
			Kind:         p3fWrapperKind,
			Route:        p3fWrapperRoute,
		},
	}
}

type p3fLaunchRequest struct {
	LaunchSpecHash        string
	Fence                 store.LaunchFence
	Deadline              time.Time
	P3cAction             string
	P3dHostSpecHash       string
	P3dSourceSnapshotHash string
	SourceManifestHash    string
	ArchiveSHA256         string
	Wrapper               p3fWrapperIdentity
	Archive               *os.File
}

func p3fRequestForActivation(activation p3fActivation, launchSpecHash string, fence store.LaunchFence, archive *os.File) p3fLaunchRequest {
	return p3fLaunchRequest{
		LaunchSpecHash:        launchSpecHash,
		Fence:                 fence,
		Deadline:              activation.Deadline,
		P3cAction:             activation.P3cAction,
		P3dHostSpecHash:       activation.P3dHostSpecHash,
		P3dSourceSnapshotHash: activation.P3dSourceSnapshotHash,
		SourceManifestHash:    activation.Manifest.SourceManifestHash,
		ArchiveSHA256:         activation.ArchiveSHA256,
		Wrapper:               activation.Wrapper,
		Archive:               archive,
	}
}

// p3fPublicOutput is the only output projection. It intentionally conveys no
// fake-child result, raw source, location, sandbox fact, descriptor, or fence.
type p3fPublicOutput struct {
	Events            []p3fPublicEvent `json:"events"`
	Result            *p3fPublicResult `json:"result"`
	SchemaVersion     string           `json:"schema_version"`
	State             string           `json:"state"`
	VerificationState string           `json:"verification_state"`
}

type p3fPublicEvent struct{}
type p3fPublicResult struct{}

func p3fFailClosedOutput() p3fPublicOutput {
	return p3fPublicOutput{
		Events:            []p3fPublicEvent{},
		Result:            nil,
		SchemaVersion:     "ananke.omp-production-output.v1",
		State:             "waiting_for_human",
		VerificationState: "not_run",
	}
}

func equalP3fPublicOutput(left, right p3fPublicOutput) bool {
	return left.SchemaVersion == right.SchemaVersion &&
		left.State == right.State &&
		left.VerificationState == right.VerificationState &&
		left.Result == nil && right.Result == nil &&
		len(left.Events) == 0 && len(right.Events) == 0
}

// p3fSandboxCapability is limited to the fake child's inherited descriptors.
// It accepts neither an executable nor a program name.
type p3fSandboxCapability interface {
	Start(context.Context, []*os.File) (p3fChild, error)
}

type p3fChild interface {
	Wait() error
}

type p3fFakeChildLauncher struct{}

func (p3fFakeChildLauncher) Start(ctx context.Context, sandbox p3fSandboxCapability, source, manifest, evidence *os.File) (p3fChild, error) {
	if sandbox == nil || source == nil || manifest == nil || evidence == nil ||
		source.Fd() == ^uintptr(0) || manifest.Fd() == ^uintptr(0) || evidence.Fd() == ^uintptr(0) {
		return nil, errP3fDenied
	}
	return sandbox.Start(ctx, []*os.File{source, manifest, evidence})
}

type p3fExecChild struct{ command *exec.Cmd }

func (child *p3fExecChild) Wait() error { return child.command.Wait() }

type p3fSeatbeltSandbox struct{}

const p3fSeatbeltProfile = "(version 1) (allow default) (deny file-write*)"

func (p3fSeatbeltSandbox) Start(ctx context.Context, descriptors []*os.File) (p3fChild, error) {
	if runtime.GOOS != "darwin" {
		return nil, errP3fSandboxUnsupported
	}
	info, err := os.Stat(p3fSeatbeltExecutable)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		return nil, errP3fSandboxUnsupported
	}
	if len(descriptors) != 3 {
		return nil, errP3fDenied
	}
	for _, descriptor := range descriptors {
		if descriptor == nil || descriptor.Fd() == ^uintptr(0) {
			return nil, errP3fDenied
		}
	}
	command := exec.CommandContext(ctx, p3fSeatbeltExecutable, "-p", p3fSeatbeltProfile, os.Args[0], "-test.run=^TestP3FFakeChild$", p3fFakeChildArgument)
	command.Dir = "/"
	command.Env = []string{}
	command.ExtraFiles = descriptors
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &p3fExecChild{command: command}, nil
}

type p3fUnsupportedSandbox struct{}

func (p3fUnsupportedSandbox) Start(context.Context, []*os.File) (p3fChild, error) {
	return nil, errP3fSandboxUnsupported
}

type p3fOwnedStage struct {
	directory *os.File
	device    uint64
	inode     uint64
	name      string
}

func (stage *p3fOwnedStage) validateDescriptor() error {
	if stage == nil || stage.directory == nil || stage.directory.Fd() == ^uintptr(0) || !p3fValidStageName(stage.name) {
		return errP3fDenied
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(stage.directory.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(stat.Dev) != stage.device || uint64(stat.Ino) != stage.inode {
		return errP3fDenied
	}
	return nil
}

func (stage *p3fOwnedStage) validateBoundAt(rootFD int) error {
	if err := stage.validateDescriptor(); err != nil {
		return err
	}
	return stage.validateOwnedAt(rootFD)
}

func (stage *p3fOwnedStage) validateOwnedAt(rootFD int) error {
	if stage == nil || !p3fValidStageName(stage.name) {
		return errP3fDenied
	}
	fd, err := unix.Openat(rootFD, stage.name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if uint64(stat.Dev) != stage.device || uint64(stat.Ino) != stage.inode {
		return errP3fDenied
	}
	return nil
}

func (stage *p3fOwnedStage) closeDescriptor() bool {
	if stage == nil {
		return false
	}
	if stage.directory == nil {
		return true
	}
	_ = stage.directory.Close()
	stage.directory = nil
	return true
}

// p3fSandboxedFakeRuntime is test-only: it can execute only the deterministic
// test binary through the OS sandbox. It has no production entry point and
// never resolves, configures, or invokes an OMP wrapper.
type p3fSandboxedFakeRuntime struct {
	fence      *store.Store
	launcher   p3fFakeChildLauncher
	sandbox    p3fSandboxCapability
	root       p3eTrustedRoot
	activation p3fActivation
	now        func() time.Time

	stageSequence atomic.Uint64
	mu            sync.Mutex

	afterStage        func(*p3fOwnedStage)
	afterPreflight    func(*p3fLaunchRequest)
	fakeChildVerified bool
	lastStageClosed   bool
	lastStageRemoved  bool
}

func newP3fSandboxedFakeRuntime(fence *store.Store, launcher p3fFakeChildLauncher, sandbox p3fSandboxCapability, root string, activation p3fActivation, now func() time.Time) (*p3fSandboxedFakeRuntime, error) {
	if fence == nil || sandbox == nil || now == nil || !p3fValidActivation(activation) {
		return nil, errP3fDenied
	}
	trustedRoot, err := newP3eTrustedRoot(root)
	if err != nil {
		return nil, errP3fDenied
	}
	return &p3fSandboxedFakeRuntime{
		fence: fence, launcher: launcher, sandbox: sandbox, root: trustedRoot, activation: p3fCopyActivation(activation), now: now,
	}, nil
}

func (runtime *p3fSandboxedFakeRuntime) start(ctx context.Context, request p3fLaunchRequest) (output p3fPublicOutput, returned error) {
	output = p3fFailClosedOutput()
	runtime.mu.Lock()
	runtime.fakeChildVerified = false
	runtime.lastStageClosed = false
	runtime.lastStageRemoved = false
	runtime.mu.Unlock()

	if err := runtime.validateRequest(ctx, request); err != nil {
		return output, p3fDeny(p3fStartStageRequest, err)
	}
	stage, err := runtime.stagePinnedArchive(request.Archive)
	if err != nil {
		return output, p3fDeny(p3fStartStageArchive, err)
	}
	defer func() {
		closed, removed, cleanupErr := runtime.cleanupStage(stage)
		runtime.mu.Lock()
		runtime.lastStageClosed = closed
		runtime.lastStageRemoved = removed
		runtime.mu.Unlock()
		if cleanupErr != nil && returned == nil {
			returned = p3fDeny(p3fStartStageCleanup, cleanupErr)
		}
	}()

	manifest, err := p3fNewManifestDescriptor(runtime.activation.Manifest)
	if err != nil {
		return output, p3fDeny(p3fStartStageDescriptor, err)
	}
	defer manifest.Close()
	evidenceReader, evidenceWriter, err := os.Pipe()
	if err != nil {
		return output, p3fDeny(p3fStartStageDescriptor, err)
	}
	defer evidenceReader.Close()
	defer evidenceWriter.Close()

	if runtime.afterStage != nil {
		runtime.afterStage(stage)
	}
	if err := runtime.validateStage(stage); err != nil {
		return output, p3fDeny(p3fStartStageDescriptor, err)
	}
	if err := runtime.validateLaunchTime(ctx, request); err != nil {
		return output, p3fDeny(p3fStartStageFence, err)
	}
	if runtime.afterPreflight != nil {
		runtime.afterPreflight(&request)
	}

	admissionErr := runtime.fence.WithLaunchFenceAdmission(ctx, request.LaunchSpecHash, request.Fence, func(boundary store.LaunchRecoveryBoundary) error {
		if err := runtime.validateStage(stage); err != nil {
			return p3fDeny(p3fStartStageDescriptor, err)
		}
		if err := runtime.validateLaunchTimeBoundary(request, boundary); err != nil {
			return p3fDeny(p3fStartStageFence, err)
		}
		child, err := runtime.launcher.Start(ctx, runtime.sandbox, stage.directory, manifest, evidenceWriter)
		if err != nil {
			return p3fDeny(p3fStartStageSandbox, err)
		}
		stage.closeDescriptor()
		if err := evidenceWriter.Close(); err != nil {
			return p3fDeny(p3fStartStageEvidence, err)
		}
		evidenceResult := make(chan p3fEvidenceRead, 1)
		go func() {
			bytes, readErr := io.ReadAll(io.LimitReader(evidenceReader, p3fManifestDescriptorLimit+1))
			evidenceResult <- p3fEvidenceRead{bytes: bytes, err: readErr}
		}()
		waitErr := child.Wait()
		evidence := <-evidenceResult
		if waitErr != nil || evidence.err != nil || string(evidence.bytes) != p3fFakeChildReadOnlyEvidence {
			return p3fDeny(p3fStartStageEvidence, errP3fDenied)
		}
		runtime.mu.Lock()
		runtime.fakeChildVerified = true
		runtime.mu.Unlock()
		return nil
	})
	if admissionErr != nil {
		if failure := new(p3fStartFailure); errors.As(admissionErr, &failure) {
			return output, admissionErr
		}
		return output, p3fDeny(p3fStartStageFence, admissionErr)
	}
	return output, nil
}

type p3fEvidenceRead struct {
	bytes []byte
	err   error
}

func (runtime *p3fSandboxedFakeRuntime) validateRequest(ctx context.Context, request p3fLaunchRequest) error {
	if request.Archive == nil {
		return errP3fDenied
	}
	if err := runtime.validateLaunchIdentity(request); err != nil {
		return err
	}
	return runtime.validateFence(ctx, request)
}

func (runtime *p3fSandboxedFakeRuntime) validateLaunchIdentity(request p3fLaunchRequest) error {
	if !p3fValidActivation(runtime.activation) || request.LaunchSpecHash != p3eLaunchSpecHash || request.Deadline != runtime.activation.Deadline ||
		request.P3cAction != runtime.activation.P3cAction || request.P3dHostSpecHash != runtime.activation.P3dHostSpecHash ||
		request.P3dSourceSnapshotHash != runtime.activation.P3dSourceSnapshotHash || request.SourceManifestHash != runtime.activation.Manifest.SourceManifestHash ||
		request.ArchiveSHA256 != runtime.activation.ArchiveSHA256 || request.Wrapper != runtime.activation.Wrapper || runtime.now().UTC().After(request.Deadline) {
		return errP3fDenied
	}
	return nil
}

func (runtime *p3fSandboxedFakeRuntime) validateLaunchTime(ctx context.Context, request p3fLaunchRequest) error {
	if err := runtime.validateRequest(ctx, request); err != nil {
		return err
	}
	return nil
}

func (runtime *p3fSandboxedFakeRuntime) validateLaunchTimeBoundary(request p3fLaunchRequest, boundary store.LaunchRecoveryBoundary) error {
	if err := runtime.validateLaunchIdentity(request); err != nil {
		return err
	}
	return validateP3fFenceBoundary(request, boundary)
}

func (runtime *p3fSandboxedFakeRuntime) validateFence(ctx context.Context, request p3fLaunchRequest) error {
	boundary, err := runtime.fence.GetLaunchRecoveryBoundary(ctx, request.LaunchSpecHash)
	if err != nil {
		return errP3fDenied
	}
	return validateP3fFenceBoundary(request, boundary)
}

func validateP3fFenceBoundary(request p3fLaunchRequest, boundary store.LaunchRecoveryBoundary) error {
	if validateFencedLaunchBoundary(boundary) != nil || boundary.Action != store.LaunchRecoveryRetryProcessAdmission ||
		boundary.Claim.LaunchFence != request.Fence || boundary.Outbox.LaunchFence != request.Fence || boundary.Materialization == nil || boundary.RunIntent == nil {
		return errP3fDenied
	}
	materialization := boundary.Materialization
	run := boundary.RunIntent
	if materialization.LaunchFence != request.Fence || materialization.MaterializationID != "materialization_p3a_001" ||
		materialization.MaterializationHash != p3eMaterializationHash || materialization.Nonce != p3eMaterializationNonce ||
		run.LaunchFence != request.Fence || run.MaterializationID != materialization.MaterializationID || run.RunID != "run_p3a_001" || run.Attempt != 1 {
		return errP3fDenied
	}
	return nil
}

func (runtime *p3fSandboxedFakeRuntime) validateStage(stage *p3fOwnedStage) error {
	rootFD, err := runtime.root.open()
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(rootFD) }()
	return stage.validateBoundAt(rootFD)
}

func (runtime *p3fSandboxedFakeRuntime) stagePinnedArchive(archive *os.File) (stage *p3fOwnedStage, err error) {
	ownedArchive, err := p3fDuplicateArchive(archive)
	if err != nil {
		return nil, err
	}
	defer ownedArchive.Close()
	archiveHash, err := p3fArchiveSHA256(ownedArchive)
	if err != nil {
		return nil, fmt.Errorf("hash pinned Git archive: %w", err)
	}
	if archiveHash != runtime.activation.ArchiveSHA256 || archiveHash != runtime.activation.Manifest.ArchiveSHA256 {
		return nil, errors.New("Git archive SHA-256 differs from pinned activation manifest")
	}
	commit, err := p3fPAXArchiveCommit(ownedArchive)
	if err != nil {
		return nil, fmt.Errorf("read Git archive PAX commit metadata: %w", err)
	}
	if commit != runtime.activation.Manifest.GitCommit {
		return nil, errors.New("Git archive PAX commit differs from pinned manifest")
	}
	if _, err := ownedArchive.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	rootFD, err := runtime.root.open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(rootFD) }()
	name := fmt.Sprintf("p3f-stage-%016x", runtime.stageSequence.Add(1))
	if err := unix.Mkdirat(rootFD, name, 0o700); err != nil {
		return nil, err
	}
	var ownedStage *p3fOwnedStage
	defer func() {
		if err != nil && ownedStage == nil {
			_ = p3eRemoveTreeAt(rootFD, name, nil)
		}
	}()
	stageFD, err := unix.Openat(rootFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(stageFD, &stat); err != nil {
		_ = unix.Close(stageFD)
		return nil, err
	}
	directory := os.NewFile(uintptr(stageFD), "p3f-owned-staging")
	if directory == nil {
		_ = unix.Close(stageFD)
		return nil, errP3fDenied
	}
	ownedStage = &p3fOwnedStage{directory: directory, device: uint64(stat.Dev), inode: uint64(stat.Ino), name: name}
	stage = ownedStage
	defer func() {
		if err == nil {
			return
		}
		ownedStage.closeDescriptor()
		_ = p3eRemoveTreeAt(rootFD, name, &p3eSealedRoot{device: ownedStage.device, inode: ownedStage.inode})
	}()

	reader := tar.NewReader(ownedArchive)
	for _, expected := range runtime.activation.Manifest.Entries {
		header, nextErr := p3fNextArchiveMember(reader)
		if nextErr != nil {
			return nil, fmt.Errorf("read pinned archive entry: %w", nextErr)
		}
		if header == nil {
			return nil, errors.New("tracked archive has an empty member")
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA) || header.Name != expected.EntryID || header.Linkname != "" {
			return nil, fmt.Errorf("tracked archive member name=%q type=%q link=%q differs from pinned entry=%q", header.Name, header.Typeflag, header.Linkname, expected.EntryID)
		}
		if err := p3fWriteArchiveEntry(int(directory.Fd()), header, reader, expected); err != nil {
			return nil, err
		}
	}
	if header, nextErr := reader.Next(); nextErr != io.EOF || header != nil {
		return nil, errors.New("tracked archive has unpinned entries")
	}
	if err := directory.Sync(); err != nil {
		return nil, err
	}
	if err := stage.validateBoundAt(rootFD); err != nil {
		return nil, err
	}
	return stage, nil
}

func p3fNextArchiveMember(reader *tar.Reader) (*tar.Header, error) {
	for {
		header, err := reader.Next()
		if err != nil || header == nil || header.Typeflag != tar.TypeXGlobalHeader {
			return header, err
		}
	}
}

func p3fDuplicateArchive(source *os.File) (*os.File, error) {
	if source == nil || source.Fd() == ^uintptr(0) {
		return nil, errP3fDenied
	}
	var initial unix.Stat_t
	if err := unix.Fstat(int(source.Fd()), &initial); err != nil || initial.Mode&unix.S_IFMT != unix.S_IFREG {
		return nil, errP3fDenied
	}
	duplicated, err := unix.Dup(int(source.Fd()))
	if err != nil {
		return nil, err
	}
	archive := os.NewFile(uintptr(duplicated), "p3f-owned-git-archive")
	if archive == nil {
		_ = unix.Close(duplicated)
		return nil, errP3fDenied
	}
	var copied unix.Stat_t
	if err := unix.Fstat(duplicated, &copied); err != nil || copied.Dev != initial.Dev || copied.Ino != initial.Ino {
		_ = archive.Close()
		return nil, errP3fDenied
	}
	return archive, nil
}

func p3fArchiveSHA256(archive *os.File) (string, error) {
	if archive == nil || archive.Fd() == ^uintptr(0) {
		return "", errP3fDenied
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, archive); err != nil {
		return "", err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// p3fPAXArchiveCommit returns Git's non-authoritative PAX commit metadata.
// Archive provenance is established separately by p3fArchiveSHA256.
func p3fPAXArchiveCommit(archive *os.File) (string, error) {
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	for {
		var header [p3fTarBlockSize]byte
		if _, err := io.ReadFull(archive, header[:]); err != nil {
			return "", err
		}
		if p3fZeroTarBlock(header[:]) {
			return "", errP3fDenied
		}
		size, err := p3fTarHeaderSize(header[:])
		if err != nil {
			return "", err
		}
		body := io.LimitReader(archive, size)
		if header[156] == tar.TypeXGlobalHeader {
			payload, readErr := io.ReadAll(body)
			if readErr != nil || int64(len(payload)) != size {
				return "", errP3fDenied
			}
			if commit, ok := p3fPAXComment(payload); ok {
				return commit, nil
			}
		} else if _, err := io.Copy(io.Discard, body); err != nil {
			return "", err
		}
		padding := (p3fTarBlockSize - size%p3fTarBlockSize) % p3fTarBlockSize
		if _, err := archive.Seek(padding, io.SeekCurrent); err != nil {
			return "", err
		}
	}
}

func p3fTarHeaderSize(header []byte) (int64, error) {
	text := strings.Trim(string(header[124:136]), " \x00")
	if text == "" {
		return 0, errP3fDenied
	}
	size, err := strconv.ParseInt(text, 8, 64)
	if err != nil || size < 0 || size > p3fArchiveEntryLimit {
		return 0, errP3fDenied
	}
	return size, nil
}

func p3fZeroTarBlock(block []byte) bool {
	for _, value := range block {
		if value != 0 {
			return false
		}
	}
	return true
}

func p3fPAXComment(payload []byte) (string, bool) {
	for len(payload) > 0 {
		space := bytesIndexByte(payload, ' ')
		if space <= 0 {
			return "", false
		}
		length, err := strconv.Atoi(string(payload[:space]))
		if err != nil || length <= space+1 || length > len(payload) {
			return "", false
		}
		record := payload[space+1 : length]
		payload = payload[length:]
		if len(record) == 0 || record[len(record)-1] != '\n' {
			return "", false
		}
		key, value, found := strings.Cut(string(record[:len(record)-1]), "=")
		if found && key == "comment" {
			return value, true
		}
	}
	return "", false
}

func bytesIndexByte(bytes []byte, target byte) int {
	for index, value := range bytes {
		if value == target {
			return index
		}
	}
	return -1
}

func p3fWriteArchiveEntry(directoryFD int, header *tar.Header, source io.Reader, expected p3fTrackedSourceEntry) error {
	if header.Size < 0 || header.Size > p3fArchiveEntryLimit || !p3fValidEntryID(expected.EntryID) || !p3fValidHash(expected.BlobSHA256) {
		return errP3fDenied
	}
	fd, err := unix.Openat(directoryFD, expected.EntryID, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	output := os.NewFile(uintptr(fd), "p3f-staged-source")
	if output == nil {
		_ = unix.Close(fd)
		return errP3fDenied
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hash), io.LimitReader(source, header.Size))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil || written != header.Size || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != expected.BlobSHA256 {
		return errP3fDenied
	}
	return nil
}

func p3fNewManifestDescriptor(manifest p3fTrackedSourceManifest) (*os.File, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	contents := []byte(p3fCanonicalTrackedSourceManifest(manifest, true))
	if len(contents) > p3fManifestDescriptorLimit {
		_ = reader.Close()
		_ = writer.Close()
		return nil, errP3fDenied
	}
	if written, writeErr := writer.Write(contents); writeErr != nil || written != len(contents) {
		_ = reader.Close()
		_ = writer.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		return nil, io.ErrShortWrite
	}
	if err := writer.Close(); err != nil {
		_ = reader.Close()
		return nil, err
	}
	return reader, nil
}

func (runtime *p3fSandboxedFakeRuntime) cleanupStage(stage *p3fOwnedStage) (closed, removed bool, err error) {
	closed = stage.closeDescriptor()
	rootFD, err := runtime.root.open()
	if err != nil {
		return closed, false, err
	}
	defer func() { _ = unix.Close(rootFD) }()
	if err := stage.validateOwnedAt(rootFD); err != nil {
		return closed, false, err
	}
	if err := p3eRemoveTreeAt(rootFD, stage.name, &p3eSealedRoot{device: stage.device, inode: stage.inode}); err != nil {
		return closed, false, err
	}
	return closed, true, nil
}

func p3fValidActivation(activation p3fActivation) bool {
	return p3fValidManifest(activation.Manifest) && p3fValidHash(activation.ArchiveSHA256) &&
		activation.ArchiveSHA256 == activation.Manifest.ArchiveSHA256 && activation.Deadline.Equal(p3fDeadline) &&
		activation.P3cAction == p3fP3cAction && activation.P3dHostSpecHash == p3fP3dHostSpecHash &&
		activation.P3dSourceSnapshotHash == p3fP3dSourceSnapshotHash && activation.Wrapper == (p3fWrapperIdentity{
		BinarySHA256: p3fWrapperBinarySHA256, Kind: p3fWrapperKind, Route: p3fWrapperRoute,
	})
}

func p3fValidManifest(manifest p3fTrackedSourceManifest) bool {
	if manifest.SchemaVersion != "ananke.tracked-source-manifest.v1" || !manifest.Tracked || !p3fValidCommit(manifest.GitCommit) ||
		!p3fValidHash(manifest.ArchiveSHA256) || manifest.P3dRequiredSourceSnapshotHash != p3fP3dSourceSnapshotHash ||
		manifest.RepositoryIdentity != p3fRepositoryIdentity || len(manifest.Entries) == 0 ||
		manifest.SourceManifestHash != p3fHashTrackedSourceManifest(manifest) {
		return false
	}
	previous := ""
	for _, entry := range manifest.Entries {
		if !p3fValidEntryID(entry.EntryID) || !p3fValidHash(entry.BlobSHA256) || entry.EntryID <= previous {
			return false
		}
		previous = entry.EntryID
	}
	return true
}

func p3fHashTrackedSourceManifest(manifest p3fTrackedSourceManifest) string {
	sum := sha256.Sum256([]byte(p3fCanonicalTrackedSourceManifest(manifest, false)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func p3fCanonicalTrackedSourceManifest(manifest p3fTrackedSourceManifest, includeHash bool) string {
	var builder strings.Builder
	builder.WriteString(`{"archive_sha256":`)
	builder.WriteString(strconv.Quote(manifest.ArchiveSHA256))
	builder.WriteString(`,"entries":[`)
	for index, entry := range manifest.Entries {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(`{"blob_sha256":`)
		builder.WriteString(strconv.Quote(entry.BlobSHA256))
		builder.WriteString(`,"entry_id":`)
		builder.WriteString(strconv.Quote(entry.EntryID))
		builder.WriteByte('}')
	}
	builder.WriteString(`],"git_commit":`)
	builder.WriteString(strconv.Quote(manifest.GitCommit))
	builder.WriteString(`,"p3d_required_source_snapshot_hash":`)
	builder.WriteString(strconv.Quote(manifest.P3dRequiredSourceSnapshotHash))
	builder.WriteString(`,"repository_identity":`)
	builder.WriteString(strconv.Quote(manifest.RepositoryIdentity))
	builder.WriteString(`,"schema_version":`)
	builder.WriteString(strconv.Quote(manifest.SchemaVersion))
	if includeHash {
		builder.WriteString(`,"source_manifest_hash":`)
		builder.WriteString(strconv.Quote(manifest.SourceManifestHash))
	}
	builder.WriteString(`,"tracked":`)
	if manifest.Tracked {
		builder.WriteString("true")
	} else {
		builder.WriteString("false")
	}
	builder.WriteByte('}')
	return builder.String()
}

func p3fCopyManifest(manifest p3fTrackedSourceManifest) p3fTrackedSourceManifest {
	copy := manifest
	copy.Entries = append([]p3fTrackedSourceEntry(nil), manifest.Entries...)
	return copy
}

func p3fCopyActivation(activation p3fActivation) p3fActivation {
	copy := activation
	copy.Manifest = p3fCopyManifest(activation.Manifest)
	return copy
}

func p3fValidCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func p3fValidHash(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func p3fValidEntryID(value string) bool {
	if len(value) < 3 || len(value) > 63 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if !(character >= 'a' && character <= 'z') && !(character >= '0' && character <= '9') && character != '_' {
			return false
		}
	}
	return true
}

func p3fValidStageName(name string) bool {
	return strings.HasPrefix(name, "p3f-stage-") && len(name) == len("p3f-stage-")+16
}

func p3fHasFakeChildArgument(arguments []string) bool {
	for _, argument := range arguments {
		if argument == p3fFakeChildArgument {
			return true
		}
	}
	return false
}
