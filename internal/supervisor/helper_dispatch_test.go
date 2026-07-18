package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"golang.org/x/sys/unix"
)

// runSupHelper dispatches a supervisor subprocess helper mode.
func runSupHelper(mode string) {
	switch mode {
	case "supervisor":
		supervisorHelper()
	default:
		os.Exit(2)
	}
}

// supervisorHelper is the real supervisor subprocess. It reads config from env,
// constructs a supervisor.Config, runs it, and writes the terminal state +
// identity to a result file.
func supervisorHelper() {
	storePath := os.Getenv("ANANKE_SUP_STORE")
	runID := os.Getenv("ANANKE_SUP_RUN")
	identityPath := os.Getenv("ANANKE_SUP_IDENTITY")
	socketPath := os.Getenv("ANANKE_SUP_SOCKET")
	token := os.Getenv("ANANKE_SUP_TOKEN")
	resultPath := os.Getenv("ANANKE_SUP_RESULT")
	transcriptPath := os.Getenv("ANANKE_SUP_TRANSCRIPT")
	graceMS := 500
	if v := os.Getenv("ANANKE_SUP_GRACE_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			graceMS = n
		}
	}

	// The worker is either the fakeworker binary (if ANANKE_SUP_WORKER is set)
	// or os.Args[0] re-execed in fakeworker mode.
	workerPath := os.Getenv("ANANKE_SUP_WORKER")
	// Build the worker env: copy the current env but strip all ANANKE_SUP_*
	// vars (so the worker doesn't re-enter supervisorHelper mode) and add the
	// fakeworker helper marker.
	workerEnv := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "ANANKE_SUP_") {
			continue
		}
		workerEnv = append(workerEnv, e)
	}
	workerEnv = append(workerEnv, fwHelperEnv+"=fakeworker")
	if workerPath == "" {
		workerPath = os.Args[0]
	}

	cfg := Config{
		StorePath:      storePath,
		RunID:          runID,
		WorkerPath:     workerPath,
		WorkerEnv:      workerEnv,
		IdentityPath:   identityPath,
		SocketPath:     socketPath,
		TranscriptPath: transcriptPath,
		Token:          token,
		GracePeriod:    time.Duration(graceMS) * time.Millisecond,
	}
	ctx := context.Background()
	s, err := New(ctx, cfg)
	if err != nil {
		writeSupResult(resultPath, supResult{Err: err.Error()})
		os.Exit(1)
	}
	terminal, err := s.Run(ctx)
	if err != nil {
		writeSupResult(resultPath, supResult{
			TerminalState: string(terminal),
			Err:           err.Error(),
		})
		os.Exit(1)
	}
	id, _ := lifecycle.ReadIdentity(identityPath)
	writeSupResult(resultPath, supResult{
		TerminalState: string(terminal),
		WorkerPID:     id.WorkerPID,
		SupervisorPID: id.SupervisorPID,
		PGID:          id.SupervisorPGID,
	})
}

func writeSupResult(path string, r supResult) {
	if path == "" {
		return
	}
	data, _ := json.Marshal(r)
	_ = os.WriteFile(path, data, 0o600)
}

// runFWHelper dispatches a fakeworker subprocess helper mode.
func runFWHelper(mode string) {
	switch mode {
	case "fakeworker":
		fakeworkerHelper()
	default:
		os.Exit(2)
	}
}

// fakeworkerHelper mirrors cmd/ananke-fakeworker's behaviour: writes NDJSON
// transcript events, can spawn children, and exits with a configured code. It
// does NOT call setpgid (inherits the supervisor's process group).
func fakeworkerHelper() {
	transcriptPath := os.Getenv("ANANKE_FW_TRANSCRIPT")
	nEvents := envIntDef("ANANKE_FW_EVENTS", 3)
	exitCode := envIntDef("ANANKE_FW_EXIT", 0)
	delayMs := envIntDef("ANANKE_FW_DELAY_MS", 0)
	partial := os.Getenv("ANANKE_FW_PARTIAL") == "1"
	noFinalNL := os.Getenv("ANANKE_FW_NO_FINAL_NL") == "1"
	spawnChild := os.Getenv("ANANKE_FW_SPAWN_CHILD") == "1"
	childMode := os.Getenv("ANANKE_FW_CHILD_MODE")
	if childMode == "" {
		childMode = "normal"
	}
	childPIDFile := os.Getenv("ANANKE_FW_CHILD_PID_FILE")

	var childPID int
	if spawnChild {
		childPID = fwSpawnChild(childMode, childPIDFile)
	}

	if transcriptPath != "" {
		fwWriteTranscript(transcriptPath, nEvents, delayMs, partial, noFinalNL)
	}

	if spawnChild && childMode == "normal" && childPID > 0 {
		// Give the normal child time to exit on its own.
		time.Sleep(200 * time.Millisecond)
	}

	// Sleep before exit so the supervisor stays alive long enough for
	// tests to issue socket commands. ANANKE_FW_EXIT_DELAY_MS is the
	// pre-exit sleep; ANANKE_FW_DELAY_MS is only inter-event spacing.
	exitDelayMs := envIntDef("ANANKE_FW_EXIT_DELAY_MS", delayMs)
	if exitDelayMs > 0 {
		time.Sleep(time.Duration(exitDelayMs) * time.Millisecond)
	}

	os.Exit(exitCode)
}

func fwWriteTranscript(path string, nEvents, delayMs int, partial, noFinalNL bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	for i := 1; i <= nEvents; i++ {
		ev := map[string]any{
			"seq":  i,
			"type": "message",
			"text": fmt.Sprintf("event %d", i),
			"ts":   time.Now().UTC().Format(time.RFC3339Nano),
		}
		data, _ := json.Marshal(ev)
		suffix := "\n"
		if i == nEvents && noFinalNL {
			suffix = ""
		}
		if partial && i == 2 {
			half := len(data) / 2
			f.Write(data[:half])
			f.Sync()
			if delayMs > 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
			f.Write(data[half:])
			f.WriteString(suffix)
		} else {
			f.Write(data)
			f.WriteString(suffix)
		}
		f.Sync()
		if delayMs > 0 && i < nEvents {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}
}

func fwSpawnChild(mode, pidFile string) int {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "ANANKE_FW_CHILD_ACTIVE="+mode)
	cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: false} // inherit our PGID
	if err := cmd.Start(); err != nil {
		return 0
	}
	pid := cmd.Process.Pid
	if pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600)
	}
	return pid
}

// init handles the spawned-child mode: if ANANKE_FW_CHILD_ACTIVE is set, this
// process is a spawned child. "resistant" children ignore SIGTERM and sleep
// until SIGKILL; "normal" children sleep briefly and exit.
func init() {
	mode := os.Getenv("ANANKE_FW_CHILD_ACTIVE")
	if mode == "" {
		return
	}
	switch mode {
	case "resistant":
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, unix.SIGTERM)
		go func() {
			for range ch {
				// swallow SIGTERM
			}
		}()
		time.Sleep(5 * time.Minute)
		os.Exit(0)
	default: // "normal"
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}
}

func envIntDef(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
