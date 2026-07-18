package lifecycle

import (
	"encoding/json"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// runHelper executes a helper-subprocess mode selected by the ANANKE_HELPER env
// var. Each mode exercises a slice of the lifecycle backend in a real process
// so the parent test can observe process-group behavior directly.
func runHelper(mode string) {
	switch mode {
	case "groupleader":
		helperGroupLeader()
	case "groupeval":
		helperGroupEval()
	case "worker":
		helperWorker()
	case "resistant":
		helperResistant()
	default:
		os.Exit(2)
	}
}

// helperGroupLeader becomes a process-group leader and reports {pid, pgid}.
func helperGroupLeader() {
	b := NewDarwinBackend()
	pgid, err := b.BecomeGroupLeader()
	writeResultFile(os.Getenv(resultEnv), map[string]any{
		"pid":  os.Getpid(),
		"pgid": pgid,
		"err":  errString(err),
	})
}

// helperGroupEval becomes a group leader, launches a worker (re-exec of the
// test binary in "worker" mode), enumerates group members, reaps the worker,
// and reports the full picture: {supervisor_pid, supervisor_pgid, worker_pid,
// worker_pgid, exit_code, group_members, err}.
func helperGroupEval() {
	resultPath := os.Getenv(resultEnv)
	b := NewDarwinBackend()

	pgid, err := b.BecomeGroupLeader()
	if err != nil {
		writeResultFile(resultPath, map[string]any{"err": errString(err)})
		return
	}
	supPID := os.Getpid()

	workerResultPath := filepath.Join(filepath.Dir(resultPath), "worker.json")
	env := append(os.Environ(),
		helperEnv+"=worker",
		resultEnv+"="+workerResultPath,
		"ANANKE_SLEEP_MS=500",
		"ANANKE_EXIT=0",
	)
	workerPID, err := b.LaunchWorker(os.Args[0], nil, env)
	if err != nil {
		writeResultFile(resultPath, map[string]any{"err": errString(err)})
		return
	}

	// Enumerate group members while the worker is still alive.
	members, _ := b.GroupMembers(pgid)

	// Wait for the worker to report its {pid, pgid}.
	var workerResult struct {
		PID  int `json:"pid"`
		PGID int `json:"pgid"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, rerr := os.ReadFile(workerResultPath); rerr == nil {
			if json.Unmarshal(data, &workerResult) == nil {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	exitCode, reapErr := b.ReapWorker(workerPID)

	writeResultFile(resultPath, map[string]any{
		"supervisor_pid":  supPID,
		"supervisor_pgid": pgid,
		"worker_pid":      workerPID,
		"worker_pgid":     workerResult.PGID,
		"exit_code":       exitCode,
		"group_members":   members,
		"err":             errString(reapErr),
	})
}

// helperWorker reports {pid, pgid} then sleeps and exits with a configured code.
// Env: ANANKE_SLEEP_MS (default 2000), ANANKE_EXIT (default 0).
func helperWorker() {
	writeResultFile(os.Getenv(resultEnv), map[string]any{
		"pid":  os.Getpid(),
		"pgid": pgidOf(0),
	})
	sleep := envInt("ANANKE_SLEEP_MS", 2000)
	if sleep > 0 {
		time.Sleep(time.Duration(sleep) * time.Millisecond)
	}
	os.Exit(envInt("ANANKE_EXIT", 0))
}

// helperResistant ignores SIGTERM and sleeps until SIGKILL. Reports {pid, pgid}.
func helperResistant() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, unix.SIGTERM)
	go func() {
		for range ch {
			// swallow SIGTERM
		}
	}()
	writeResultFile(os.Getenv(resultEnv), map[string]any{
		"pid":  os.Getpid(),
		"pgid": pgidOf(0),
	})
	time.Sleep(5 * time.Minute)
	os.Exit(0)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func pgidOf(pid int) int {
	g, err := unix.Getpgid(pid)
	if err != nil {
		return 0
	}
	return g
}
