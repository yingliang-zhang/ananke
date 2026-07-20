package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

func TestSupervisorConsumesCancellationRequestedWhileCreated(t *testing.T) {
	const runID = "cancel-created-startup"
	exited := make(chan struct{})
	close(exited)
	backend := &failClosedBackend{
		launchPID: 61002,
		exited:    exited,
		groupResults: []groupResult{
			{members: []int{61002}},
			{},
		},
		reapCode: 0,
	}
	s, st, cfg := newFailClosedSupervisor(t, runID, backend)
	cfg.CleanupRetryMin = time.Millisecond
	cfg.CleanupRetryMax = 2 * time.Millisecond
	s.cfg = cfg

	state, err := st.RequestCancellation(context.Background(), runID)
	if err != nil {
		t.Fatalf("RequestCancellation while created: %v", err)
	}
	if state != store.StateCreated {
		t.Fatalf("accepted state = %q, want created", state)
	}

	terminal, err := s.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if terminal != store.StateCancelled {
		t.Fatalf("terminal = %q, want cancelled", terminal)
	}
	run, err := st.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.State != store.StateCancelled || !run.CancelRequested {
		t.Fatalf("durable run = %+v, want cancelled with retained intent", run)
	}
	outbox, err := st.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if outbox.TerminalState != store.StateCancelled || outbox.Acknowledged != 1 {
		t.Fatalf("outbox = %+v, want acknowledged cancelled finalization", outbox)
	}
	backend.mu.Lock()
	groupSignals := append([]unix.Signal(nil), backend.groupSignals...)
	backend.mu.Unlock()
	if len(groupSignals) != 1 || groupSignals[0] != unix.SIGTERM {
		t.Fatalf("startup cancellation group signals = %v, want one SIGTERM", groupSignals)
	}
}

func TestSupervisorTerminalDecisionHonorsDurableCancellation(t *testing.T) {
	const runID = "cancel-natural-exit-race"
	storePath := seedRunStore(t, runID)
	st, err := store.Open(storePath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Transition(ctx, runID, store.StateRunning, "launched"); err != nil {
		t.Fatalf("Transition running: %v", err)
	}
	if _, err := st.RequestCancellation(ctx, runID); err != nil {
		t.Fatalf("RequestCancellation: %v", err)
	}

	s := &Supervisor{cfg: Config{RunID: runID}, store: st}
	terminal, _ := s.decideTerminal(0)
	if terminal != store.StateCancelled {
		t.Fatalf("zero-exit terminal = %q, want cancelled after accepted intent", terminal)
	}
}
