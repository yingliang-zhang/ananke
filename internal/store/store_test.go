package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCreateProjectWorkstreamRun(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.CreateProject(ctx, "proj-1", "my-project", "/tmp/root"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if err := s.CreateWorkstream(ctx, "ws-1", "proj-1", "main"); err != nil {
		t.Fatalf("CreateWorkstream: %v", err)
	}

	spec := RunSpec{
		WorkerPath:     "/bin/echo",
		WorkerArgs:     []string{"hello"},
		WorkerEnv:      []string{"FOO=bar"},
		TranscriptPath: "/tmp/transcript.ndjson",
		SocketPath:     "/tmp/sock",
		Token:          "secret-token",
		IdentityPath:   "/tmp/identity.json",
	}
	beforeCreate := time.Now()
	if err := s.CreateRun(ctx, "run-1", "proj-1", "ws-1", spec); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	run, err := s.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.ID != "run-1" {
		t.Errorf("ID = %q, want run-1", run.ID)
	}
	if run.ProjectID != "proj-1" || run.WorkstreamID != "ws-1" {
		t.Errorf("project/workstream = %q/%q, want proj-1/ws-1", run.ProjectID, run.WorkstreamID)
	}
	if run.State != StateCreated {
		t.Errorf("State = %q, want created", run.State)
	}
	if run.WorkerPath != "/bin/echo" {
		t.Errorf("WorkerPath = %q", run.WorkerPath)
	}
	if len(run.WorkerArgs) != 1 || run.WorkerArgs[0] != "hello" {
		t.Errorf("WorkerArgs = %v", run.WorkerArgs)
	}
	if len(run.WorkerEnv) != 1 || run.WorkerEnv[0] != "FOO=bar" {
		t.Errorf("WorkerEnv = %v", run.WorkerEnv)
	}
	if run.TranscriptPath != "/tmp/transcript.ndjson" {
		t.Errorf("TranscriptPath = %q", run.TranscriptPath)
	}
	if run.SocketPath != "/tmp/sock" || run.Token != "secret-token" || run.IdentityPath != "/tmp/identity.json" {
		t.Errorf("socket/token/identity = %q/%q/%q", run.SocketPath, run.Token, run.IdentityPath)
	}
	if run.SupervisorPID != 0 || run.SupervisorPGID != 0 || run.WorkerPID != 0 {
		t.Errorf("pids = %d/%d/%d, want all 0", run.SupervisorPID, run.SupervisorPGID, run.WorkerPID)
	}
	if run.CreatedAt.Before(beforeCreate) {
		t.Errorf("CreatedAt %v before create time %v", run.CreatedAt, beforeCreate)
	}
	if run.UpdatedAt.Before(run.CreatedAt) {
		t.Errorf("UpdatedAt %v before CreatedAt %v", run.UpdatedAt, run.CreatedAt)
	}
}
