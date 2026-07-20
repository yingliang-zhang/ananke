package supervisor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
)

func authorityBackend() *failClosedBackend {
	exited := make(chan struct{})
	close(exited)
	return &failClosedBackend{
		pgid:      57001,
		launchPID: 57002,
		exited:    exited,
		groupResults: []groupResult{
			{},
		},
	}
}

func assertDurableAuthority(t *testing.T, st *store.Store, cfg Config, runID string, wantAck int) {
	t.Helper()
	run, err := st.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	identity, err := lifecycle.ReadIdentity(cfg.IdentityPath)
	if err != nil {
		t.Fatalf("ReadIdentity: %v", err)
	}
	outbox, err := st.GetOutbox(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetOutbox: %v", err)
	}
	if run.SupervisorPID <= 0 || run.SupervisorPGID <= 0 || run.WorkerPID <= 0 {
		t.Fatalf("run authority is incomplete: supervisor=%d pgid=%d worker=%d",
			run.SupervisorPID, run.SupervisorPGID, run.WorkerPID)
	}
	if identity.RunID != run.ID || identity.SupervisorPID != run.SupervisorPID ||
		identity.SupervisorPGID != run.SupervisorPGID || identity.WorkerPID != run.WorkerPID ||
		identity.SocketPath != run.SocketPath || identity.Token != run.Token ||
		identity.TranscriptPath != run.TranscriptPath {
		t.Fatalf("identity %+v does not match run %+v", identity, run)
	}
	if outbox.RunID != run.ID || outbox.TerminalState != run.State ||
		outbox.SupervisorPID != run.SupervisorPID || outbox.SupervisorPGID != run.SupervisorPGID ||
		outbox.SocketPath != run.SocketPath || outbox.Token != run.Token || outbox.Acknowledged != wantAck {
		t.Fatalf("outbox %+v does not match run authority %+v", outbox, run)
	}
}

func assertReachableAuthority(t *testing.T, cfg Config, runID string, terminal store.State) {
	t.Helper()
	response := sendCmd(t, cfg.SocketPath, cfg.Token, "finalize")
	if response["ok"] != true || response["run_id"] != runID || response["state"] != string(terminal) {
		t.Fatalf("finalization authority response = %v", response)
	}
	if workerPID, _ := toInt(response["worker_pid"]); workerPID != 57002 {
		t.Fatalf("worker_pid = %d, want 57002", workerPID)
	}
	if pgid, _ := toInt(response["pgid"]); pgid != 57002 {
		t.Fatalf("pgid = %d, want worker group leader 57002", pgid)
	}
}

func TestSupervisorTransientAuthorityFailuresRetryBeforeTerminal(t *testing.T) {
	tests := []struct {
		name    string
		install func(*Supervisor, chan<- struct{})
	}{
		{
			name: "post-launch identity",
			install: func(s *Supervisor, attempts chan<- struct{}) {
				original := s.writeIdentity
				failed := false
				s.writeIdentity = func(path string, identity lifecycle.Identity) error {
					if identity.WorkerPID > 0 && !failed {
						failed = true
						attempts <- struct{}{}
						return errors.New("injected identity rewrite failure")
					}
					return original(path, identity)
				}
			},
		},
		{
			name: "run-row identity",
			install: func(s *Supervisor, attempts chan<- struct{}) {
				original := s.setRunSupervisor
				failed := false
				s.setRunSupervisor = func(ctx context.Context, runID string, supervisorPID, pgid, workerPID int) error {
					if !failed {
						failed = true
						attempts <- struct{}{}
						return errors.New("injected run-row identity failure")
					}
					return original(ctx, runID, supervisorPID, pgid, workerPID)
				}
			},
		},
		{
			name: "socket authority",
			install: func(s *Supervisor, attempts chan<- struct{}) {
				original := s.listenAuthority
				failed := false
				s.listenAuthority = func() error {
					if !failed {
						failed = true
						attempts <- struct{}{}
						return errors.New("injected socket setup failure")
					}
					return original()
				}
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runID := fmt.Sprintf("authority-transient-%d", i)
			s, st, cfg := newFailClosedSupervisor(t, runID, authorityBackend())
			cfg.CleanupRetryMin = time.Millisecond
			cfg.CleanupRetryMax = 2 * time.Millisecond
			s.cfg = cfg
			attempts := make(chan struct{}, 2)
			test.install(s, attempts)

			terminal, err := s.Run(context.Background())
			if err == nil {
				t.Fatal("Run error = nil, want original authority failure")
			}
			if terminal != store.StateFailed {
				t.Fatalf("terminal = %q, want failed", terminal)
			}
			select {
			case <-attempts:
			default:
				t.Fatal("fault hook was not exercised")
			}
			assertDurableAuthority(t, st, cfg, runID, 1)
		})
	}
}

func TestSupervisorPermanentAuthorityFailuresStayNonterminal(t *testing.T) {
	tests := []struct {
		name    string
		install func(*Supervisor, <-chan struct{}, chan<- struct{})
	}{
		{
			name: "post-launch identity",
			install: func(s *Supervisor, release <-chan struct{}, attempts chan<- struct{}) {
				original := s.writeIdentity
				s.writeIdentity = func(path string, identity lifecycle.Identity) error {
					if identity.WorkerPID == 0 {
						return original(path, identity)
					}
					select {
					case <-release:
						return original(path, identity)
					default:
						select {
						case attempts <- struct{}{}:
						default:
						}
						return errors.New("identity authority unavailable")
					}
				}
			},
		},
		{
			name: "run-row identity",
			install: func(s *Supervisor, release <-chan struct{}, attempts chan<- struct{}) {
				original := s.setRunSupervisor
				s.setRunSupervisor = func(ctx context.Context, runID string, supervisorPID, pgid, workerPID int) error {
					select {
					case <-release:
						return original(ctx, runID, supervisorPID, pgid, workerPID)
					default:
						select {
						case attempts <- struct{}{}:
						default:
						}
						return errors.New("run-row authority unavailable")
					}
				}
			},
		},
		{
			name: "socket authority",
			install: func(s *Supervisor, release <-chan struct{}, attempts chan<- struct{}) {
				original := s.listenAuthority
				s.listenAuthority = func() error {
					select {
					case <-release:
						return original()
					default:
						select {
						case attempts <- struct{}{}:
						default:
						}
						return errors.New("socket authority unavailable")
					}
				}
			},
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runID := fmt.Sprintf("authority-permanent-%d", i)
			s, st, cfg := newFailClosedSupervisor(t, runID, authorityBackend())
			cfg.CleanupRetryMin = time.Millisecond
			cfg.CleanupRetryMax = 2 * time.Millisecond
			s.cfg = cfg
			release := make(chan struct{})
			attempts := make(chan struct{}, 8)
			test.install(s, release, attempts)
			type result struct {
				terminal store.State
				err      error
			}
			done := make(chan result, 1)
			go func() {
				terminal, err := s.Run(context.Background())
				done <- result{terminal: terminal, err: err}
			}()

			for range 2 {
				select {
				case <-attempts:
				case <-time.After(2 * time.Second):
					t.Fatal("authority establishment was not retried")
				}
			}
			run, err := st.GetRun(context.Background(), runID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if run.State != store.StateCleanupRequired {
				t.Fatalf("state = %q, want cleanup_required", run.State)
			}
			if _, err := st.GetOutbox(context.Background(), runID); !errors.Is(err, store.ErrOutboxNotFound) {
				t.Fatalf("outbox before authority = error %v, want not found", err)
			}
			select {
			case result := <-done:
				t.Fatalf("Run returned without authority: terminal=%q error=%v", result.terminal, result.err)
			default:
			}
			close(release)
			select {
			case result := <-done:
				if result.terminal != store.StateFailed || result.err == nil {
					t.Fatalf("resolved Run = terminal %q error %v", result.terminal, result.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Run did not finalize after authority recovered")
			}
			assertDurableAuthority(t, st, cfg, runID, 1)
		})
	}
}

func TestSupervisorOutboxAcknowledgementRetriesAndConcurrentAck(t *testing.T) {
	tests := []struct {
		name    string
		install func(*Supervisor, chan<- struct{})
	}{
		{
			name: "one-shot failure",
			install: func(s *Supervisor, attempts chan<- struct{}) {
				original := s.acknowledgeOutbox
				failed := false
				s.acknowledgeOutbox = func(ctx context.Context, runID string) error {
					attempts <- struct{}{}
					if !failed {
						failed = true
						return errors.New("injected acknowledgement failure")
					}
					return original(ctx, runID)
				}
			},
		},
		{
			name: "daemon wins race",
			install: func(s *Supervisor, attempts chan<- struct{}) {
				original := s.acknowledgeOutbox
				s.acknowledgeOutbox = func(ctx context.Context, runID string) error {
					attempts <- struct{}{}
					if err := original(ctx, runID); err != nil {
						return err
					}
					return store.ErrOutboxNotFound
				}
			},
		},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runID := fmt.Sprintf("ack-retry-%d", i)
			s, st, cfg := newFailClosedSupervisor(t, runID, authorityBackend())
			cfg.CleanupRetryMin = time.Millisecond
			cfg.CleanupRetryMax = 2 * time.Millisecond
			s.cfg = cfg
			attempts := make(chan struct{}, 4)
			test.install(s, attempts)
			terminal, err := s.Run(context.Background())
			if err != nil || terminal != store.StateCompleted {
				t.Fatalf("Run = terminal %q error %v", terminal, err)
			}
			select {
			case <-attempts:
			default:
				t.Fatal("acknowledgement hook was not exercised")
			}
			assertDurableAuthority(t, st, cfg, runID, 1)
		})
	}
}

func TestSupervisorPermanentAcknowledgementFailureKeepsAuthorityReachable(t *testing.T) {
	const runID = "ack-permanent"
	s, st, cfg := newFailClosedSupervisor(t, runID, authorityBackend())
	cfg.CleanupRetryMin = time.Millisecond
	cfg.CleanupRetryMax = 2 * time.Millisecond
	s.cfg = cfg
	release := make(chan struct{})
	attempts := make(chan struct{}, 8)
	original := s.acknowledgeOutbox
	s.acknowledgeOutbox = func(ctx context.Context, runID string) error {
		select {
		case <-release:
			return original(ctx, runID)
		default:
			select {
			case attempts <- struct{}{}:
			default:
			}
			return errors.New("acknowledgement unavailable")
		}
	}
	type result struct {
		terminal store.State
		err      error
	}
	done := make(chan result, 1)
	go func() {
		terminal, err := s.Run(context.Background())
		done <- result{terminal: terminal, err: err}
	}()
	for range 2 {
		select {
		case <-attempts:
		case <-time.After(2 * time.Second):
			t.Fatal("acknowledgement was not retried")
		}
	}
	assertDurableAuthority(t, st, cfg, runID, 0)
	assertReachableAuthority(t, cfg, runID, store.StateCompleted)
	select {
	case result := <-done:
		t.Fatalf("Run returned with pending outbox: terminal=%q error=%v", result.terminal, result.err)
	default:
	}
	close(release)
	select {
	case result := <-done:
		if result.terminal != store.StateCompleted || result.err != nil {
			t.Fatalf("resolved Run = terminal %q error %v", result.terminal, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after acknowledgement recovered")
	}
	assertDurableAuthority(t, st, cfg, runID, 1)
}

func TestPendingPostStartFailureOutboxReconcilesAfterRestart(t *testing.T) {
	const runID = "post-start-crash-window"
	s, st, cfg := newFailClosedSupervisor(t, runID, authorityBackend())
	cfg.CleanupRetryMin = time.Millisecond
	cfg.CleanupRetryMax = 2 * time.Millisecond
	s.cfg = cfg
	originalWrite := s.writeIdentity
	failed := false
	s.writeIdentity = func(path string, identity lifecycle.Identity) error {
		if identity.WorkerPID > 0 && !failed {
			failed = true
			return errors.New("injected initial identity rewrite failure")
		}
		return originalWrite(path, identity)
	}
	attempts := make(chan struct{}, 8)
	s.acknowledgeOutbox = func(context.Context, string) error {
		select {
		case attempts <- struct{}{}:
		default:
		}
		return errors.New("supervisor acknowledgement unavailable")
	}
	type result struct {
		terminal store.State
		err      error
	}
	done := make(chan result, 1)
	go func() {
		terminal, err := s.Run(context.Background())
		done <- result{terminal: terminal, err: err}
	}()
	select {
	case <-attempts:
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not reach pending terminal outbox")
	}
	assertDurableAuthority(t, st, cfg, runID, 0)
	assertReachableAuthority(t, cfg, runID, store.StateFailed)

	engineSocket := filepath.Join(t.TempDir(), "engine.sock")
	engine, err := lifecycle.NewEngine(lifecycle.EngineConfig{
		StorePath:     cfg.StorePath,
		SocketPath:    engineSocket,
		SupervisorBin: "/bin/true",
		DataDir:       t.TempDir(),
		Token:         "restart-token",
	})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()
	if err := engine.Recover(context.Background()); err != nil {
		t.Fatalf("Recover: %v", err)
	}
	assertDurableAuthority(t, st, cfg, runID, 1)
	select {
	case result := <-done:
		if result.terminal != store.StateFailed || result.err == nil {
			t.Fatalf("Run after reconciliation = terminal %q error %v", result.terminal, result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("supervisor did not observe concurrent daemon acknowledgement")
	}
}
