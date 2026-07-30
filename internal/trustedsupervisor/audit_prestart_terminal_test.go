package trustedsupervisor

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditExecutorPreparedGatewayFailurePersistsWaitingWithoutHiddenCloseError(t *testing.T) {
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journal, err := openServerJournal(filepath.Join(material.directory, "prestart-gateway.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	authority.bind(t, journal)
	intent := auditIntentForPolicyTest(t, material.entry, "prestart_gateway")
	if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
		t.Fatal(err)
	}

	var providerCalls atomic.Int32
	dependencies := fakeAuditBrokerDependencies()
	dependencies.LookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return nil, errors.New("injected gateway setup failure with private details")
	}
	dependencies.DialContext = func(context.Context, string, string) (net.Conn, error) {
		providerCalls.Add(1)
		return nil, errors.New("provider call must not occur")
	}
	material.policy.testBrokerDependencies = dependencies

	executor, err := newUnrecoveredAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	runAuditExecutorToIdleForTest(t, executor, intent.EnvelopeHash)
	if err := executor.Close(); err != nil {
		t.Fatalf("executor close exposed hidden terminal error: %v", err)
	}

	_, events, err := journal.loadAuditExecution(context.Background(), intent.EnvelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].State != auditStatePrepared || events[1].State != auditStateWaitingForHuman {
		t.Fatalf("pre-start gateway history = %+v; want prepared, waiting_for_human", events)
	}
	terminal := events[1]
	if terminal.FailureClass != "provider_gateway_setup_failed" || terminal.PID != 0 || terminal.PGID != 0 || terminal.ProcessStartIdentity != "" {
		t.Fatalf("pre-start gateway terminal = %+v", terminal)
	}
	if providerCalls.Load() != 0 {
		t.Fatalf("pre-start gateway failure made %d provider calls", providerCalls.Load())
	}
}

func TestAuditExecutorPreRunningFailuresPersistClosedWaiting(t *testing.T) {
	type effects struct {
		providerCalls atomic.Int32
		startCalls    atomic.Int32
	}
	testCases := []struct {
		name           string
		failureClass   string
		wantStartCalls int32
		configure      func(*testing.T, *gitArchivePolicyMaterial, *auditExecutor, *activeAuditExecution, auditExecutionIntent)
	}{
		{
			name:         "runtime authority verification",
			failureClass: "runtime_authority_verification_failed",
			configure: func(_ *testing.T, material *gitArchivePolicyMaterial, _ *auditExecutor, _ *activeAuditExecution, _ auditExecutionIntent) {
				material.policy.atomicRuntimeAuthorityVerifier = atomicRuntimeAuthorityVerifierFunc(func(executionPolicyEntry, []byte) (*atomicRuntimeAuthorityLease, error) {
					return nil, unsupportedAtomicRuntimeBoundary(AtomicRuntimeBoundaryExecutable, AtomicRuntimeBoundaryIdentityChanged)
				})
			},
		},
		{
			name:         "provider gateway setup",
			failureClass: "provider_gateway_setup_failed",
			configure: func(_ *testing.T, material *gitArchivePolicyMaterial, _ *auditExecutor, _ *activeAuditExecution, _ auditExecutionIntent) {
				dependencies := material.policy.testBrokerDependencies
				dependencies.LookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
					return nil, errors.New("injected provider gateway failure /private/credential")
				}
				material.policy.testBrokerDependencies = dependencies
			},
		},
		{
			name:         "transport binding",
			failureClass: "transport_binding_failed",
			configure: func(_ *testing.T, material *gitArchivePolicyMaterial, _ *auditExecutor, _ *activeAuditExecution, intent auditExecutionIntent) {
				_, _, _, _, temporaryPath := auditAttemptPaths(intent, material.entry, 1)
				dependencies := material.policy.testBrokerDependencies
				listenConfig := &net.ListenConfig{}
				dependencies.ListenContext = func(ctx context.Context, network, address string) (net.Listener, error) {
					listener, err := listenConfig.Listen(ctx, network, address)
					if err == nil && network == "tcp6" {
						if mkdirErr := os.Mkdir(filepath.Join(temporaryPath, "omp-agent"), 0o700); mkdirErr != nil {
							_ = listener.Close()
							return nil, mkdirErr
						}
					}
					return listener, err
				}
				material.policy.testBrokerDependencies = dependencies
			},
		},
		{
			name:         "start gate",
			failureClass: "start_gate_failed",
			configure: func(_ *testing.T, _ *gitArchivePolicyMaterial, executor *auditExecutor, active *activeAuditExecution, _ auditExecutionIntent) {
				executor.hooks.beforeStart = func(string) { active.cancel() }
			},
		},
		{
			name:           "command start",
			failureClass:   "command_start_failed",
			wantStartCalls: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			material, journal, intent, executor := newPrestartAuditExecutorForTest(t, strings.ReplaceAll(testCase.name, " ", "_"))
			observed := &effects{}
			dependencies := material.policy.testBrokerDependencies
			dependencies.DialContext = func(context.Context, string, string) (net.Conn, error) {
				observed.providerCalls.Add(1)
				return nil, errors.New("provider call must not occur")
			}
			material.policy.testBrokerDependencies = dependencies
			executor.hooks.invocation.StartCommand = func(*exec.Cmd, *os.File, *os.File) error {
				observed.startCalls.Add(1)
				return errors.New("injected command start failure /private/credential")
			}
			active := executor.newActive()
			if testCase.configure != nil {
				testCase.configure(t, &material, executor, active, intent)
			}
			runActiveAuditExecutorToIdleForTest(t, executor, intent.EnvelopeHash, active)
			if err := executor.Close(); err != nil {
				t.Fatalf("executor close: %v", err)
			}

			_, events, err := journal.loadAuditExecution(context.Background(), intent.EnvelopeHash)
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 2 || events[0].State != auditStatePrepared || events[1].State != auditStateWaitingForHuman {
				t.Fatalf("pre-running %s history = %+v", testCase.name, events)
			}
			terminal := events[1]
			if terminal.FailureClass != testCase.failureClass || terminal.PID != 0 || terminal.PGID != 0 || terminal.ProcessStartIdentity != "" ||
				terminal.ProcessStartedAt != "" || terminal.ProcessFinishedAt != "" {
				t.Fatalf("pre-running %s terminal = %+v", testCase.name, terminal)
			}
			if strings.Contains(terminal.FailureClass, "private") || strings.Contains(terminal.FailureClass, "credential") {
				t.Fatalf("pre-running failure class leaked raw detail: %q", terminal.FailureClass)
			}
			if observed.providerCalls.Load() != 0 || observed.startCalls.Load() != testCase.wantStartCalls {
				t.Fatalf("pre-running %s effects: provider=%d start=%d", testCase.name, observed.providerCalls.Load(), observed.startCalls.Load())
			}
		})
	}
}

func TestAuditExecutorTerminalJournalFailureIsObservableAndRestartRecoverable(t *testing.T) {
	material, journal, intent, executor := newPrestartAuditExecutorForTest(t, "terminal_journal_failure")
	var triggerErr error
	dependencies := material.policy.testBrokerDependencies
	dependencies.LookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		_, triggerErr = journal.db.Exec(`CREATE TRIGGER force_waiting_append_failure BEFORE INSERT ON trusted_supervisor_audit_events
			WHEN NEW.state = 'waiting_for_human' BEGIN SELECT RAISE(ABORT, 'forced terminal append failure'); END`)
		return nil, errors.New("injected gateway failure")
	}
	material.policy.testBrokerDependencies = dependencies

	runAuditExecutorToIdleForTest(t, executor, intent.EnvelopeHash)
	closeErr := executor.Close()
	if triggerErr != nil {
		t.Fatalf("install terminal failure trigger: %v", triggerErr)
	}
	if closeErr == nil {
		t.Fatal("executor close hid forced terminal journal failure")
	}
	if _, err := journal.db.Exec(`DROP TRIGGER force_waiting_append_failure`); err != nil {
		t.Fatal(err)
	}
	_, events, err := journal.loadAuditExecution(context.Background(), intent.EnvelopeHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].State != auditStatePrepared {
		t.Fatalf("failed terminal persistence fabricated history: %+v", events)
	}

	recovered, err := newAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	_, events = waitForAuditState(t, journal, intent.EnvelopeHash, auditStateWaitingForHuman)
	if terminal := events[len(events)-1]; terminal.FailureClass != "restart_after_prepared_unknown" {
		t.Fatalf("restart recovery terminal = %+v", terminal)
	}
	if err := recovered.Close(); err != nil {
		t.Fatalf("recovered executor close: %v", err)
	}
}

func newPrestartAuditExecutorForTest(t *testing.T, suffix string) (gitArchivePolicyMaterial, *serverJournal, auditExecutionIntent, *auditExecutor) {
	t.Helper()
	material := newGitArchivePolicyMaterial(t)
	authority := newDeterministicAuditAuthorityTestFixture(t, material.policy)
	journal, err := openServerJournal(filepath.Join(material.directory, suffix+".sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	authority.bind(t, journal)
	intent := auditIntentForPolicyTest(t, material.entry, suffix)
	if err := journal.storeAuditIntent(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	executor, err := newUnrecoveredAuditExecutor(journal, material.policy)
	if err != nil {
		t.Fatal(err)
	}
	return material, journal, intent, executor
}

func runAuditExecutorToIdleForTest(t *testing.T, executor *auditExecutor, envelopeHash string) {
	t.Helper()
	active := executor.newActive()
	executor.mu.Lock()
	executor.active[envelopeHash] = active
	executor.mu.Unlock()
	go executor.run(envelopeHash, active)
	select {
	case <-active.done:
	case <-time.After(10 * time.Second):
		t.Fatal("audit executor did not become idle")
	}
}

func runActiveAuditExecutorToIdleForTest(t *testing.T, executor *auditExecutor, envelopeHash string, active *activeAuditExecution) {
	t.Helper()
	executor.mu.Lock()
	executor.active[envelopeHash] = active
	executor.mu.Unlock()
	go executor.run(envelopeHash, active)
	select {
	case <-active.done:
	case <-time.After(10 * time.Second):
		t.Fatal("audit executor did not become idle")
	}
}
