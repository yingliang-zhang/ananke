package lifecycle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// helperEnv selects the helper mode when the test binary is re-executed as a
// subprocess. TestMain dispatches on it before running the test suite.
const helperEnv = "ANANKE_HELPER"

// resultEnv names the file a helper writes its JSON result to.
const resultEnv = "ANANKE_RESULT"

// TestMain dispatches helper subprocess modes; otherwise runs the suite.
func TestMain(m *testing.M) {
	if WorkerTrampolineRequested() {
		if RunWorkerTrampoline() != nil {
			os.Exit(125)
		}
		os.Exit(0)
	}
	if mode := os.Getenv(helperEnv); mode != "" {
		runHelper(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// forkHelper re-executes the test binary in the requested helper mode and
// returns the command plus the result-file path it will write.
func forkHelper(t *testing.T, mode string, extra map[string]string) (*exec.Cmd, string) {
	t.Helper()
	dir := t.TempDir()
	resultPath := filepath.Join(dir, "result.json")
	env := append(os.Environ(), helperEnv+"="+mode, resultEnv+"="+resultPath)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = env
	cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: false}
	if err := cmd.Start(); err != nil {
		t.Fatalf("fork helper %q: %v", mode, err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(unix.SIGKILL)
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd, resultPath
}

// waitForResult polls for the helper's result file up to the deadline.
func waitForResult(t *testing.T, path string, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			return data
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("helper result file %s not written within %v", path, timeout)
	return nil
}

// waitUntil blocks until cond returns true or the deadline elapses.
func waitUntil(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition %q not satisfied within %v", what, timeout)
}

// writeResultFile writes a JSON result for a helper.
func writeResultFile(path string, v any) {
	data, _ := json.Marshal(v)
	_ = os.WriteFile(path, data, 0o600)
}

// readResultFile reads and unmarshals a helper result.
func readResultFile(t *testing.T, path string, timeout time.Duration, out any) {
	t.Helper()
	data := waitForResult(t, path, timeout)
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal result: %v\nraw: %s", err, data)
	}
}
