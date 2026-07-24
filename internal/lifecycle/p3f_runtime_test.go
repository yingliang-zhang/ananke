package lifecycle

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

func TestP3FFakeChild(t *testing.T) {
	if !p3fHasFakeChildArgument(os.Args[1:]) {
		return
	}
	for _, argument := range os.Args[1:] {
		if argument != "-test.run=^TestP3FFakeChild$" && argument != p3fFakeChildArgument && strings.Contains(argument, "/") {
			t.Fatalf("fake child received raw path argument %q", argument)
		}
	}
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		lower := strings.ToLower(name)
		for _, forbidden := range []string{"credential", "password", "secret", "token"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("fake child received credential environment %q", name)
			}
		}
	}

	source := os.NewFile(uintptr(p3fSourceDescriptor), "p3f-fake-source")
	manifest := os.NewFile(uintptr(p3fManifestDescriptor), "p3f-fake-manifest")
	evidence := os.NewFile(uintptr(p3fEvidenceDescriptor), "p3f-fake-evidence")
	if source == nil || manifest == nil || evidence == nil {
		t.Fatal("fake child inherited an incomplete FD-only interface")
	}
	defer source.Close()
	defer manifest.Close()
	defer evidence.Close()

	manifestBytes, err := io.ReadAll(io.LimitReader(manifest, p3fManifestDescriptorLimit))
	if err != nil || !bytes.Contains(manifestBytes, []byte(`"schema_version":"ananke.tracked-source-manifest.v1"`)) {
		t.Fatalf("fake child could not read the inherited manifest descriptor: %v", err)
	}
	info, err := source.Stat()
	if err != nil {
		t.Fatalf("stat inherited source descriptor: %v", err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("source descriptor is DAC read-only; sandbox proof would be inconclusive: %v", info.Mode())
	}

	readFD, err := unix.Openat(int(source.Fd()), "go_module", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatalf("open staged source through inherited descriptor: %v", err)
	}
	readFile := os.NewFile(uintptr(readFD), "p3f-fake-source-read")
	contents, readErr := io.ReadAll(readFile)
	closeErr := readFile.Close()
	if readErr != nil || closeErr != nil || string(contents) != "module fake.example/p3f\n" {
		t.Fatalf("read staged fake source = %q read=%v close=%v", contents, readErr, closeErr)
	}

	writeFD, writeErr := unix.Openat(int(source.Fd()), "go_module", unix.O_WRONLY|unix.O_NOFOLLOW, 0)
	if writeErr == nil {
		_ = unix.Close(writeFD)
		t.Fatal("OS sandbox allowed a source write through the inherited descriptor")
	}
	if !errors.Is(writeErr, unix.EACCES) && !errors.Is(writeErr, unix.EPERM) {
		t.Fatalf("source write error = %v, want OS write denial", writeErr)
	}
	if _, err := evidence.WriteString(p3fFakeChildReadOnlyEvidence); err != nil {
		t.Fatalf("write inherited evidence descriptor: %v", err)
	}
}

// TestP3FProductionBuildExcludesFakeExecution proves that the P3f execution
// proof is test-only. It examines the compiler's non-test file set instead of
// trusting a package-private comment or an unreachable callsite.
func TestP3FProductionBuildExcludesFakeExecution(t *testing.T) {
	const fakeRuntimeSource = "p3f_fake_runtime_test.go"

	command := exec.Command("go", "list", "-json", ".")
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("list production lifecycle package: %v", err)
	}
	var listed struct {
		GoFiles     []string
		TestGoFiles []string
	}
	if err := json.Unmarshal(output, &listed); err != nil {
		t.Fatalf("decode production lifecycle package listing: %v", err)
	}
	for _, name := range listed.GoFiles {
		if strings.Contains(strings.ToLower(name), "p3f") {
			t.Fatalf("P3f source %q is compiled into the production lifecycle package", name)
		}
	}
	if p3fListedFile(listed.GoFiles, fakeRuntimeSource) || !p3fListedFile(listed.TestGoFiles, fakeRuntimeSource) {
		t.Fatalf("fake runtime build selection = production:%v test:%v, want test-only %q", listed.GoFiles, listed.TestGoFiles, fakeRuntimeSource)
	}
	p3fAssertHardBoundTestExecution(t, fakeRuntimeSource)
}

func p3fListedFile(files []string, want string) bool {
	for _, name := range files {
		if name == want {
			return true
		}
	}
	return false
}

func p3fAssertHardBoundTestExecution(t *testing.T, source string) {
	t.Helper()
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, source, nil, 0)
	if err != nil {
		t.Fatalf("parse fake runtime source: %v", err)
	}
	p3fAssertEmptyStruct(t, parsed, "p3fFakeChildLauncher")
	p3fAssertEmptyStruct(t, parsed, "p3fSeatbeltSandbox")
	p3fAssertSandboxDescriptorOnly(t, parsed)
	p3fAssertSeatbeltUsesTestBinary(t, parsed)
}

func p3fAssertEmptyStruct(t *testing.T, file *ast.File, name string) {
	t.Helper()
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || generic.Tok != token.TYPE {
			continue
		}
		for _, specification := range generic.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != name {
				continue
			}
			structure, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s must be a struct", name)
			}
			if structure.Fields != nil && len(structure.Fields.List) != 0 {
				t.Fatalf("%s has configurable fields", name)
			}
			return
		}
	}
	t.Fatalf("missing %s", name)
}

func p3fAssertSandboxDescriptorOnly(t *testing.T, file *ast.File) {
	t.Helper()
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || generic.Tok != token.TYPE {
			continue
		}
		for _, specification := range generic.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "p3fSandboxCapability" {
				continue
			}
			contract, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok || contract.Methods == nil || len(contract.Methods.List) != 1 || len(contract.Methods.List[0].Names) != 1 || contract.Methods.List[0].Names[0].Name != "Start" {
				t.Fatal("P3f sandbox capability must expose only Start")
			}
			start, ok := contract.Methods.List[0].Type.(*ast.FuncType)
			if !ok || start.Params == nil || len(start.Params.List) != 2 || p3fASTContainsString(start.Params) {
				t.Fatal("P3f sandbox Start must accept only context and inherited descriptors, never a program")
			}
			return
		}
	}
	t.Fatal("missing p3fSandboxCapability")
}

func p3fASTContainsString(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		identifier, ok := current.(*ast.Ident)
		if ok && identifier.Name == "string" {
			found = true
		}
		return !found
	})
	return found
}

func p3fAssertSeatbeltUsesTestBinary(t *testing.T, file *ast.File) {
	t.Helper()
	var start *ast.FuncDecl
	for _, declaration := range file.Decls {
		candidate, ok := declaration.(*ast.FuncDecl)
		if !ok || candidate.Name.Name != "Start" || candidate.Recv == nil || len(candidate.Recv.List) != 1 {
			continue
		}
		receiver, ok := candidate.Recv.List[0].Type.(*ast.Ident)
		if ok && receiver.Name == "p3fSeatbeltSandbox" {
			start = candidate
			break
		}
	}
	if start == nil {
		t.Fatal("missing p3fSeatbeltSandbox.Start")
	}
	usesTestBinary := false
	ast.Inspect(start.Body, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "CommandContext" {
			return true
		}
		packageName, ok := selector.X.(*ast.Ident)
		if !ok || packageName.Name != "exec" {
			return true
		}
		for _, argument := range call.Args {
			index, ok := argument.(*ast.IndexExpr)
			if !ok {
				continue
			}
			arguments, ok := index.X.(*ast.SelectorExpr)
			if !ok || arguments.Sel.Name != "Args" {
				continue
			}
			packageName, ok := arguments.X.(*ast.Ident)
			literal, literalOK := index.Index.(*ast.BasicLit)
			if ok && packageName.Name == "os" && literalOK && literal.Value == "0" {
				usesTestBinary = true
			}
		}
		return true
	})
	if !usesTestBinary {
		t.Fatal("P3f Seatbelt launcher is not hard-bound to the test binary")
	}
}

func TestP3FSandboxedFakeChildStagesPinnedGitArchiveWithFDOnlyProof(t *testing.T) {
	runtime, request, root := newP3fFixtureRuntime(t, p3fSeatbeltSandbox{})

	output, err := runtime.start(context.Background(), request)
	if err != nil {
		failure := assertP3fStartFailure(t, err, p3fStartStageSandbox)
		if !errors.Is(failure.cause, errP3fSandboxUnsupported) {
			t.Fatalf("sandboxed fake child failed instead of proving OS enforcement: stage=%s cause=%v", failure.stage, failure.cause)
		}
		assertP3fFailClosedPublicOutput(t, output, root)
		if runtime.fakeChildVerified {
			t.Fatal("unsupported sandbox claimed fake-child verification")
		}
		t.Log("P3f OS sandbox capability unavailable: fake child was fail-closed before execution")
		return
	}
	assertP3fFailClosedPublicOutput(t, output, root)
	if !runtime.fakeChildVerified {
		t.Fatal("sandboxed fake child did not prove descriptor read-only/write denial")
	}
	if !runtime.lastStageClosed || !runtime.lastStageRemoved {
		t.Fatalf("descriptor-owned cleanup = closed:%v removed:%v, want both true", runtime.lastStageClosed, runtime.lastStageRemoved)
	}
	t.Log("P3f Darwin Seatbelt sandbox enforced inherited-source reads and denied writes")
}

func TestP3FLaunchTimeIdentityDriftFailsClosedBeforeFakeChild(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*p3fLaunchRequest)
	}{
		{name: "private fence", mutate: func(request *p3fLaunchRequest) { request.Fence.ClaimTokenHash = "sha256:" + strings.Repeat("f", 64) }},
		{name: "P3c action", mutate: func(request *p3fLaunchRequest) { request.P3cAction = "retry_other_action" }},
		{name: "deadline", mutate: func(request *p3fLaunchRequest) { request.Deadline = request.Deadline.Add(time.Second) }},
		{name: "P3d HostSpec", mutate: func(request *p3fLaunchRequest) { request.P3dHostSpecHash = "sha256:" + strings.Repeat("a", 64) }},
		{name: "P3d source snapshot", mutate: func(request *p3fLaunchRequest) { request.P3dSourceSnapshotHash = "sha256:" + strings.Repeat("b", 64) }},
		{name: "source manifest", mutate: func(request *p3fLaunchRequest) { request.SourceManifestHash = "sha256:" + strings.Repeat("c", 64) }},
		{name: "archive hash", mutate: func(request *p3fLaunchRequest) { request.ArchiveSHA256 = "sha256:" + strings.Repeat("e", 64) }},
		{name: "wrapper hash", mutate: func(request *p3fLaunchRequest) { request.Wrapper.BinarySHA256 = "sha256:" + strings.Repeat("d", 64) }},
		{name: "wrapper kind", mutate: func(request *p3fLaunchRequest) { request.Wrapper.Kind = "other_wrapper" }},
		{name: "route", mutate: func(request *p3fLaunchRequest) { request.Wrapper.Route = "other_route" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, request, root := newP3fFixtureRuntime(t, p3fSeatbeltSandbox{})
			tc.mutate(&request)

			output, err := runtime.start(context.Background(), request)
			if !errors.Is(err, errP3fDenied) {
				t.Fatalf("unsafe %s start error = %v, want %v", tc.name, err, errP3fDenied)
			}
			assertP3fFailClosedPublicOutput(t, output, root)
			if runtime.fakeChildVerified {
				t.Fatalf("unsafe %s invoked the fake child", tc.name)
			}
		})
	}
}

func TestP3FFinalLaunchBoundaryRechecksActivationIdentity(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*p3fLaunchRequest)
	}{
		{name: "deadline", mutate: func(request *p3fLaunchRequest) { request.Deadline = request.Deadline.Add(time.Second) }},
		{name: "P3c action", mutate: func(request *p3fLaunchRequest) { request.P3cAction = "retry_other_action" }},
		{name: "P3d HostSpec", mutate: func(request *p3fLaunchRequest) { request.P3dHostSpecHash = "sha256:" + strings.Repeat("a", 64) }},
		{name: "P3d source snapshot", mutate: func(request *p3fLaunchRequest) { request.P3dSourceSnapshotHash = "sha256:" + strings.Repeat("b", 64) }},
		{name: "source manifest", mutate: func(request *p3fLaunchRequest) { request.SourceManifestHash = "sha256:" + strings.Repeat("c", 64) }},
		{name: "archive hash", mutate: func(request *p3fLaunchRequest) { request.ArchiveSHA256 = "sha256:" + strings.Repeat("e", 64) }},
		{name: "wrapper hash", mutate: func(request *p3fLaunchRequest) { request.Wrapper.BinarySHA256 = "sha256:" + strings.Repeat("d", 64) }},
		{name: "wrapper kind", mutate: func(request *p3fLaunchRequest) { request.Wrapper.Kind = "other_wrapper" }},
		{name: "route", mutate: func(request *p3fLaunchRequest) { request.Wrapper.Route = "other_route" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, request, root := newP3fFixtureRuntime(t, p3fSeatbeltSandbox{})
			runtime.afterPreflight = tc.mutate

			output, err := runtime.start(context.Background(), request)
			assertP3fStartFailure(t, err, p3fStartStageFence)
			assertP3fFailClosedPublicOutput(t, output, root)
			if runtime.fakeChildVerified {
				t.Fatalf("final-boundary %s identity drift invoked the fake child", tc.name)
			}
			if !runtime.lastStageClosed || !runtime.lastStageRemoved {
				t.Fatalf("final-boundary %s cleanup = closed:%v removed:%v, want both true", tc.name, runtime.lastStageClosed, runtime.lastStageRemoved)
			}
		})
	}
}

func TestP3FFinalLaunchBoundaryRechecksFullPrivateFence(t *testing.T) {
	runtime, request, root := newP3fFixtureRuntime(t, p3fSeatbeltSandbox{})
	runtime.afterPreflight = func(*p3fLaunchRequest) {
		if _, err := runtime.fence.ReclaimLaunchClaim(context.Background(), store.LaunchClaimReclaimRequest{
			ExpectedFence: request.Fence,
			Claim: store.LaunchClaimRequest{
				LaunchSpecHash: request.LaunchSpecHash,
				ClaimID:        "claim_p3f_reclaimed",
				ClaimTokenHash: "sha256:" + strings.Repeat("e", 64),
				OwnerID:        "p3f_fake_runtime",
				Attempt:        2,
			},
		}); err != nil {
			t.Fatalf("reclaim full private fence after preflight: %v", err)
		}
	}

	output, err := runtime.start(context.Background(), request)
	assertP3fStartFailure(t, err, p3fStartStageFence)
	assertP3fFailClosedPublicOutput(t, output, root)
	if runtime.fakeChildVerified {
		t.Fatal("reclaimed full private fence invoked the fake child")
	}
}

func TestP3FRejectsUntrackedArchiveBeforeFakeChild(t *testing.T) {
	archive, manifest := newP3fPlainArchive(t)
	t.Cleanup(func() { _ = archive.Close() })
	journal, fence := newP3fAdmittedFence(t)
	root := filepath.Join(t.TempDir(), "p3f-staging-root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create P3f staging root: %v", err)
	}
	activation := newP3fActivation(manifest)
	runtime, err := newP3fSandboxedFakeRuntime(
		journal,
		p3fFakeChildLauncher{},
		p3fSeatbeltSandbox{},
		root,
		activation,
		func() time.Time { return p3fFixtureNow },
	)
	if err != nil {
		t.Fatalf("construct P3f fake runtime: %v", err)
	}
	request := p3fRequestForActivation(activation, p3eLaunchSpecHash, fence, archive)

	output, startErr := runtime.start(context.Background(), request)
	assertP3fStartFailure(t, startErr, p3fStartStageArchive)
	assertP3fFailClosedPublicOutput(t, output, root)
	if runtime.fakeChildVerified {
		t.Fatal("plain tar archive reached the fake child")
	}
}

// TestP3FRejectsForgedPAXArchiveBeforeFakeChild proves that a PAX comment is
// metadata only: an otherwise identical hand-built tar cannot stand in for the
// immutable Git archive bound into the activation manifest.
func TestP3FRejectsForgedPAXArchiveBeforeFakeChild(t *testing.T) {
	tracked, manifest := newP3fTrackedGitArchive(t)
	t.Cleanup(func() { _ = tracked.Close() })
	archive := newP3fForgedPAXArchive(t, manifest.GitCommit)
	t.Cleanup(func() { _ = archive.Close() })
	paxCommit, err := p3fPAXArchiveCommit(archive)
	if err != nil || paxCommit != manifest.GitCommit {
		t.Fatalf("forged PAX commit = %q err=%v, want %q", paxCommit, err, manifest.GitCommit)
	}
	assertP3fArchiveMatchesManifest(t, archive, manifest)
	archiveHash, err := p3fArchiveSHA256(archive)
	if err != nil {
		t.Fatalf("hash forged P3f archive: %v", err)
	}
	if archiveHash == manifest.ArchiveSHA256 {
		t.Fatal("hand-built forged archive unexpectedly has the pinned Git archive bytes")
	}
	journal, fence := newP3fAdmittedFence(t)
	root := filepath.Join(t.TempDir(), "p3f-staging-root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create P3f staging root: %v", err)
	}
	activation := newP3fActivation(manifest)
	runtime, err := newP3fSandboxedFakeRuntime(
		journal,
		p3fFakeChildLauncher{},
		p3fUnsupportedSandbox{},
		root,
		activation,
		func() time.Time { return p3fFixtureNow },
	)
	if err != nil {
		t.Fatalf("construct P3f fake runtime: %v", err)
	}
	request := p3fRequestForActivation(activation, p3eLaunchSpecHash, fence, archive)

	output, startErr := runtime.start(context.Background(), request)
	assertP3fStartFailure(t, startErr, p3fStartStageArchive)
	assertP3fFailClosedPublicOutput(t, output, root)
	if runtime.fakeChildVerified {
		t.Fatal("forged PAX archive reached the fake child")
	}
}

func TestP3FDescriptorOwnedCleanupPreservesReplacement(t *testing.T) {
	runtime, request, root := newP3fFixtureRuntime(t, p3fSeatbeltSandbox{})
	foreignPath := ""
	runtime.afterStage = func(stage *p3fOwnedStage) {
		foreignPath = filepath.Join(root, stage.name)
		if err := os.RemoveAll(foreignPath); err != nil {
			t.Fatalf("remove owned stage before replacement: %v", err)
		}
		if err := os.Mkdir(foreignPath, 0o700); err != nil {
			t.Fatalf("create foreign replacement stage: %v", err)
		}
		if err := os.WriteFile(filepath.Join(foreignPath, "foreign"), []byte("preserve"), 0o600); err != nil {
			t.Fatalf("seed foreign replacement stage: %v", err)
		}
	}

	output, err := runtime.start(context.Background(), request)
	assertP3fStartFailure(t, err, p3fStartStageDescriptor)
	assertP3fFailClosedPublicOutput(t, output, root)
	if runtime.fakeChildVerified {
		t.Fatal("replaced stage reached the fake child")
	}
	if !runtime.lastStageClosed || runtime.lastStageRemoved {
		t.Fatalf("replacement cleanup = closed:%v removed:%v, want close-only", runtime.lastStageClosed, runtime.lastStageRemoved)
	}
	contents, readErr := os.ReadFile(filepath.Join(foreignPath, "foreign"))
	if readErr != nil || string(contents) != "preserve" {
		t.Fatalf("foreign replacement was changed: contents=%q err=%v", contents, readErr)
	}
}

func TestP3FUnsupportedSandboxFailsClosedBeforeFakeChild(t *testing.T) {
	runtime, request, root := newP3fFixtureRuntime(t, p3fUnsupportedSandbox{})
	output, err := runtime.start(context.Background(), request)
	failure := assertP3fStartFailure(t, err, p3fStartStageSandbox)
	if !errors.Is(failure.cause, errP3fSandboxUnsupported) {
		t.Fatalf("unsupported sandbox cause = %v, want %v", failure.cause, errP3fSandboxUnsupported)
	}
	assertP3fFailClosedPublicOutput(t, output, root)
	if runtime.fakeChildVerified {
		t.Fatal("unsupported sandbox reached the fake child")
	}
}

func newP3fFixtureRuntime(t *testing.T, sandbox p3fSandboxCapability) (*p3fSandboxedFakeRuntime, p3fLaunchRequest, string) {
	t.Helper()
	archive, manifest := newP3fTrackedGitArchive(t)
	t.Cleanup(func() { _ = archive.Close() })
	journal, fence := newP3fAdmittedFence(t)
	root := filepath.Join(t.TempDir(), "p3f-staging-root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create P3f staging root: %v", err)
	}
	activation := newP3fActivation(manifest)
	runtime, err := newP3fSandboxedFakeRuntime(
		journal,
		p3fFakeChildLauncher{},
		sandbox,
		root,
		activation,
		func() time.Time { return p3fFixtureNow },
	)
	if err != nil {
		t.Fatalf("construct P3f fake runtime: %v", err)
	}
	return runtime, p3fRequestForActivation(activation, p3eLaunchSpecHash, fence, archive), root
}

func newP3fAdmittedFence(t *testing.T) (*store.Store, store.LaunchFence) {
	t.Helper()
	orchestration, journal := newP3CTestOrchestration(t)
	admission := p3aAdmissionRequest()
	claim := p3cClaimRequest(admission.LaunchSpecHash)
	action, err := orchestration.admit(context.Background(), admission, claim)
	if err != nil {
		t.Fatalf("admit P3f fence: %v", err)
	}
	action, err = orchestration.recordTrustedMaterializationReady(context.Background(), p3aMaterializationRequest(action.Boundary.Claim.Fence))
	if err != nil {
		t.Fatalf("record P3f materialization: %v", err)
	}
	action, err = orchestration.admitRunIntent(context.Background(), store.LaunchRunIntentRequest{
		Fence: action.Boundary.Claim.Fence, MaterializationID: "materialization_p3a_001", RunID: "run_p3a_001", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("admit P3f run intent: %v", err)
	}
	if action.Boundary.Action != store.LaunchRecoveryRetryProcessAdmission {
		t.Fatalf("P3f fence action = %q, want process admission", action.Boundary.Action)
	}
	return journal, action.Boundary.Claim.Fence
}

func newP3fTrackedGitArchive(t *testing.T) (*os.File, p3fTrackedSourceManifest) {
	t.Helper()
	repository := t.TempDir()
	runP3fGit(t, repository, "init", "--initial-branch=p3f-fixture")
	for _, file := range []struct {
		name     string
		contents string
	}{
		{name: "go_module", contents: "module fake.example/p3f\n"},
		{name: "lifecycle_core", contents: "fake lifecycle\n"},
		{name: "supervisor_core", contents: "fake supervisor\n"},
	} {
		if err := os.WriteFile(filepath.Join(repository, file.name), []byte(file.contents), 0o600); err != nil {
			t.Fatalf("write P3f git fixture %s: %v", file.name, err)
		}
	}
	runP3fGit(t, repository, "add", "go_module", "lifecycle_core", "supervisor_core")
	runP3fGit(t, repository, "-c", "user.name=P3f Fixture", "-c", "user.email=p3f.fixture@example.invalid", "commit", "--quiet", "-m", "p3f fixture")
	commit := strings.TrimSpace(runP3fGit(t, repository, "rev-parse", "HEAD"))
	archivePath := filepath.Join(t.TempDir(), "p3f-tracked-source.tar")
	runP3fGit(t, repository, "archive", "--format=tar", "--output="+archivePath, commit)
	archive, err := os.Open(archivePath)
	if err != nil {
		t.Fatalf("open P3f git archive descriptor: %v", err)
	}
	archiveHash, err := p3fArchiveSHA256(archive)
	if err != nil {
		_ = archive.Close()
		t.Fatalf("hash P3f git archive: %v", err)
	}
	return archive, newP3fManifestForContents(commit, archiveHash, []p3fFixtureSourceFile{
		{id: "go_module", contents: "module fake.example/p3f\n"},
		{id: "lifecycle_core", contents: "fake lifecycle\n"},
		{id: "supervisor_core", contents: "fake supervisor\n"},
	})
}

func newP3fPlainArchive(t *testing.T) (*os.File, p3fTrackedSourceManifest) {
	t.Helper()
	contents := []p3fFixtureSourceFile{
		{id: "go_module", contents: "module fake.example/p3f\n"},
		{id: "lifecycle_core", contents: "fake lifecycle\n"},
		{id: "supervisor_core", contents: "fake supervisor\n"},
	}
	path := filepath.Join(t.TempDir(), "p3f-untracked-source.tar")
	output, err := os.Create(path)
	if err != nil {
		t.Fatalf("create plain P3f archive: %v", err)
	}
	writer := tar.NewWriter(output)
	for _, file := range contents {
		if err := writer.WriteHeader(&tar.Header{Name: file.id, Mode: 0o600, Size: int64(len(file.contents)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("write plain P3f archive header: %v", err)
		}
		if _, err := writer.Write([]byte(file.contents)); err != nil {
			t.Fatalf("write plain P3f archive body: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close plain P3f archive: %v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatalf("close plain P3f archive output: %v", err)
	}
	archive, err := os.Open(path)
	if err != nil {
		t.Fatalf("open plain P3f archive descriptor: %v", err)
	}
	archiveHash, err := p3fArchiveSHA256(archive)
	if err != nil {
		_ = archive.Close()
		t.Fatalf("hash plain P3f archive: %v", err)
	}
	return archive, newP3fManifestForContents(strings.Repeat("a", 40), archiveHash, contents)
}

func newP3fForgedPAXArchive(t *testing.T, commit string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p3f-forged-pax-source.tar")
	output, err := os.Create(path)
	if err != nil {
		t.Fatalf("create forged P3f archive: %v", err)
	}
	writer := tar.NewWriter(output)
	if err := writer.WriteHeader(&tar.Header{
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": commit},
	}); err != nil {
		t.Fatalf("write forged PAX global header: %v", err)
	}
	for _, file := range []p3fFixtureSourceFile{
		{id: "go_module", contents: "module fake.example/p3f\n"},
		{id: "lifecycle_core", contents: "fake lifecycle\n"},
		{id: "supervisor_core", contents: "fake supervisor\n"},
	} {
		if err := writer.WriteHeader(&tar.Header{Name: file.id, Mode: 0o600, Size: int64(len(file.contents)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("write forged P3f archive header: %v", err)
		}
		if _, err := writer.Write([]byte(file.contents)); err != nil {
			t.Fatalf("write forged P3f archive body: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close forged P3f archive: %v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatalf("close forged P3f archive output: %v", err)
	}
	archive, err := os.Open(path)
	if err != nil {
		t.Fatalf("open forged P3f archive descriptor: %v", err)
	}
	return archive
}

func assertP3fArchiveMatchesManifest(t *testing.T, archive *os.File, manifest p3fTrackedSourceManifest) {
	t.Helper()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind forged P3f archive: %v", err)
	}
	reader := tar.NewReader(archive)
	for _, expected := range manifest.Entries {
		header, err := p3fNextArchiveMember(reader)
		if err != nil || header == nil {
			t.Fatalf("read forged P3f archive member: header=%v err=%v", header, err)
		}
		if header.Typeflag != tar.TypeReg || header.Name != expected.EntryID || header.Linkname != "" {
			t.Fatalf("forged P3f archive member = name:%q type:%q link:%q, want pinned regular %q", header.Name, header.Typeflag, header.Linkname, expected.EntryID)
		}
		contents, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read forged P3f archive body %q: %v", expected.EntryID, err)
		}
		sum := sha256.Sum256(contents)
		if got := "sha256:" + hex.EncodeToString(sum[:]); got != expected.BlobSHA256 {
			t.Fatalf("forged P3f archive hash for %q = %q, want %q", expected.EntryID, got, expected.BlobSHA256)
		}
	}
	if header, err := reader.Next(); err != io.EOF || header != nil {
		t.Fatalf("forged P3f archive has unexpected member: header=%v err=%v", header, err)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("rewind verified forged P3f archive: %v", err)
	}
}

type p3fFixtureSourceFile struct {
	id       string
	contents string
}

func newP3fManifestForContents(commit, archiveSHA256 string, files []p3fFixtureSourceFile) p3fTrackedSourceManifest {
	entries := make([]p3fTrackedSourceEntry, len(files))
	for index, file := range files {
		sum := sha256.Sum256([]byte(file.contents))
		entries[index] = p3fTrackedSourceEntry{EntryID: file.id, BlobSHA256: "sha256:" + hex.EncodeToString(sum[:])}
	}
	manifest := p3fTrackedSourceManifest{
		ArchiveSHA256:                 archiveSHA256,
		SchemaVersion:                 "ananke.tracked-source-manifest.v1",
		GitCommit:                     commit,
		P3dRequiredSourceSnapshotHash: p3fP3dSourceSnapshotHash,
		RepositoryIdentity:            p3fRepositoryIdentity,
		Tracked:                       true,
		Entries:                       entries,
	}
	manifest.SourceManifestHash = p3fHashTrackedSourceManifest(manifest)
	return manifest
}

func runP3fGit(t *testing.T, directory string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", directory}, arguments...)...)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir()}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run fake git archive command %q: %v\n%s", arguments, err, output)
	}
	return string(output)
}

func assertP3fStartFailure(t *testing.T, err error, want p3fStartStage) *p3fStartFailure {
	t.Helper()
	if err == nil {
		t.Fatalf("start error = nil, want denied %s", want)
	}
	if !errors.Is(err, errP3fDenied) || err.Error() != errP3fDenied.Error() {
		t.Fatalf("start error = %v, want sanitized %v", err, errP3fDenied)
	}
	var failure *p3fStartFailure
	if !errors.As(err, &failure) {
		t.Fatalf("start error %T does not retain a private P3f failure", err)
	}
	if failure.stage != want || failure.cause == nil {
		t.Fatalf("start failure stage=%v cause=%v, want private %s cause", failure.stage, failure.cause, want)
	}
	return failure
}

func assertP3fFailClosedPublicOutput(t *testing.T, output p3fPublicOutput, root string) {
	t.Helper()
	if !equalP3fPublicOutput(output, p3fFailClosedOutput()) {
		t.Fatalf("public output = %+v, want exact fail closed", output)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal P3f public output: %v", err)
	}
	for _, forbidden := range []string{root, "fake.example", "lifecycle_core", "supervisor_core", "credential", "sandbox", `"result":{`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public output leaks %q: %s", forbidden, encoded)
		}
	}
}
