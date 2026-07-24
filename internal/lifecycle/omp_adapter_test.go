package lifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func TestP3EFakeAdapterExecutable(t *testing.T) {
	if os.Getenv("ANANKE_P3E_FAKE_ADAPTER") != "1" {
		return
	}
	if invocationPath := os.Getenv("ANANKE_P3E_FAKE_INVOCATIONS"); invocationPath != "" {
		if err := os.WriteFile(invocationPath, []byte("fake-adapter\n"), 0o600); err != nil {
			os.Exit(91)
		}
	}

	emit := func(event string) {
		fmt.Printf(`{"dialect":"omp_audit_stream_v1","event":%q,"source":"omp_readonly_wrapper_transcript_v1"}`+"\n", event)
	}
	switch os.Getenv("ANANKE_P3E_FAKE_MODE") {
	case "complete":
		emit("audit_started")
		emit("audit_finding")
		emit("audit_completed")
	case "unknown":
		emit("unrecognized_event")
	case "crash":
		emit("audit_started")
		os.Exit(17)
	case "duplicate":
		fmt.Println(`{"dialect":"omp_audit_stream_v1","event":"audit_started","event":"audit_completed","source":"omp_readonly_wrapper_transcript_v1"}`)
	case "hold":
		emit("audit_started")
		terminated := make(chan os.Signal, 1)
		signal.Notify(terminated, syscall.SIGTERM)
		<-terminated
		select {}
	case "block":
		emit("audit_started")
		terminated := make(chan os.Signal, 1)
		signal.Notify(terminated, syscall.SIGTERM)
		<-terminated
	default:
		os.Exit(92)
	}
	os.Exit(0)
}

type p3eFakeStartGateAdapter struct {
	adapter              p3eExecAdapter
	waitForInvocation    func(context.Context) error
	invocationObserved   chan<- struct{}
	releaseAdapterReturn <-chan struct{}
}

func (a p3eFakeStartGateAdapter) Start(ctx context.Context, invocation ompReadOnlyInvocation) (ompReadOnlyProcess, error) {
	process, err := a.adapter.Start(ctx, invocation)
	if err != nil {
		return nil, err
	}
	if err := a.waitForInvocation(ctx); err != nil {
		_ = process.Kill()
		_ = process.Wait()
		return nil, err
	}
	close(a.invocationObserved)
	select {
	case <-a.releaseAdapterReturn:
		return process, nil
	case <-ctx.Done():
		_ = process.Kill()
		_ = process.Wait()
		return nil, ctx.Err()
	}
}

type p3eFailingAdapter struct{ cause error }

func (a p3eFailingAdapter) Start(context.Context, ompReadOnlyInvocation) (ompReadOnlyProcess, error) {
	return nil, a.cause
}

func waitP3EFakeInvocation(ctx context.Context, invocationPath string) error {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		contents, err := os.ReadFile(invocationPath)
		if err == nil {
			if string(contents) != "fake-adapter\n" {
				return fmt.Errorf("fake invocation record = %q", contents)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("read fake invocation record: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitP3EWaitingForSQLiteConnection(t *testing.T, ctx context.Context, db *sql.DB, previousWaitCount int64) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if db.Stats().WaitCount > previousWaitCount {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for reclaim to queue on pinned SQLite connection: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestP3EControlledFakeAdapterLifecycleNormalizesAndCleansUp(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)

	initial, err := runtime.start(ctx, request)
	if err != nil {
		t.Fatalf("start controlled fake adapter: %v", err)
	}
	if initial.AdapterState != p3eAdapterStateMonitoring || initial.Result != nil {
		t.Fatalf("initial public state = %+v, want monitoring without result", initial)
	}
	state := waitP3EState(t, runtime, request.RequestID, p3eAdapterStateCompleted)
	if got, want := state.Events, []p3eAuditEvent{
		{Sequence: 1, Kind: p3eAuditStarted},
		{Sequence: 2, Kind: p3eAuditFinding},
		{Sequence: 3, Kind: p3eAuditCompleted},
	}; !equalP3eEvents(got, want) {
		t.Fatalf("normalized events = %+v, want %+v", got, want)
	}
	if state.Result == nil || state.Result.EventCount != 3 || state.Result.AdvisoryFindings != 1 || state.Result.BlockingFindings != 0 || state.Result.VerificationState != "not_run" {
		t.Fatalf("bounded normalized result = %+v, want exact completed summary", state.Result)
	}
	assertP3ENoRawPublicIR(t, state, root)
	assertP3EFakeInvoked(t, invocationPath)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}

func TestP3EControlledFakeAdapterReconnectAndCrashFailClosed(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name        string
		mode        string
		awaitPrefix bool
	}{
		{name: "reconnect while known prefix is live", mode: "block", awaitPrefix: true},
		{name: "crash after known prefix", mode: "crash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, request, invocationPath, root := newP3EFakeRuntime(t, tc.mode, time.Now)
			if _, err := runtime.start(ctx, request); err != nil {
				t.Fatalf("start controlled fake adapter: %v", err)
			}
			if tc.awaitPrefix {
				waitP3EEventCount(t, runtime, request.RequestID, 1)
			} else {
				waitP3EState(t, runtime, request.RequestID, p3eAdapterStateWaitingForHuman)
			}
			recovery, err := runtime.recover(ctx, request)
			if err != nil {
				t.Fatalf("recover controlled fake adapter: %v", err)
			}
			if recovery.Action != p3eRecoveryReconnectTranscript || recovery.State.AdapterState != p3eAdapterStateWaitingForHuman || len(recovery.State.Events) != 0 || recovery.State.Result != nil {
				t.Fatalf("recovery = %+v, want bounded reconnect without inferred events/result", recovery)
			}
			assertP3ENoRawPublicIR(t, recovery.State, root)
			assertP3EFakeInvoked(t, invocationPath)

			if tc.mode == "block" {
				cancelCtx, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				if err := runtime.cancel(cancelCtx, request.RequestID); err != nil {
					t.Fatalf("cancel live fake adapter: %v", err)
				}
			}
			state := waitP3EState(t, runtime, request.RequestID, p3eAdapterStateWaitingForHuman)
			if state.Result != nil || len(state.Events) != 0 {
				t.Fatalf("crash/reconnect final state = %+v, want no inferred event/result", state)
			}
			assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
		})
	}
}

func TestP3EControlledFakeAdapterCancellationAndTimeoutCleanUp(t *testing.T) {
	ctx := context.Background()
	t.Run("bounded cancellation", func(t *testing.T) {
		runtime, request, invocationPath, root := newP3EFakeRuntime(t, "block", time.Now)
		if _, err := runtime.start(ctx, request); err != nil {
			t.Fatalf("start controlled fake adapter: %v", err)
		}
		waitP3EEventCount(t, runtime, request.RequestID, 1)
		cancelCtx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := runtime.cancel(cancelCtx, request.RequestID); err != nil {
			t.Fatalf("cancel controlled fake adapter: %v", err)
		}
		state := waitP3EState(t, runtime, request.RequestID, p3eAdapterStateWaitingForHuman)
		if state.Result != nil || len(state.Events) != 0 {
			t.Fatalf("cancel public state = %+v, want no terminal inference", state)
		}
		assertP3EFakeInvoked(t, invocationPath)
		assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
	})
	t.Run("deadline timeout", func(t *testing.T) {
		nearDeadline := func() time.Time { return time.Date(2026, 7, 30, 11, 59, 59, 900000000, time.UTC) }
		runtime, request, invocationPath, root := newP3EFakeRuntime(t, "block", nearDeadline)
		if _, err := runtime.start(ctx, request); err != nil {
			t.Fatalf("start controlled fake adapter: %v", err)
		}
		state := waitP3EState(t, runtime, request.RequestID, p3eAdapterStateWaitingForHuman)
		if state.Result != nil || len(state.Events) != 0 {
			t.Fatalf("timeout public state = %+v, want no terminal inference", state)
		}
		assertP3EFakeInvoked(t, invocationPath)
		assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
	})
}

func TestP3ERejectsUnsafeBoundaryBeforeFakeExecution(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, runtime *ompReadOnlyRuntime, request *p3eAdapterRequest)
	}{
		{name: "bare route", mutate: func(_ *testing.T, _ *ompReadOnlyRuntime, request *p3eAdapterRequest) { request.HostSpec.Route = "omp" }},
		{name: "unknown route", mutate: func(_ *testing.T, _ *ompReadOnlyRuntime, request *p3eAdapterRequest) {
			request.HostSpec.Route = "other_omp_route"
		}},
		{name: "bad source", mutate: func(_ *testing.T, _ *ompReadOnlyRuntime, request *p3eAdapterRequest) {
			request.Materialization.Files[0].Contents = []byte("mutated source")
		}},
		{name: "bad root", mutate: func(t *testing.T, runtime *ompReadOnlyRuntime, _ *p3eAdapterRequest) {
			outside := t.TempDir()
			if err := os.Remove(runtime.root.path); err != nil {
				t.Fatalf("remove fake root: %v", err)
			}
			if err := os.Symlink(outside, runtime.root.path); err != nil {
				t.Fatalf("replace fake root with symlink: %v", err)
			}
		}},
		{name: "bad materialization hash", mutate: func(_ *testing.T, _ *ompReadOnlyRuntime, request *p3eAdapterRequest) {
			request.Materialization.Sealed.MaterializationHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}},
		{name: "bad nonce", mutate: func(_ *testing.T, _ *ompReadOnlyRuntime, request *p3eAdapterRequest) {
			request.Materialization.Sealed.Nonce = "nonce:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}},
		{name: "writable surface", mutate: func(_ *testing.T, _ *ompReadOnlyRuntime, request *p3eAdapterRequest) {
			request.HostSpec.Writes = "allowed"
		}},
		{name: "stale fence", mutate: func(t *testing.T, runtime *ompReadOnlyRuntime, request *p3eAdapterRequest) {
			if _, err := runtime.fence.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
				ExpectedFence: request.Fence,
				Claim: store.LaunchClaimRequest{
					LaunchSpecHash: request.LaunchSpecHash,
					ClaimID:        "claim_p3e_reclaimed",
					ClaimTokenHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					OwnerID:        "p3e_runtime",
					Attempt:        2,
				},
			}); err != nil {
				t.Fatalf("reclaim launch fence: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
			tc.mutate(t, runtime, &request)
			state, err := runtime.start(ctx, request)
			if !errors.Is(err, errP3eDenied) {
				t.Fatalf("start unsafe %s error = %v, want %v", tc.name, err, errP3eDenied)
			}
			if state.AdapterState != p3eAdapterStateWaitingForHuman || len(state.Events) != 0 || state.Result != nil {
				t.Fatalf("unsafe %s public state = %+v, want fail closed", tc.name, state)
			}
			assertP3ENoRawPublicIR(t, state, root)
			assertP3EFakeNotInvoked(t, invocationPath)
		})
	}
}

func TestP3EUnknownTranscriptFailsClosedAndCleansUp(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root := newP3EFakeRuntime(t, "unknown", time.Now)
	if _, err := runtime.start(ctx, request); err != nil {
		t.Fatalf("start controlled fake adapter: %v", err)
	}
	state := waitP3EState(t, runtime, request.RequestID, p3eAdapterStateWaitingForHuman)
	if len(state.Events) != 0 || state.Result != nil || state.VerificationState != "not_run" {
		t.Fatalf("unknown transcript public state = %+v, want bounded fail closed", state)
	}
	assertP3ENoRawPublicIR(t, state, root)
	assertP3EFakeInvoked(t, invocationPath)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}

func TestP3EMaterializationRejectsTraversalAndTOCTOUWithoutEscapingRoot(t *testing.T) {
	ctx := context.Background()
	t.Run("source traversal", func(t *testing.T) {
		runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
		request.Materialization.Files[0].Path = "../outside.txt"
		if _, err := runtime.start(ctx, request); !errors.Is(err, errP3eDenied) {
			t.Fatalf("start source traversal error = %v, want %v", err, errP3eDenied)
		}
		assertP3EFakeNotInvoked(t, invocationPath)
		if _, err := os.Stat(filepath.Join(filepath.Dir(root), "outside.txt")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("source traversal created outside root: %v", err)
		}
	})
	t.Run("materialization directory replacement", func(t *testing.T) {
		runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
		outside := t.TempDir()
		runtime.beforeMaterializationWrite = func(materializationPath string) {
			if err := os.RemoveAll(materializationPath); err != nil {
				t.Fatalf("remove anchored materialization directory: %v", err)
			}
			if err := os.Symlink(outside, materializationPath); err != nil {
				t.Fatalf("replace materialization directory: %v", err)
			}
		}
		if _, err := runtime.start(ctx, request); !errors.Is(err, errP3eDenied) {
			t.Fatalf("start TOCTOU materialization error = %v, want %v", err, errP3eDenied)
		}
		assertP3EFakeNotInvoked(t, invocationPath)
		entries, err := os.ReadDir(outside)
		if err != nil {
			t.Fatalf("read outside directory: %v", err)
		}
		if len(entries) != 0 {
			t.Fatalf("TOCTOU wrote outside trusted root: %v", entries)
		}
		if _, err := os.Lstat(filepath.Join(root, request.Materialization.ID)); err != nil {
			t.Fatalf("inspect rejected materialization replacement: %v", err)
		}
	})
}

func TestP3ESealedTupleRejectsRecomputedSourceBypass(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
	request.Materialization.Files[0].Contents = []byte("recomputed caller source\n")
	request.Materialization.Sealed.PayloadHash = hashP3eMaterializationFiles(request.Materialization.Files)
	request.Materialization.Sealed.SealFingerprint = p3eSealFingerprint(request.Materialization.Sealed)

	state, err := runtime.start(ctx, request)
	if !errors.Is(err, errP3eDenied) {
		t.Fatalf("start recomputed source bypass error = %v, want %v", err, errP3eDenied)
	}
	if !equalP3ePublicStates(state, p3eFailClosedState()) {
		t.Fatalf("recomputed source bypass state = %+v, want fail closed", state)
	}
	assertP3EFakeNotInvoked(t, invocationPath)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}

func TestP3ERejectsPostSealReplacementBeforeFakeExecution(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
	outside := t.TempDir()
	runtime.beforeAdapterStart = func(materializationPath string) {
		if err := os.Chmod(materializationPath, 0o700); err != nil {
			t.Fatalf("make sealed materialization replaceable: %v", err)
		}
		if err := os.RemoveAll(materializationPath); err != nil {
			t.Fatalf("remove sealed materialization: %v", err)
		}
		if err := os.Symlink(outside, materializationPath); err != nil {
			t.Fatalf("replace sealed materialization: %v", err)
		}
	}

	state, err := runtime.start(ctx, request)
	assertP3EStartFailure(t, err, p3eStartStageDescriptorValidation)
	if !equalP3ePublicStates(state, p3eFailClosedState()) {
		t.Fatalf("post-seal replacement state = %+v, want fail closed", state)
	}
	assertP3EFakeNotInvoked(t, invocationPath)
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("read replacement target: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("post-seal replacement wrote outside root: %v", entries)
	}
	if info, err := os.Lstat(filepath.Join(root, request.Materialization.ID)); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("post-seal replacement cleanup touched foreign path: info=%v err=%v", info, err)
	}
}

func TestP3EFakeStartFailureStageRemainsSanitized(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
	cause := errors.New("injected fake start failure")
	runtime.adapter = p3eFailingAdapter{cause: cause}

	state, err := runtime.start(ctx, request)
	failure := assertP3EStartFailure(t, err, p3eStartStageFakeStart)
	if failure.cause != cause {
		t.Fatalf("fake-start cause = %v, want %v", failure.cause, cause)
	}
	if !equalP3ePublicStates(state, p3eFailClosedState()) {
		t.Fatalf("fake-start failure state = %+v, want fail closed", state)
	}
	assertP3EFakeNotInvoked(t, invocationPath)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}

func TestP3EMaterializationCleansPartialTreeAndPreservesDuplicatePath(t *testing.T) {
	ctx := context.Background()
	t.Run("duplicate materialization path survives", func(t *testing.T) {
		runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
		duplicatePath := filepath.Join(root, request.Materialization.ID)
		if err := os.Mkdir(duplicatePath, 0o700); err != nil {
			t.Fatalf("create duplicate materialization: %v", err)
		}
		if err := os.WriteFile(filepath.Join(duplicatePath, "foreign"), []byte("keep"), 0o600); err != nil {
			t.Fatalf("seed duplicate materialization: %v", err)
		}

		if _, err := runtime.start(ctx, request); !errors.Is(err, errP3eDenied) {
			t.Fatalf("start duplicate materialization error = %v, want %v", err, errP3eDenied)
		}
		assertP3EFakeNotInvoked(t, invocationPath)
		contents, err := os.ReadFile(filepath.Join(duplicatePath, "foreign"))
		if err != nil || string(contents) != "keep" {
			t.Fatalf("duplicate materialization cleanup changed foreign tree: contents=%q err=%v", contents, err)
		}
	})
	t.Run("partial materialization is removed", func(t *testing.T) {
		runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
		runtime.afterMaterializationWrite = func(_ string, index int) error {
			if index == 0 {
				return errors.New("injected partial materialization failure")
			}
			return nil
		}

		if _, err := runtime.start(ctx, request); !errors.Is(err, errP3eDenied) {
			t.Fatalf("start partial materialization error = %v, want %v", err, errP3eDenied)
		}
		assertP3EFakeNotInvoked(t, invocationPath)
		assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
	})
}

func TestP3ECancellationRecoveryBeforeTerminalFailsClosed(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root := newP3EFakeRuntime(t, "hold", time.Now)
	if _, err := runtime.start(ctx, request); err != nil {
		t.Fatalf("start controlled fake adapter: %v", err)
	}
	waitP3EEventCount(t, runtime, request.RequestID, 1)
	cancelCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- runtime.cancel(cancelCtx, request.RequestID) }()
	waitP3EState(t, runtime, request.RequestID, p3eAdapterStateCancelRequested)

	recovery, err := runtime.recover(ctx, request)
	if err != nil {
		t.Fatalf("recover cancellation-requested adapter: %v", err)
	}
	if recovery.Action != p3eRecoveryRetryBoundedCancellation || !equalP3ePublicStates(recovery.State, p3eFailClosedState()) {
		t.Fatalf("cancellation recovery = %+v, want exact bounded cancellation and empty fail-closed state", recovery)
	}
	if err := <-cancelDone; err != nil {
		t.Fatalf("cancel held fake adapter: %v", err)
	}
	state := waitP3EState(t, runtime, request.RequestID, p3eAdapterStateWaitingForHuman)
	if !equalP3ePublicStates(state, p3eFailClosedState()) {
		t.Fatalf("cancelled final state = %+v, want fail closed", state)
	}
	assertP3EFakeInvoked(t, invocationPath)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}

func TestP3EDuplicateTranscriptMembersFailClosed(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root := newP3EFakeRuntime(t, "duplicate", time.Now)
	if _, err := runtime.start(ctx, request); err != nil {
		t.Fatalf("start duplicate-member fake adapter: %v", err)
	}
	state := waitP3EState(t, runtime, request.RequestID, p3eAdapterStateWaitingForHuman)
	if !equalP3ePublicStates(state, p3eFailClosedState()) {
		t.Fatalf("duplicate-member state = %+v, want fail closed", state)
	}
	assertP3EFakeInvoked(t, invocationPath)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}

func TestP3EPreAdmissionReclaimFromSecondHandlePreventsFakeExecution(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root, reclaimer := newP3EFakeRuntimeWithSecondStore(t, "complete", time.Now)
	finalBoundaryPassed := false
	runtime.afterFinalLaunchBoundaryValidation = func() {
		finalBoundaryPassed = true
		if _, err := reclaimer.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
			ExpectedFence: request.Fence,
			Claim: store.LaunchClaimRequest{
				LaunchSpecHash: request.LaunchSpecHash,
				ClaimID:        "claim_p3e_pre_admission_reclaimed",
				ClaimTokenHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				OwnerID:        "p3e_runtime",
				Attempt:        2,
			},
		}); err != nil {
			t.Fatalf("reclaim launch fence before admission transaction: %v", err)
		}
	}

	state, err := runtime.start(ctx, request)
	if !finalBoundaryPassed {
		t.Fatal("test did not reach the final launch-boundary validation")
	}
	assertP3EStartFailure(t, err, p3eStartStageFenceBoundaryValidation)
	if !equalP3ePublicStates(state, p3eFailClosedState()) {
		t.Fatalf("pre-admission reclaim state = %+v, want fail closed", state)
	}
	assertP3EFakeNotInvoked(t, invocationPath)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}

func TestP3ECrossHandleAdmissionBusyBeforeFakeStartThenReclaims(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	runtime, request, invocationPath, root, reclaimer := newP3EFakeRuntimeWithSecondStore(t, "complete", time.Now)
	admissionValidated := make(chan struct{})
	releaseFakeStart := make(chan struct{})
	runtime.afterFenceAdmissionValidation = func() {
		close(admissionValidated)
		<-releaseFakeStart
	}
	execAdapter, ok := runtime.adapter.(p3eExecAdapter)
	if !ok {
		t.Fatalf("runtime adapter = %T, want p3eExecAdapter", runtime.adapter)
	}
	fakeInvocationObserved := make(chan struct{})
	releaseAdapterReturn := make(chan struct{})
	defer func() {
		cancel()
		select {
		case <-releaseFakeStart:
		default:
			close(releaseFakeStart)
		}
		select {
		case <-releaseAdapterReturn:
		default:
			close(releaseAdapterReturn)
		}
	}()
	runtime.adapter = p3eFakeStartGateAdapter{
		adapter: execAdapter,
		waitForInvocation: func(ctx context.Context) error {
			return waitP3EFakeInvocation(ctx, invocationPath)
		},
		invocationObserved:   fakeInvocationObserved,
		releaseAdapterReturn: releaseAdapterReturn,
	}
	reclaimRequest := store.LaunchClaimReclaimRequest{
		ExpectedFence: request.Fence,
		Claim: store.LaunchClaimRequest{
			LaunchSpecHash: request.LaunchSpecHash,
			ClaimID:        "claim_p3e_cross_handle_reclaimed",
			ClaimTokenHash: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			OwnerID:        "p3e_runtime",
			Attempt:        2,
		},
	}
	type startResult struct {
		state p3ePublicState
		err   error
	}
	startDone := make(chan startResult, 1)
	go func() {
		state, err := runtime.start(ctx, request)
		startDone <- startResult{state: state, err: err}
	}()
	select {
	case <-admissionValidated:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for final in-transaction validation: %v", ctx.Err())
	}

	// Configure and retain the exact single connection until ReclaimLaunchClaim
	// is queued behind it. Closing that connection hands the zero-timeout busy
	// handler to the queued real reclaim instead of relying on a pool race.
	db := reclaimer.DB()
	contentionConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("pin second-handle SQLite connection: %v", err)
	}
	defer func() { _ = contentionConn.Close() }()
	if _, err := contentionConn.ExecContext(ctx, `PRAGMA busy_timeout = 0`); err != nil {
		t.Fatalf("set pinned second-handle SQLite busy timeout: %v", err)
	}
	waitCount := db.Stats().WaitCount
	reclaimDone := make(chan error, 1)
	go func() {
		_, err := reclaimer.ReclaimLaunchClaim(ctx, reclaimRequest)
		reclaimDone <- err
	}()
	waitP3EWaitingForSQLiteConnection(t, ctx, db, waitCount)
	if err := contentionConn.Close(); err != nil {
		t.Fatalf("release pinned second-handle SQLite connection: %v", err)
	}
	select {
	case err = <-reclaimDone:
	case <-ctx.Done():
		t.Fatalf("timed out waiting for second-handle reclaim contention: %v", ctx.Err())
	}
	if err == nil {
		t.Fatal("second-handle reclaim succeeded while final admission held")
	}
	if !strings.Contains(err.Error(), "SQLITE_BUSY") && !strings.Contains(strings.ToLower(err.Error()), "database is locked") {
		t.Fatalf("second-handle reclaim error = %v, want SQLITE_BUSY or database-is-locked semantics", err)
	}
	blockedRuntime, blockedInvocationPath, blockedRoot := newP3EFakeRuntimeForRequest(t, reclaimer, "complete", time.Now, request)
	blockedState, blockedErr := blockedRuntime.start(ctx, request)
	assertP3EStartFailure(t, blockedErr, p3eStartStageSQLiteAdmission)
	if !equalP3ePublicStates(blockedState, p3eFailClosedState()) {
		t.Fatalf("blocked SQLite admission state = %+v, want fail closed", blockedState)
	}
	assertP3EFakeNotInvoked(t, blockedInvocationPath)
	assertP3EMaterializationCleaned(t, blockedRoot, request.Materialization.ID)
	assertP3EFakeNotInvoked(t, invocationPath)

	close(releaseFakeStart)
	select {
	case <-fakeInvocationObserved:
	case result := <-startDone:
		var failure *p3eStartFailure
		if errors.As(result.err, &failure) {
			t.Fatalf("start returned before fake invocation: stage=%s cause=%v state=%+v", failure.stage, failure.cause, result.state)
		}
		t.Fatalf("start returned before fake invocation: state=%+v err=%v", result.state, result.err)
	case <-ctx.Done():
		t.Fatalf("timed out waiting for fake invocation: %v", ctx.Err())
	}
	assertP3EFakeInvoked(t, invocationPath)
	close(releaseAdapterReturn)
	select {
	case result := <-startDone:
		if result.err != nil {
			t.Fatalf("start after final fence admission: %v", result.err)
		}
		if result.state.AdapterState != p3eAdapterStateMonitoring || result.state.Result != nil {
			t.Fatalf("initial public state = %+v, want monitoring without result", result.state)
		}
	case <-ctx.Done():
		t.Fatalf("timed out releasing fake start: %v", ctx.Err())
	}
	reclaimed, err := reclaimer.ReclaimLaunchClaim(ctx, reclaimRequest)
	if err != nil {
		t.Fatalf("second-handle reclaim after admission rollback: %v", err)
	}
	if reclaimed.FenceGeneration != request.Fence.FenceGeneration+1 {
		t.Fatalf("reclaimed fence generation = %d, want %d", reclaimed.FenceGeneration, request.Fence.FenceGeneration+1)
	}
	active, err := reclaimer.GetLaunchClaim(ctx, request.LaunchSpecHash)
	if err != nil {
		t.Fatalf("load reclaimed active fence: %v", err)
	}
	if active.Fence != reclaimed.Fence {
		t.Fatalf("active reclaimed fence = %+v, want %+v", active.Fence, reclaimed.Fence)
	}
	waitP3EState(t, runtime, request.RequestID, p3eAdapterStateCompleted)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}

func TestP3EStaleFenceAfterValidationPreventsFakeExecution(t *testing.T) {
	ctx := context.Background()
	runtime, request, invocationPath, root := newP3EFakeRuntime(t, "complete", time.Now)
	runtime.beforeAdapterStart = func(_ string) {
		if _, err := runtime.fence.ReclaimLaunchClaim(ctx, store.LaunchClaimReclaimRequest{
			ExpectedFence: request.Fence,
			Claim: store.LaunchClaimRequest{
				LaunchSpecHash: request.LaunchSpecHash,
				ClaimID:        "claim_p3e_late_reclaimed",
				ClaimTokenHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				OwnerID:        "p3e_runtime",
				Attempt:        2,
			},
		}); err != nil {
			t.Fatalf("reclaim launch fence after validation: %v", err)
		}
	}

	state, err := runtime.start(ctx, request)
	if !errors.Is(err, errP3eDenied) {
		t.Fatalf("start late stale fence error = %v, want %v", err, errP3eDenied)
	}
	if !equalP3ePublicStates(state, p3eFailClosedState()) {
		t.Fatalf("late stale fence state = %+v, want fail closed", state)
	}
	assertP3EFakeNotInvoked(t, invocationPath)
	assertP3EMaterializationCleaned(t, root, request.Materialization.ID)
}
func newP3EFakeRuntime(t *testing.T, mode string, now func() time.Time) (*ompReadOnlyRuntime, p3eAdapterRequest, string, string) {
	t.Helper()
	_, journal := newP3CTestOrchestration(t)
	return newP3EFakeRuntimeWithJournal(t, journal, mode, now)
}

func newP3EFakeRuntimeWithSecondStore(t *testing.T, mode string, now func() time.Time) (*ompReadOnlyRuntime, p3eAdapterRequest, string, string, *store.Store) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "p3e.sqlite")
	journal, err := store.Open(databasePath)
	if err != nil {
		t.Fatalf("open P3e journal: %v", err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	seedP3aApprovedRevision(t, journal)
	runtime, request, invocationPath, root := newP3EFakeRuntimeWithJournal(t, journal, mode, now)
	reclaimer, err := store.Open(databasePath)
	if err != nil {
		t.Fatalf("open second P3e journal handle: %v", err)
	}
	t.Cleanup(func() { _ = reclaimer.Close() })
	return runtime, request, invocationPath, root, reclaimer
}

func newP3EFakeRuntimeWithJournal(t *testing.T, journal *store.Store, mode string, now func() time.Time) (*ompReadOnlyRuntime, p3eAdapterRequest, string, string) {
	t.Helper()
	orchestration := newFencedLaunchOrchestrator(journal)
	admission := p3aAdmissionRequest()
	claim := p3cClaimRequest(admission.LaunchSpecHash)
	action, err := orchestration.admit(context.Background(), admission, claim)
	if err != nil {
		t.Fatalf("admit P3e fence: %v", err)
	}
	action, err = orchestration.recordTrustedMaterializationReady(context.Background(), p3aMaterializationRequest(action.Boundary.Claim.Fence))
	if err != nil {
		t.Fatalf("record P3e materialization: %v", err)
	}
	action, err = orchestration.admitRunIntent(context.Background(), store.LaunchRunIntentRequest{
		Fence: action.Boundary.Claim.Fence, MaterializationID: "materialization_p3a_001", RunID: "run_p3a_001", Attempt: 1,
	})
	if err != nil {
		t.Fatalf("admit P3e run intent: %v", err)
	}
	if action.Boundary.Action != store.LaunchRecoveryRetryProcessAdmission {
		t.Fatalf("P3e fence action = %q, want process admission", action.Boundary.Action)
	}
	files := []p3eMaterializationFile{{Path: "fixture.txt", Contents: []byte("controlled fake source\n")}}
	request := p3eAdapterRequest{
		RequestID:      "omp_audit_request_p3e_001",
		LaunchSpecHash: admission.LaunchSpecHash,
		Fence:          action.Boundary.Claim.Fence,
		HostSpec:       canonicalP3eHostSpec(),
		Materialization: p3eMaterialization{
			ID:     "materialization_p3a_001",
			Sealed: canonicalP3eSealedMaterialization(),
			Files:  files,
		},
	}
	runtime, invocationPath, root := newP3EFakeRuntimeForRequest(t, journal, mode, now, request)
	return runtime, request, invocationPath, root
}

func newP3EFakeRuntimeForRequest(t *testing.T, journal *store.Store, mode string, now func() time.Time, request p3eAdapterRequest) (*ompReadOnlyRuntime, string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "trusted-root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create fake trusted root: %v", err)
	}
	invocationPath := filepath.Join(t.TempDir(), "fake-adapter-invocations")
	runtime, err := newOMPReadOnlyRuntime(journal, p3eExecAdapter{
		executable: os.Args[0],
		args:       []string{"-test.run=^TestP3EFakeAdapterExecutable$"},
		env: []string{
			"ANANKE_P3E_FAKE_ADAPTER=1",
			"ANANKE_P3E_FAKE_MODE=" + mode,
			"ANANKE_P3E_FAKE_INVOCATIONS=" + invocationPath,
		},
	}, root, newP3eSealedSource(request.Materialization.Files), now)
	if err != nil {
		t.Fatalf("new controlled fake runtime: %v", err)
	}
	return runtime, invocationPath, root
}

func waitP3EState(t *testing.T, runtime *ompReadOnlyRuntime, requestID string, want p3eAdapterState) p3ePublicState {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		state, found := runtime.status(requestID)
		if found && state.AdapterState == want {
			return state
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for P3e state %q; latest = %+v, found=%v", want, state, found)
		case <-ticker.C:
		}
	}
}

func waitP3EEventCount(t *testing.T, runtime *ompReadOnlyRuntime, requestID string, want int) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		state, found := runtime.status(requestID)
		if found && len(state.Events) == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for P3e event count %d; latest = %+v, found=%v", want, state, found)
		case <-ticker.C:
		}
	}
}

func assertP3ENoRawPublicIR(t *testing.T, state p3ePublicState, root string) {
	t.Helper()
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal public state: %v", err)
	}
	for _, forbidden := range []string{"raw", "prompt", "command", "token", "socket", root, "controlled fake source"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public state leaks %q: %s", forbidden, encoded)
		}
	}
}

func assertP3EFakeInvoked(t *testing.T, invocationPath string) {
	t.Helper()
	contents, err := os.ReadFile(invocationPath)
	if err != nil {
		t.Fatalf("read fake invocation record: %v", err)
	}
	if string(contents) != "fake-adapter\n" {
		t.Fatalf("fake invocation record = %q, want controlled fake adapter", contents)
	}
}

func assertP3EFakeNotInvoked(t *testing.T, invocationPath string) {
	t.Helper()
	if _, err := os.Stat(invocationPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe request invoked adapter: %v", err)
	}
}

func assertP3EMaterializationCleaned(t *testing.T, root, materializationID string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(root, materializationID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("materialization cleanup = %v, want removed sealed root", err)
	}
}

func assertP3EStartFailure(t *testing.T, err error, want p3eStartStage) *p3eStartFailure {
	t.Helper()
	if err == nil {
		t.Fatalf("start error = nil, want denied %s", want)
	}
	if !errors.Is(err, errP3eDenied) {
		t.Fatalf("start error = %v, want %v", err, errP3eDenied)
	}
	if err.Error() != errP3eDenied.Error() {
		t.Fatalf("start error text = %q, want sanitized %q", err, errP3eDenied)
	}
	var failure *p3eStartFailure
	if !errors.As(err, &failure) {
		t.Fatalf("start error = %v, want private staged failure", err)
	}
	if failure.stage != want {
		t.Fatalf("start failure stage = %s, want %s", failure.stage, want)
	}
	if failure.cause == nil {
		t.Fatalf("start failure stage %s lost its private cause", want)
	}
	return failure
}

func equalP3eEvents(left, right []p3eAuditEvent) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalP3ePublicStates(left, right p3ePublicState) bool {
	if left.AdapterState != right.AdapterState || left.VerificationState != right.VerificationState || !equalP3eEvents(left.Events, right.Events) {
		return false
	}
	if left.Result == nil || right.Result == nil {
		return left.Result == nil && right.Result == nil
	}
	return *left.Result == *right.Result

}
