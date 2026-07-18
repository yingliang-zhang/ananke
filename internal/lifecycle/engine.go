// Package lifecycle also hosts the daemon engine, which manages the store,
// launches supervisors, exposes a Unix-socket JSON API, and runs the recovery
// loop described in ADR-0003 §2-3.
package lifecycle

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
}

// Engine is the daemon core: it owns the store, launches supervisor subprocesses,
// serves a Unix-socket JSON API, and runs a recovery loop that monitors active
// runs, tails transcripts, and reconciles pending finalization outbox rows.
type Engine struct {
	cfg      EngineConfig
	store    *store.Store
	backend  LifecycleBackend
	mu       sync.Mutex
	active   map[string]*runHandle
	tails    map[string]*transcriptTail
	listener net.Listener
	closed   bool
}

// runHandle tracks a launched supervisor subprocess.
type runHandle struct {
	runID    string
	cmd      *exec.Cmd
	identity string
	socket   string
}

// transcriptTail tracks the read position in a run's transcript file.
type transcriptTail struct {
	runID  string
	path   string
	offset int64
	file   *os.File
	reader *bufio.Reader
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
		cfg.Token = generateToken()
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
	return &Engine{
		cfg:     cfg,
		store:   st,
		backend: NewDarwinBackend(),
		active:  make(map[string]*runHandle),
		tails:   make(map[string]*transcriptTail),
	}, nil
}

// generateToken returns a random 16-byte hex token.
func generateToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Token returns the engine's auth token (needed by API clients).
func (e *Engine) Token() string { return e.cfg.Token }

// Store returns the underlying store (for tests).
func (e *Engine) Store() *store.Store { return e.store }

// Close stops the engine and releases resources.
func (e *Engine) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()
	if e.listener != nil {
		_ = e.listener.Close()
	}
	e.mu.Lock()
	for _, t := range e.tails {
		_ = t.file.Close()
	}
	e.mu.Unlock()
	return e.store.Close()
}

// Run starts the API server and recovery loop. It blocks until ctx is done.
func (e *Engine) Run(ctx context.Context) error {
	// Startup reconciliation.
	if err := e.Recover(ctx); err != nil {
		return fmt.Errorf("engine: recover: %w", err)
	}

	// Listen on the API socket.
	_ = os.Remove(e.cfg.SocketPath)
	l, err := net.Listen("unix", e.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("engine: listen %s: %w", e.cfg.SocketPath, err)
	}
	e.mu.Lock()
	e.listener = l
	e.mu.Unlock()
	defer os.Remove(e.cfg.SocketPath)

	// Recovery loop.
	go e.recoveryLoop(ctx)

	// Accept loop.
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go e.handleAPIConn(ctx, conn)
		}
	}()

	<-ctx.Done()
	return nil
}

// --- Recovery ---

// Recover performs startup reconciliation: it processes pending outbox rows
// and checks active runs for dead supervisors.
func (e *Engine) Recover(ctx context.Context) error {
	// Process pending outbox rows.
	pending, err := e.store.ListPendingOutbox(ctx)
	if err != nil {
		return fmt.Errorf("list pending outbox: %w", err)
	}
	for _, row := range pending {
		if row.SupervisorPID > 0 && !e.backend.ProcessAlive(row.SupervisorPID) {
			// Supervisor is dead; mark outbox as abandoned.
			_ = e.store.AbandonOutbox(ctx, row.RunID, "supervisor dead on startup")
			continue
		}
		// Supervisor is alive (or pid unknown); try to acknowledge.
		_ = e.store.AcknowledgeOutbox(ctx, row.RunID)
	}

	// Check active runs for dead supervisors.
	runs, err := e.store.ListActiveRuns(ctx)
	if err != nil {
		return fmt.Errorf("list active runs: %w", err)
	}
	for _, r := range runs {
		if r.SupervisorPID > 0 && !e.backend.ProcessAlive(r.SupervisorPID) {
			// Supervisor is dead. Transition to recovery_unknown if currently
			// in a nonterminal state.
			if !store.IsTerminal(r.State) && r.State != store.StateRecoveryUnknown {
				if store.CanTransition(r.State, store.StateRecoveryUnknown) {
					_ = e.store.Transition(ctx, r.ID, store.StateRecoveryUnknown, "supervisor dead on startup")
				}
			}
		}
		// Start tailing transcripts for active runs that have a transcript path.
		if r.TranscriptPath != "" && !store.IsTerminal(r.State) {
			// M5 mutation: reset offset to 0 on reconnect, causing duplicate
			// events instead of resuming from the committed offset.
			off := r.CommittedOffset
			if mutationHooks.resetOffset {
				off = 0
			}
			e.startTailing(ctx, r.ID, r.TranscriptPath, off)
		}
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

// tick checks active runs: tails transcripts, detects dead supervisors, and
// reconciles pending outbox rows.
func (e *Engine) tick(ctx context.Context) {
	runs, err := e.store.ListActiveRuns(ctx)
	if err != nil {
		return
	}
	for _, r := range runs {
		// Tail transcripts for active runs.
		if r.TranscriptPath != "" && !store.IsTerminal(r.State) {
			e.tailTranscript(ctx, r.ID, r.TranscriptPath)
		}

		// Detect dead supervisors for nonterminal runs.
		if r.SupervisorPID > 0 && !store.IsTerminal(r.State) {
			if !e.backend.ProcessAlive(r.SupervisorPID) {
				if r.State != store.StateRecoveryUnknown && store.CanTransition(r.State, store.StateRecoveryUnknown) {
					_ = e.store.Transition(ctx, r.ID, store.StateRecoveryUnknown, "supervisor unreachable")
				}
			}
		}
	}

	// Reconcile pending outbox rows.
	pending, err := e.store.ListPendingOutbox(ctx)
	if err != nil {
		return
	}
	for _, row := range pending {
		if row.SupervisorPID > 0 && !e.backend.ProcessAlive(row.SupervisorPID) {
			_ = e.store.AbandonOutbox(ctx, row.RunID, "supervisor dead during tick")
		} else {
			_ = e.store.AcknowledgeOutbox(ctx, row.RunID)
		}
	}
}

// --- Transcript tailing ---

func (e *Engine) startTailing(ctx context.Context, runID, path string, offset int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.tails[runID]; ok {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		// Transcript file doesn't exist yet; will retry on next tick.
		return
	}
	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			_ = f.Close()
			return
		}
	}
	t := &transcriptTail{
		runID:  runID,
		path:   path,
		offset: offset,
		file:   f,
		reader: bufio.NewReader(f),
	}
	e.tails[runID] = t
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
	for {
		line, err := t.reader.ReadBytes('\n')
		if len(line) > 0 {
			lineOffset := t.offset + int64(len(line))
			trimmed := line
			if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
				trimmed = trimmed[:len(trimmed)-1]
			}
			if len(trimmed) == 0 {
				t.offset = lineOffset
				continue
			}
			// Parse the JSON line.
			var evt struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if perr := json.Unmarshal(trimmed, &evt); perr != nil {
				// Not valid JSON — could be a partial line (no newline) or
				// genuine corruption. Only treat as corruption if we read a
				// complete line (ended with \n).
				if line[len(line)-1] == '\n' {
					// Complete line but invalid JSON → corruption.
					r, _ := e.store.GetRun(ctx, runID)
					if !store.IsTerminal(r.State) && r.State != store.StateCleanupRequired {
						// M4 mutation: go directly to failed instead of
						// cleanup_required, bypassing authenticated quiescence.
						target := store.StateCleanupRequired
						if mutationHooks.terminalWhileAlive {
							target = store.StateFailed
						}
						if store.CanTransition(r.State, target) {
							_ = e.store.Transition(ctx, runID, target, "transcript corruption: invalid JSON line")
						}
					}
					t.offset = lineOffset
					continue
				}
				// Partial line (no newline) — put it back for next time.
				// Seek back so we re-read it on the next tick.
				if _, serr := t.file.Seek(t.offset, 0); serr == nil {
					t.reader = bufio.NewReader(t.file)
				}
				return
			}
			// Valid event — append to store.
			typ := evt.Type
			if typ == "" {
				typ = "event"
			}
			_, _ = e.store.AppendEvent(ctx, runID, typ, evt.Payload, lineOffset)
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
	workerEnv := make([]string, 0, len(req.WorkerEnv)+1)
	workerEnv = append(workerEnv, req.WorkerEnv...)
	workerEnv = append(workerEnv, "ANANKE_FW_TRANSCRIPT="+transcriptPath)
	spec := store.RunSpec{
		WorkerPath:     req.WorkerPath,
		WorkerArgs:     req.WorkerArgs,
		WorkerEnv:      workerEnv,
		TranscriptPath: transcriptPath,
		SocketPath:     socketPath,
		Token:          generateToken(),
		IdentityPath:   identityPath,
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
		_ = e.store.Transition(ctx, runID, store.StateFailed, "supervisor launch failed: "+err.Error())
		return apiResponse{OK: false, Error: "launch supervisor: " + err.Error()}
	}
	e.mu.Lock()
	e.active[runID] = &runHandle{
		runID:    runID,
		cmd:      cmd,
		identity: identityPath,
		socket:   socketPath,
	}
	e.mu.Unlock()

	// Start tailing the transcript.
	e.startTailing(ctx, runID, transcriptPath, 0)

	// Reap the supervisor in the background (don't block the API).
	go func() { _, _ = cmd.Process.Wait() }()

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
	r, err := e.store.GetRun(ctx, req.ID)
	if err != nil {
		return apiResponse{OK: false, Error: err.Error()}
	}
	if store.IsTerminal(r.State) {
		return apiResponse{OK: false, Error: "run is already terminal: " + string(r.State)}
	}
	// Connect to the supervisor socket and send cancel.
	resp, err := e.sendSupervisorCmd(r.SocketPath, r.Token, "cancel")
	if err != nil {
		return apiResponse{OK: false, Error: "supervisor unreachable: " + err.Error()}
	}
	if !resp.OK {
		return apiResponse{OK: false, Error: "supervisor error: " + resp.Error}
	}
	// Asynchronous: return accepted immediately.
	return apiResponse{OK: true, Accepted: true, State: "cancelling"}
}

// supCmdResponse matches the supervisor's cmdResponse.
type supCmdResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	State     string `json:"state,omitempty"`
	WorkerPID int    `json:"worker_pid,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	PGID      int    `json:"pgid,omitempty"`
}

func (e *Engine) sendSupervisorCmd(socketPath, token, cmd string) (supCmdResponse, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return supCmdResponse{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	req, _ := json.Marshal(map[string]string{"cmd": cmd, "token": token})
	_, _ = conn.Write(req)
	dec := json.NewDecoder(conn)
	var resp supCmdResponse
	if err := dec.Decode(&resp); err != nil {
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
