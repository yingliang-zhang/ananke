// Command ananke-trusted-supervisor serves the signed local Unix protocol boundary
// and can execute operator-pinned read-only audits selected by execution policy.
// Audits use an immutable Git snapshot, route-aware wrapper, Darwin sandbox,
// typed evidence, durable exact-session resume, and durable cancellation. The
// sandboxed wrapper cannot mutate repository or snapshot source. After confirmed
// process exit, the trusted supervisor scrubs and removes its owned snapshot and
// invocation roots. It cannot execute repairs or create Runs. A provider-free real
// OMP transport preflight has run; no real model or provider API canary has run.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/trustedsupervisor"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "trusted-supervisor: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("ananke-trusted-supervisor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	socketPath := flags.String("socket", "", "operator-selected Unix socket path")
	trustBundlePath := flags.String("trust-bundle", "", "operator-selected canonical public trust bundle path")
	privateKeyBundlePath := flags.String("private-key-bundle", "", "operator-selected owner-only private signing-key bundle path")
	journalPath := flags.String("journal", "", "operator-selected durable server SQLite journal path")
	repositoryPolicyPath := flags.String("repository-policy", "", "operator-selected canonical public repository identity policy path")
	executionPolicyPath := flags.String("execution-policy", "", "operator-selected owner-only canonical audit execution policy path")
	expectedClientUserID := flags.Int64("expected-client-uid", -1, "expected local client Unix user ID")
	runtimeUserID := flags.Int64("runtime-uid", -1, "untrusted audit child user ID")
	runtimeGroupID := flags.Int64("runtime-gid", -1, "untrusted audit child group ID")
	timeout := flags.Duration("timeout", 2*time.Second, "per-connection deadline (maximum 10s)")
	frameLimit := flags.Uint("max-frame-bytes", 64*1024, "frame limit (1024..65536)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *socketPath == "" || *trustBundlePath == "" || *privateKeyBundlePath == "" || *journalPath == "" ||
		*repositoryPolicyPath == "" || *executionPolicyPath == "" || *expectedClientUserID < 0 || *expectedClientUserID > int64(^uint32(0)) ||
		*runtimeUserID <= 0 || *runtimeUserID > int64(^uint32(0)) || *runtimeGroupID <= 0 || *runtimeGroupID > int64(^uint32(0)) {
		return fmt.Errorf("--socket, --trust-bundle, --private-key-bundle, --journal, --repository-policy, --execution-policy, valid --expected-client-uid, nonzero --runtime-uid, and nonzero --runtime-gid are required")
	}
	if *frameLimit > uint(^uint32(0)) {
		return fmt.Errorf("invalid frame limit")
	}
	server, err := newTrustedSupervisorServer(trustedsupervisor.ServerConfig{
		SocketPath: *socketPath, TrustBundlePath: *trustBundlePath, PrivateKeyBundlePath: *privateKeyBundlePath,
		JournalPath: *journalPath, RepositoryPolicyPath: *repositoryPolicyPath, ExecutionPolicyPath: *executionPolicyPath,
		ExpectedPredecessorReleaseIdentity: lifecycle.ExternalSupervisorPredecessorReleaseIdentity(),
		ExpectedClientUserID:               uint32(*expectedClientUserID), ConnectionTimeout: *timeout, MaxFrameBytes: uint32(*frameLimit),
		RuntimeUserID: uint32(*runtimeUserID), RuntimeGroupID: uint32(*runtimeGroupID),
	})
	if err != nil {
		return err
	}
	defer server.Close()
	return server.Serve(ctx)
}
