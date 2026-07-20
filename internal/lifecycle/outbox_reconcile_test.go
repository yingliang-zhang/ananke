package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

func newPendingOutboxFixture(t *testing.T, terminal store.State) *recoveryFixture {
	t.Helper()
	fixture := newRecoveryFixture(t, store.StateRunning)
	if _, err := fixture.engine.store.DB().ExecContext(context.Background(),
		`UPDATE runs SET transcript_required = 0 WHERE id = ?`, fixture.run.ID); err != nil {
		t.Fatalf("disable transcript contract for process-authority fixture: %v", err)
	}
	var err error
	fixture.run, err = fixture.engine.store.GetRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("reload process-authority fixture: %v", err)
	}
	if err := fixture.engine.store.CommitTerminal(context.Background(), fixture.run.ID, terminal, "seed terminal outbox", store.OutboxRow{
		RunID:          fixture.run.ID,
		TerminalState:  terminal,
		SupervisorPID:  fixture.run.SupervisorPID,
		SupervisorPGID: fixture.run.SupervisorPGID,
		SocketPath:     fixture.run.SocketPath,
		Token:          fixture.run.Token,
	}); err != nil {
		t.Fatalf("CommitTerminal: %v", err)
	}
	run, err := fixture.engine.store.GetRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun terminal fixture: %v", err)
	}
	fixture.run = run
	writeRecoveryIdentityValues(t, run.IdentityPath, recoveryIdentityValues(run))
	return fixture
}

func reconcileOutbox(t *testing.T, engine *Engine) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := engine.reconcilePendingOutbox(ctx); err != nil {
		t.Fatalf("reconcilePendingOutbox: %v", err)
	}
}

func outboxAfterReconcile(t *testing.T, fixture *recoveryFixture) store.OutboxRow {
	t.Helper()
	row, err := fixture.engine.store.GetOutbox(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	return row
}

func assertNoReconciliationSignals(t *testing.T, backend *cleanupProbeBackend) int {
	t.Helper()
	groupCalls, signals := backend.snapshotCalls()
	if len(signals) != 0 {
		t.Fatalf("reconciliation signals = %v, want none", signals)
	}
	return groupCalls
}

func TestReconcilePendingOutboxAliveAuthenticatedTerminalMatch(t *testing.T) {
	fixture := newPendingOutboxFixture(t, store.StateCompleted)
	requests := startRecoverySocket(t, fixture.run.SocketPath, func(map[string]string) []byte {
		response := recoveryStatus(fixture.run)
		response["state"] = string(store.StateCompleted)
		return encodeRecoveryResponse(t, response)
	})

	reconcileOutbox(t, fixture.engine)

	request := receiveRecoveryRequest(t, requests)
	if request["cmd"] != "finalize" || request["token"] != fixture.run.Token {
		t.Fatalf("request = %v, want authenticated finalize", request)
	}
	row := outboxAfterReconcile(t, fixture)
	if row.Acknowledged != 1 {
		t.Fatalf("acknowledged = %d, want 1", row.Acknowledged)
	}
	if groupCalls := assertNoReconciliationSignals(t, fixture.backend); groupCalls != 0 {
		t.Fatalf("GroupMembers calls = %d, want 0 for live supervisor", groupCalls)
	}
}

func TestReconcilePendingOutboxAliveFailuresRemainPending(t *testing.T) {
	tests := []struct {
		name    string
		start   bool
		respond func(*testing.T, *recoveryFixture) []byte
	}{
		{name: "unreachable"},
		{name: "auth failed", start: true, respond: func(t *testing.T, _ *recoveryFixture) []byte {
			return encodeRecoveryResponse(t, map[string]any{"ok": false, "error": "auth failed"})
		}},
		{name: "malformed", start: true, respond: func(_ *testing.T, _ *recoveryFixture) []byte { return []byte("{broken") }},
		{name: "nonterminal", start: true, respond: func(t *testing.T, fixture *recoveryFixture) []byte {
			return encodeRecoveryResponse(t, recoveryStatus(fixture.run))
		}},
		{name: "terminal state mismatch", start: true, respond: func(t *testing.T, fixture *recoveryFixture) []byte {
			response := recoveryStatus(fixture.run)
			response["state"] = string(store.StateFailed)
			return encodeRecoveryResponse(t, response)
		}},
		{name: "run ID mismatch", start: true, respond: func(t *testing.T, fixture *recoveryFixture) []byte {
			response := recoveryStatus(fixture.run)
			response["state"] = string(store.StateCompleted)
			response["run_id"] = "other-run"
			return encodeRecoveryResponse(t, response)
		}},
		{name: "supervisor PID mismatch", start: true, respond: func(t *testing.T, fixture *recoveryFixture) []byte {
			response := recoveryStatus(fixture.run)
			response["state"] = string(store.StateCompleted)
			response["supervisor_pid"] = fixture.run.SupervisorPID + 1
			return encodeRecoveryResponse(t, response)
		}},
		{name: "worker PID mismatch", start: true, respond: func(t *testing.T, fixture *recoveryFixture) []byte {
			response := recoveryStatus(fixture.run)
			response["state"] = string(store.StateCompleted)
			response["worker_pid"] = fixture.run.WorkerPID + 1
			return encodeRecoveryResponse(t, response)
		}},
		{name: "PGID mismatch", start: true, respond: func(t *testing.T, fixture *recoveryFixture) []byte {
			response := recoveryStatus(fixture.run)
			response["state"] = string(store.StateCompleted)
			response["pgid"] = fixture.run.SupervisorPGID + 1
			return encodeRecoveryResponse(t, response)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPendingOutboxFixture(t, store.StateCompleted)
			var requests <-chan map[string]string
			if tc.start {
				requests = startRecoverySocket(t, fixture.run.SocketPath, func(map[string]string) []byte {
					return tc.respond(t, fixture)
				})
			}

			reconcileOutbox(t, fixture.engine)

			if requests != nil {
				if request := receiveRecoveryRequest(t, requests); request["cmd"] != "finalize" {
					t.Fatalf("request = %v, want finalize", request)
				}
			}
			if row := outboxAfterReconcile(t, fixture); row.Acknowledged != 0 {
				t.Fatalf("acknowledged = %d, want pending", row.Acknowledged)
			}
			if groupCalls := assertNoReconciliationSignals(t, fixture.backend); groupCalls != 0 {
				t.Fatalf("GroupMembers calls = %d, want 0", groupCalls)
			}
		})
	}
}

func TestReconcilePendingOutboxInvalidAuthorityRemainsPending(t *testing.T) {
	tests := []struct {
		name           string
		identity       func(*testing.T, *recoveryFixture, map[string]any)
		identityValues func(map[string]any)
		storeMutation  string
	}{
		{name: "missing identity", identity: func(t *testing.T, fixture *recoveryFixture, _ map[string]any) {
			if err := os.Remove(fixture.run.IdentityPath); err != nil {
				t.Fatalf("remove identity: %v", err)
			}
		}},
		{name: "malformed identity", identity: func(t *testing.T, fixture *recoveryFixture, _ map[string]any) {
			if err := os.WriteFile(fixture.run.IdentityPath, []byte("{broken"), 0o600); err != nil {
				t.Fatalf("write malformed identity: %v", err)
			}
		}},
		{name: "empty identity run ID", identityValues: func(values map[string]any) { values["run_id"] = "" }},
		{name: "mismatched identity run ID", identityValues: func(values map[string]any) { values["run_id"] = "other" }},
		{name: "empty identity supervisor PID", identityValues: func(values map[string]any) { values["supervisor_pid"] = 0 }},
		{name: "mismatched identity supervisor PID", identityValues: func(values map[string]any) { values["supervisor_pid"] = recoveryTestSupervisorPID + 1 }},
		{name: "empty identity supervisor PGID", identityValues: func(values map[string]any) { values["supervisor_pgid"] = 0 }},
		{name: "mismatched identity supervisor PGID", identityValues: func(values map[string]any) { values["supervisor_pgid"] = recoveryTestSupervisorPGID + 1 }},
		{name: "empty identity worker PID", identityValues: func(values map[string]any) { values["worker_pid"] = 0 }},
		{name: "mismatched identity worker PID", identityValues: func(values map[string]any) { values["worker_pid"] = recoveryTestWorkerPID + 1 }},
		{name: "empty identity socket", identityValues: func(values map[string]any) { values["socket_path"] = "" }},
		{name: "mismatched identity socket", identityValues: func(values map[string]any) { values["socket_path"] = "/tmp/other.sock" }},
		{name: "empty identity token", identityValues: func(values map[string]any) { values["token"] = "" }},
		{name: "mismatched identity token", identityValues: func(values map[string]any) { values["token"] = "other" }},
		{name: "empty identity transcript", identityValues: func(values map[string]any) { values["transcript_path"] = "" }},
		{name: "mismatched identity transcript", identityValues: func(values map[string]any) { values["transcript_path"] = "/tmp/other.ndjson" }},
		{name: "missing row supervisor PID", storeMutation: `UPDATE finalization_outbox SET supervisor_pid = NULL`},
		{name: "mismatched row supervisor PID", storeMutation: `UPDATE finalization_outbox SET supervisor_pid = supervisor_pid + 1`},
		{name: "missing row supervisor PGID", storeMutation: `UPDATE finalization_outbox SET supervisor_pgid = NULL`},
		{name: "mismatched row supervisor PGID", storeMutation: `UPDATE finalization_outbox SET supervisor_pgid = supervisor_pgid + 1`},
		{name: "missing row socket", storeMutation: `UPDATE finalization_outbox SET socket_path = NULL`},
		{name: "mismatched row socket", storeMutation: `UPDATE finalization_outbox SET socket_path = '/tmp/other.sock'`},
		{name: "missing row token", storeMutation: `UPDATE finalization_outbox SET token = NULL`},
		{name: "mismatched row token", storeMutation: `UPDATE finalization_outbox SET token = 'other'`},
		{name: "empty outbox terminal state", storeMutation: `UPDATE finalization_outbox SET terminal_state = ''`},
		{name: "nonterminal outbox state", storeMutation: `UPDATE finalization_outbox SET terminal_state = 'running'`},
		{name: "mismatched outbox terminal state", storeMutation: `UPDATE finalization_outbox SET terminal_state = 'failed'`},
		{name: "nonterminal durable run state", storeMutation: `UPDATE runs SET state = 'running'`},
		{name: "empty durable identity path", storeMutation: `UPDATE runs SET identity_path = ''`},
		{name: "empty durable transcript path", storeMutation: `UPDATE runs SET transcript_path = ''`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPendingOutboxFixture(t, store.StateCompleted)
			values := recoveryIdentityValues(fixture.run)
			if tc.identityValues != nil {
				tc.identityValues(values)
				writeRecoveryIdentityValues(t, fixture.run.IdentityPath, values)
			}
			if tc.identity != nil {
				tc.identity(t, fixture, values)
			}
			if tc.storeMutation != "" {
				if _, err := fixture.engine.store.DB().ExecContext(context.Background(), tc.storeMutation); err != nil {
					t.Fatalf("store authority mutation: %v", err)
				}
			}

			reconcileOutbox(t, fixture.engine)

			if row := outboxAfterReconcile(t, fixture); row.Acknowledged != 0 {
				t.Fatalf("acknowledged = %d, want pending", row.Acknowledged)
			}
			assertNoReconciliationSignals(t, fixture.backend)
		})
	}
}

func TestReconcilePendingOutboxDeadSupervisorSafetyGates(t *testing.T) {
	tests := []struct {
		name           string
		workerAlive    bool
		members        []int
		groupErr       error
		wantAck        int
		wantGroupCalls int
	}{
		{name: "live worker", workerAlive: true, wantAck: 0},
		{name: "nonempty group", members: []int{recoveryTestWorkerPID + 10}, wantAck: 0, wantGroupCalls: 1},
		{name: "unowned supervisor PID remains occupancy", members: []int{recoveryTestSupervisorPID}, wantAck: 0, wantGroupCalls: 1},
		{name: "group enumeration error", groupErr: errors.New("membership unavailable"), wantAck: 0, wantGroupCalls: 1},
		{name: "validated quiescence", wantAck: -1, wantGroupCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newPendingOutboxFixture(t, store.StateFailed)
			fixture.backend.alive[fixture.run.SupervisorPID] = false
			fixture.backend.alive[fixture.run.WorkerPID] = tc.workerAlive
			fixture.backend.members = tc.members
			fixture.backend.groupErr = tc.groupErr

			reconcileOutbox(t, fixture.engine)

			row := outboxAfterReconcile(t, fixture)
			if row.Acknowledged != tc.wantAck {
				t.Fatalf("acknowledged = %d, want %d", row.Acknowledged, tc.wantAck)
			}
			if tc.wantAck == -1 && !strings.Contains(row.Diagnostic, "supervisor confirmed dead") {
				t.Fatalf("abandon diagnostic = %q", row.Diagnostic)
			}
			if tc.wantAck == 0 && row.Diagnostic != "" {
				t.Fatalf("pending diagnostic = %q, want empty", row.Diagnostic)
			}
			if groupCalls := assertNoReconciliationSignals(t, fixture.backend); groupCalls != tc.wantGroupCalls {
				t.Fatalf("GroupMembers calls = %d, want %d", groupCalls, tc.wantGroupCalls)
			}
		})
	}
}

func TestReconcilePendingOutboxExactLocalZombiePinnedUntilResolution(t *testing.T) {
	fixture := newPendingOutboxFixture(t, store.StateFailed)
	cmd := exec.Command("/bin/sh", "-c", "sleep 0.5")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start local supervisor child: %v", err)
	}
	watcher, err := newProcessExitWatcher(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("watch local supervisor child: %v", err)
	}
	handle := &runHandle{
		runID:    fixture.run.ID,
		cmd:      cmd,
		watcher:  watcher,
		reapDone: make(chan struct{}),
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	pid := cmd.Process.Pid
	if _, err := fixture.engine.store.DB().ExecContext(context.Background(),
		`UPDATE runs SET supervisor_pid = ?, supervisor_pgid = ? WHERE id = ?`, pid, pid, fixture.run.ID); err != nil {
		t.Fatalf("update run local identity: %v", err)
	}
	if _, err := fixture.engine.store.DB().ExecContext(context.Background(),
		`UPDATE finalization_outbox SET supervisor_pid = ?, supervisor_pgid = ? WHERE run_id = ?`, pid, pid, fixture.run.ID); err != nil {
		t.Fatalf("update outbox local identity: %v", err)
	}
	run, err := fixture.engine.store.GetRun(context.Background(), fixture.run.ID)
	if err != nil {
		t.Fatalf("GetRun local identity: %v", err)
	}
	fixture.run = run
	writeRecoveryIdentityValues(t, run.IdentityPath, recoveryIdentityValues(run))
	fixture.backend.alive[pid] = true
	fixture.backend.alive[run.WorkerPID] = false
	fixture.backend.members = []int{pid}
	fixture.engine.mu.Lock()
	fixture.engine.active[run.ID] = handle
	fixture.engine.mu.Unlock()

	select {
	case <-watcher.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("local supervisor NOTE_EXIT not observed")
	}
	if cmd.ProcessState != nil {
		t.Fatal("local supervisor reaped before reconciliation")
	}

	reconcileOutbox(t, fixture.engine)

	row := outboxAfterReconcile(t, fixture)
	if row.Acknowledged != -1 || row.Diagnostic == "" {
		t.Fatalf("resolved row = %+v, want abandoned diagnostic", row)
	}
	if cmd.ProcessState != nil {
		t.Fatal("local supervisor reaped during durable reconciliation")
	}
	fixture.engine.mu.Lock()
	stillPinned := fixture.engine.active[run.ID] == handle
	fixture.engine.mu.Unlock()
	if !stillPinned {
		t.Fatal("local supervisor ownership released before finalization")
	}
	assertNoReconciliationSignals(t, fixture.backend)

	fixture.engine.finalizeLocalSupervisors(context.Background())

	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatal("resolved local supervisor was not reaped")
	}
	fixture.engine.mu.Lock()
	_, stillTracked := fixture.engine.active[run.ID]
	fixture.engine.mu.Unlock()
	if stillTracked {
		t.Fatal("reaped local supervisor remains tracked")
	}
}

func TestReconcilePendingOutboxStartupTickFreshEngineAndIdempotency(t *testing.T) {
	t.Run("startup", func(t *testing.T) {
		fixture := newPendingOutboxFixture(t, store.StateFailed)
		fixture.backend.alive[fixture.run.SupervisorPID] = false
		fixture.backend.alive[fixture.run.WorkerPID] = false
		recoverWithDeadline(t, fixture.engine)
		if row := outboxAfterReconcile(t, fixture); row.Acknowledged != -1 {
			t.Fatalf("startup acknowledged = %d, want -1", row.Acknowledged)
		}
		assertNoReconciliationSignals(t, fixture.backend)
	})

	t.Run("tick", func(t *testing.T) {
		fixture := newPendingOutboxFixture(t, store.StateFailed)
		fixture.backend.alive[fixture.run.SupervisorPID] = false
		fixture.backend.alive[fixture.run.WorkerPID] = false
		fixture.engine.tick(context.Background())
		if row := outboxAfterReconcile(t, fixture); row.Acknowledged != -1 {
			t.Fatalf("tick acknowledged = %d, want -1", row.Acknowledged)
		}
		assertNoReconciliationSignals(t, fixture.backend)
	})

	t.Run("fresh engine repeated cycles", func(t *testing.T) {
		fixture := newPendingOutboxFixture(t, store.StateFailed)
		storePath := fixture.storePath
		runID := fixture.run.ID
		if err := fixture.engine.Close(); err != nil {
			t.Fatalf("close first engine: %v", err)
		}
		engineSocket := filepath.Join("/tmp", fmt.Sprintf("ananke-m3-fresh-%d.sock", time.Now().UnixNano()))
		fresh, err := NewEngine(EngineConfig{
			StorePath:     storePath,
			SocketPath:    engineSocket,
			SupervisorBin: "/bin/true",
			DataDir:       t.TempDir(),
			Token:         "fresh-token",
		})
		if err != nil {
			t.Fatalf("NewEngine fresh: %v", err)
		}
		t.Cleanup(func() {
			_ = fresh.Close()
			_ = os.Remove(engineSocket)
		})
		backend := &cleanupProbeBackend{alive: map[int]bool{
			recoveryTestSupervisorPID: false,
			recoveryTestWorkerPID:     false,
		}}
		fresh.backend = backend

		recoverWithDeadline(t, fresh)
		first, err := fresh.store.GetOutbox(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetOutbox fresh: %v", err)
		}
		if first.Acknowledged != -1 || first.Diagnostic == "" {
			t.Fatalf("fresh reconciliation row = %+v", first)
		}
		for range 3 {
			recoverWithDeadline(t, fresh)
			fresh.tick(context.Background())
		}
		after, err := fresh.store.GetOutbox(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetOutbox repeated: %v", err)
		}
		if after.Acknowledged != first.Acknowledged || after.Diagnostic != first.Diagnostic || !after.AcknowledgedAt.Equal(first.AcknowledgedAt) {
			t.Fatalf("resolved row changed: first=%+v after=%+v", first, after)
		}
		assertNoReconciliationSignals(t, backend)
	})
}

func TestReconcilePendingOutboxAcceptsConcurrentSupervisorAcknowledge(t *testing.T) {
	fixture := newPendingOutboxFixture(t, store.StateCompleted)
	requests := startRecoverySocket(t, fixture.run.SocketPath, func(map[string]string) []byte {
		if err := fixture.engine.store.AcknowledgeOutbox(context.Background(), fixture.run.ID); err != nil {
			t.Errorf("concurrent supervisor acknowledge: %v", err)
		}
		response := recoveryStatus(fixture.run)
		response["state"] = string(store.StateCompleted)
		return encodeRecoveryResponse(t, response)
	})

	if err := fixture.engine.Recover(context.Background()); err != nil {
		t.Fatalf("Recover after concurrent acknowledge: %v", err)
	}

	if request := receiveRecoveryRequest(t, requests); request["cmd"] != "finalize" {
		t.Fatalf("request = %v, want finalize", request)
	}
	if row := outboxAfterReconcile(t, fixture); row.Acknowledged != 1 {
		t.Fatalf("acknowledged = %d, want 1", row.Acknowledged)
	}
	assertNoReconciliationSignals(t, fixture.backend)
}
