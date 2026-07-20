// Package lifecycle also hosts the daemon engine, which manages the store,
// launches supervisors, exposes a Unix-socket JSON API, and runs the recovery
// loop described in ADR-0003 §2-3.
package lifecycle

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

// EngineConfig configures the daemon engine.
type EngineConfig struct {
	StorePath     string        // SQLite database path
	SocketPath    string        // daemon API Unix socket path
	SupervisorBin string        // path to the ananke-supervisor binary
	DataDir       string        // directory for per-run identity/socket/transcript files
	Token         string        // API auth token (generated if empty)
	TickInterval  time.Duration // recovery loop tick interval (default 500ms)
	ReportError   func(error)   // optional sink for background recovery and persistence errors
}

// Engine is the daemon core: it owns the store, launches supervisor subprocesses,
// serves a Unix-socket JSON API, and runs a recovery loop that monitors active
// runs, tails transcripts, and reconciles pending finalization outbox rows.
type Engine struct {
	cfg              EngineConfig
	store            *store.Store
	backend          LifecycleBackend
	newExitWatcher   func(int) (*processExitWatcher, error)
	tokenReader      io.Reader
	mu               sync.Mutex
	recoveryMu       sync.Mutex
	workCtx          context.Context
	cancelWork       context.CancelFunc
	wg               sync.WaitGroup
	closeDone        chan struct{}
	closeErr         error
	active           map[string]*runHandle
	tails            map[string]*transcriptTail
	cleanupRequested map[string]struct{}
	connections      map[net.Conn]struct{}
	listener         net.Listener
	cleanupDelivery  func(context.Context, string, string) (supCmdResponse, error)
	running          bool
	closing          bool
	closed           bool
}

// runHandle owns a locally launched supervisor, its exact exit watcher, and
// the sole eventual exec.Cmd.Wait. Restarted engines intentionally have no
// handle for supervisors they did not launch.
type runHandle struct {
	watcherMu sync.Mutex
	runID     string
	cmd       *exec.Cmd
	watcher   *processExitWatcher
	reapOnce  sync.Once
	reapDone  chan struct{}
	reapErr   error
	identity  string
	socket    string
}

func (h *runHandle) hasExited() bool {
	h.watcherMu.Lock()
	watcher := h.watcher
	h.watcherMu.Unlock()
	if watcher == nil {
		return false
	}
	select {
	case <-watcher.exited:
		return true
	default:
		return false
	}
}

func (h *runHandle) hasWatcher() bool {
	h.watcherMu.Lock()
	defer h.watcherMu.Unlock()
	return h.watcher != nil
}

func (h *runHandle) reap() error {
	h.reapOnce.Do(func() {
		h.reapErr = h.cmd.Wait()
		close(h.reapDone)
	})
	<-h.reapDone
	return h.reapErr
}

// transcriptTail tracks the read position in a run's transcript file.
type transcriptTail struct {
	runID  string
	path   string
	offset int64
	file   *os.File
	reader *bufio.Reader
}

// releaseTranscriptTail idempotently removes a run's tail and closes its file.
func (e *Engine) releaseTranscriptTail(runID string) {
	e.mu.Lock()
	tail := e.tails[runID]
	if tail != nil {
		delete(e.tails, runID)
	}
	e.mu.Unlock()
	if tail != nil {
		_ = tail.file.Close()
	}
}

func (e *Engine) releaseUnneededTranscriptTails(runs []store.Run) {
	needed := make(map[string]struct{}, len(runs))
	for _, run := range runs {
		if !store.IsTerminal(run.State) {
			needed[run.ID] = struct{}{}
		}
	}
	e.mu.Lock()
	stale := make([]string, 0)
	for runID := range e.tails {
		if _, ok := needed[runID]; !ok {
			stale = append(stale, runID)
		}
	}
	e.mu.Unlock()
	for _, runID := range stale {
		e.releaseTranscriptTail(runID)
	}
}

// NewEngine opens the store and returns an Engine ready to Run.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	if cfg.SupervisorBin == "" {
		return nil, fmt.Errorf("engine: supervisor binary path required")
	}
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("engine: data directory required")
	}
	if cfg.SocketPath == "" {
		return nil, fmt.Errorf("engine: socket path required")
	}
	if cfg.Token == "" {
		token, err := generateToken(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("engine: generate auth token: %w", err)
		}
		cfg.Token = token
	}
	if cfg.TickInterval == 0 {
		cfg.TickInterval = 500 * time.Millisecond
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("engine: create data dir: %w", err)
	}
	st, err := store.Open(cfg.StorePath)
	if err != nil {
		return nil, fmt.Errorf("engine: open store: %w", err)
	}
	engine := &Engine{
		cfg:              cfg,
		store:            st,
		backend:          NewDarwinBackend(),
		newExitWatcher:   newProcessExitWatcher,
		tokenReader:      rand.Reader,
		active:           make(map[string]*runHandle),
		tails:            make(map[string]*transcriptTail),
		cleanupRequested: make(map[string]struct{}),
		connections:      make(map[net.Conn]struct{}),
	}
	engine.initRuntimeLocked()
	return engine, nil
}

// generateToken returns a random 16-byte hex token or propagates entropy
// failure. Authentication must never silently degrade to a predictable token.
func generateToken(reader io.Reader) (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Token returns the engine's auth token (needed by API clients).
func (e *Engine) Token() string { return e.cfg.Token }

func (e *Engine) reportBackgroundError(err error) {
	if err != nil && e.cfg.ReportError != nil {
		e.cfg.ReportError(err)
	}
}

// Store returns the underlying store (for tests).
func (e *Engine) Store() *store.Store { return e.store }

func (e *Engine) initRuntimeLocked() {
	if e.workCtx == nil {
		e.workCtx, e.cancelWork = context.WithCancel(context.Background())
	}
	if e.closeDone == nil {
		e.closeDone = make(chan struct{})
	}
	if e.active == nil {
		e.active = make(map[string]*runHandle)
	}
	if e.tails == nil {
		e.tails = make(map[string]*transcriptTail)
	}
	if e.cleanupRequested == nil {
		e.cleanupRequested = make(map[string]struct{})
	}
	if e.connections == nil {
		e.connections = make(map[net.Conn]struct{})
	}
}

// Close stops and joins all engine-owned work before releasing files and the
// store. It intentionally does not signal or reap live supervisors.
func (e *Engine) Close() error {
	e.mu.Lock()
	e.initRuntimeLocked()
	if e.closed {
		err := e.closeErr
		e.mu.Unlock()
		return err
	}
	if e.closing {
		done := e.closeDone
		e.mu.Unlock()
		<-done
		e.mu.Lock()
		err := e.closeErr
		e.mu.Unlock()
		return err
	}
	e.closing = true
	e.cancelWork()
	listener := e.listener
	connections := make([]net.Conn, 0, len(e.connections))
	for conn := range e.connections {
		connections = append(connections, conn)
	}
	e.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
	e.wg.Wait()

	e.mu.Lock()
	for _, tail := range e.tails {
		_ = tail.file.Close()
	}
	e.mu.Unlock()
	err := e.store.Close()

	e.mu.Lock()
	e.closeErr = err
	e.closed = true
	close(e.closeDone)
	e.mu.Unlock()
	return err
}

// Run starts the API server and recovery loop. It blocks until its caller is
// cancelled or Close begins shutdown.
func (e *Engine) Run(ctx context.Context) error {
	e.mu.Lock()
	e.initRuntimeLocked()
	if e.closing {
		e.mu.Unlock()
		return nil
	}
	if e.running {
		e.mu.Unlock()
		return errors.New("engine is already running")
	}
	e.running = true
	e.wg.Add(1)
	workCtx := e.workCtx
	cancelWork := e.cancelWork
	e.mu.Unlock()

	stopCaller := context.AfterFunc(ctx, cancelWork)
	if ctx.Err() != nil {
		cancelWork()
	}
	defer func() {
		stopCaller()
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		e.wg.Done()
	}()

	if err := e.Recover(workCtx); err != nil {
		if workCtx.Err() != nil {
			return nil
		}
		return fmt.Errorf("engine: recover: %w", err)
	}
	if workCtx.Err() != nil {
		return nil
	}

	if err := removeExistingUnixSocket(e.cfg.SocketPath); err != nil {
		return err
	}
	listener, err := net.Listen("unix", e.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("engine: listen %s: %w", e.cfg.SocketPath, err)
	}
	defer func() { _ = removeExistingUnixSocket(e.cfg.SocketPath) }()

	e.mu.Lock()
	if e.closing || workCtx.Err() != nil {
		e.mu.Unlock()
		_ = listener.Close()
		return nil
	}
	e.listener = listener
	e.wg.Add(2)
	e.mu.Unlock()
	go func() {
		defer e.wg.Done()
		e.recoveryLoop(workCtx)
	}()
	go e.acceptLoop(workCtx, listener)

	<-workCtx.Done()
	_ = listener.Close()
	return nil
}

func removeExistingUnixSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("engine: inspect socket path %s: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("engine: socket path %s is not a Unix socket", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("engine: remove stale socket %s: %w", path, err)
	}
	return nil
}

func (e *Engine) acceptLoop(ctx context.Context, listener net.Listener) {
	defer e.wg.Done()
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		e.mu.Lock()
		if e.closing {
			e.mu.Unlock()
			_ = conn.Close()
			return
		}
		e.connections[conn] = struct{}{}
		e.wg.Add(1)
		e.mu.Unlock()
		go func() {
			defer e.wg.Done()
			defer func() {
				e.mu.Lock()
				delete(e.connections, conn)
				e.mu.Unlock()
			}()
			e.handleAPIConn(ctx, conn)
		}()
	}
}

// --- Recovery ---

// Recover performs startup reconciliation: it processes pending outbox rows
// and checks active runs for dead supervisors.
func (e *Engine) Recover(ctx context.Context) error {
	e.recoveryMu.Lock()
	defer e.recoveryMu.Unlock()

	if err := e.reconcilePendingOutbox(ctx); err != nil {
		return err
	}

	runs, err := e.store.ListActiveRuns(ctx)
	if err != nil {
		return fmt.Errorf("list active runs: %w", err)
	}
	for _, r := range runs {
		if store.IsTerminal(r.State) {
			continue
		}
		result := e.inspectRecoverySupervisor(ctx, r)
		if r.State == store.StateCreated && r.CancelRequested && result.kind != recoveryAuthenticated {
			// A created run may still be bootstrapping an exact child launched by
			// this engine. A restarted engine waits for durable identity and the
			// status/adopt handshake before delivery.
			if e.localSupervisorRunning(r.ID) {
				e.requestCleanup(ctx, r)
			}
			continue
		}
		if r.State == store.StateRecoveryUnknown {
			if err := e.progressRecoveryUnknown(ctx, r, result); err != nil {
				return fmt.Errorf("progress recovery for run %s: %w", r.ID, err)
			}
			if r.CancelRequested && result.kind == recoveryAuthenticated {
				e.requestCleanup(ctx, r)
			}
			continue
		}
		if result.kind != recoveryAuthenticated {
			if rejectErr := e.rejectRecovery(ctx, r, result.diagnostic); rejectErr != nil {
				return rejectErr
			}
			continue
		}

		if r.CancelRequested {
			e.requestCleanup(ctx, r)
		}
		if r.State == store.StateCleanupRequired {
			e.progressCleanupRequired(ctx, r)
		}
		if r.TranscriptPath != "" && !store.IsTerminal(r.State) {
			off := r.TranscriptConsumedOffset
			if mutationHooks.resetOffset {
				off = 0
			}
			e.startTailing(ctx, r.ID, r.TranscriptPath, off)
		}
	}
	return nil
}

func readRecoveryIdentity(r store.Run) (Identity, error) {
	id, err := ReadIdentity(r.IdentityPath)
	if err != nil {
		return Identity{}, fmt.Errorf("read identity %q: %w", r.IdentityPath, err)
	}
	if err := validateRecoveryIdentity(r, id); err != nil {
		return Identity{}, err
	}
	return id, nil
}

func validateRecoveryIdentity(r store.Run, id Identity) error {
	if strings.TrimSpace(r.ID) == "" {
		return fmt.Errorf("store run_id is empty")
	}
	if strings.TrimSpace(id.RunID) == "" {
		return fmt.Errorf("identity run_id is empty")
	}
	if id.RunID != r.ID {
		return fmt.Errorf("identity run_id does not match store run_id")
	}
	if r.SupervisorPID <= 0 {
		return fmt.Errorf("store supervisor_pid is not positive")
	}
	if id.SupervisorPID <= 0 {
		return fmt.Errorf("identity supervisor_pid is not positive")
	}
	if id.SupervisorPID != r.SupervisorPID {
		return fmt.Errorf("identity supervisor_pid does not match store supervisor_pid")
	}
	if r.SupervisorPGID <= 0 {
		return fmt.Errorf("store supervisor_pgid is not positive")
	}
	if id.SupervisorPGID <= 0 {
		return fmt.Errorf("identity supervisor_pgid is not positive")
	}
	if id.SupervisorPGID != r.SupervisorPGID {
		return fmt.Errorf("identity supervisor_pgid does not match store supervisor_pgid")
	}
	if r.WorkerPID <= 0 {
		return fmt.Errorf("store worker_pid is not positive")
	}
	if id.WorkerPID <= 0 {
		return fmt.Errorf("identity worker_pid is not positive")
	}
	if id.WorkerPID != r.WorkerPID {
		return fmt.Errorf("identity worker_pid does not match store worker_pid")
	}
	if strings.TrimSpace(r.SocketPath) == "" {
		return fmt.Errorf("store socket_path is empty")
	}
	if strings.TrimSpace(id.SocketPath) == "" {
		return fmt.Errorf("identity socket_path is empty")
	}
	if id.SocketPath != r.SocketPath {
		return fmt.Errorf("identity socket_path does not match store socket_path")
	}
	if strings.TrimSpace(r.Token) == "" {
		return fmt.Errorf("store token is empty")
	}
	if strings.TrimSpace(id.Token) == "" {
		return fmt.Errorf("identity token is empty")
	}
	if id.Token != r.Token {
		return fmt.Errorf("identity token does not match store token")
	}
	if strings.TrimSpace(r.TranscriptPath) == "" {
		return fmt.Errorf("store transcript_path is empty")
	}
	if strings.TrimSpace(id.TranscriptPath) == "" {
		return fmt.Errorf("identity transcript_path is empty")
	}
	if id.TranscriptPath != r.TranscriptPath {
		return fmt.Errorf("identity transcript_path does not match store transcript_path")
	}
	if r.TranscriptRequired {
		if err := r.TranscriptIdentity.Validate(); err != nil {
			return fmt.Errorf("store transcript identity is invalid: %w", err)
		}
		if err := id.TranscriptIdentity.Validate(); err != nil {
			return fmt.Errorf("identity transcript identity is invalid: %w", err)
		}
		if id.TranscriptIdentity != r.TranscriptIdentity {
			return fmt.Errorf("identity transcript identity does not match store transcript identity")
		}
	}
	return nil
}

const abandonedOutboxDiagnostic = "validated durable identity; supervisor confirmed dead; worker absent; process group quiescent"

func validatePendingOutboxAuthority(row store.OutboxRow, r store.Run, id Identity) error {
	if strings.TrimSpace(row.RunID) == "" || row.RunID != r.ID || row.RunID != id.RunID {
		return fmt.Errorf("outbox run_id does not match durable identity")
	}
	if !store.IsTerminal(r.State) {
		return fmt.Errorf("durable run state %q is not terminal", r.State)
	}
	if !store.IsTerminal(row.TerminalState) {
		return fmt.Errorf("outbox terminal_state %q is not terminal", row.TerminalState)
	}
	if row.TerminalState != r.State {
		return fmt.Errorf("outbox terminal_state does not match durable run state")
	}
	if row.SupervisorPID <= 0 || row.SupervisorPID != r.SupervisorPID || row.SupervisorPID != id.SupervisorPID {
		return fmt.Errorf("outbox supervisor_pid does not match durable identity")
	}
	if row.SupervisorPGID <= 0 || row.SupervisorPGID != r.SupervisorPGID || row.SupervisorPGID != id.SupervisorPGID {
		return fmt.Errorf("outbox supervisor_pgid does not match durable identity")
	}
	if strings.TrimSpace(row.SocketPath) == "" || row.SocketPath != r.SocketPath || row.SocketPath != id.SocketPath {
		return fmt.Errorf("outbox socket_path does not match durable identity")
	}
	if strings.TrimSpace(row.Token) == "" || row.Token != r.Token || row.Token != id.Token {
		return fmt.Errorf("outbox token does not match durable identity")
	}
	return nil
}

func validateFinalizationResponse(row store.OutboxRow, r store.Run, resp supCmdResponse) error {
	if !resp.OK {
		if resp.Error == "" {
			return fmt.Errorf("finalize response not OK")
		}
		return fmt.Errorf("finalize response not OK: %s", resp.Error)
	}
	state := store.State(resp.State)
	if !store.IsTerminal(state) {
		return fmt.Errorf("finalize response state %q is not terminal", resp.State)
	}
	if state != row.TerminalState || state != r.State {
		return fmt.Errorf("finalize response state does not match durable terminal state")
	}
	if resp.RunID == "" || resp.RunID != row.RunID || resp.RunID != r.ID {
		return fmt.Errorf("finalize response run_id does not match durable identity")
	}
	if resp.SupervisorPID <= 0 || resp.SupervisorPID != row.SupervisorPID || resp.SupervisorPID != r.SupervisorPID {
		return fmt.Errorf("finalize response supervisor_pid does not match durable identity")
	}
	if resp.WorkerPID <= 0 || resp.WorkerPID != r.WorkerPID {
		return fmt.Errorf("finalize response worker_pid does not match durable identity")
	}
	if resp.PGID <= 0 || resp.PGID != row.SupervisorPGID || resp.PGID != r.SupervisorPGID {
		return fmt.Errorf("finalize response pgid does not match durable identity")
	}
	if r.TranscriptRequired {
		if err := r.TranscriptIdentity.Validate(); err != nil {
			return fmt.Errorf("durable transcript identity is invalid: %w", err)
		}
		if resp.TranscriptDevice != r.TranscriptIdentity.Device || resp.TranscriptInode != r.TranscriptIdentity.Inode {
			return fmt.Errorf("finalize response transcript identity does not match durable identity")
		}
	}
	return nil
}

func (e *Engine) resolvePendingOutbox(ctx context.Context, runID string, acknowledged int, diagnostic string) error {
	var err error
	switch acknowledged {
	case 1:
		err = e.store.AcknowledgeOutbox(ctx, runID)
	case -1:
		err = e.store.AbandonOutbox(ctx, runID, diagnostic)
	default:
		return fmt.Errorf("invalid outbox resolution %d", acknowledged)
	}
	if err == nil {
		e.releaseTranscriptTail(runID)
		return nil
	}
	if !errors.Is(err, store.ErrOutboxNotFound) {
		return err
	}
	row, getErr := e.store.GetOutbox(ctx, runID)
	if getErr == nil && row.Acknowledged != 0 {
		e.releaseTranscriptTail(runID)
		return nil
	}
	return err
}

func (e *Engine) reconcileConfirmedDeadOutbox(ctx context.Context, row store.OutboxRow, r store.Run, id Identity) error {
	if e.backend.ProcessAlive(id.WorkerPID) {
		return nil
	}
	members, err := e.backend.GroupMembers(id.SupervisorPGID)
	if err != nil {
		return nil
	}
	if len(members) != 0 {
		tracked, exited := e.localSupervisorStatus(r.ID, id.SupervisorPID)
		if !tracked || !exited {
			return nil
		}
		for _, memberPID := range members {
			if memberPID != id.SupervisorPID {
				return nil
			}
		}
	}
	return e.resolvePendingOutbox(ctx, row.RunID, -1, abandonedOutboxDiagnostic)
}

// reconcilePendingOutbox is the sole startup and periodic finalization path.
// Invalid or ambiguous authority remains pending; reconciliation never signals.
func (e *Engine) reconcilePendingOutbox(ctx context.Context) error {
	pending, err := e.store.ListPendingOutbox(ctx)
	if err != nil {
		return fmt.Errorf("list pending outbox: %w", err)
	}
	for _, row := range pending {
		r, err := e.store.GetRun(ctx, row.RunID)
		if err != nil {
			continue
		}
		id, err := readRecoveryIdentity(r)
		if err != nil {
			continue
		}
		if err := validatePendingOutboxAuthority(row, r, id); err != nil {
			continue
		}

		tracked, exited := e.localSupervisorStatus(r.ID, id.SupervisorPID)
		if tracked && exited {
			if err := e.reconcileConfirmedDeadOutbox(ctx, row, r, id); err != nil {
				return fmt.Errorf("reconcile dead outbox %s: %w", row.RunID, err)
			}
			continue
		}
		if !tracked && !e.backend.ProcessAlive(id.SupervisorPID) {
			if err := e.reconcileConfirmedDeadOutbox(ctx, row, r, id); err != nil {
				return fmt.Errorf("reconcile dead outbox %s: %w", row.RunID, err)
			}
			continue
		}

		response, err := e.sendSupervisorCmd(ctx, id.SocketPath, id.Token, "finalize")
		if err != nil || validateFinalizationResponse(row, r, response) != nil {
			continue
		}
		if err := e.resolvePendingOutbox(ctx, row.RunID, 1, ""); err != nil {
			return fmt.Errorf("acknowledge outbox %s: %w", row.RunID, err)
		}
	}
	return nil
}

type recoveryKind uint8

const (
	recoveryAmbiguous recoveryKind = iota
	recoveryAuthenticated
	recoverySupervisorDead
)

type recoveryResult struct {
	kind          recoveryKind
	identity      Identity
	reportedState store.State
	diagnostic    string
}

func (e *Engine) inspectRecoverySupervisor(ctx context.Context, r store.Run) recoveryResult {
	id, err := readRecoveryIdentity(r)
	if err != nil {
		return recoveryResult{
			kind:       recoveryAmbiguous,
			diagnostic: "recovery identity rejected: " + err.Error(),
		}
	}

	tracked, exited := e.localSupervisorStatus(r.ID, r.SupervisorPID)
	if tracked && exited {
		return recoveryResult{
			kind:       recoverySupervisorDead,
			identity:   id,
			diagnostic: "exact local supervisor exit observed",
		}
	}
	if !tracked && !e.backend.ProcessAlive(r.SupervisorPID) {
		return recoveryResult{
			kind:       recoverySupervisorDead,
			identity:   id,
			diagnostic: "validated non-child supervisor PID is absent",
		}
	}

	status, _, err := e.authenticateRecoveredSupervisor(ctx, r)
	if err != nil {
		// Exact local exit evidence may arrive while authentication is in
		// flight. A non-child authentication failure remains ambiguous even if
		// its numeric PID changes immediately afterward.
		if trackedNow, exitedNow := e.localSupervisorStatus(r.ID, r.SupervisorPID); trackedNow && exitedNow {
			return recoveryResult{
				kind:       recoverySupervisorDead,
				identity:   id,
				diagnostic: "exact local supervisor exit observed during authentication",
			}
		}
		return recoveryResult{
			kind:       recoveryAmbiguous,
			identity:   id,
			diagnostic: "recovery supervisor authentication failed: " + err.Error(),
		}
	}
	return recoveryResult{
		kind:          recoveryAuthenticated,
		identity:      id,
		reportedState: store.State(status.State),
		diagnostic:    "authenticated supervisor adoption",
	}
}

func (e *Engine) authenticateRecoveredSupervisor(ctx context.Context, r store.Run) (supCmdResponse, supCmdResponse, error) {
	status, err := e.sendSupervisorCmd(ctx, r.SocketPath, r.Token, "status")
	if err != nil {
		return supCmdResponse{}, supCmdResponse{}, fmt.Errorf("status command: %w", err)
	}
	if err := validateRecoveredSupervisorResponse("status", r, status); err != nil {
		return supCmdResponse{}, supCmdResponse{}, err
	}
	adopt, err := e.sendSupervisorCmd(ctx, r.SocketPath, r.Token, "adopt")
	if err != nil {
		return supCmdResponse{}, supCmdResponse{}, fmt.Errorf("adopt command: %w", err)
	}
	if err := validateRecoveredSupervisorResponse("adopt", r, adopt); err != nil {
		return supCmdResponse{}, supCmdResponse{}, err
	}
	if status.State != adopt.State {
		return supCmdResponse{}, supCmdResponse{}, fmt.Errorf("status/adopt state disagreement: %q != %q", status.State, adopt.State)
	}
	if status.RunID != adopt.RunID ||
		status.SupervisorPID != adopt.SupervisorPID ||
		status.WorkerPID != adopt.WorkerPID ||
		status.PGID != adopt.PGID ||
		(r.TranscriptRequired && (status.TranscriptDevice != adopt.TranscriptDevice ||
			status.TranscriptInode != adopt.TranscriptInode)) {
		return supCmdResponse{}, supCmdResponse{}, fmt.Errorf("status/adopt identity disagreement")
	}
	return status, adopt, nil
}

func validateRecoveredSupervisorResponse(command string, r store.Run, resp supCmdResponse) error {
	if !resp.OK {
		if resp.Error == "" {
			return fmt.Errorf("%s response not OK", command)
		}
		return fmt.Errorf("%s response not OK: %s", command, resp.Error)
	}
	switch store.State(resp.State) {
	case store.StateRunning, store.StateCancelling, store.StateCleanupRequired:
	default:
		return fmt.Errorf("%s response has incompatible nonterminal state %q", command, resp.State)
	}
	if resp.RunID == "" || resp.RunID != r.ID {
		return fmt.Errorf("%s response run_id does not match store run_id", command)
	}
	if resp.SupervisorPID <= 0 || resp.SupervisorPID != r.SupervisorPID {
		return fmt.Errorf("%s response supervisor_pid does not match store supervisor_pid", command)
	}
	if resp.WorkerPID <= 0 || resp.WorkerPID != r.WorkerPID {
		return fmt.Errorf("%s response worker_pid does not match store worker_pid", command)
	}
	if resp.PGID <= 0 || resp.PGID != r.SupervisorPGID {
		return fmt.Errorf("%s response pgid does not match store supervisor_pgid", command)
	}
	if r.TranscriptRequired {
		if err := r.TranscriptIdentity.Validate(); err != nil {
			return fmt.Errorf("%s durable transcript identity is invalid: %w", command, err)
		}
		if resp.TranscriptDevice != r.TranscriptIdentity.Device || resp.TranscriptInode != r.TranscriptIdentity.Inode {
			return fmt.Errorf("%s response transcript identity does not match durable identity", command)
		}
	}
	return nil
}

const recoveryQuiescentFailureReason = "recovery supervisor confirmed dead; worker and group quiescent"

func (e *Engine) progressRecoveryUnknown(ctx context.Context, r store.Run, result recoveryResult) error {
	switch result.kind {
	case recoveryAuthenticated:
		target := result.reportedState
		reason := "authenticated supervisor reported " + string(target)
		if r.CancelRequested && target == store.StateRunning {
			target = store.StateCancelling
			reason = "authenticated supervisor adopted with durable cancellation intent"
		}
		if !store.CanTransition(r.State, target) {
			return fmt.Errorf("cannot transition %q to authenticated state %q", r.State, target)
		}
		if err := e.store.Transition(ctx, r.ID, target, reason); err != nil {
			return err
		}
		if r.TranscriptPath != "" {
			e.startTailing(ctx, r.ID, r.TranscriptPath, r.TranscriptConsumedOffset)
		}
		return nil
	case recoverySupervisorDead:
		id := result.identity
		if id.WorkerPID <= 0 || id.SupervisorPGID <= 0 {
			return nil
		}
		if e.backend.ProcessAlive(id.WorkerPID) {
			return nil
		}
		members, err := e.backend.GroupMembers(id.SupervisorPGID)
		if err != nil {
			return nil
		}
		if len(members) != 0 {
			tracked, exited := e.localSupervisorStatus(r.ID, id.SupervisorPID)
			if !tracked || !exited {
				return nil
			}
			for _, memberPID := range members {
				if memberPID != id.SupervisorPID {
					return nil
				}
			}
		}
		complete, err := e.progressQuiescentTranscriptHandoff(ctx, r)
		if err != nil {
			return fmt.Errorf("progress recovery transcript handoff: %w", err)
		}
		if !complete {
			return nil
		}
		if err := e.store.CommitTerminal(ctx, r.ID, store.StateFailed,
			recoveryQuiescentFailureReason, store.OutboxRow{
				RunID:          r.ID,
				TerminalState:  store.StateFailed,
				SupervisorPID:  id.SupervisorPID,
				SupervisorPGID: id.SupervisorPGID,
				SocketPath:     id.SocketPath,
				Token:          id.Token,
			}); err != nil {
			return err
		}
		e.releaseTranscriptTail(r.ID)
	}
	return nil
}

func (e *Engine) rejectRecovery(ctx context.Context, r store.Run, diagnostic string) error {
	if store.IsTerminal(r.State) || r.State == store.StateRecoveryUnknown {
		return nil
	}
	if !store.CanTransition(r.State, store.StateRecoveryUnknown) {
		return fmt.Errorf("reject recovery for run %s: cannot transition %q to recovery_unknown", r.ID, r.State)
	}
	if err := e.store.Transition(ctx, r.ID, store.StateRecoveryUnknown, diagnostic); err != nil {
		current, getErr := e.store.GetRun(ctx, r.ID)
		if getErr == nil && (store.IsTerminal(current.State) || current.State == store.StateRecoveryUnknown) {
			return nil
		}
		return fmt.Errorf("reject recovery for run %s: %w", r.ID, err)
	}
	return nil
}

// recoveryLoop ticks periodically, checking active runs.
func (e *Engine) recoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.TickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

func (e *Engine) localSupervisorRunning(runID string) bool {
	e.mu.Lock()
	h := e.active[runID]
	e.mu.Unlock()
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return false
	}
	return !h.hasExited()
}

func (e *Engine) localSupervisorStatus(runID string, pid int) (tracked, exited bool) {
	e.mu.Lock()
	h := e.active[runID]
	e.mu.Unlock()
	if h == nil || h.cmd == nil || h.cmd.Process == nil || h.cmd.Process.Pid != pid {
		return false, false
	}
	return true, h.hasExited()
}

func (e *Engine) supervisorAlive(runID string, pid int) bool {
	if tracked, exited := e.localSupervisorStatus(runID, pid); tracked {
		return !exited
	}
	return e.backend.ProcessAlive(pid)
}

func (e *Engine) ensureSupervisorWatcher(h *runHandle) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return context.Canceled
	}
	factory := e.newExitWatcher
	if factory == nil {
		factory = newProcessExitWatcher
	}
	h.watcherMu.Lock()
	defer h.watcherMu.Unlock()
	if h.watcher != nil {
		return nil
	}
	watcher, err := factory(h.cmd.Process.Pid)
	if err != nil {
		return err
	}
	h.watcher = watcher
	return nil
}

func (e *Engine) retrySupervisorWatchers() {
	e.mu.Lock()
	handles := make([]*runHandle, 0, len(e.active))
	for _, h := range e.active {
		handles = append(handles, h)
	}
	e.mu.Unlock()
	for _, h := range handles {
		if h.hasWatcher() {
			continue
		}
		if err := e.ensureSupervisorWatcher(h); err != nil {
			e.reportBackgroundError(fmt.Errorf("watch local supervisor for run %s: %w", h.runID, err))
		}
	}
}

func (e *Engine) finalizeLocalSupervisors(ctx context.Context) {
	e.mu.Lock()
	handles := make([]*runHandle, 0, len(e.active))
	for _, h := range e.active {
		handles = append(handles, h)
	}
	e.mu.Unlock()

	for _, h := range handles {
		if !h.hasExited() {
			continue
		}
		r, err := e.store.GetRun(ctx, h.runID)
		if err != nil || !store.IsTerminal(r.State) {
			continue
		}
		outbox, err := e.store.GetOutbox(ctx, h.runID)
		if err != nil || outbox.Acknowledged == 0 {
			continue
		}
		e.releaseTranscriptTail(h.runID)
		_ = h.reap()
		if h.cmd.ProcessState == nil {
			continue
		}
		e.mu.Lock()
		if e.active[h.runID] == h {
			delete(e.active, h.runID)
		}
		e.mu.Unlock()
	}
}

// tick checks active runs: tails transcripts, detects dead supervisors, and
// reconciles pending outbox rows.
func (e *Engine) tick(ctx context.Context) {
	e.recoveryMu.Lock()
	defer e.recoveryMu.Unlock()
	e.retrySupervisorWatchers()

	runs, err := e.store.ListActiveRuns(ctx)
	if err != nil {
		e.reportBackgroundError(fmt.Errorf("list active runs: %w", err))
		return
	}
	e.releaseUnneededTranscriptTails(runs)
	for _, r := range runs {
		var inspected *recoveryResult
		if r.CancelRequested {
			if e.localSupervisorRunning(r.ID) {
				e.requestCleanup(ctx, r)
			} else {
				result := e.inspectRecoverySupervisor(ctx, r)
				inspected = &result
				if result.kind == recoveryAuthenticated {
					e.requestCleanup(ctx, r)
				}
			}
		}
		if r.State == store.StateRecoveryUnknown {
			result := e.inspectRecoverySupervisor(ctx, r)
			if inspected != nil {
				result = *inspected
			}
			if err := e.progressRecoveryUnknown(ctx, r, result); err != nil {
				e.reportBackgroundError(fmt.Errorf("progress recovery for run %s: %w", r.ID, err))
			}
			continue
		}

		// Tail transcripts for active runs.
		if r.TranscriptPath != "" && !store.IsTerminal(r.State) {
			e.tailTranscript(ctx, r.ID, r.TranscriptPath)
		}

		if r.State == store.StateCleanupRequired {
			e.progressCleanupRequired(ctx, r)
			continue
		}

		// Exact local watcher evidence is preferred by supervisorAlive. For a
		// non-child, PID absence can only move the run into the ambiguous
		// recovery state; it cannot authenticate identity or authorize cleanup.
		if r.SupervisorPID > 0 && !store.IsTerminal(r.State) {
			if !e.supervisorAlive(r.ID, r.SupervisorPID) {
				if store.CanTransition(r.State, store.StateRecoveryUnknown) {
					if err := e.store.Transition(ctx, r.ID, store.StateRecoveryUnknown, "supervisor unreachable"); err != nil {
						e.reportBackgroundError(fmt.Errorf("persist recovery_unknown for run %s: %w", r.ID, err))
					}
				}
			}
		}
	}

	if err := e.reconcilePendingOutbox(ctx); err != nil {
		e.reportBackgroundError(fmt.Errorf("reconcile pending outbox: %w", err))
		return
	}

	// Waiting is safe only after exact exit observation and durable terminal
	// finalization. Nonterminal crashed supervisors remain pinned as children.
	e.finalizeLocalSupervisors(ctx)
}

func (e *Engine) bindTranscriptTail(runID, path string, offset int64, expected os.FileInfo) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		return errors.New("engine is closing")
	}
	if current := e.tails[runID]; current != nil {
		info, err := current.file.Stat()
		if err != nil {
			return fmt.Errorf("stat open transcript for run %s: %w", runID, err)
		}
		if !os.SameFile(expected, info) || !info.Mode().IsRegular() || info.Size() != expected.Size() {
			return fmt.Errorf("open transcript identity or size changed for run %s", runID)
		}
		if current.offset != offset {
			if _, err := current.file.Seek(offset, io.SeekStart); err != nil {
				return fmt.Errorf("seek transcript for run %s to durable offset %d: %w", runID, offset, err)
			}
			current.reader.Reset(current.file)
			current.offset = offset
		}
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open transcript %q: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat opened transcript %q: %w", path, err)
	}
	if !os.SameFile(expected, info) || !info.Mode().IsRegular() || info.Size() != expected.Size() {
		_ = f.Close()
		return fmt.Errorf("transcript %q changed identity or size while opening", path)
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		return fmt.Errorf("seek transcript %q to durable offset %d: %w", path, offset, err)
	}
	e.tails[runID] = &transcriptTail{
		runID:  runID,
		path:   path,
		offset: offset,
		file:   f,
		reader: bufio.NewReader(f),
	}
	return nil
}

// progressQuiescentTranscriptHandoff takes transcript authority only after the
// caller has proven supervisor death, worker absence, and group quiescence.
// It seals and drains the named regular file, then revalidates its identity
// and size before terminal publication. Process-backed runs require the named
// file; only CommitNoProcessFailure may synthesize an empty transcript seal.
func (e *Engine) progressQuiescentTranscriptHandoff(ctx context.Context, r store.Run) (bool, error) {
	if !r.TranscriptRequired {
		return true, nil
	}
	if strings.TrimSpace(r.TranscriptPath) == "" {
		return false, errors.New("required transcript path is empty")
	}
	if err := r.TranscriptIdentity.Validate(); err != nil {
		return false, fmt.Errorf("required transcript identity is invalid: %w", err)
	}
	progress, err := e.store.GetTranscriptProgress(ctx, r.ID)
	if err != nil {
		return false, fmt.Errorf("load transcript progress for run %s: %w", r.ID, err)
	}
	pathInfo, err := os.Stat(r.TranscriptPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("required process transcript %q is missing", r.TranscriptPath)
	}
	if err != nil {
		return false, fmt.Errorf("stat transcript %q: %w", r.TranscriptPath, err)
	}
	if !pathInfo.Mode().IsRegular() {
		return false, fmt.Errorf("transcript %q is not a regular file", r.TranscriptPath)
	}
	if err := ValidateTranscriptIdentity(pathInfo, r.TranscriptIdentity); err != nil {
		return false, fmt.Errorf("named transcript %q does not match durable identity: %w", r.TranscriptPath, err)
	}
	finalSize := pathInfo.Size()
	if progress.ConsumedOffset > finalSize {
		return false, fmt.Errorf("transcript %q size %d is below consumed offset %d", r.TranscriptPath, finalSize, progress.ConsumedOffset)
	}
	if err := e.store.SealTranscript(ctx, r.ID, finalSize); err != nil {
		return false, fmt.Errorf("seal transcript for run %s at %d bytes: %w", r.ID, finalSize, err)
	}
	progress, err = e.store.GetTranscriptProgress(ctx, r.ID)
	if err != nil {
		return false, fmt.Errorf("reload transcript seal for run %s: %w", r.ID, err)
	}
	if progress.FinalSize != finalSize || progress.ConsumedOffset > finalSize {
		return false, fmt.Errorf("transcript progress for run %s does not match observed size %d: %+v", r.ID, finalSize, progress)
	}
	if err := e.bindTranscriptTail(r.ID, r.TranscriptPath, progress.ConsumedOffset, pathInfo); err != nil {
		return false, err
	}
	if progress.ConsumedOffset < finalSize {
		e.tailTranscript(ctx, r.ID, r.TranscriptPath)
	}
	progress, err = e.store.GetTranscriptProgress(ctx, r.ID)
	if err != nil {
		return false, fmt.Errorf("load accounted transcript progress for run %s: %w", r.ID, err)
	}
	postInfo, err := os.Stat(r.TranscriptPath)
	if err != nil {
		return false, fmt.Errorf("recheck transcript %q: %w", r.TranscriptPath, err)
	}
	e.mu.Lock()
	tail := e.tails[r.ID]
	var tailInfo os.FileInfo
	if tail != nil {
		tailInfo, err = tail.file.Stat()
	}
	e.mu.Unlock()
	if postInfo != nil {
		if identityErr := ValidateTranscriptIdentity(postInfo, r.TranscriptIdentity); identityErr != nil {
			return false, fmt.Errorf("rechecked transcript %q does not match durable identity: %w", r.TranscriptPath, identityErr)
		}
	}
	if tailInfo != nil {
		if identityErr := ValidateTranscriptIdentity(tailInfo, r.TranscriptIdentity); identityErr != nil {
			return false, fmt.Errorf("open transcript for run %s does not match durable identity: %w", r.ID, identityErr)
		}
	}
	if err != nil || tailInfo == nil || !os.SameFile(pathInfo, postInfo) || !os.SameFile(pathInfo, tailInfo) ||
		!postInfo.Mode().IsRegular() || postInfo.Size() != finalSize || tailInfo.Size() != finalSize {
		return false, fmt.Errorf("transcript %q changed identity or size during handoff", r.TranscriptPath)
	}
	return progress.FinalSize == finalSize && progress.ConsumedOffset == finalSize, nil
}

func (e *Engine) progressCleanupRequired(ctx context.Context, r store.Run) {
	if r.SupervisorPID <= 0 {
		return
	}
	if e.supervisorAlive(r.ID, r.SupervisorPID) {
		e.requestCleanup(ctx, r)
		return
	}

	// Once the group anchor is dead, observation is the only safe operation:
	// never signal the stored numeric PGID. Missing identity, a live worker,
	// surviving group members, or a failed enumeration keeps the durable
	// cleanup obligation nonterminal.
	if r.WorkerPID <= 0 || r.SupervisorPGID <= 0 || e.backend.ProcessAlive(r.WorkerPID) {
		return
	}
	members, err := e.backend.GroupMembers(r.SupervisorPGID)
	if err != nil || len(members) != 0 {
		return
	}
	complete, err := e.progressQuiescentTranscriptHandoff(ctx, r)
	if err != nil {
		e.reportBackgroundError(fmt.Errorf("progress cleanup transcript handoff for run %s: %w", r.ID, err))
		return
	}
	if !complete {
		return
	}

	if err := e.store.CommitTerminal(ctx, r.ID, store.StateFailed,
		"transcript corruption required group cleanup", store.OutboxRow{
			RunID:          r.ID,
			TerminalState:  store.StateFailed,
			SupervisorPID:  r.SupervisorPID,
			SupervisorPGID: r.SupervisorPGID,
			SocketPath:     r.SocketPath,
			Token:          r.Token,
		}); err != nil {
		e.reportBackgroundError(fmt.Errorf("persist cleanup terminal for run %s: %w", r.ID, err))
	} else {
		e.releaseTranscriptTail(r.ID)
	}
}

func (e *Engine) requestCleanup(_ context.Context, r store.Run) {
	if r.SocketPath == "" || r.Token == "" {
		return
	}
	e.mu.Lock()
	e.initRuntimeLocked()
	if e.closing {
		e.mu.Unlock()
		return
	}
	if _, requested := e.cleanupRequested[r.ID]; requested {
		e.mu.Unlock()
		return
	}
	delivery := e.cleanupDelivery
	if delivery == nil {
		delivery = func(ctx context.Context, socketPath, token string) (supCmdResponse, error) {
			return e.sendSupervisorCmd(ctx, socketPath, token, "cancel")
		}
	}
	e.cleanupRequested[r.ID] = struct{}{}
	workCtx := e.workCtx
	e.wg.Add(1)
	e.mu.Unlock()

	go func() {
		defer e.wg.Done()
		resp, err := delivery(workCtx, r.SocketPath, r.Token)
		if err == nil && resp.OK {
			return
		}
		e.mu.Lock()
		delete(e.cleanupRequested, r.ID)
		e.mu.Unlock()
	}()
}

// --- Transcript tailing ---

func (e *Engine) markTranscriptAuthorityFailure(ctx context.Context, r store.Run, reason string) {
	if store.IsTerminal(r.State) || r.State == store.StateCleanupRequired ||
		!store.CanTransition(r.State, store.StateCleanupRequired) {
		return
	}
	if err := e.store.Transition(ctx, r.ID, store.StateCleanupRequired, reason); err != nil {
		e.reportBackgroundError(fmt.Errorf("persist transcript authority failure for run %s: %w", r.ID, err))
	}
}

func (e *Engine) startTailing(ctx context.Context, runID, path string, offset int64) {
	r, err := e.store.GetRun(ctx, runID)
	if err != nil || store.IsTerminal(r.State) {
		return
	}
	waitingForPrelaunch := r.State == store.StateCreated && r.SupervisorPID == 0 && r.WorkerPID == 0
	if path != r.TranscriptPath {
		e.markTranscriptAuthorityFailure(ctx, r, "transcript path does not match durable run authority")
		return
	}
	if err := r.TranscriptIdentity.Validate(); err != nil {
		if waitingForPrelaunch {
			return
		}
		e.markTranscriptAuthorityFailure(ctx, r, "transcript durable file identity is missing or invalid")
		return
	}
	f, err := os.Open(path)
	if err != nil {
		if !waitingForPrelaunch {
			e.markTranscriptAuthorityFailure(ctx, r, "durable transcript path is unavailable")
		}
		return
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		e.markTranscriptAuthorityFailure(ctx, r, "cannot stat opened durable transcript")
		return
	}
	if err := ValidateTranscriptIdentity(info, r.TranscriptIdentity); err != nil {
		_ = f.Close()
		e.markTranscriptAuthorityFailure(ctx, r, "named transcript does not match durable file identity")
		return
	}
	if offset < 0 || offset > info.Size() {
		_ = f.Close()
		e.markTranscriptAuthorityFailure(ctx, r, "durable transcript offset is outside the named file")
		return
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		_ = f.Close()
		e.reportBackgroundError(fmt.Errorf("seek transcript %q to durable offset %d: %w", path, offset, err))
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closing {
		_ = f.Close()
		return
	}
	if _, ok := e.tails[runID]; ok {
		_ = f.Close()
		return
	}
	e.tails[runID] = &transcriptTail{
		runID:  runID,
		path:   path,
		offset: offset,
		file:   f,
		reader: bufio.NewReader(f),
	}
}

func (e *Engine) retryTranscriptLine(t *transcriptTail, persistErr error) {
	e.reportBackgroundError(persistErr)
	if _, err := t.file.Seek(t.offset, 0); err != nil {
		e.reportBackgroundError(fmt.Errorf("rewind transcript for run %s to offset %d: %w", t.runID, t.offset, err))
		return
	}
	t.reader.Reset(t.file)
}

// tailTranscript reads new lines from the transcript file and appends them as
// events to the store. If a complete line is not valid JSON, the run is
// transitioned to cleanup_required (ADR-0003 §3).
func (e *Engine) tailTranscript(ctx context.Context, runID, path string) {
	e.mu.Lock()
	t, ok := e.tails[runID]
	e.mu.Unlock()
	if !ok {
		e.startTailing(ctx, runID, path, 0)
		e.mu.Lock()
		t, ok = e.tails[runID]
		e.mu.Unlock()
		if !ok {
			return
		}
	}
	r, runErr := e.store.GetRun(ctx, runID)
	if runErr != nil || store.IsTerminal(r.State) {
		return
	}
	pathInfo, pathErr := os.Stat(path)
	tailInfo, tailErr := t.file.Stat()
	progress, _ := e.store.GetTranscriptProgress(ctx, runID)
	trackedOffset := t.offset
	if progress.ConsumedOffset > trackedOffset {
		trackedOffset = progress.ConsumedOffset
	}
	reason := ""
	if pathErr != nil {
		reason = "transcript path became unavailable while tailing"
	} else if err := ValidateTranscriptIdentity(pathInfo, r.TranscriptIdentity); err != nil {
		reason = "transcript path no longer matches durable file identity"
	} else if tailErr != nil {
		reason = "open transcript handle became unavailable while tailing"
	} else if err := ValidateTranscriptIdentity(tailInfo, r.TranscriptIdentity); err != nil {
		reason = "open transcript handle no longer matches durable file identity"
	} else if !os.SameFile(pathInfo, tailInfo) {
		reason = "transcript replaced while tailing"
	} else if pathInfo.Size() < trackedOffset {
		reason = "transcript truncated below committed offset"
	}
	if reason != "" {
		e.markTranscriptAuthorityFailure(ctx, r, reason)
		return
	}
	for {
		line, err := t.reader.ReadBytes('\n')
		if len(line) > 0 {
			lineOffset := t.offset + int64(len(line))
			completeLine := line[len(line)-1] == '\n'
			if !completeLine {
				if errors.Is(err, io.EOF) {
					progress, progressErr := e.store.GetTranscriptProgress(ctx, runID)
					if progressErr != nil {
						e.retryTranscriptLine(t, fmt.Errorf("load transcript seal for run %s: %w", runID, progressErr))
						return
					}
					completeLine = progress.FinalSize >= 0 && lineOffset == progress.FinalSize
				}
				if !completeLine {
					e.retryTranscriptLine(t, nil)
					return
				}
			}

			trimmed := line
			if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
				trimmed = trimmed[:len(trimmed)-1]
			}
			if len(trimmed) == 0 {
				if err := e.store.AdvanceTranscriptConsumed(ctx, runID, lineOffset); err != nil {
					e.retryTranscriptLine(t, fmt.Errorf("advance transcript consumption for run %s to %d: %w", runID, lineOffset, err))
					return
				}
				t.offset = lineOffset
				continue
			}
			// Parse the JSON line.
			var evt struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			perr := json.Unmarshal(trimmed, &evt)
			if perr == nil && strings.TrimSpace(evt.Type) == "" {
				perr = errors.New("transcript event type is missing or blank")
			}
			payload := bytes.TrimSpace(evt.Payload)
			if perr == nil && (len(payload) == 0 || bytes.Equal(payload, []byte("null"))) {
				perr = errors.New("transcript event payload is missing or null")
			}
			if perr != nil {
				// Newline framing, or an exact durable final-size seal, was proven
				// before parsing. A malformed envelope is therefore complete.
				if completeLine {
					// A complete malformed envelope is durable transcript corruption.
					r, runErr := e.store.GetRun(ctx, runID)
					if runErr != nil {
						e.retryTranscriptLine(t, fmt.Errorf("load run %s for transcript corruption: %w", runID, runErr))
						return
					}
					if store.IsTerminal(r.State) {
						t.offset = lineOffset
						continue
					}
					var persistErr error
					if mutationHooks.terminalWhileAlive {
						persistErr = e.store.CommitTerminal(ctx, runID, store.StateFailed,
							"mutation: terminal while worker alive", store.OutboxRow{
								RunID:          r.ID,
								TerminalState:  store.StateFailed,
								SupervisorPID:  r.SupervisorPID,
								SupervisorPGID: r.SupervisorPGID,
								SocketPath:     r.SocketPath,
								Token:          r.Token,
							})
						if persistErr != nil {
							persistErr = fmt.Errorf("persist mutation terminal for run %s: %w", runID, persistErr)
						}
					} else if r.State != store.StateCleanupRequired {
						if store.CanTransition(r.State, store.StateCleanupRequired) {
							persistErr = e.store.Transition(ctx, runID, store.StateCleanupRequired, "transcript corruption: malformed event envelope")
							if persistErr != nil {
								persistErr = fmt.Errorf("persist transcript cleanup_required for run %s: %w", runID, persistErr)
							}
						} else {
							persistErr = fmt.Errorf("persist transcript cleanup_required for run %s: cannot transition from %q", runID, r.State)
						}
					}
					if persistErr != nil {
						e.retryTranscriptLine(t, persistErr)
						return
					}
					if err := e.store.AdvanceTranscriptConsumed(ctx, runID, lineOffset); err != nil {
						e.retryTranscriptLine(t, fmt.Errorf("account malformed transcript record for run %s to %d: %w", runID, lineOffset, err))
						return
					}
					t.offset = lineOffset
					continue
				}
				// Partial line (no newline) — put it back for next time.
				// Seek back so we re-read it on the next tick.
				if _, serr := t.file.Seek(t.offset, 0); serr == nil {
					t.reader.Reset(t.file)
				}
				return
			}
			// Valid non-null JSON payloads may be objects, arrays, or scalars.
			if _, err := e.store.AppendEvent(ctx, runID, evt.Type, evt.Payload, lineOffset); err != nil {
				e.retryTranscriptLine(t, fmt.Errorf("append transcript event for run %s at offset %d: %w", runID, lineOffset, err))
				return
			}
			t.offset = lineOffset
		}
		if err != nil {
			return // EOF or error; will retry on next tick
		}
	}
}

// --- API server ---

type apiRequest struct {
	Cmd          string   `json:"cmd"`
	Token        string   `json:"token"`
	ID           string   `json:"id,omitempty"`
	Name         string   `json:"name,omitempty"`
	Root         string   `json:"root,omitempty"`
	ProjectID    string   `json:"project_id,omitempty"`
	WorkstreamID string   `json:"workstream_id,omitempty"`
	WorkerPath   string   `json:"worker_path,omitempty"`
	WorkerArgs   []string `json:"worker_args,omitempty"`
	WorkerEnv    []string `json:"worker_env,omitempty"`
	AfterSeq     int64    `json:"after_seq,omitempty"`
}

type apiResponse struct {
	OK       bool        `json:"ok"`
	Error    string      `json:"error,omitempty"`
	State    string      `json:"state,omitempty"`
	Run      *jsonRun    `json:"run,omitempty"`
	Runs     []jsonRun   `json:"runs,omitempty"`
	Events   []jsonEvent `json:"events,omitempty"`
	Accepted bool        `json:"accepted,omitempty"`
}

type jsonRun struct {
	ID              string `json:"id"`
	ProjectID       string `json:"project_id"`
	WorkstreamID    string `json:"workstream_id"`
	State           string `json:"state"`
	WorkerPID       int    `json:"worker_pid"`
	SupervisorPID   int    `json:"supervisor_pid"`
	CommittedOffset int64  `json:"committed_offset"`
}

type jsonEvent struct {
	Seq     int64           `json:"seq"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (e *Engine) handleAPIConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	var req apiRequest
	if err := dec.Decode(&req); err != nil {
		_ = enc.Encode(apiResponse{OK: false, Error: "bad request: " + err.Error()})
		return
	}
	if req.Token != e.cfg.Token {
		_ = enc.Encode(apiResponse{OK: false, Error: "auth failed"})
		return
	}
	resp := e.handleCmd(ctx, &req)
	_ = enc.Encode(resp)
}

func (e *Engine) handleCmd(ctx context.Context, req *apiRequest) apiResponse {
	switch req.Cmd {
	case "ping":
		return apiResponse{OK: true}
	case "create-project":
		if err := e.store.CreateProject(ctx, req.ID, req.Name, req.Root); err != nil {
			return apiResponse{OK: false, Error: err.Error()}
		}
		return apiResponse{OK: true}
	case "create-workstream":
		if err := e.store.CreateWorkstream(ctx, req.ID, req.ProjectID, req.Name); err != nil {
			return apiResponse{OK: false, Error: err.Error()}
		}
		return apiResponse{OK: true}
	case "launch-run":
		return e.handleLaunchRun(ctx, req)
	case "get-run":
		return e.handleGetRun(ctx, req)
	case "list-runs":
		return e.handleListRuns(ctx, req)
	case "list-events":
		return e.handleListEvents(ctx, req)
	case "cancel-run":
		return e.handleCancelRun(ctx, req)
	default:
		return apiResponse{OK: false, Error: "unknown command: " + req.Cmd}
	}
}

func (e *Engine) handleLaunchRun(ctx context.Context, req *apiRequest) apiResponse {
	runID := req.ID
	if runID == "" {
		return apiResponse{OK: false, Error: "run id required"}
	}
	if req.WorkerPath == "" {
		return apiResponse{OK: false, Error: "worker_path required"}
	}
	runDir := filepath.Join(e.cfg.DataDir, runID)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return apiResponse{OK: false, Error: "create run dir: " + err.Error()}
	}
	identityPath := filepath.Join(runDir, "identity.json")
	socketPath := filepath.Join(runDir, "supervisor.sock")
	transcriptPath := filepath.Join(runDir, "transcript.ndjson")
	tokenReader := e.tokenReader
	if tokenReader == nil {
		tokenReader = rand.Reader
	}
	runToken, err := generateToken(tokenReader)
	if err != nil {
		return apiResponse{OK: false, Error: "generate run auth token: " + err.Error()}
	}
	workerEnv := make([]string, 0, len(req.WorkerEnv)+1)
	workerEnv = append(workerEnv, req.WorkerEnv...)
	workerEnv = append(workerEnv, "ANANKE_FW_TRANSCRIPT="+transcriptPath)
	spec := store.RunSpec{
		WorkerPath:         req.WorkerPath,
		WorkerArgs:         req.WorkerArgs,
		WorkerEnv:          workerEnv,
		TranscriptPath:     transcriptPath,
		SocketPath:         socketPath,
		Token:              runToken,
		IdentityPath:       identityPath,
		TranscriptRequired: true,
	}
	if err := e.store.CreateRun(ctx, runID, req.ProjectID, req.WorkstreamID, spec); err != nil {
		return apiResponse{OK: false, Error: "create run: " + err.Error()}
	}

	// Fork the supervisor binary.
	args := []string{
		"-store", e.cfg.StorePath,
		"-run", runID,
		"-worker", req.WorkerPath,
		"-identity", identityPath,
		"-socket", socketPath,
		"-transcript", transcriptPath,
		"-token", spec.Token,
	}
	cmd := exec.Command(e.cfg.SupervisorBin, args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		launchErr := fmt.Errorf("launch supervisor: %w", err)
		if persistErr := e.store.CommitNoProcessFailure(context.WithoutCancel(ctx), runID,
			"supervisor launch failed: "+err.Error()); persistErr != nil {
			return apiResponse{OK: false, Error: errors.Join(
				launchErr, fmt.Errorf("persist no-process failure: %w", persistErr)).Error()}
		}
		return apiResponse{OK: false, Error: launchErr.Error()}
	}
	handle := &runHandle{
		runID:    runID,
		cmd:      cmd,
		reapDone: make(chan struct{}),
		identity: identityPath,
		socket:   socketPath,
	}
	// Ownership is recorded before watcher construction. A watcher failure is
	// monitoring degradation, not authority to kill the supervisor: it may
	// already be anchoring a live worker group.
	e.mu.Lock()
	e.active[runID] = handle
	e.mu.Unlock()
	if err := e.ensureSupervisorWatcher(handle); err != nil {
		e.reportBackgroundError(fmt.Errorf("watch local supervisor for run %s: %w", runID, err))
	}

	return apiResponse{OK: true, Run: runToJSON(store.Run{
		ID:           runID,
		ProjectID:    req.ProjectID,
		WorkstreamID: req.WorkstreamID,
		State:        store.StateCreated,
	})}
}

func (e *Engine) handleGetRun(ctx context.Context, req *apiRequest) apiResponse {
	r, err := e.store.GetRun(ctx, req.ID)
	if err != nil {
		return apiResponse{OK: false, Error: err.Error()}
	}
	return apiResponse{OK: true, Run: runToJSON(r), State: string(r.State)}
}

func (e *Engine) handleListRuns(ctx context.Context, req *apiRequest) apiResponse {
	if req.ProjectID == "" {
		return apiResponse{OK: false, Error: "project_id required"}
	}
	runs, err := e.store.ListRunsByProject(ctx, req.ProjectID)
	if err != nil {
		return apiResponse{OK: false, Error: err.Error()}
	}
	jsonRuns := make([]jsonRun, 0, len(runs))
	for _, run := range runs {
		if req.WorkstreamID != "" && run.WorkstreamID != req.WorkstreamID {
			continue
		}
		jsonRuns = append(jsonRuns, *runToJSON(run))
	}
	return apiResponse{OK: true, Runs: jsonRuns}
}

func (e *Engine) handleListEvents(ctx context.Context, req *apiRequest) apiResponse {
	events, err := e.store.ListEvents(ctx, req.ID, req.AfterSeq)
	if err != nil {
		return apiResponse{OK: false, Error: err.Error()}
	}
	je := make([]jsonEvent, 0, len(events))
	for _, ev := range events {
		payload := json.RawMessage(ev.Payload)
		if len(payload) == 0 {
			payload = json.RawMessage("null")
		}
		je = append(je, jsonEvent{
			Seq:     ev.Seq,
			Type:    ev.Type,
			Payload: payload,
		})
	}
	return apiResponse{OK: true, Events: je}
}

func (e *Engine) handleCancelRun(ctx context.Context, req *apiRequest) apiResponse {
	run, err := e.store.GetRun(ctx, req.ID)
	if err != nil {
		return apiResponse{OK: false, Error: err.Error()}
	}
	state, err := e.store.RequestCancellation(ctx, req.ID)
	if err != nil {
		return apiResponse{OK: false, Error: err.Error()}
	}
	run.State = state
	run.CancelRequested = true
	e.requestCleanup(ctx, run)
	return apiResponse{OK: true, Accepted: true, State: string(state)}
}

// supCmdResponse matches the supervisor's cmdResponse.
type supCmdResponse struct {
	OK               bool   `json:"ok"`
	Error            string `json:"error,omitempty"`
	State            string `json:"state,omitempty"`
	RunID            string `json:"run_id,omitempty"`
	SupervisorPID    int    `json:"supervisor_pid,omitempty"`
	WorkerPID        int    `json:"worker_pid,omitempty"`
	ExitCode         int    `json:"exit_code,omitempty"`
	PGID             int    `json:"pgid,omitempty"`
	TranscriptDevice int64  `json:"transcript_device,omitempty"`
	TranscriptInode  int64  `json:"transcript_inode,omitempty"`
}

func (e *Engine) sendSupervisorCmd(ctx context.Context, socketPath, token, cmd string) (supCmdResponse, error) {
	if strings.TrimSpace(socketPath) == "" {
		return supCmdResponse{}, fmt.Errorf("supervisor socket path is empty")
	}
	if strings.TrimSpace(token) == "" {
		return supCmdResponse{}, fmt.Errorf("supervisor token is empty")
	}
	if strings.TrimSpace(cmd) == "" {
		return supCmdResponse{}, fmt.Errorf("supervisor command is empty")
	}
	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(commandCtx, "unix", socketPath)
	if err != nil {
		return supCmdResponse{}, err
	}
	defer conn.Close()
	if deadline, ok := commandCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	interrupt := make(chan struct{})
	interrupted := make(chan struct{})
	go func() {
		defer close(interrupted)
		select {
		case <-commandCtx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-interrupt:
		}
	}()
	defer func() {
		close(interrupt)
		<-interrupted
	}()
	if err := json.NewEncoder(conn).Encode(map[string]string{"cmd": cmd, "token": token}); err != nil {
		return supCmdResponse{}, err
	}
	var resp supCmdResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return supCmdResponse{}, err
	}
	return resp, nil
}

func runToJSON(r store.Run) *jsonRun {
	return &jsonRun{
		ID:              r.ID,
		ProjectID:       r.ProjectID,
		WorkstreamID:    r.WorkstreamID,
		State:           string(r.State),
		WorkerPID:       r.WorkerPID,
		SupervisorPID:   r.SupervisorPID,
		CommittedOffset: r.CommittedOffset,
	}
}
