// Command ananke-trusted-supervisor serves the signed, identity-only local Unix
// protocol boundary. It has no OMP, subprocess, network, source, artifact,
// evidence, repair, or Run capability.
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
	expectedClientUserID := flags.Int64("expected-client-uid", -1, "expected local client Unix user ID")
	timeout := flags.Duration("timeout", 2*time.Second, "per-connection deadline (maximum 10s)")
	frameLimit := flags.Uint("max-frame-bytes", 64*1024, "frame limit (1024..65536)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || *socketPath == "" || *trustBundlePath == "" || *privateKeyBundlePath == "" || *journalPath == "" ||
		*repositoryPolicyPath == "" || *expectedClientUserID < 0 || *expectedClientUserID > int64(^uint32(0)) {
		return fmt.Errorf("--socket, --trust-bundle, --private-key-bundle, --journal, --repository-policy, and valid --expected-client-uid are required")
	}
	if *frameLimit > uint(^uint32(0)) {
		return fmt.Errorf("invalid frame limit")
	}
	server, err := trustedsupervisor.NewServer(trustedsupervisor.ServerConfig{
		SocketPath: *socketPath, TrustBundlePath: *trustBundlePath, PrivateKeyBundlePath: *privateKeyBundlePath,
		JournalPath: *journalPath, RepositoryPolicyPath: *repositoryPolicyPath,
		ExpectedPredecessorReleaseIdentity: lifecycle.ExternalSupervisorPredecessorReleaseIdentity(),
		ExpectedClientUserID:               uint32(*expectedClientUserID), ConnectionTimeout: *timeout, MaxFrameBytes: uint32(*frameLimit),
	})
	if err != nil {
		return err
	}
	defer server.Close()
	return server.Serve(ctx)
}
