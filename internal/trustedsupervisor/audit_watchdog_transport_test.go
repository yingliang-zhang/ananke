package trustedsupervisor

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func TestP5RuntimeAuthorityDeclaresClosedV3DirectExecContract(t *testing.T) {
	fixture := newAtomicRuntimeAuthorityFixture(t)
	if atomicOMPRuntimeAuthoritySchemaVersion != "ananke.atomic-omp-runtime-authority.v3" ||
		atomicOMPRuntimeAuthorityPolicyVersion != "root-owned-immutable-hierarchy-direct-exec.v3" {
		t.Fatalf("runtime authority versions = %q/%q, want explicit v3 direct-exec contract", atomicOMPRuntimeAuthoritySchemaVersion, atomicOMPRuntimeAuthorityPolicyVersion)
	}
	contents, err := marshalCanonical(fixture.entry.OMPRuntimeAuthority)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"launcher_mode":"sandbox_exec_direct_pinned_omp_v1"`,
		`"omp_argv_policy":"omp_print_exact_prompt_sudo_route_v1"`,
		`"output_transport":"supervisor_bounded_stdout_private_file_v1"`,
		`"sandbox_target_policy":"exact_pinned_omp_executable_v1"`,
		`"timeout_owner":"trusted_supervisor_typed_observation_v1"`,
	} {
		if !bytes.Contains(contents, []byte(field)) {
			t.Fatalf("canonical runtime authority lacks %s", field)
		}
	}
	for _, forbidden := range []string{`"bootstrap_sha256"`, `"framed_wrapper_stream_sha256"`, `"watchdog_wait_fd"`, `"watchdog_wait_fd_policy"`} {
		if bytes.Contains(contents, []byte(forbidden)) {
			t.Fatalf("canonical v3 runtime authority retained legacy active field %s", forbidden)
		}
	}
}

func TestP5DirectPinnedOMPEarlyExitLeavesVerifiedGroupGonePromptly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin process-group contract")
	}
	ompPath := filepath.Join(t.TempDir(), "omp")
	if err := os.WriteFile(ompPath, []byte("#!/bin/sh\n/bin/sleep 0.1\nexit 0\n"), 0o500); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(ompPath)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waiter := startAuditProcessWaiter(command)
	identity, err := inspectAuditProcess(command.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = terminateOwnedAuditProcess(context.Background(), identity, waiter, systemAuditProcessOperations{}, directP5TerminationBounds(500*time.Millisecond))
	}()
	if err := waiter.await(context.Background(), 3*time.Second); err != nil {
		t.Fatalf("wait direct pinned OMP: %v", err)
	}
	started := time.Now()
	confirmation := confirmAuditProcessExit(context.Background(), identity, waiter, systemAuditProcessOperations{}, directP5TerminationBounds(500*time.Millisecond))
	if confirmation.Outcome != auditTerminationConfirmedExit || confirmation.Failure != nil {
		t.Fatalf("direct OMP process group exit was not confirmed")
	}
	if elapsed := time.Since(started); elapsed > 400*time.Millisecond {
		t.Fatalf("verified direct OMP group disappearance took %v", elapsed)
	}
}

func directP5TerminationBounds(residual time.Duration) auditTerminationBounds {
	return auditTerminationBounds{
		TermGrace: residual, KillGrace: residual, ResidualExitGrace: residual, PollInterval: 5 * time.Millisecond,
	}
}
