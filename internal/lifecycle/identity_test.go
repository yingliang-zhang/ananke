package lifecycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yingliang-zhang/ananke/internal/store"
)

// TestIdentityFileRoundTrip verifies an identity file can be written atomically
// and read back with all fields preserved.
func TestIdentityFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")

	id := Identity{
		RunID:              "run-identity-round-trip",
		SupervisorPID:      12345,
		SupervisorPGID:     12345,
		WorkerPID:          67890,
		WorkerArgs:         []string{"--foo", "bar"},
		SocketPath:         filepath.Join(dir, "sock"),
		Token:              "test-token-abc",
		TranscriptPath:     filepath.Join(dir, "transcript.ndjson"),
		TranscriptIdentity: store.TranscriptFileIdentity{Device: 123, Inode: 456},
		LaunchTime:         time.Now().UTC().Truncate(time.Millisecond),
	}

	if err := WriteIdentity(path, id); err != nil {
		t.Fatalf("WriteIdentity: %v", err)
	}

	// No temp file should remain alongside the final file.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	count := 0
	for _, e := range entries {
		if e.Name() == "identity.json" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 identity.json entry, got %d (temp file leaked?)", count)
	}

	got, err := ReadIdentity(path)
	if err != nil {
		t.Fatalf("ReadIdentity: %v", err)
	}
	if got.RunID != id.RunID {
		t.Errorf("RunID = %q, want %q", got.RunID, id.RunID)
	}
	if got.SupervisorPID != id.SupervisorPID {
		t.Errorf("SupervisorPID = %d, want %d", got.SupervisorPID, id.SupervisorPID)
	}
	if got.SupervisorPGID != id.SupervisorPGID {
		t.Errorf("SupervisorPGID = %d, want %d", got.SupervisorPGID, id.SupervisorPGID)
	}
	if got.WorkerPID != id.WorkerPID {
		t.Errorf("WorkerPID = %d, want %d", got.WorkerPID, id.WorkerPID)
	}
	if len(got.WorkerArgs) != len(id.WorkerArgs) || got.WorkerArgs[0] != id.WorkerArgs[0] {
		t.Errorf("WorkerArgs = %v, want %v", got.WorkerArgs, id.WorkerArgs)
	}
	if got.SocketPath != id.SocketPath {
		t.Errorf("SocketPath = %q, want %q", got.SocketPath, id.SocketPath)
	}
	if got.Token != id.Token {
		t.Errorf("Token = %q, want %q", got.Token, id.Token)
	}
	if got.TranscriptPath != id.TranscriptPath {
		t.Errorf("TranscriptPath = %q, want %q", got.TranscriptPath, id.TranscriptPath)
	}
	if got.TranscriptIdentity != id.TranscriptIdentity {
		t.Errorf("TranscriptIdentity = %+v, want %+v", got.TranscriptIdentity, id.TranscriptIdentity)
	}
	if !got.LaunchTime.Equal(id.LaunchTime) {
		t.Errorf("LaunchTime = %v, want %v", got.LaunchTime, id.LaunchTime)
	}
}

// TestIdentityFileAtomicReplace verifies that writing an identity file where a
// stale file already exists replaces it atomically (readers never see a
// partially-written file).
func TestIdentityFileAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")

	// Stale file from a previous run.
	stale := Identity{SupervisorPID: 1, Token: "stale"}
	if err := WriteIdentity(path, stale); err != nil {
		t.Fatalf("initial WriteIdentity: %v", err)
	}

	fresh := Identity{SupervisorPID: 999, Token: "fresh"}
	if err := WriteIdentity(path, fresh); err != nil {
		t.Fatalf("replace WriteIdentity: %v", err)
	}
	got, err := ReadIdentity(path)
	if err != nil {
		t.Fatalf("ReadIdentity: %v", err)
	}
	if got.Token != "fresh" {
		t.Errorf("Token = %q, want %q (stale file not replaced)", got.Token, "fresh")
	}
	if got.SupervisorPID != 999 {
		t.Errorf("SupervisorPID = %d, want 999", got.SupervisorPID)
	}
}

// TestReadIdentityMissingFile verifies a missing identity file yields a
// well-typed error, not a generic one.
func TestReadIdentityMissingFile(t *testing.T) {
	_, err := ReadIdentity(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil {
		t.Fatal("ReadIdentity returned nil error for missing file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want os.IsNotExist", err)
	}
}

// TestIdentityFileCorruptJSON verifies a corrupt identity file yields a
// well-typed error.
func TestIdentityFileCorruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := ReadIdentity(path)
	if err == nil {
		t.Fatal("ReadIdentity returned nil error for corrupt file")
	}
	if _, ok := err.(*json.SyntaxError); !ok && err.Error() == "" {
		t.Errorf("err = %v, want non-empty", err)
	}
}

func TestTranscriptIdentityFromOpenFileRejectsNamedReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.ndjson")
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatalf("create transcript: %v", err)
	}
	defer file.Close()

	identity, err := TranscriptIdentityFromFile(file)
	if err != nil {
		t.Fatalf("TranscriptIdentityFromFile: %v", err)
	}
	if err := identity.Validate(); err != nil {
		t.Fatalf("derived identity invalid: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat transcript: %v", err)
	}
	if err := ValidateTranscriptIdentity(info, identity); err != nil {
		t.Fatalf("ValidateTranscriptIdentity(original): %v", err)
	}
	if err := ValidateTranscriptIdentity(info, store.TranscriptFileIdentity{}); err == nil {
		t.Fatal("ValidateTranscriptIdentity accepted unknown expected identity")
	}

	replacement := filepath.Join(dir, "replacement.ndjson")
	if err := os.WriteFile(replacement, nil, 0o600); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("replace transcript: %v", err)
	}
	replacementInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat replacement: %v", err)
	}
	if err := ValidateTranscriptIdentity(replacementInfo, identity); err == nil {
		t.Fatal("ValidateTranscriptIdentity accepted replacement inode")
	}
	openInfo, err := file.Stat()
	if err != nil {
		t.Fatalf("stat open original: %v", err)
	}
	if err := ValidateTranscriptIdentity(openInfo, identity); err != nil {
		t.Fatalf("open original lost identity: %v", err)
	}
}
