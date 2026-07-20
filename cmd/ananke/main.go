// Command ananke is the daemon that manages Ananke runs.
//
// It launches supervisors, monitors identity files, provides a local Unix
// socket JSON API, and runs the recovery loop described in ADR-0003.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
)

func main() {
	var (
		storePath     = flag.String("store", "ananke.sqlite", "path to the SQLite store")
		socketPath    = flag.String("socket", "ananke.sock", "path to the daemon API Unix socket")
		supervisorBin = flag.String("supervisor-bin", "ananke-supervisor", "path to the ananke-supervisor binary")
		dataDir       = flag.String("data-dir", "ananke-data", "directory for per-run files")
		token         = flag.String("token", "", "auth token (generated if empty)")
	)
	flag.Parse()

	eng, err := lifecycle.NewEngine(lifecycle.EngineConfig{
		StorePath:     *storePath,
		SocketPath:    *socketPath,
		SupervisorBin: *supervisorBin,
		DataDir:       *dataDir,
		Token:         *token,
		ReportError: func(err error) {
			fmt.Fprintf(os.Stderr, "ananke: background error: %v\n", err)
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ananke: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "ananke: listening on %s\n", *socketPath)
	if err := eng.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "ananke: %v\n", err)
		os.Exit(1)
	}
	_ = eng.Close()
}
