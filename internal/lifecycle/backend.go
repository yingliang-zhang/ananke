// Package lifecycle implements the supervisor lifecycle backend (ADR-0002 §5).
//
// The supervisor stays outside the owned worker group. A paused trampoline is
// its leader, and the unreaped leader pins PGID identity through cleanup. The
// Darwin backend is the initial implementation; Linux and Windows backends
// remain future work behind the same interface.
package lifecycle

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io"
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
	// LaunchWorker starts a paused trampoline as a new process-group leader.
	// The returned positive PID is both the worker PID and owned worker PGID;
	// exact exit observation is installed before return.
	LaunchWorker(path string, args []string, env []string) (pid int, err error)

	// ReleaseWorker lets the paused trampoline exec the configured worker in
	// place, preserving its PID and PGID.
	ReleaseWorker(pid int) error

	// WorkerExited returns a channel that closes when the exact worker exits.
	// The call is non-blocking and does not reap; Darwin uses EVFILT_PROC with
	// NOTE_EXIT so the PID and wait status remain pinned until ReapWorker.
	WorkerExited(pid int) (<-chan struct{}, error)

	// ReapWorker returns the worker's exit code. It blocks until the worker
	// has exited. Only call after group cleanup is complete. Returns the exit
	// code for normal exit, or a negative signal-encoded value if terminated
	// by a signal.
	ReapWorker(pid int) (exitCode int, err error)

	// GroupMembers returns live, non-zombie members of the owned worker group.
	// It is a fail-closed quiescence oracle, never signalling authority.
	GroupMembers(pgid int) ([]int, error)

	// SignalGroup atomically signals the positive owned worker PGID.
	SignalGroup(pgid int, sig Signal) error

	// ProcessAlive reports whether a PID is alive without sending a signal.
	ProcessAlive(pid int) bool
}

const workerTrampolineArg = "__ananke_worker_trampoline"

type workerLaunchConfig struct {
	Path string
	Args []string
	Env  []string
}

// WorkerTrampolineRequested reports whether this process was re-executed as
// the isolated pre-exec worker trampoline.
func WorkerTrampolineRequested() bool {
	return len(os.Args) == 2 && os.Args[1] == workerTrampolineArg
}

// RunWorkerTrampoline reads worker configuration from inherited descriptor 3,
// waits for one release byte on descriptor 4, then execs the worker in place.
// The caller must invoke this before normal command or test dispatch.
func RunWorkerTrampoline() error {
	configFile := os.NewFile(3, "ananke-worker-config")
	releaseFile := os.NewFile(4, "ananke-worker-release")
	if configFile == nil || releaseFile == nil {
		return errors.New("worker trampoline inherited descriptors are unavailable")
	}
	defer configFile.Close()
	defer releaseFile.Close()

	var config workerLaunchConfig
	if err := gob.NewDecoder(configFile).Decode(&config); err != nil {
		return fmt.Errorf("decode worker trampoline config: %w", err)
	}
	if config.Path == "" {
		return errors.New("worker trampoline path is empty")
	}
	pgid, err := unix.Getpgid(0)
	if err != nil {
		return fmt.Errorf("get worker trampoline pgid: %w", err)
	}
	if pgid != os.Getpid() {
		return fmt.Errorf("worker trampoline pgid %d does not equal pid %d", pgid, os.Getpid())
	}
	var release [1]byte
	if _, err := io.ReadFull(releaseFile, release[:]); err != nil {
		return fmt.Errorf("wait for worker trampoline release: %w", err)
	}
	env := config.Env
	if env == nil {
		env = os.Environ()
	}
	argv := make([]string, 1, len(config.Args)+1)
	argv[0] = config.Path
	argv = append(argv, config.Args...)
	if err := unix.Exec(config.Path, argv, env); err != nil {
		return fmt.Errorf("exec worker %q: %w", config.Path, err)
	}
	return nil
}

// workerEntry tracks a launched worker's non-reaping exit notification,
// retained release barrier, and eventual wait status.
type workerEntry struct {
	pid         int
	exited      chan struct{}
	release     *os.File
	releaseOnce sync.Once
	releaseErr  error
	reapDone    chan struct{}
	reapOnce    sync.Once
	exitCode    int
	exitErr     error
}

// processExitWatcher owns one kqueue descriptor from successful registration
// until NOTE_EXIT delivery or a terminal kevent error. Observation never reaps.
type processExitWatcher struct {
	exited chan struct{}
}

// newProcessExitWatcher installs NOTE_EXIT before returning, so even a child
// that exits immediately after launch cannot outrun observation.
func newProcessExitWatcher(pid int) (*processExitWatcher, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("kqueue: %w", err)
	}
	change := unix.Kevent_t{}
	unix.SetKevent(&change, pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ENABLE|unix.EV_ONESHOT)
	change.Fflags = unix.NOTE_EXIT
	if _, err := unix.Kevent(kq, []unix.Kevent_t{change}, nil, nil); err != nil {
		_ = unix.Close(kq)
		return nil, fmt.Errorf("register NOTE_EXIT for pid %d: %w", pid, err)
	}
	w := &processExitWatcher{exited: make(chan struct{})}
	go w.observe(kq)
	return w, nil
}

// observe owns kq and closes it after NOTE_EXIT delivery or an unrecoverable
// kevent error. Only an exact NOTE_EXIT closes the readiness channel.
func (w *processExitWatcher) observe(kq int) {
	defer unix.Close(kq)
	defer close(w.exited)
	events := make([]unix.Kevent_t, 1)
	for {
		n, err := unix.Kevent(kq, nil, events, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n != 1 || events[0].Flags&unix.EV_ERROR != 0 {
			return
		}
		if events[0].Filter == unix.EVFILT_PROC && events[0].Fflags&unix.NOTE_EXIT != 0 {
			return
		}
	}
}

// DarwinBackend implements LifecycleBackend on Darwin using golang.org/x/sys/unix.
type DarwinBackend struct {
	mu             sync.Mutex
	workers        map[int]*workerEntry
	newExitWatcher func(int) (*processExitWatcher, error)
	wait4          func(int, *unix.WaitStatus, int, *unix.Rusage) (int, error)
}

// NewDarwinBackend returns a Darwin lifecycle backend.
func NewDarwinBackend() *DarwinBackend {
	return &DarwinBackend{
		workers:        make(map[int]*workerEntry),
		newExitWatcher: newProcessExitWatcher,
		wait4:          unix.Wait4,
	}
}

// Compile-time interface satisfaction.
var _ LifecycleBackend = (*DarwinBackend)(nil)

// LaunchWorker starts an inherited-FD trampoline as its own process-group
// leader. The real worker cannot execute until ReleaseWorker succeeds.
func (b *DarwinBackend) LaunchWorker(path string, args []string, env []string) (int, error) {
	resolvedPath, err := exec.LookPath(path)
	if err != nil {
		return 0, fmt.Errorf("resolve worker %q: %w", path, err)
	}
	configFile, err := os.CreateTemp("", ".ananke-worker-config-*")
	if err != nil {
		return 0, fmt.Errorf("create worker trampoline config: %w", err)
	}
	configName := configFile.Name()
	defer os.Remove(configName)
	closeConfig := func() { _ = configFile.Close() }
	if err := gob.NewEncoder(configFile).Encode(workerLaunchConfig{Path: resolvedPath, Args: args, Env: env}); err != nil {
		closeConfig()
		return 0, fmt.Errorf("encode worker trampoline config: %w", err)
	}
	if _, err := configFile.Seek(0, 0); err != nil {
		closeConfig()
		return 0, fmt.Errorf("rewind worker trampoline config: %w", err)
	}
	releaseReader, releaseWriter, err := os.Pipe()
	if err != nil {
		closeConfig()
		return 0, fmt.Errorf("create worker trampoline release pipe: %w", err)
	}

	executable, err := os.Executable()
	if err != nil {
		closeConfig()
		_ = releaseReader.Close()
		_ = releaseWriter.Close()
		return 0, fmt.Errorf("resolve trampoline executable: %w", err)
	}
	cmd := exec.Command(executable, workerTrampolineArg)
	cmd.ExtraFiles = []*os.File{configFile, releaseReader}
	cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		closeConfig()
		_ = releaseReader.Close()
		_ = releaseWriter.Close()
		return 0, fmt.Errorf("launch worker trampoline for %q: %w", path, err)
	}
	closeConfig()
	_ = releaseReader.Close()

	pid := cmd.Process.Pid
	watcherFactory := b.newExitWatcher
	if watcherFactory == nil {
		watcherFactory = newProcessExitWatcher
	}
	watcher, watchErr := watcherFactory(pid)
	exited := make(chan struct{})
	if watcher != nil {
		exited = watcher.exited
	}
	w := &workerEntry{pid: pid, exited: exited, release: releaseWriter, reapDone: make(chan struct{})}
	b.mu.Lock()
	b.workers[pid] = w
	b.mu.Unlock()
	if watchErr != nil {
		return pid, watchErr
	}
	return pid, nil
}

// ReleaseWorker lets a configured trampoline exec its real worker in place.
func (b *DarwinBackend) ReleaseWorker(pid int) error {
	b.mu.Lock()
	w, ok := b.workers[pid]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("unknown worker pid %d", pid)
	}
	w.releaseOnce.Do(func() {
		n, err := w.release.Write([]byte{1})
		if err != nil {
			w.releaseErr = err
		} else if n != 1 {
			w.releaseErr = io.ErrShortWrite
		}
		_ = w.release.Close()
	})
	if w.releaseErr != nil {
		return fmt.Errorf("release worker pid %d: %w", pid, w.releaseErr)
	}
	return nil
}

// WorkerExited returns a channel that closes when kqueue reports NOTE_EXIT for
// the exact worker PID. Observation does not consume the child's wait status.
func (b *DarwinBackend) WorkerExited(pid int) (<-chan struct{}, error) {
	b.mu.Lock()
	w, ok := b.workers[pid]
	b.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown worker pid %d", pid)
	}
	return w.exited, nil
}

// ReapWorker is the sole wait/reap operation. It blocks until the exact child
// exits, then returns its real exit status. Call only after group cleanup.
func (b *DarwinBackend) ReapWorker(pid int) (int, error) {
	b.mu.Lock()
	w, ok := b.workers[pid]
	b.mu.Unlock()
	if !ok {
		return 0, fmt.Errorf("unknown worker pid %d", pid)
	}
	w.releaseOnce.Do(func() {
		w.releaseErr = w.release.Close()
	})
	w.reapOnce.Do(func() {
		wait4 := b.wait4
		if wait4 == nil {
			wait4 = unix.Wait4
		}
		var status unix.WaitStatus
		for {
			waitedPID, err := wait4(w.pid, &status, 0, nil)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				w.exitErr = err
				w.exitCode = -1
			} else if waitedPID != w.pid {
				w.exitErr = fmt.Errorf("wait4 returned pid %d, want %d", waitedPID, w.pid)
				w.exitCode = -1
			} else if status.Signaled() {
				w.exitCode = -int(status.Signal())
			} else {
				w.exitCode = status.ExitStatus()
			}
			break
		}
		close(w.reapDone)
	})
	<-w.reapDone
	if w.exitErr == nil {
		b.mu.Lock()
		delete(b.workers, pid)
		b.mu.Unlock()
	}
	return w.exitCode, w.exitErr
}

// SignalGroup atomically signals every member of a positive process group.
func (b *DarwinBackend) SignalGroup(pgid int, sig Signal) error {
	if pgid <= 0 {
		return fmt.Errorf("signal group: pgid %d is not positive", pgid)
	}
	return unix.Kill(-pgid, sig)
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

// GroupMembers enumerates live PIDs in the given process group by parsing
// `ps -A -o pid,pgid,stat`, excluding the caller and exited zombies. A zombie
// has no executable process left to signal; its exact status remains pinned
// for ReapWorker. Malformed enumeration is an error, never empty-group proof.
func (b *DarwinBackend) GroupMembers(pgid int) ([]int, error) {
	cmd := exec.Command("ps", "-A", "-o", "pid=,pgid=,stat=")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	caller := os.Getpid()
	enumerator := cmd.Process.Pid
	var members []int
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("parse ps group member line %q", line)
		}
		pid, perr := strconv.Atoi(fields[0])
		group, gerr := strconv.Atoi(fields[1])
		if perr != nil || gerr != nil {
			return nil, fmt.Errorf("parse ps group member line %q: pid=%v pgid=%v", line, perr, gerr)
		}
		if group != pgid || pid == caller || pid == enumerator || strings.HasPrefix(fields[2], "Z") {
			continue
		}
		members = append(members, pid)
	}
	return members, nil
}
