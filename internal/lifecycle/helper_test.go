package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
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

// resultFileBeforePublish lets the atomic-publication regression pause a write
// at the last point before the result path becomes readable.
var resultFileBeforePublish func()

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

func TestWriteResultFilePublishesOnlyCompleteJSON(t *testing.T) {
	resultPath := filepath.Join(t.TempDir(), "result.json")
	ready := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	resultFileBeforePublish = func() {
		close(ready)
		<-release
	}
	t.Cleanup(func() { resultFileBeforePublish = nil })

	go func() {
		defer close(done)
		writeResultFile(resultPath, map[string]string{"state": "complete"})
	}()

	<-ready
	published, readErr := os.ReadFile(resultPath)
	close(release)
	<-done

	if readErr == nil && !json.Valid(published) {
		t.Fatalf("reader observed incomplete published result: %q", published)
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read result while write paused: %v", readErr)
	}

	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read published result: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal published result: %v\nraw: %s", err, data)
	}
	if result["state"] != "complete" {
		t.Fatalf("published state = %q, want complete", result["state"])
	}
}

// writeResultFile writes a JSON result for a helper.
func writeResultFile(path string, v any) {
	if err := writeResultFileAtomically(path, v); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "write helper result %q: %v\n", path, err)
		os.Exit(125)
	}
}

func writeResultFileAtomically(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create result temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := func(closeFile bool) error {
		var cleanupErrs []error
		if closeFile {
			if err := temp.Close(); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("close result temp file: %w", err))
			}
		}
		if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("remove result temp file: %w", err))
		}
		return errors.Join(cleanupErrs...)
	}

	if _, err := temp.Write(data); err != nil {
		return errors.Join(fmt.Errorf("write result temp file: %w", err), cleanup(true))
	}
	if err := temp.Close(); err != nil {
		return errors.Join(fmt.Errorf("close result temp file: %w", err), cleanup(false))
	}
	if resultFileBeforePublish != nil {
		resultFileBeforePublish()
	}
	if err := os.Rename(tempPath, path); err != nil {
		return errors.Join(fmt.Errorf("publish result file: %w", err), cleanup(false))
	}
	return nil
}

// readResultFile reads and unmarshals a helper result.
func readResultFile(t *testing.T, path string, timeout time.Duration, out any) {
	t.Helper()
	data := waitForResult(t, path, timeout)
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("unmarshal result: %v\nraw: %s", err, data)
	}
}
