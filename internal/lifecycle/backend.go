// Package lifecycle implements the supervisor lifecycle backend (ADR-0002 §5).
//
// The supervisor is the process-group anchor: it calls setpgid(0, 0) at
// startup, launches the worker into its own group, and never signals the
// numeric PGID after reaping the worker. The Darwin backend is the initial
// implementation; Linux (pidfd) and Windows (Job Object) backends are future
// work behind the same interface.
package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// Signal is a platform signal. On Darwin/Linux it is an alias for unix.Signal.
type Signal = unix.Signal

// LifecycleBackend abstracts the OS-specific process lifecycle operations a
// supervisor needs (ADR-0002 §5). The supervisor logic is written against this
// interface so it can be unit-tested with a mock and run against a real Darwin
// backend in production.
type LifecycleBackend interface {
	// BecomeGroupLeader makes the calling process the leader of a new process
	// group. Returns the new PGID (equal to the caller's PID).
	BecomeGroupLeader() (pgid int, err error)

	// LaunchWorker fork+execs the worker into the caller's process group (the
	// worker inherits the supervisor's PGID). Returns the worker PID.
	LaunchWorker(path string, args []string, env []string) (pid int, err error)

	// WorkerExited returns a channel that closes when the worker has exited.
	// The call is non-blocking; the caller selects on the returned channel.
	// On Darwin (no pidfd) the background goroutine uses waitpid, which reaps
	// the worker; the PGID remains pinned by the supervisor (group leader),
	// so this is safe per ADR-0002 §2.
	WorkerExited(pid int) (<-chan struct{}, error)

	// ReapWorker returns the worker's exit code. It blocks until the worker
	// has exited. Only call after group cleanup is complete. Returns the exit
	// code for normal exit, or a negative signal-encoded value if terminated
	// by a signal.
	ReapWorker(pid int) (exitCode int, err error)

	// GroupMembers returns the PIDs of all processes in the given process
	// group, excluding the caller.
	GroupMembers(pgid int) ([]int, error)

	// SignalProcess sends a signal to a specific PID (never the negated PGID
	// group form after identity is lost).
	SignalProcess(pid int, sig Signal) error

	// ProcessAlive reports whether a PID is alive without sending a signal.
	ProcessAlive(pid int) bool
}

// workerEntry tracks a launched worker and its exit status.
type workerEntry struct {
	pid      int
	exited   chan struct{}
	exitCode int
	exitErr  error
	once     sync.Once
}

// startWait launches (at most once) a background goroutine that blocking-waits
// for the worker, caches the exit status, and closes the exited channel.
func (w *workerEntry) startWait() {
	w.once.Do(func() {
		go func() {
			var status unix.WaitStatus
			_, err := unix.Wait4(w.pid, &status, 0, nil)
			if err != nil {
				w.exitErr = err
				w.exitCode = -1
			} else if status.Signaled() {
				w.exitCode = -int(status.Signal())
			} else {
				w.exitCode = status.ExitStatus()
			}
			close(w.exited)
		}()
	})
}

// DarwinBackend implements LifecycleBackend on Darwin using golang.org/x/sys/unix.
type DarwinBackend struct {
	mu      sync.Mutex
	workers map[int]*workerEntry
}

// NewDarwinBackend returns a Darwin lifecycle backend.
func NewDarwinBackend() *DarwinBackend {
	return &DarwinBackend{workers: make(map[int]*workerEntry)}
}

// Compile-time interface satisfaction.
var _ LifecycleBackend = (*DarwinBackend)(nil)

// BecomeGroupLeader makes the calling process a new process-group leader via
// setpgid(0, 0) and returns the resulting PGID.
func (b *DarwinBackend) BecomeGroupLeader() (int, error) {
	if err := unix.Setpgid(0, 0); err != nil {
		return 0, fmt.Errorf("setpgid(0,0): %w", err)
	}
	pgid, err := unix.Getpgid(0)
	if err != nil {
		return 0, fmt.Errorf("getpgid(0): %w", err)
	}
	return pgid, nil
}

// LaunchWorker fork+execs the worker binary into the caller's process group.
// The worker inherits the supervisor's PGID (Setpgid is false by default).
func (b *DarwinBackend) LaunchWorker(path string, args []string, env []string) (int, error) {
	cmd := exec.Command(path, args...)
	if env != nil {
		cmd.Env = env
	}
	// Setpgid: false means the child inherits the caller's process group.
	cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: false}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("launch worker %q: %w", path, err)
	}
	pid := cmd.Process.Pid
	w := &workerEntry{pid: pid, exited: make(chan struct{})}
	b.mu.Lock()
	b.workers[pid] = w
	b.mu.Unlock()
	return pid, nil
}

// WorkerExited returns a channel that closes when the worker exits. The call
// does not block; a background goroutine waitpids the worker and closes the
// channel on exit. See ADR-0002 §2 for why reaping the worker (not the group
// leader) is safe.
func (b *DarwinBackend) WorkerExited(pid int) (<-chan struct{}, error) {
	b.mu.Lock()
	w, ok := b.workers[pid]
	b.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown worker pid %d", pid)
	}
	w.startWait()
	return w.exited, nil
}

// ReapWorker blocks until the worker has exited and returns the exit code.
// A normal exit yields the exit status; signal termination yields the negated
// signal number.
func (b *DarwinBackend) ReapWorker(pid int) (int, error) {
	b.mu.Lock()
	w, ok := b.workers[pid]
	b.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("unknown worker pid %d", pid)
	}
	w.startWait()
	<-w.exited
	return w.exitCode, w.exitErr
}

// SignalProcess sends a signal to a specific PID via kill(2).
func (b *DarwinBackend) SignalProcess(pid int, sig Signal) error {
	return unix.Kill(pid, sig)
}

// ProcessAlive reports whether a PID is alive using kill(pid, 0). ESRCH means
// the process does not exist; any other error (e.g. EPERM) means the process
// exists but is not signalable, which we treat as alive.
func (b *DarwinBackend) ProcessAlive(pid int) bool {
	if err := unix.Kill(pid, 0); err == nil {
		return true
	} else if errors.Is(err, unix.ESRCH) {
		return false
	}
	return true
}

// GroupMembers enumerates all PIDs in the given process group by parsing
// `ps -A -o pid,pgid`, excluding the caller's own PID. SIGKILL escalation is
// rare and not in the hot path, so shelling out to ps is acceptable (ADR-0002
// §4).
func (b *DarwinBackend) GroupMembers(pgid int) ([]int, error) {
	out, err := exec.Command("ps", "-A", "-o", "pid=,pgid=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	caller := os.Getpid()
	var members []int
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, perr := strconv.Atoi(fields[0])
		g, gerr := strconv.Atoi(fields[1])
		if perr != nil || gerr != nil {
			continue
		}
		if g != pgid || pid == caller {
			continue
		}
		members = append(members, pid)
	}
	return members, nil
}
