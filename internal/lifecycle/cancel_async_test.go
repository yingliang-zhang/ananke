package lifecycle

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

const cancelAsyncTestToken = "cancel-async-supervisor-token"

func newCancelAsyncFixture(t *testing.T, socketPath string) (*Engine, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "store.sqlite"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	if err := st.CreateProject(ctx, "cancel-project", "cancel", "/tmp"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := st.CreateWorkstream(ctx, "cancel-workstream", "cancel-project", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}
	runID := "cancel-run-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := st.CreateRun(ctx, runID, "cancel-project", "cancel-workstream", store.RunSpec{
		WorkerPath: "/bin/true",
		SocketPath: socketPath,
		Token:      cancelAsyncTestToken,
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := st.Transition(ctx, runID, store.StateRunning, "launched"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}

	return &Engine{
		store:            st,
		active:           make(map[string]*runHandle),
		tails:            make(map[string]*transcriptTail),
		cleanupRequested: make(map[string]struct{}),
	}, runID
}

func startBlockingCancelSocket(t *testing.T, responseGate <-chan struct{}) (string, <-chan map[string]string, <-chan struct{}) {
	t.Helper()
	socketPath := filepath.Join("/tmp", "ananke-cancel-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	requests := make(chan map[string]string, 32)
	responses := make(chan struct{}, 32)
	stop := make(chan struct{})
	acceptDone := make(chan struct{})
	var handlers sync.WaitGroup
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer conn.Close()
				var req map[string]string
				if err := json.NewDecoder(conn).Decode(&req); err != nil {
					return
				}
				select {
				case requests <- req:
				case <-stop:
					return
				}
				select {
				case <-responseGate:
				case <-stop:
					return
				}
				if json.NewEncoder(conn).Encode(map[string]any{"ok": true, "state": "cancelling"}) == nil {
					responses <- struct{}{}
				}
			}()
		}
	}()

	t.Cleanup(func() {
		close(stop)
		_ = listener.Close()
		<-acceptDone
		handlers.Wait()
		_ = os.Remove(socketPath)
	})
	return socketPath, requests, responses
}

func startRetryCancelSocket(t *testing.T) (string, <-chan map[string]string, <-chan struct{}) {
	t.Helper()
	socketPath := filepath.Join("/tmp", "ananke-cancel-retry-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	requests := make(chan map[string]string, 2)
	success := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for attempt := 0; attempt < 2; attempt++ {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			var req map[string]string
			if err := json.NewDecoder(conn).Decode(&req); err != nil {
				_ = conn.Close()
				return
			}
			requests <- req
			response := map[string]any{"ok": false, "error": "retryable failure"}
			if attempt == 1 {
				response = map[string]any{"ok": true, "state": "cancelling"}
			}
			if err := json.NewEncoder(conn).Encode(response); err != nil {
				_ = conn.Close()
				return
			}
			_ = conn.Close()
			if attempt == 1 {
				success <- struct{}{}
			}
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("retry cancel socket did not stop")
		}
		_ = os.Remove(socketPath)
	})
	return socketPath, requests, success
}

func closeCancelGate(gate chan struct{}) {
	select {
	case <-gate:
	default:
		close(gate)
	}
}

func requireImmediateCancelAcceptance(t *testing.T, response apiResponse) {
	t.Helper()
	if !response.OK || !response.Accepted || response.State != "cancelling" {
		t.Fatalf("cancel response = %+v, want accepted cancelling", response)
	}
}

func TestEngineCancelRunReturnsBeforeSupervisorResponse(t *testing.T) {
	responseGate := make(chan struct{})
	defer closeCancelGate(responseGate)
	socketPath, requests, responses := startBlockingCancelSocket(t, responseGate)
	engine, runID := newCancelAsyncFixture(t, socketPath)

	responseCh := make(chan apiResponse, 1)
	started := time.Now()
	go func() {
		responseCh <- engine.handleCancelRun(context.Background(), &apiRequest{ID: runID})
	}()

	var response apiResponse
	select {
	case response = <-responseCh:
		if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
			t.Fatalf("cancel returned after %v, want <500ms", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		closeCancelGate(responseGate)
		<-responseCh
		t.Fatal("cancel waited for the blocked supervisor response")
	}
	requireImmediateCancelAcceptance(t, response)

	select {
	case request := <-requests:
		if request["cmd"] != "cancel" || request["token"] != cancelAsyncTestToken {
			t.Fatalf("supervisor request = %v, want exact authenticated cancel", request)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not receive background cancel request")
	}

	closeCancelGate(responseGate)
	select {
	case <-responses:
	case <-time.After(time.Second):
		t.Fatal("background cancel did not consume released supervisor response")
	}
}

func TestEngineCancelRunUnreachableSupervisorAccepted(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "missing.sock")
	engine, runID := newCancelAsyncFixture(t, socketPath)

	started := time.Now()
	response := engine.handleCancelRun(context.Background(), &apiRequest{ID: runID})
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("cancel returned after %v, want <500ms", elapsed)
	}
	requireImmediateCancelAcceptance(t, response)
}

func TestEngineCancelRunFailedBackgroundRequestAllowsRetry(t *testing.T) {
	socketPath, requests, success := startRetryCancelSocket(t)
	engine, runID := newCancelAsyncFixture(t, socketPath)

	requireImmediateCancelAcceptance(t, engine.handleCancelRun(context.Background(), &apiRequest{ID: runID}))
	select {
	case request := <-requests:
		if request["cmd"] != "cancel" || request["token"] != cancelAsyncTestToken {
			t.Fatalf("first supervisor request = %v, want exact authenticated cancel", request)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not receive first background cancel request")
	}
	waitUntil(t, "failed cleanup marker cleared", time.Second, func() bool {
		engine.mu.Lock()
		_, requested := engine.cleanupRequested[runID]
		engine.mu.Unlock()
		return !requested
	})

	requireImmediateCancelAcceptance(t, engine.handleCancelRun(context.Background(), &apiRequest{ID: runID}))
	select {
	case request := <-requests:
		if request["cmd"] != "cancel" || request["token"] != cancelAsyncTestToken {
			t.Fatalf("retry supervisor request = %v, want exact authenticated cancel", request)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not receive retried background cancel request")
	}
	select {
	case <-success:
	case <-time.After(time.Second):
		t.Fatal("retried background cancel did not complete successfully")
	}
	waitUntil(t, "successful cleanup marker retained", time.Second, func() bool {
		engine.mu.Lock()
		_, requested := engine.cleanupRequested[runID]
		engine.mu.Unlock()
		return requested
	})
}

func TestEngineCancelRunConcurrentDuplicatesSendOnce(t *testing.T) {
	responseGate := make(chan struct{})
	defer closeCancelGate(responseGate)
	socketPath, requests, responses := startBlockingCancelSocket(t, responseGate)
	engine, runID := newCancelAsyncFixture(t, socketPath)

	const callers = 16
	start := make(chan struct{})
	results := make(chan apiResponse, callers)
	for range callers {
		go func() {
			<-start
			results <- engine.handleCancelRun(context.Background(), &apiRequest{ID: runID})
		}()
	}
	close(start)
	for range callers {
		select {
		case response := <-results:
			requireImmediateCancelAcceptance(t, response)
		case <-time.After(500 * time.Millisecond):
			t.Fatal("concurrent cancel call did not return immediately")
		}
	}
	run, err := engine.store.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun after duplicate cancellation: %v", err)
	}
	if !run.CancelRequested || run.State != store.StateCancelling {
		t.Fatalf("run = %+v, want one durable cancelling intent", run)
	}
	transitions, err := engine.store.Transitions(context.Background(), runID)
	if err != nil {
		t.Fatalf("Transitions: %v", err)
	}
	cancellingTransitions := 0
	for _, transition := range transitions {
		if transition.ToState == store.StateCancelling {
			cancellingTransitions++
		}
	}
	if cancellingTransitions != 1 {
		t.Fatalf("cancelling transitions = %d, want 1", cancellingTransitions)
	}

	select {
	case request := <-requests:
		if request["cmd"] != "cancel" || request["token"] != cancelAsyncTestToken {
			t.Fatalf("supervisor request = %v, want exact authenticated cancel", request)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not receive background cancel request")
	}
	select {
	case request := <-requests:
		t.Fatalf("duplicate supervisor cancel request = %v", request)
	case <-time.After(200 * time.Millisecond):
	}

	closeCancelGate(responseGate)
	select {
	case <-responses:
	case <-time.After(time.Second):
		t.Fatal("background cancel did not finish after response release")
	}
}

func TestEngineCancelRunRejectsMissingAndTerminalWithoutRequest(t *testing.T) {
	responseGate := make(chan struct{})
	defer closeCancelGate(responseGate)
	socketPath, requests, _ := startBlockingCancelSocket(t, responseGate)
	engine, runID := newCancelAsyncFixture(t, socketPath)

	ctx := context.Background()
	run, err := engine.store.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if err := engine.store.CommitTerminal(ctx, runID, store.StateCancelled, "cancelled", store.OutboxRow{
		RunID:         runID,
		TerminalState: store.StateCancelled,
		SocketPath:    run.SocketPath,
		Token:         run.Token,
	}); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}

	missing := engine.handleCancelRun(ctx, &apiRequest{ID: "missing-run"})
	if missing.OK {
		t.Fatalf("missing run cancel response = %+v, want rejection", missing)
	}
	terminal := engine.handleCancelRun(ctx, &apiRequest{ID: runID})
	if terminal.OK {
		t.Fatalf("terminal run cancel response = %+v, want rejection", terminal)
	}

	select {
	case request := <-requests:
		t.Fatalf("rejected cancel scheduled supervisor request = %v", request)
	case <-time.After(200 * time.Millisecond):
	}
}
