package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

const ompProductionExecutionFakeEvidence = "ananke-omp-production-fake-wrapper-evidence-v1"

func TestOMPProductionFakeWrapperExecutorRunsOnlyFixedTestBinary(t *testing.T) {
	fixture := newOMPProductionExecutionFixture(t, func() time.Time { return p3fFixtureNow })

	output, err := fixture.executor.execute(context.Background(), fixture.prepared)
	if err != nil {
		var failure *ompProductionExecutionFailure
		if errors.As(err, &failure) {
			t.Fatalf("execute sealed fake wrapper: stage=%s cause=%v", failure.stage, failure.cause)
		}
		t.Fatalf("execute sealed fake wrapper: %v", err)
	}
	assertOMPProductionExecutionOutput(t, output)
	assertOMPProductionExecutionEvidence(t, fixture.descriptors.evidence)
	assertOMPProductionExecutionSourceUnchanged(t, fixture.descriptors.source)
}

func TestOMPProductionFakeWrapperChild(t *testing.T) {
	if !ompProductionHasFakeWrapperChildArgument(os.Args[1:]) {
		return
	}
	for _, argument := range os.Args[1:] {
		if argument != "-test.run=^TestOMPProductionFakeWrapperChild$" && argument != ompProductionFakeWrapperChildArgument && strings.Contains(argument, "/") {
			t.Fatalf("fake wrapper child received raw path argument %q", argument)
		}
	}
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		lower := strings.ToLower(name)
		for _, forbidden := range []string{"credential", "password", "secret", "token"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("fake wrapper child received credential environment %q", name)
			}
		}
	}

	source := os.NewFile(uintptr(3), "omp-production-fake-source")
	manifest := os.NewFile(uintptr(4), "omp-production-fake-manifest")
	evidence := os.NewFile(uintptr(5), "omp-production-fake-evidence")
	if source == nil || manifest == nil || evidence == nil {
		t.Fatal("fake wrapper child inherited an incomplete FD-only interface")
	}
	defer source.Close()
	defer manifest.Close()
	defer evidence.Close()

	manifestBytes, err := io.ReadAll(io.LimitReader(manifest, 4096))
	if err != nil || string(manifestBytes) != "sealed test manifest\n" {
		t.Fatalf("fake wrapper child read inherited manifest = %q err=%v", manifestBytes, err)
	}
	info, err := source.Stat()
	if err != nil {
		t.Fatalf("stat inherited source descriptor: %v", err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Fatalf("source descriptor is DAC read-only; sandbox proof would be inconclusive: %v", info.Mode())
	}

	readFD, readErr := unix.Openat(int(source.Fd()), "source", unix.O_RDONLY|unix.O_NOFOLLOW, 0)
	if readErr != nil {
		t.Fatalf("open source through inherited descriptor: %v", readErr)
	}
	readFile := os.NewFile(uintptr(readFD), "omp-production-fake-source-read")
	contents, sourceErr := io.ReadAll(readFile)
	closeErr := readFile.Close()
	if sourceErr != nil || closeErr != nil || string(contents) != "sealed test source\n" {
		t.Fatalf("read staged fake source = %q read=%v close=%v", contents, sourceErr, closeErr)
	}

	writeFD, writeErr := unix.Openat(int(source.Fd()), "source", unix.O_WRONLY|unix.O_NOFOLLOW, 0)
	if writeErr == nil {
		_ = unix.Close(writeFD)
		t.Fatal("OS sandbox allowed a source write through the inherited descriptor")
	}
	if !errors.Is(writeErr, unix.EACCES) && !errors.Is(writeErr, unix.EPERM) {
		t.Fatalf("source write error = %v, want OS write denial", writeErr)
	}
	if _, err := evidence.WriteString(ompProductionExecutionFakeEvidence); err != nil {
		t.Fatalf("write inherited evidence descriptor: %v", err)
	}
}

func TestOMPProductionWrapperExecutorRejectsIdentityAndDescriptorDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ompPreparedFDActivationRequest)
	}{
		{name: "wrapper digest", mutate: func(request *ompPreparedFDActivationRequest) {
			request.wrapper.binarySHA256 = "sha256:" + strings.Repeat("a", 64)
		}},
		{name: "wrapper kind", mutate: func(request *ompPreparedFDActivationRequest) { request.wrapper.kind = "other_wrapper" }},
		{name: "wrapper route", mutate: func(request *ompPreparedFDActivationRequest) { request.wrapper.route = "other_route" }},
		{name: "source manifest", mutate: func(request *ompPreparedFDActivationRequest) {
			request.sourceManifestHash = "sha256:" + strings.Repeat("b", 64)
		}},
		{name: "P3d source snapshot", mutate: func(request *ompPreparedFDActivationRequest) {
			request.p3dSourceSnapshotHash = "sha256:" + strings.Repeat("c", 64)
		}},
		{name: "P3c action", mutate: func(request *ompPreparedFDActivationRequest) { request.p3cAction = "other_action" }},
		{name: "deadline", mutate: func(request *ompPreparedFDActivationRequest) { request.deadline = request.deadline.Add(time.Second) }},
		{name: "closed source descriptor", mutate: func(request *ompPreparedFDActivationRequest) { _ = request.source.Close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newOMPProductionExecutionFixture(t, func() time.Time { return p3fFixtureNow })
			tc.mutate(&fixture.prepared)

			output, err := fixture.executor.execute(context.Background(), fixture.prepared)
			assertOMPProductionExecutionDenied(t, output, err, ompProductionExecutionStageRequest)
			assertOMPProductionExecutionEvidenceEmpty(t, fixture.descriptors.evidence)
		})
	}
}

func TestOMPProductionWrapperExecutorRejectsArtifactHashDriftAtFinalBoundary(t *testing.T) {
	fixture := newOMPProductionExecutionFixture(t, func() time.Time { return p3fFixtureNow })
	if _, err := fixture.executor.wrapper.descriptor.file.WriteAt([]byte{0}, 0); err != nil {
		t.Fatalf("mutate sealed fake wrapper copy: %v", err)
	}

	output, err := fixture.executor.execute(context.Background(), fixture.prepared)
	assertOMPProductionExecutionDenied(t, output, err, ompProductionExecutionStageAdmission)
	assertOMPProductionExecutionEvidenceEmpty(t, fixture.descriptors.evidence)
}

func TestOMPProductionWrapperExecutorRechecksDeadlineAndFullFenceAtAdmission(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		calls := 0
		fixture := newOMPProductionExecutionFixture(t, func() time.Time {
			calls++
			if calls == 1 {
				return p3fFixtureNow
			}
			return ompProductionDeadline
		})

		output, err := fixture.executor.execute(context.Background(), fixture.prepared)
		assertOMPProductionExecutionDenied(t, output, err, ompProductionExecutionStageAdmission)
		assertOMPProductionExecutionEvidenceEmpty(t, fixture.descriptors.evidence)
	})

	t.Run("full private fence", func(t *testing.T) {
		fixture := newOMPProductionExecutionFixture(t, func() time.Time { return p3fFixtureNow })
		fixture.executor.fence = &ompProductionFenceReclaimer{
			store: fixture.journal,
			fence: fixture.prepared.fence,
		}

		output, err := fixture.executor.execute(context.Background(), fixture.prepared)
		assertOMPProductionExecutionDenied(t, output, err, ompProductionExecutionStageAdmission)
		assertOMPProductionExecutionEvidenceEmpty(t, fixture.descriptors.evidence)
	})
}

func TestOMPProductionWrapperExecutorClosesOwnedDescriptors(t *testing.T) {
	fixture := newOMPProductionExecutionFixture(t, func() time.Time { return p3fFixtureNow })
	probe := &ompProductionDescriptorCloseProbe{}
	fixture.executor.sandbox = probe

	output, err := fixture.executor.execute(context.Background(), fixture.prepared)
	assertOMPProductionExecutionDenied(t, output, err, ompProductionExecutionStageSandbox)
	if probe.files.source == nil || probe.files.manifest == nil || probe.files.evidence == nil {
		t.Fatal("sandbox did not receive all inherited descriptors")
	}
	for _, descriptor := range []*os.File{probe.files.source, probe.files.manifest, probe.files.evidence} {
		if _, statErr := descriptor.Stat(); !errors.Is(statErr, os.ErrClosed) {
			t.Fatalf("owned descriptor stat error = %v, want os.ErrClosed", statErr)
		}
	}
	if _, statErr := fixture.descriptors.source.Stat(); statErr != nil {
		t.Fatalf("executor closed caller-owned source descriptor: %v", statErr)
	}
}

func TestOMPProductionWrapperExecutorCleanupClosesOwnedWrapperAfterPathReplacement(t *testing.T) {
	fixture := newOMPProductionExecutionFixture(t, func() time.Time { return p3fFixtureNow })
	descriptor := fixture.executor.wrapper.descriptor.file
	parent := fixture.executor.wrapper.parent
	if descriptor == nil || parent == nil {
		t.Fatal("fixture did not retain owned wrapper descriptors")
	}

	foreignPath := parent.Name()
	if err := os.RemoveAll(foreignPath); err != nil {
		t.Fatalf("remove owned wrapper directory before replacement: %v", err)
	}
	if err := os.Mkdir(foreignPath, 0o700); err != nil {
		t.Fatalf("create foreign wrapper replacement: %v", err)
	}
	if err := os.WriteFile(filepath.Join(foreignPath, "foreign"), []byte("preserve"), 0o600); err != nil {
		t.Fatalf("seed foreign wrapper replacement: %v", err)
	}

	closeErr := fixture.executor.close()
	if !errors.Is(closeErr, errOMPProductionExecutionDenied) {
		t.Fatalf("replacement cleanup error = %v, want sanitized ownership denial", closeErr)
	}
	for _, owned := range []*os.File{descriptor, parent} {
		if _, err := owned.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("replacement cleanup left owned descriptor open: %v", err)
		}
	}
	contents, err := os.ReadFile(filepath.Join(foreignPath, "foreign"))
	if err != nil || string(contents) != "preserve" {
		t.Fatalf("foreign wrapper replacement changed: contents=%q err=%v", contents, err)
	}
}

func TestOMPProductionWrapperExecutorUnsupportedSandboxFailsClosed(t *testing.T) {
	fixture := newOMPProductionExecutionFixture(t, func() time.Time { return p3fFixtureNow })
	fixture.executor.sandbox = ompProductionUnsupportedSandbox{}

	output, err := fixture.executor.execute(context.Background(), fixture.prepared)
	failure := assertOMPProductionExecutionDenied(t, output, err, ompProductionExecutionStageSandbox)
	if !errors.Is(failure.cause, errOMPProductionSandboxUnsupported) {
		t.Fatalf("unsupported sandbox cause = %v, want %v", failure.cause, errOMPProductionSandboxUnsupported)
	}
	assertOMPProductionExecutionEvidenceEmpty(t, fixture.descriptors.evidence)
}

func TestOMPProductionFakeExecutionSurfaceAcceptsNoCallerAuthority(t *testing.T) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "omp_production_fake_execution_test.go", nil, 0)
	if err != nil {
		t.Fatalf("parse test-only fake wrapper executor: %v", err)
	}
	forbiddenFields := map[string]bool{
		"argv":        true,
		"command":     true,
		"env":         true,
		"environment": true,
		"executable":  true,
		"path":        true,
		"program":     true,
	}
	ast.Inspect(parsed, func(node ast.Node) bool {
		field, ok := node.(*ast.Field)
		if !ok {
			return true
		}
		for _, name := range field.Names {
			if forbiddenFields[normalizeOMPProductionASTName(name.Name)] {
				t.Fatalf("test-only fake wrapper executor exposes caller authority field %q", name.Name)
			}
		}
		return true
	})
}

type ompProductionExecutionFixture struct {
	executor    *ompProductionFakeWrapperExecutor
	prepared    ompPreparedFDActivationRequest
	descriptors ompProductionActivationDescriptors
	journal     *store.Store
}

func newOMPProductionExecutionFixture(t *testing.T, now func() time.Time) ompProductionExecutionFixture {
	t.Helper()
	approval := ompProductionApprovedWrapperIdentityForTest()
	journal, fence := newP3fAdmittedFence(t)
	descriptors := ompProductionExecutionDescriptorsForTest(t)
	preparer, err := newOMPProductionActivationPreparer(journal, approval, now)
	if err != nil {
		t.Fatalf("construct production activation preparer: %v", err)
	}
	prepared, err := preparer.prepare(context.Background(), ompProductionActivationInput{
		launchSpecHash:        p3eLaunchSpecHash,
		fence:                 fence,
		deadline:              ompProductionDeadline,
		p3cAction:             ompProductionP3cAction,
		p3dHostSpecHash:       ompProductionP3dHostSpecHash,
		p3dSourceSnapshotHash: ompProductionP3dSourceSnapshotHash,
		sourceManifestHash:    ompProductionSourceManifestHash,
		wrapper:               approval,
		descriptors:           descriptors,
	})
	if err != nil {
		t.Fatalf("prepare execution request: %v", err)
	}
	executor, err := newOMPProductionFakeWrapperExecutor(journal, now)
	if err != nil {
		t.Fatalf("construct fixed test-binary fake wrapper executor: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := executor.close(); closeErr != nil {
			t.Errorf("close test-only fake wrapper executor: %v", closeErr)
		}
	})
	return ompProductionExecutionFixture{
		executor: executor, prepared: prepared, descriptors: descriptors, journal: journal,
	}
}

func ompProductionExecutionDescriptorsForTest(t *testing.T) ompProductionActivationDescriptors {
	t.Helper()
	open := func(name, contents string) *os.File {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		file, err := os.OpenFile(path, os.O_RDWR, 0)
		if err != nil {
			t.Fatalf("open %s descriptor: %v", name, err)
		}
		t.Cleanup(func() { _ = file.Close() })
		return file
	}
	sourceDirectory := filepath.Join(t.TempDir(), "source")
	if err := os.Mkdir(sourceDirectory, 0o700); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDirectory, "source"), []byte("sealed test source\n"), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	source, err := os.Open(sourceDirectory)
	if err != nil {
		t.Fatalf("open source directory descriptor: %v", err)
	}
	t.Cleanup(func() { _ = source.Close() })
	return ompProductionActivationDescriptors{
		source:   source,
		manifest: open("manifest", "sealed test manifest\n"),
		evidence: open("evidence", ""),
	}
}

type ompProductionFenceReclaimer struct {
	store     *store.Store
	fence     store.LaunchFence
	reclaimed bool
}

func (reclaimer *ompProductionFenceReclaimer) GetLaunchRecoveryBoundary(ctx context.Context, launchSpecHash string) (store.LaunchRecoveryBoundary, error) {
	return reclaimer.store.GetLaunchRecoveryBoundary(ctx, launchSpecHash)
}

func (reclaimer *ompProductionFenceReclaimer) WithLaunchFenceAdmission(ctx context.Context, launchSpecHash string, fence store.LaunchFence, invoke func(store.LaunchRecoveryBoundary) error) error {
	if !reclaimer.reclaimed {
		reclaimer.reclaimed = true
		_, err := reclaimer.store.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
			ExpectedFence: reclaimer.fence,
			Claim: store.LaunchClaimRequest{
				LaunchSpecHash: launchSpecHash,
				ClaimID:        "claim_p3f_execution_reclaimed",
				ClaimTokenHash: "sha256:" + strings.Repeat("e", 64),
				OwnerID:        "p3f_execution_runtime",
				Attempt:        2,
			},
		})
		if err != nil {
			return err
		}
	}
	return reclaimer.store.WithLaunchFenceAdmission(ctx, launchSpecHash, fence, invoke)
}

type ompProductionDescriptorCloseProbe struct {
	files ompProductionSandboxFiles
}

func (probe *ompProductionDescriptorCloseProbe) start(_ context.Context, files ompProductionSandboxFiles) error {
	probe.files = files
	return errOMPProductionSandboxUnsupported
}

func ompProductionHasFakeWrapperChildArgument(arguments []string) bool {
	for _, argument := range arguments {
		if argument == ompProductionFakeWrapperChildArgument {
			return true
		}
	}
	return false
}

func assertOMPProductionExecutionOutput(t *testing.T, output ompProductionExecutionOutput) {
	t.Helper()
	if output.SchemaVersion != "ananke.omp-production-output.v1" || output.State != "waiting_for_human" || output.VerificationState != "not_run" || output.Result != nil || len(output.Events) != 0 {
		t.Fatalf("production execution output = %+v, want closed normalized waiting_for_human", output)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal production execution output: %v", err)
	}
	if string(encoded) != `{"events":[],"result":null,"schema_version":"ananke.omp-production-output.v1","state":"waiting_for_human","verification_state":"not_run"}` {
		t.Fatalf("production execution output JSON = %s, want exact normalized shape", encoded)
	}
}

func assertOMPProductionExecutionDenied(t *testing.T, output ompProductionExecutionOutput, err error, stage ompProductionExecutionStage) *ompProductionExecutionFailure {
	t.Helper()
	assertOMPProductionExecutionOutput(t, output)
	if !errors.Is(err, errOMPProductionExecutionDenied) || err.Error() != errOMPProductionExecutionDenied.Error() {
		t.Fatalf("production execution error = %v, want sanitized %v", err, errOMPProductionExecutionDenied)
	}
	var failure *ompProductionExecutionFailure
	if !errors.As(err, &failure) || failure.stage != stage || failure.cause == nil {
		t.Fatalf("production execution failure = %#v, want private %s cause", failure, stage)
	}
	return failure
}

func assertOMPProductionExecutionEvidence(t *testing.T, evidence *os.File) {
	t.Helper()
	contents := make([]byte, len(ompProductionExecutionFakeEvidence)+1)
	count, err := evidence.ReadAt(contents, 0)
	if err != nil && !errors.Is(err, os.ErrClosed) {
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrInvalid) && count != len(ompProductionExecutionFakeEvidence) {
			t.Fatalf("read fake wrapper evidence: count=%d err=%v", count, err)
		}
	}
	if string(contents[:count]) != ompProductionExecutionFakeEvidence {
		t.Fatalf("fake wrapper evidence = %q, want %q", contents[:count], ompProductionExecutionFakeEvidence)
	}
}

func assertOMPProductionExecutionEvidenceEmpty(t *testing.T, evidence *os.File) {
	t.Helper()
	info, err := evidence.Stat()
	if err != nil {
		t.Fatalf("stat denied execution evidence: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("denied execution wrote evidence size %d", info.Size())
	}
}

func assertOMPProductionExecutionSourceUnchanged(t *testing.T, source *os.File) {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(source.Name(), "source"))
	if err != nil || string(contents) != "sealed test source\n" {
		t.Fatalf("source after sandboxed fake wrapper = %q err=%v", contents, err)
	}
}
