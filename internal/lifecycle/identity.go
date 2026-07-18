package lifecycle

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Identity is the durable supervisor identity record written before the worker
// is launched (ADR-0002 §3). The daemon reads it on restart to reconnect to a
// still-alive supervisor or to detect supervisor death and enter
// recovery_unknown.
type Identity struct {
	SupervisorPID  int       `json:"supervisor_pid"`
	SupervisorPGID int       `json:"supervisor_pgid"`
	WorkerPID      int       `json:"worker_pid"`
	WorkerArgs     []string  `json:"worker_args"`
	SocketPath     string    `json:"socket_path"`
	Token          string    `json:"token"`
	TranscriptPath string    `json:"transcript_path"`
	LaunchTime     time.Time `json:"launch_time"`
}

// WorkerPGID returns the worker's process-group ID. The worker inherits the
// supervisor's process group (ADR-0002 §1), so it equals SupervisorPGID.
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
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("lifecycle: close identity temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("lifecycle: rename identity: %w", err)
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
