package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

// Identity is the durable supervisor and worker-group authority record. It is
// published after the paused group-leading trampoline starts and before that
// trampoline may exec the real worker. The daemon uses it for authenticated
// restart recovery.
type Identity struct {
	RunID              string                       `json:"run_id"`
	SupervisorPID      int                          `json:"supervisor_pid"`
	SupervisorPGID     int                          `json:"supervisor_pgid"` // compatibility name for the owned worker PGID
	WorkerPID          int                          `json:"worker_pid"`
	WorkerArgs         []string                     `json:"worker_args"`
	SocketPath         string                       `json:"socket_path"`
	Token              string                       `json:"token"`
	TranscriptPath     string                       `json:"transcript_path"`
	TranscriptIdentity store.TranscriptFileIdentity `json:"transcript_identity"`
	LaunchTime         time.Time                    `json:"launch_time"`
}

// WorkerPGID returns the owned worker process-group ID. In this pre-v0.1
// schema, SupervisorPGID is retained as the compatibility field name.
func (id Identity) WorkerPGID() int { return id.SupervisorPGID }

// WriteIdentity writes the identity record to path atomically: it serializes
// to a temp file in the same directory and renames it over the final path.
// The same-directory temp guarantees rename(2) is atomic on the same
// filesystem. A pre-existing file is replaced.
func WriteIdentity(path string, id Identity) error {
	if path == "" {
		return errors.New("lifecycle: identity path is empty")
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return fmt.Errorf("lifecycle: marshal identity: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("lifecycle: mkdir identity dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".identity-*.tmp")
	if err != nil {
		return fmt.Errorf("lifecycle: create identity temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("lifecycle: write identity temp: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("lifecycle: chmod identity temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("lifecycle: sync identity temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("lifecycle: close identity temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("lifecycle: rename identity: %w", err)
	}
	dirFile, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("lifecycle: open identity directory for sync: %w", err)
	}
	if err := dirFile.Sync(); err != nil {
		dirFile.Close()
		return fmt.Errorf("lifecycle: sync identity directory: %w", err)
	}
	if err := dirFile.Close(); err != nil {
		return fmt.Errorf("lifecycle: close identity directory: %w", err)
	}
	return nil
}

// ReadIdentity reads and unmarshals an identity record. A missing file yields
// an error satisfying os.IsNotExist.
func ReadIdentity(path string) (Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, err
	}
	var id Identity
	if err := json.Unmarshal(data, &id); err != nil {
		return Identity{}, fmt.Errorf("lifecycle: unmarshal identity: %w", err)
	}
	return id, nil
}

// TranscriptIdentityFromFile derives a durable identity from an already-open
// transcript, avoiding a pathname race between creation and publication.
func TranscriptIdentityFromFile(file *os.File) (store.TranscriptFileIdentity, error) {
	if file == nil {
		return store.TranscriptFileIdentity{}, errors.New("lifecycle: transcript file is nil")
	}
	info, err := file.Stat()
	if err != nil {
		return store.TranscriptFileIdentity{}, fmt.Errorf("lifecycle: stat open transcript: %w", err)
	}
	return TranscriptIdentityFromInfo(info)
}

// TranscriptIdentityFromInfo converts the platform stat device/inode pair to
// SQLite's signed 64-bit representation. Zero, negative, and overflowing
// platform values are rejected rather than truncated.
func TranscriptIdentityFromInfo(info os.FileInfo) (store.TranscriptFileIdentity, error) {
	if info == nil {
		return store.TranscriptFileIdentity{}, errors.New("lifecycle: transcript file info is nil")
	}
	if !info.Mode().IsRegular() {
		return store.TranscriptFileIdentity{}, fmt.Errorf("lifecycle: transcript mode %v is not regular", info.Mode())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return store.TranscriptFileIdentity{}, fmt.Errorf("lifecycle: unsupported transcript stat type %T", info.Sys())
	}
	device := uint64(stat.Dev)
	inode := uint64(stat.Ino)
	if device > math.MaxInt64 || inode > math.MaxInt64 {
		return store.TranscriptFileIdentity{}, fmt.Errorf("lifecycle: transcript identity exceeds signed 64-bit range: device=%d inode=%d", device, inode)
	}
	identity := store.TranscriptFileIdentity{Device: int64(device), Inode: int64(inode)}
	if err := identity.Validate(); err != nil {
		return store.TranscriptFileIdentity{}, fmt.Errorf("lifecycle: invalid transcript identity: %w", err)
	}
	return identity, nil
}

// ValidateTranscriptIdentity proves info names the durable transcript identity.
func ValidateTranscriptIdentity(info os.FileInfo, expected store.TranscriptFileIdentity) error {
	if err := expected.Validate(); err != nil {
		return fmt.Errorf("lifecycle: expected transcript identity: %w", err)
	}
	actual, err := TranscriptIdentityFromInfo(info)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("lifecycle: transcript identity mismatch: expected device=%d inode=%d, got device=%d inode=%d",
			expected.Device, expected.Inode, actual.Device, actual.Inode)
	}
	return nil
}
