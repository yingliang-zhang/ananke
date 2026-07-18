// Command ananke-fakeworker is a test fixture worker for Ananke slice 1.
//
// It writes NDJSON transcript events to a file, can split a JSON write across
// two writes (partial line test), can omit the final newline, can spawn normal
// or SIGTERM-resistant children, and exits with a configurable code. It does
// NOT call setpgid — it inherits the supervisor's process group (ADR-0002 §1).
//
// All behaviour is controlled by environment variables so the supervisor can
// pass them through WorkerEnv:
//
//	ANANKE_FW_TRANSCRIPT    transcript file path (required to emit events)
//	ANANKE_FW_EVENTS        number of NDJSON events to write (default 3)
//	ANANKE_FW_EXIT          exit code (default 0)
//	ANANKE_FW_DELAY_MS      delay between events in ms (default 0)
//	ANANKE_FW_PARTIAL       if "1", split event 2's JSON write across two writes
//	ANANKE_FW_NO_FINAL_NL   if "1", omit the final newline on the last event
//	ANANKE_FW_SPAWN_CHILD   if "1", spawn a child process
//	ANANKE_FW_CHILD_MODE    "normal" (default) or "resistant"
//	ANANKE_FW_CHILD_PID_FILE write the spawned child's PID to this file
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	transcriptPath := os.Getenv("ANANKE_FW_TRANSCRIPT")
	nEvents := envInt("ANANKE_FW_EVENTS", 3)
	exitCode := envInt("ANANKE_FW_EXIT", 0)
	delayMs := envInt("ANANKE_FW_DELAY_MS", 0)
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
		childPID = spawnChildProc(childMode, childPIDFile)
	}

	if transcriptPath != "" {
		writeTranscript(transcriptPath, nEvents, delayMs, partial, noFinalNL)
	}

	// Wait for the child if we spawned one (unless resistant — then just exit).
	if spawnChild && childMode == "normal" && childPID > 0 {
		_ = unix.Kill(childPID, 0) // child is still running; let it finish
		// The child sleeps briefly and exits on its own.
		time.Sleep(200 * time.Millisecond)
	}

	os.Exit(exitCode)
}

func writeTranscript(path string, nEvents, delayMs int, partial, noFinalNL bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	for i := 1; i <= nEvents; i++ {
		ev := map[string]any{
			"seq":  i,
			"type": "message",
			"text": "event " + strconv.Itoa(i),
			"ts":   time.Now().UTC().Format(time.RFC3339Nano),
		}
		data, _ := json.Marshal(ev)
		isLast := i == nEvents
		suffix := "\n"
		if isLast && noFinalNL {
			suffix = ""
		}

		if partial && i == 2 {
			// Split event 2 across two writes: first half, then the rest.
			half := len(data) / 2
			_, _ = f.Write(data[:half])
			_ = f.Sync()
			if delayMs > 0 {
				time.Sleep(time.Duration(delayMs) * time.Millisecond)
			}
			_, _ = f.Write(data[half:])
			_, _ = f.WriteString(suffix)
		} else {
			_, _ = f.Write(data)
			_, _ = f.WriteString(suffix)
		}
		_ = f.Sync()
		if delayMs > 0 && !isLast {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
	}
}

// spawnChildProc forks a child process (the fakeworker binary itself in a
// child mode) that inherits the current process group. If childMode is
// "resistant", the child ignores SIGTERM and sleeps until SIGKILL.
func spawnChildProc(mode, pidFile string) int {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "ANANKE_FW_CHILD_MODE_ACTIVE="+mode)
	cmd.Stdout = nil
	cmd.Stderr = nil
	// Setpgid: false → child inherits our (the worker's) process group.
	cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: false}
	if err := cmd.Start(); err != nil {
		return 0
	}
	pid := cmd.Process.Pid
	if pidFile != "" {
		_ = os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600)
	}
	// Detach: don't wait. The parent worker exits and the child is reparented
	// to the supervisor's group (inherits PGID).
	return pid
}

// init handles the child mode: if ANANKE_FW_CHILD_MODE_ACTIVE is set, this
// process is a spawned child. "resistant" children ignore SIGTERM and sleep.
func init() {
	mode := os.Getenv("ANANKE_FW_CHILD_MODE_ACTIVE")
	if mode == "" {
		return
	}
	// Write a PID file for the child if requested.
	if pidFile := os.Getenv("ANANKE_FW_CHILD_PID_FILE"); pidFile != "" {
		// Don't overwrite the parent's PID file; use a child-specific one if
		// the parent didn't already write ours.
		if existing, err := os.ReadFile(pidFile); err != nil || strings.TrimSpace(string(existing)) == "" {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
		}
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

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
