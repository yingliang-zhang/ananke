package lifecycle

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

const (
	p3eLaunchSpecHash           = "sha256:bbc43093a3b00c49c1d2ac26db08e6dd36ff72174ded15de9408702af3a9e658"
	p3eMaterializationHash      = "sha256:27f6f25e5a3cd790f634a3541d60d5681fa23c5d4a19c1b294ea70e168363ef7"
	p3eMaterializationNonce     = "nonce:034836706f8a359785406c36188f90edb94522896c44e3f000e9eede2d658f29"
	p3ePayloadHash              = "sha256:8294e0b7c8d3a5f2e1b0c9d8f7a6e5d4c3b2a190876543210fedcba987654321"
	p3eCanonicalSealFingerprint = "sha256:d50cec5aada78a1c4797b5071ffbf84cbebbfc4d9ca032cc5de56bb029315b0a"

	p3eTranscriptSource  = "omp_readonly_wrapper_transcript_v1"
	p3eTranscriptDialect = "omp_audit_stream_v1"
	p3eTranscriptLimit   = 64 * 1024
	p3eCancelGrace       = 100 * time.Millisecond
)

var errP3eDenied = errors.New("controlled OMP adapter request denied")

type p3eStartStage string

const (
	p3eStartStageRequestValidation       p3eStartStage = "request_validation"
	p3eStartStageSourceBinding           p3eStartStage = "source_binding"
	p3eStartStageMaterialization         p3eStartStage = "materialization"
	p3eStartStageDeadline                p3eStartStage = "deadline"
	p3eStartStageDescriptorValidation    p3eStartStage = "descriptor_validation"
	p3eStartStageFenceBoundaryValidation p3eStartStage = "fence_boundary_validation"
	p3eStartStageSQLiteAdmission         p3eStartStage = "sqlite_admission"
	p3eStartStageFakeStart               p3eStartStage = "fake_start"
)

// p3eStartFailure retains an internal cause for deterministic tests while
// exposing only the existing fail-closed denial to runtime callers.
type p3eStartFailure struct {
	stage p3eStartStage
	cause error
}

func (failure *p3eStartFailure) Error() string { return errP3eDenied.Error() }
func (failure *p3eStartFailure) Unwrap() error { return errP3eDenied }

func p3eDenyStart(stage p3eStartStage, cause error) error {
	return &p3eStartFailure{stage: stage, cause: cause}
}

// ompReadOnlyAdapter is private by design. No daemon, renderer, or command
// surface can select an executable, pass arguments, or provide transcript
// authority. The only implementation in this package accepts an internally
// configured executable and only a sealed read-only materialization root.
type ompReadOnlyAdapter interface {
	Start(context.Context, ompReadOnlyInvocation) (ompReadOnlyProcess, error)
}

type ompReadOnlyProcess interface {
	Transcript() io.ReadCloser
	Wait() error
	Terminate() error
	Kill() error
}

type ompReadOnlyInvocation struct {
	materializationDirectory *os.File
	device                   uint64
	inode                    uint64
}

// p3eExecAdapter is intentionally unexported and has no route, argv, prompt,
// or environment input from p3eAdapterRequest. Tests configure it with the
// deterministic test executable; no production entry point instantiates it.
type p3eExecAdapter struct {
	executable string
	args       []string
	env        []string
}

type p3eExecProcess struct {
	cmd        *exec.Cmd
	transcript io.ReadCloser
}

func (a p3eExecAdapter) Start(ctx context.Context, invocation ompReadOnlyInvocation) (ompReadOnlyProcess, error) {
	if a.executable == "" || invocation.materializationDirectory == nil || invocation.validate() != nil {
		return nil, errP3eDenied
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(a.executable, a.args...)
	// The child receives only a descriptor for the sealed directory. It never
	// receives the mutable materialization name or a lexical worktree path.
	cmd.Dir = "/"
	cmd.ExtraFiles = []*os.File{invocation.materializationDirectory}
	cmd.Env = append(p3eBaseEnvironment(os.Environ()), a.env...)
	transcript, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &p3eExecProcess{cmd: cmd, transcript: transcript}, nil
}

func (invocation ompReadOnlyInvocation) validate() error {
	if invocation.materializationDirectory == nil || invocation.materializationDirectory.Fd() == ^uintptr(0) {
		return errP3eDenied
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(invocation.materializationDirectory.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(stat.Dev) != invocation.device || uint64(stat.Ino) != invocation.inode {
		return errP3eDenied
	}
	return nil
}

func p3eBaseEnvironment(environment []string) []string {
	base := make([]string, 0, len(environment))
	for _, value := range environment {
		if !strings.HasPrefix(value, "ANANKE_P3E_") {
			base = append(base, value)
		}
	}
	return base
}

func (p *p3eExecProcess) Transcript() io.ReadCloser { return p.transcript }
func (p *p3eExecProcess) Wait() error               { return p.cmd.Wait() }

func (p *p3eExecProcess) Terminate() error {
	if p.cmd.Process == nil {
		return errP3eDenied
	}
	return p.cmd.Process.Signal(syscall.SIGTERM)
}

func (p *p3eExecProcess) Kill() error {
	if p.cmd.Process == nil {
		return errP3eDenied
	}
	return p.cmd.Process.Kill()
}

type p3eAdapterState string

const (
	p3eAdapterStateMonitoring      p3eAdapterState = "monitoring"
	p3eAdapterStateCancelRequested p3eAdapterState = "cancel_requested"
	p3eAdapterStateWaitingForHuman p3eAdapterState = "waiting_for_human"
	p3eAdapterStateCompleted       p3eAdapterState = "completed"
)

type p3eAuditKind string

const (
	p3eAuditStarted   p3eAuditKind = "audit_started"
	p3eAuditFinding   p3eAuditKind = "audit_finding"
	p3eAuditCompleted p3eAuditKind = "audit_completed"
)

// p3eAuditEvent is the bounded normalized transcript projection. It carries
// neither the raw line nor a role, error, source location, or execution input.
type p3eAuditEvent struct {
	Sequence int          `json:"sequence"`
	Kind     p3eAuditKind `json:"kind"`
}

// p3eAuditResult is bounded public IR. Verification remains not_run: this
// runtime never turns P3d's verification fingerprint into an execution request.
type p3eAuditResult struct {
	RequestID         string `json:"request_id"`
	EventCount        int    `json:"event_count"`
	AdvisoryFindings  int    `json:"advisory_findings"`
	BlockingFindings  int    `json:"blocking_findings"`
	State             string `json:"state"`
	VerificationState string `json:"verification_state"`
}

type p3ePublicState struct {
	AdapterState      p3eAdapterState `json:"adapter_state"`
	Events            []p3eAuditEvent `json:"events"`
	Result            *p3eAuditResult `json:"result"`
	VerificationState string          `json:"verification_state"`
}

type p3eRecoveryAction string

const (
	p3eRecoveryRetryAdapter             p3eRecoveryAction = "retry_adapter_admission"
	p3eRecoveryReconnectTranscript      p3eRecoveryAction = "reconnect_transcript_source"
	p3eRecoveryRetryBoundedCancellation p3eRecoveryAction = "retry_bounded_cancellation"
)

type p3eRecovery struct {
	Action p3eRecoveryAction
	State  p3ePublicState
}

// p3eHostSpec mirrors the closed P3d HostSpec with private Go fields. It is
// deliberately an equality-checked value: an arbitrary route, broad write
// capability, model, source dialect, or host fingerprint cannot be partially
// accepted.
type p3eHostSpec struct {
	SchemaVersion               string
	Route                       string
	WrapperKind                 string
	Provider                    string
	Model                       string
	Deadline                    string
	AttemptCap                  int
	Capabilities                string
	Access                      string
	Materialization             string
	Writes                      string
	HostSpecHash                string
	RepositoryIdentity          string
	TargetKind                  string
	RootIdentityFingerprint     string
	RequiredSourceSnapshotHash  string
	TranscriptSource            string
	InputDialect                string
	OutputDialect               string
	Normalization               string
	TranscriptSourceFingerprint string
	VerificationName            string
	VerificationMode            string
	VerificationCommandHash     string
}

func canonicalP3eHostSpec() p3eHostSpec {
	return p3eHostSpec{
		SchemaVersion:               "ananke.omp-readonly-host-spec.v1",
		Route:                       "ananke_omp_read_only_audit_v1",
		WrapperKind:                 "ananke_omp_readonly_wrapper_v1",
		Provider:                    "omp",
		Model:                       "omp_audit_model_v1",
		Deadline:                    "2026-07-30T12:00:00Z",
		AttemptCap:                  3,
		Capabilities:                "bounded_cancellation,read_only_audit,reconnect_recovery,transcript_normalization,verification",
		Access:                      "read_only",
		Materialization:             "sealed_payload_only",
		Writes:                      "forbidden",
		HostSpecHash:                "sha256:f176da10c1c055b930b17137d790580400c9a0e6963e967290a19cb608512e8b",
		RepositoryIdentity:          "github.com/yingliang-zhang/ananke",
		TargetKind:                  "canonical_ananke_repository",
		RootIdentityFingerprint:     "sha256:0876d8d61df302e652ee9a9b1c2c4d6e8f0123456789abcdef0123456789abcd",
		RequiredSourceSnapshotHash:  "sha256:1d19f39b6c1f3db6164580e9903d4ac129a4c387d4eea25d5baab1b0f1c2d3e4",
		TranscriptSource:            p3eTranscriptSource,
		InputDialect:                p3eTranscriptDialect,
		OutputDialect:               "ananke_omp_audit_event_v1",
		Normalization:               "known_omp_events_to_ananke_audit_v1",
		TranscriptSourceFingerprint: "sha256:4329a8b7c6d5e4f30123456789abcdef0123456789abcdef0123456789abcdef",
		VerificationName:            "ananke_contract_verify_v1",
		VerificationMode:            "read_only",
		VerificationCommandHash:     "sha256:54a6b8c0d2e4f60123456789abcdef0123456789abcdef0123456789abcdef01",
	}
}

type p3eMaterializationFile struct {
	Path     string
	Contents []byte
}

// p3eSealedMaterialization is the complete opaque P3d identity. The
// payload hash and canonical seal fingerprint are separate from the private
// test source digest; callers cannot replace source bytes by supplying a new
// recomputable digest.
type p3eSealedMaterialization struct {
	MaterializationHash string
	Nonce               string
	PayloadHash         string
	SealFingerprint     string
}

type p3eSourceSeal struct {
	materialization p3eSealedMaterialization
	sourceHash      string
}

// p3eMaterialization carries caller-provided bytes only until start binds a
// copied snapshot to the independently established p3eSourceSeal.
type p3eMaterialization struct {
	ID     string
	Sealed p3eSealedMaterialization
	Files  []p3eMaterializationFile
}

type p3eAdapterRequest struct {
	RequestID       string
	LaunchSpecHash  string
	Fence           store.LaunchFence
	HostSpec        p3eHostSpec
	Materialization p3eMaterialization
}

type p3eTrustedRoot struct {
	path   string
	info   os.FileInfo
	device uint64
	inode  uint64
}

// p3eSealedRoot owns the directory descriptor created by materialization.
// Its name exists only for descriptor-checked cleanup, never adapter launch.
type p3eSealedRoot struct {
	directory *os.File
	device    uint64
	inode     uint64
	name      string
}

func canonicalP3eSealedMaterialization() p3eSealedMaterialization {
	return p3eSealedMaterialization{
		MaterializationHash: p3eMaterializationHash,
		Nonce:               p3eMaterializationNonce,
		PayloadHash:         p3ePayloadHash,
		SealFingerprint:     p3eCanonicalSealFingerprint,
	}
}

func p3eSealFingerprint(materialization p3eSealedMaterialization) string {
	canonical := fmt.Sprintf(`{"materialization_hash":%q,"nonce":%q,"payload_hash":%q}`,
		materialization.MaterializationHash, materialization.Nonce, materialization.PayloadHash)
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validP3eSealedMaterialization(materialization p3eSealedMaterialization) bool {
	return materialization == canonicalP3eSealedMaterialization() && materialization.SealFingerprint == p3eSealFingerprint(materialization)
}

func newP3eSealedSource(files []p3eMaterializationFile) p3eSourceSeal {
	return p3eSourceSeal{
		materialization: canonicalP3eSealedMaterialization(),
		sourceHash:      hashP3eMaterializationFiles(files),
	}
}

func (source p3eSourceSeal) bind(materialization p3eMaterialization) (p3eMaterialization, error) {
	if !validP3eSealedMaterialization(source.materialization) || materialization.Sealed != source.materialization || hashP3eMaterializationFiles(materialization.Files) != source.sourceHash {
		return p3eMaterialization{}, errP3eDenied
	}
	bound := materialization
	bound.Files = make([]p3eMaterializationFile, len(materialization.Files))
	for index, file := range materialization.Files {
		bound.Files[index] = p3eMaterializationFile{Path: file.Path, Contents: append([]byte(nil), file.Contents...)}
	}
	if hashP3eMaterializationFiles(bound.Files) != source.sourceHash {
		return p3eMaterialization{}, errP3eDenied
	}
	return bound, nil
}

type ompReadOnlyRuntime struct {
	fence      *store.Store
	adapter    ompReadOnlyAdapter
	root       p3eTrustedRoot
	sourceSeal p3eSourceSeal
	now        func() time.Time

	mu                                 sync.Mutex
	sessions                           map[string]*p3eSession
	materializing                      map[string]struct{}
	beforeMaterializationWrite         func(string)
	afterMaterializationWrite          func(string, int) error
	beforeAdapterStart                 func(string)
	afterFinalLaunchBoundaryValidation func()
	afterFenceAdmissionValidation      func()
}

type p3eSession struct {
	requestID string
	process   ompReadOnlyProcess
	sealed    *p3eSealedRoot
	done      chan struct{}
	timer     *time.Timer
	rejected  bool
	state     p3ePublicState
}

func newOMPReadOnlyRuntime(fence *store.Store, adapter ompReadOnlyAdapter, root string, source p3eSourceSeal, now func() time.Time) (*ompReadOnlyRuntime, error) {
	if fence == nil || adapter == nil || now == nil || !validP3eSealedMaterialization(source.materialization) || source.sourceHash == "" {
		return nil, errP3eDenied
	}
	sealedRoot, err := newP3eTrustedRoot(root)
	if err != nil {
		return nil, errP3eDenied
	}
	return &ompReadOnlyRuntime{
		fence: fence, adapter: adapter, root: sealedRoot, sourceSeal: source, now: now,
		sessions: make(map[string]*p3eSession), materializing: make(map[string]struct{}),
	}, nil
}

func newP3eTrustedRoot(path string) (p3eTrustedRoot, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return p3eTrustedRoot{}, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return p3eTrustedRoot{}, err
	}
	info, err := p3eInspectDirectory(resolved)
	if err != nil {
		return p3eTrustedRoot{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return p3eTrustedRoot{}, errors.New("trusted root stat unavailable")
	}
	return p3eTrustedRoot{path: resolved, info: info, device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func (root p3eTrustedRoot) validate() error {
	info, err := p3eInspectDirectory(root.path)
	if err != nil {
		return err
	}
	if !os.SameFile(root.info, info) {
		return errors.New("trusted root identity changed")
	}
	return nil
}

func (root p3eTrustedRoot) open() (int, error) {
	if err := root.validate(); err != nil {
		return -1, err
	}
	fd, err := unix.Open(root.path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if uint64(stat.Dev) != root.device || uint64(stat.Ino) != root.inode {
		_ = unix.Close(fd)
		return -1, errors.New("opened trusted root identity changed")
	}
	return fd, nil
}

func p3eInspectDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("unsafe trusted root")
	}
	return info, nil
}

func (sealed *p3eSealedRoot) validateDescriptor() error {
	if sealed == nil || sealed.directory == nil || sealed.directory.Fd() == ^uintptr(0) || !p3eSafeMaterializationName(sealed.name) {
		return errP3eDenied
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(sealed.directory.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(stat.Dev) != sealed.device || uint64(stat.Ino) != sealed.inode {
		return errP3eDenied
	}
	return nil
}

func (sealed *p3eSealedRoot) validateBound(root p3eTrustedRoot) error {
	rootFD, err := root.open()
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(rootFD) }()
	return sealed.validateBoundAt(rootFD)
}

func (sealed *p3eSealedRoot) validateBoundAt(rootFD int) error {
	if err := sealed.validateDescriptor(); err != nil {
		return err
	}
	fd, err := unix.Openat(rootFD, sealed.name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return err
	}
	if uint64(stat.Dev) != sealed.device || uint64(stat.Ino) != sealed.inode {
		return errP3eDenied
	}
	return nil
}

func (sealed *p3eSealedRoot) close() {
	if sealed != nil && sealed.directory != nil {
		_ = sealed.directory.Close()
		sealed.directory = nil
	}
}

func (r *ompReadOnlyRuntime) start(ctx context.Context, request p3eAdapterRequest) (p3ePublicState, error) {
	if err := r.validateRequest(ctx, request); err != nil {
		return p3eFailClosedState(), p3eDenyStart(p3eStartStageRequestValidation, err)
	}
	materialization, err := r.sourceSeal.bind(request.Materialization)
	if err != nil {
		return p3eFailClosedState(), p3eDenyStart(p3eStartStageSourceBinding, err)
	}
	r.mu.Lock()
	if existing := r.sessions[request.RequestID]; existing != nil {
		state := copyP3ePublicState(existing.state)
		r.mu.Unlock()
		return state, p3eDenyStart(p3eStartStageRequestValidation, errP3eDenied)
	}
	if _, materializing := r.materializing[materialization.ID]; materializing {
		r.mu.Unlock()
		return p3eFailClosedState(), p3eDenyStart(p3eStartStageMaterialization, errP3eDenied)
	}
	r.materializing[materialization.ID] = struct{}{}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.materializing, materialization.ID)
		r.mu.Unlock()
	}()

	sealed, err := r.materialize(materialization)
	if err != nil {
		return p3eFailClosedState(), p3eDenyStart(p3eStartStageMaterialization, err)
	}
	deadline, _ := time.Parse(time.RFC3339, request.HostSpec.Deadline)
	remaining := deadline.Sub(r.now().UTC())
	if remaining <= 0 {
		_ = r.cleanupMaterialization(sealed)
		return p3eFailClosedState(), p3eDenyStart(p3eStartStageDeadline, errP3eDenied)
	}
	if err := r.validateLaunchBoundary(ctx, request, sealed); err != nil {
		_ = r.cleanupMaterialization(sealed)
		return p3eFailClosedState(), err
	}
	if r.beforeAdapterStart != nil {
		r.beforeAdapterStart(filepath.Join(r.root.path, sealed.name))
	}
	if err := r.validateLaunchBoundary(ctx, request, sealed); err != nil {
		_ = r.cleanupMaterialization(sealed)
		return p3eFailClosedState(), err
	}
	if r.afterFinalLaunchBoundaryValidation != nil {
		r.afterFinalLaunchBoundaryValidation()
	}
	var process ompReadOnlyProcess
	admissionErr := r.fence.WithLaunchFenceAdmission(ctx, request.LaunchSpecHash, request.Fence, func(boundary store.LaunchRecoveryBoundary) error {
		if sealed == nil {
			return p3eDenyStart(p3eStartStageDescriptorValidation, errP3eDenied)
		}
		if err := sealed.validateBound(r.root); err != nil {
			return p3eDenyStart(p3eStartStageDescriptorValidation, err)
		}
		if err := validateP3eFenceBoundary(request, boundary); err != nil {
			return p3eDenyStart(p3eStartStageFenceBoundaryValidation, err)
		}
		if r.afterFenceAdmissionValidation != nil {
			r.afterFenceAdmissionValidation()
		}
		var startErr error
		process, startErr = r.adapter.Start(ctx, ompReadOnlyInvocation{
			materializationDirectory: sealed.directory,
			device:                   sealed.device,
			inode:                    sealed.inode,
		})
		if startErr != nil {
			return p3eDenyStart(p3eStartStageFakeStart, startErr)
		}
		return nil
	})
	if admissionErr != nil {
		_ = r.cleanupMaterialization(sealed)
		var failure *p3eStartFailure
		if errors.As(admissionErr, &failure) {
			return p3eFailClosedState(), admissionErr
		}
		if errors.Is(admissionErr, store.ErrLaunchStaleFence) {
			return p3eFailClosedState(), p3eDenyStart(p3eStartStageFenceBoundaryValidation, admissionErr)
		}
		return p3eFailClosedState(), p3eDenyStart(p3eStartStageSQLiteAdmission, admissionErr)
	}
	session := &p3eSession{
		requestID: request.RequestID,
		process:   process,
		sealed:    sealed,
		done:      make(chan struct{}),
		state: p3ePublicState{
			AdapterState:      p3eAdapterStateMonitoring,
			Events:            []p3eAuditEvent{},
			VerificationState: "not_run",
		},
	}
	r.mu.Lock()
	r.sessions[request.RequestID] = session
	r.mu.Unlock()
	session.timer = time.AfterFunc(remaining, func() { r.reject(session) })
	go r.monitor(session)
	return copyP3ePublicState(session.state), nil
}

func (r *ompReadOnlyRuntime) validateRequest(ctx context.Context, request p3eAdapterRequest) error {
	if request.RequestID == "" || request.LaunchSpecHash != p3eLaunchSpecHash || request.HostSpec != canonicalP3eHostSpec() {
		return errP3eDenied
	}
	if request.Materialization.ID != "materialization_p3a_001" || !validP3eSealedMaterialization(request.Materialization.Sealed) || request.Materialization.Sealed != r.sourceSeal.materialization {
		return errP3eDenied
	}
	if err := r.root.validate(); err != nil {
		return errP3eDenied
	}
	return r.validateFence(ctx, request)
}

func (r *ompReadOnlyRuntime) validateFence(ctx context.Context, request p3eAdapterRequest) error {
	boundary, err := r.fence.GetLaunchRecoveryBoundary(ctx, request.LaunchSpecHash)
	if err != nil {
		return errP3eDenied
	}
	return validateP3eFenceBoundary(request, boundary)
}

func validateP3eFenceBoundary(request p3eAdapterRequest, boundary store.LaunchRecoveryBoundary) error {
	if validateFencedLaunchBoundary(boundary) != nil {
		return errP3eDenied
	}
	if boundary.Action != store.LaunchRecoveryRetryProcessAdmission || boundary.Claim.LaunchFence != request.Fence || boundary.Outbox.LaunchFence != request.Fence || boundary.Materialization == nil || boundary.RunIntent == nil {
		return errP3eDenied
	}
	materialization := boundary.Materialization
	run := boundary.RunIntent
	if materialization.MaterializationID != request.Materialization.ID || materialization.MaterializationHash != request.Materialization.Sealed.MaterializationHash || materialization.Nonce != request.Materialization.Sealed.Nonce || materialization.LaunchFence != request.Fence || run.RunID != "run_p3a_001" || run.MaterializationID != request.Materialization.ID || run.Attempt != 1 || run.LaunchFence != request.Fence {
		return errP3eDenied
	}
	return nil
}

func (r *ompReadOnlyRuntime) validateLaunchBoundary(ctx context.Context, request p3eAdapterRequest, sealed *p3eSealedRoot) error {
	if sealed == nil {
		return p3eDenyStart(p3eStartStageDescriptorValidation, errP3eDenied)
	}
	if err := sealed.validateBound(r.root); err != nil {
		return p3eDenyStart(p3eStartStageDescriptorValidation, err)
	}
	if err := r.validateFence(ctx, request); err != nil {
		return p3eDenyStart(p3eStartStageFenceBoundaryValidation, err)
	}
	return nil
}

func (r *ompReadOnlyRuntime) materialize(materialization p3eMaterialization) (sealed *p3eSealedRoot, err error) {
	if err := r.root.validate(); err != nil {
		return nil, err
	}
	if len(materialization.Files) == 0 || !p3eSafeMaterializationName(materialization.ID) {
		return nil, errP3eDenied
	}
	for _, file := range materialization.Files {
		if !p3eSafeRelativePath(file.Path) {
			return nil, errP3eDenied
		}
	}
	rootFD, err := r.root.open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(rootFD) }()
	if err = unix.Mkdirat(rootFD, materialization.ID, 0o700); err != nil {
		return nil, err
	}
	var createdSealed *p3eSealedRoot
	defer func() {
		if err == nil {
			return
		}
		if createdSealed != nil {
			_ = r.cleanupMaterialization(createdSealed)
			return
		}
		_ = p3eRemoveTreeAt(rootFD, materialization.ID, nil)
	}()

	materializationFD, openErr := unix.Openat(rootFD, materialization.ID, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if openErr != nil {
		return nil, openErr
	}
	var stat unix.Stat_t
	if err = unix.Fstat(materializationFD, &stat); err != nil {
		_ = unix.Close(materializationFD)
		return nil, err
	}
	directory := os.NewFile(uintptr(materializationFD), "p3e-sealed-materialization")
	if directory == nil {
		_ = unix.Close(materializationFD)
		return nil, errors.New("open sealed materialization descriptor")
	}
	sealed = &p3eSealedRoot{directory: directory, device: uint64(stat.Dev), inode: uint64(stat.Ino), name: materialization.ID}
	createdSealed = sealed
	materializationPath := filepath.Join(r.root.path, materialization.ID)
	if r.beforeMaterializationWrite != nil {
		r.beforeMaterializationWrite(materializationPath)
	}
	if err = sealed.validateBound(r.root); err != nil {
		return nil, err
	}
	for index, file := range materialization.Files {
		if err = p3eWriteSealedFile(int(directory.Fd()), file); err != nil {
			return nil, err
		}
		if r.afterMaterializationWrite != nil {
			if err = r.afterMaterializationWrite(materializationPath, index); err != nil {
				return nil, err
			}
		}
	}
	if err = unix.Fchmod(int(directory.Fd()), 0o500); err != nil {
		return nil, err
	}
	if err = unix.Fsync(int(directory.Fd())); err != nil {
		return nil, err
	}
	if err = sealed.validateBound(r.root); err != nil {
		return nil, err
	}
	return sealed, nil
}

func p3eWriteSealedFile(rootFD int, file p3eMaterializationFile) error {
	segments := strings.Split(file.Path, string(filepath.Separator))
	currentFD := rootFD
	for _, segment := range segments[:len(segments)-1] {
		nextFD, err := unix.Openat(currentFD, segment, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if err != nil {
			if !errors.Is(err, unix.ENOENT) {
				return err
			}
			if err := unix.Mkdirat(currentFD, segment, 0o500); err != nil && !errors.Is(err, unix.EEXIST) {
				return err
			}
			nextFD, err = unix.Openat(currentFD, segment, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
			if err != nil {
				return err
			}
		}
		if currentFD != rootFD {
			_ = unix.Close(currentFD)
		}
		currentFD = nextFD
	}
	defer func() {
		if currentFD != rootFD {
			_ = unix.Close(currentFD)
		}
	}()
	fd, err := unix.Openat(currentFD, segments[len(segments)-1], unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW, 0o400)
	if err != nil {
		return err
	}
	output := os.NewFile(uintptr(fd), "p3e-sealed-source")
	if output == nil {
		_ = unix.Close(fd)
		return errors.New("open sealed source descriptor")
	}
	defer output.Close()
	if _, err := output.Write(file.Contents); err != nil {
		return err
	}
	if err := output.Chmod(0o400); err != nil {
		return err
	}
	return output.Sync()
}

func p3eSafeMaterializationName(name string) bool {
	return name != "" && filepath.Base(name) == name && name != "." && name != ".." && !strings.ContainsRune(name, filepath.Separator)
}

func p3eSafeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || path == ".." {
		return false
	}
	for _, segment := range strings.Split(path, string(filepath.Separator)) {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func hashP3eMaterializationFiles(files []p3eMaterializationFile) string {
	ordered := append([]p3eMaterializationFile(nil), files...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	hash := sha256.New()
	var length [8]byte
	for _, file := range ordered {
		binary.BigEndian.PutUint64(length[:], uint64(len(file.Path)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(file.Path))
		binary.BigEndian.PutUint64(length[:], uint64(len(file.Contents)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(file.Contents)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func (r *ompReadOnlyRuntime) monitor(session *p3eSession) {
	scanner := bufio.NewScanner(session.process.Transcript())
	scanner.Buffer(make([]byte, 0, 4096), p3eTranscriptLimit)
	for scanner.Scan() {
		if !r.acceptTranscriptEvent(session, scanner.Bytes()) {
			r.reject(session)
			break
		}
	}
	scanErr := scanner.Err()
	waitErr := session.process.Wait()
	if session.timer != nil {
		session.timer.Stop()
	}

	r.mu.Lock()
	rejected := session.rejected
	knownComplete := len(session.state.Events) == 3 && !rejected
	r.mu.Unlock()
	cleanupErr := r.cleanupMaterialization(session.sealed)

	r.mu.Lock()
	if rejected || scanErr != nil || waitErr != nil || !knownComplete || cleanupErr != nil {
		session.state = p3eFailClosedState()
	} else {
		session.state = p3ePublicState{
			AdapterState: p3eAdapterStateCompleted,
			Events:       append([]p3eAuditEvent(nil), session.state.Events...),
			Result: &p3eAuditResult{
				RequestID: requestIDForSession(session), EventCount: 3, AdvisoryFindings: 1, BlockingFindings: 0,
				State: "completed", VerificationState: "not_run",
			},
			VerificationState: "not_run",
		}
	}
	close(session.done)
	r.mu.Unlock()
}

func requestIDForSession(session *p3eSession) string { return session.requestID }

func (r *ompReadOnlyRuntime) acceptTranscriptEvent(session *p3eSession, raw []byte) bool {
	kind, ok := normalizeP3eTranscript(raw)
	if !ok {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if session.rejected || session.state.AdapterState != p3eAdapterStateMonitoring {
		return false
	}
	sequence := len(session.state.Events) + 1
	if (sequence == 1 && kind != p3eAuditStarted) || (sequence == 2 && kind != p3eAuditFinding) || (sequence == 3 && kind != p3eAuditCompleted) || sequence > 3 {
		return false
	}
	session.state.Events = append(session.state.Events, p3eAuditEvent{Sequence: sequence, Kind: kind})
	return true
}

func normalizeP3eTranscript(raw []byte) (p3eAuditKind, bool) {
	event, ok := decodeP3eTranscript(raw)
	if !ok || len(event) != 3 || event["source"] != p3eTranscriptSource || event["dialect"] != p3eTranscriptDialect {
		return "", false
	}
	for key := range event {
		if key != "source" && key != "dialect" && key != "event" {
			return "", false
		}
	}
	switch p3eAuditKind(event["event"]) {
	case p3eAuditStarted, p3eAuditFinding, p3eAuditCompleted:
		return p3eAuditKind(event["event"]), true
	default:
		return "", false
	}
}

func decodeP3eTranscript(raw []byte) (map[string]string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, false
	}
	event := make(map[string]string, 3)
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, false
		}
		key, ok := token.(string)
		if !ok {
			return nil, false
		}
		if _, duplicate := event[key]; duplicate {
			return nil, false
		}
		var value string
		if err := decoder.Decode(&value); err != nil {
			return nil, false
		}
		event[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, false
	}
	return event, true
}

func (r *ompReadOnlyRuntime) reject(session *p3eSession) {
	r.rejectWithState(session, false)
}

func (r *ompReadOnlyRuntime) requestCancellation(session *p3eSession) {
	r.rejectWithState(session, true)
}

func (r *ompReadOnlyRuntime) rejectWithState(session *p3eSession, cancellationRequested bool) {
	r.mu.Lock()
	alreadyRejected := session.rejected
	session.rejected = true
	if cancellationRequested && session.state.AdapterState != p3eAdapterStateCompleted {
		session.state = p3ePublicState{
			AdapterState:      p3eAdapterStateCancelRequested,
			Events:            []p3eAuditEvent{},
			VerificationState: "not_run",
		}
	}
	r.mu.Unlock()
	if alreadyRejected {
		return
	}
	_ = session.process.Terminate()
	time.AfterFunc(p3eCancelGrace, func() {
		select {
		case <-session.done:
		default:
			_ = session.process.Kill()
		}
	})
}

func (r *ompReadOnlyRuntime) cancel(ctx context.Context, requestID string) error {
	r.mu.Lock()
	session := r.sessions[requestID]
	r.mu.Unlock()
	if session == nil {
		return errP3eDenied
	}
	r.requestCancellation(session)
	select {
	case <-session.done:
		return nil
	case <-ctx.Done():
		_ = session.process.Kill()
		select {
		case <-session.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (r *ompReadOnlyRuntime) recover(ctx context.Context, request p3eAdapterRequest) (p3eRecovery, error) {
	if err := r.validateRequest(ctx, request); err != nil {
		return p3eRecovery{Action: p3eRecoveryRetryAdapter, State: p3eFailClosedState()}, fmt.Errorf("%w: recovery validation", errP3eDenied)
	}
	r.mu.Lock()
	session := r.sessions[request.RequestID]
	cancellationRequested := session != nil && session.state.AdapterState == p3eAdapterStateCancelRequested
	r.mu.Unlock()
	if session == nil {
		return p3eRecovery{Action: p3eRecoveryRetryAdapter, State: p3eFailClosedState()}, nil
	}
	if cancellationRequested {
		r.requestCancellation(session)
		return p3eRecovery{Action: p3eRecoveryRetryBoundedCancellation, State: p3eFailClosedState()}, nil
	}
	select {
	case <-session.done:
	default:
		r.reject(session)
	}
	return p3eRecovery{Action: p3eRecoveryReconnectTranscript, State: p3eFailClosedState()}, nil
}

func (r *ompReadOnlyRuntime) status(requestID string) (p3ePublicState, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[requestID]
	if session == nil {
		return p3ePublicState{}, false
	}
	return copyP3ePublicState(session.state), true
}

func p3eFailClosedState() p3ePublicState {
	return p3ePublicState{
		AdapterState:      p3eAdapterStateWaitingForHuman,
		Events:            []p3eAuditEvent{},
		Result:            nil,
		VerificationState: "not_run",
	}
}

func copyP3ePublicState(state p3ePublicState) p3ePublicState {
	copy := state
	copy.Events = append([]p3eAuditEvent(nil), state.Events...)
	if state.Result != nil {
		result := *state.Result
		copy.Result = &result
	}
	return copy
}

func (r *ompReadOnlyRuntime) cleanupMaterialization(sealed *p3eSealedRoot) error {
	if sealed == nil {
		return errP3eDenied
	}
	defer sealed.close()
	rootFD, err := r.root.open()
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(rootFD) }()
	if err := sealed.validateBoundAt(rootFD); err != nil {
		return errP3eDenied
	}
	return p3eRemoveTreeAt(rootFD, sealed.name, sealed)
}

func p3eRemoveTreeAt(parentFD int, name string, expected *p3eSealedRoot) error {
	fd, err := unix.Openat(parentFD, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	if expected != nil {
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil || uint64(stat.Dev) != expected.device || uint64(stat.Ino) != expected.inode {
			_ = unix.Close(fd)
			return errP3eDenied
		}
	}
	return p3eRemoveOpenedTree(parentFD, name, fd)
}

func p3eRemoveOpenedTree(parentFD int, name string, fd int) error {
	directory := os.NewFile(uintptr(fd), "p3e-cleanup")
	if directory == nil {
		_ = unix.Close(fd)
		return errors.New("open sealed cleanup directory")
	}
	if err := unix.Fchmod(fd, 0o700); err != nil {
		_ = directory.Close()
		return err
	}
	names, readErr := directory.Readdirnames(-1)
	if readErr != nil {
		_ = directory.Close()
		return readErr
	}
	for _, child := range names {
		if child == "." || child == ".." {
			_ = directory.Close()
			return errP3eDenied
		}
		childFD, openErr := unix.Openat(fd, child, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
		if openErr == nil {
			if err := p3eRemoveOpenedTree(fd, child, childFD); err != nil {
				_ = directory.Close()
				return err
			}
			continue
		}
		if err := unix.Unlinkat(fd, child, 0); err != nil {
			_ = directory.Close()
			return err
		}
	}
	if err := directory.Close(); err != nil {
		return err
	}
	return unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
}
