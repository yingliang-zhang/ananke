package supervisor

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/store"
	"golang.org/x/sys/unix"
)

const (
	supHelperEnv = "ANANKE_SUP_HELPER"
	fwHelperEnv  = "ANANKE_FW_HELPER"
)

// TestMain dispatches helper subprocess modes for the supervisor test binary.
func TestMain(m *testing.M) {
	if mode := os.Getenv(supHelperEnv); mode != "" {
		runSupHelper(mode)
		os.Exit(0)
	}
	if mode := os.Getenv(fwHelperEnv); mode != "" {
		runFWHelper(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// supEnv holds the env vars passed to a supervisor helper.
type supEnv struct {
	StorePath      string
	RunID          string
	WorkerPath     string // if "", re-exec os.Args[0] in fakeworker mode
	IdentityPath   string
	SocketPath     string
	TranscriptPath string
	Token          string
	GraceMS        int
	// Fakeworker config (passed as ANANKE_FW_* to the worker):
	FWEvents       int
	FWExit         int
	FWDelayMS      int
	FWPartial      bool
	FWNoNL         bool
	FWSpawnChild   bool
	FWChildMode    string // "normal" or "resistant"
	FWChildPIDFile string
}

// forkSupervisor re-executes the test binary as a supervisor helper. Returns
// the cmd, the socket path (for client connections), the result-file path, and
// the identity-file path.
func forkSupervisor(t *testing.T, storePath, runID string, env supEnv) (*exec.Cmd, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	// Use a short socket path: macOS limits unix socket paths to 104 bytes,
	// and t.TempDir() is already very long. Use a unique short path under /tmp.
	shortSocket := filepath.Join("/tmp", "anankesup-"+strconv.Itoa(int(time.Now().UnixNano()%100000))+"-"+runID+".sock")
	if env.IdentityPath == "" {
		env.IdentityPath = filepath.Join(dir, "identity.json")
	}
	if env.SocketPath == "" {
		env.SocketPath = shortSocket
	}
	if env.Token == "" {
		env.Token = "test-token"
	}
	if env.GraceMS == 0 {
		env.GraceMS = 500
	}
	resultPath := filepath.Join(dir, "supervisor_result.json")

	args := []string{
		supHelperEnv + "=supervisor",
		"ANANKE_SUP_STORE=" + storePath,
		"ANANKE_SUP_RUN=" + runID,
		"ANANKE_SUP_IDENTITY=" + env.IdentityPath,
		"ANANKE_SUP_SOCKET=" + env.SocketPath,
		"ANANKE_SUP_TOKEN=" + env.Token,
		"ANANKE_SUP_RESULT=" + resultPath,
		"ANANKE_SUP_GRACE_MS=" + strconv.Itoa(env.GraceMS),
	}
	if env.WorkerPath != "" {
		args = append(args, "ANANKE_SUP_WORKER="+env.WorkerPath)
	}
	if env.TranscriptPath != "" {
		args = append(args, "ANANKE_SUP_TRANSCRIPT="+env.TranscriptPath)
	}
	// Fakeworker config
	args = append(args, "ANANKE_FW_EVENTS="+strconv.Itoa(env.FWEvents))
	args = append(args, "ANANKE_FW_EXIT="+strconv.Itoa(env.FWExit))
	args = append(args, "ANANKE_FW_DELAY_MS="+strconv.Itoa(env.FWDelayMS))
	if env.FWPartial {
		args = append(args, "ANANKE_FW_PARTIAL=1")
	}
	if env.FWNoNL {
		args = append(args, "ANANKE_FW_NO_FINAL_NL=1")
	}
	if env.FWSpawnChild {
		args = append(args, "ANANKE_FW_SPAWN_CHILD=1")
		args = append(args, "ANANKE_FW_CHILD_MODE="+env.FWChildMode)
	}
	if env.FWChildPIDFile != "" {
		args = append(args, "ANANKE_FW_CHILD_PID_FILE="+env.FWChildPIDFile)
	}
	if env.TranscriptPath != "" {
		args = append(args, "ANANKE_FW_TRANSCRIPT="+env.TranscriptPath)
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), args...)
	// Setpgid: false so the supervisor helper can call BecomeGroupLeader.
	cmd.SysProcAttr = nil // inherit process group; supervisor will setpgid(0,0)
	// Capture stderr for debugging to a stable path.
	stderrPath := filepath.Join(os.TempDir(), "ananke-sup-stderr-"+runID+".log")
	f, ferr := os.Create(stderrPath)
	if ferr == nil {
		cmd.Stderr = f
		cmd.Stdout = f
		t.Cleanup(func() { f.Close() })
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("fork supervisor: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Remove(env.SocketPath)
		if cmd.Process != nil {
			_ = cmd.Process.Signal(unix.SIGKILL)
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd, env.SocketPath, resultPath, env.IdentityPath
}

// supResult is what the supervisor helper writes to its result file.
type supResult struct {
	TerminalState string `json:"terminal_state"`
	WorkerPID     int    `json:"worker_pid"`
	SupervisorPID int    `json:"supervisor_pid"`
	PGID          int    `json:"pgid"`
	Err           string `json:"err"`
}

// waitForSocket polls until the unix socket exists and accepts a connection.
func waitForSocket(t *testing.T, socketPath string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := net.Dial("unix", socketPath); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("supervisor socket %s not ready within %v", socketPath, timeout)
}

// readSupResult polls for the supervisor result file.
func readSupResult(t *testing.T, path string, timeout time.Duration) supResult {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			var r supResult
			if json.Unmarshal(data, &r) == nil {
				return r
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("supervisor result not written within %v", timeout)
	return supResult{}
}

// sendCmd connects to the supervisor socket, sends a JSON command, and returns
// the JSON response.
func sendCmd(t *testing.T, socketPath, token, cmd string) map[string]any {
	t.Helper()
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial supervisor socket: %v", err)
	}
	defer conn.Close()
	req, _ := json.Marshal(map[string]string{"cmd": cmd, "token": token})
	_, _ = conn.Write(req)
	buf := make([]byte, 4096)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read supervisor response: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		t.Fatalf("unmarshal supervisor response: %v\nraw: %s", err, buf[:n])
	}
	return resp
}

// waitUntilState polls the store until the run reaches the target state.
func waitUntilState(t *testing.T, st *store.Store, runID string, target store.State, timeout time.Duration) store.Run {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		r, err := st.GetRun(t.Context(), runID)
		if err == nil && r.State == target {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	r, _ := st.GetRun(t.Context(), runID)
	t.Fatalf("run %s did not reach state %q within %v (current: %q)", runID, target, timeout, r.State)
	return r
}

// readIdentityFile reads and parses the identity file, failing on error.
func readIdentityFile(t *testing.T, path string) lifecycle.Identity {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read identity file: %v", err)
	}
	var id lifecycle.Identity
	if err := json.Unmarshal(data, &id); err != nil {
		t.Fatalf("unmarshal identity: %v", err)
	}
	return id
}

// containsString is a small helper for substring checks in tests.
func containsString(s, sub string) bool {
	return strings.Contains(s, sub)
}

// toInt converts an any (from a JSON map) to an int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

// atoi parses a string to an int, ignoring leading/trailing whitespace.
func atoi(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	return n, err
}

// waitPID reaps a specific PID (non-blocking). Returns (pid, error); error is
// non-nil if the process does not exist or is not our child.
func waitPID(pid int) (int, error) {
	var status unix.WaitStatus
	wpid, err := unix.Wait4(pid, &status, unix.WNOHANG, nil)
	return wpid, err
}

// processAlive reports whether a PID is alive using kill(pid, 0).
func processAlive(pid int) bool {
	if err := unix.Kill(pid, 0); err == nil {
		return true
	}
	return false
}
