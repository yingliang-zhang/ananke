// Command ananke-supervisor is the process-group anchor for a single Ananke run.
//
// It is launched by the daemon (ananke) with flags pointing at the store, run
// id, worker binary, and identity/socket paths. It becomes a process-group
// leader, launches the worker, monitors for exit or cancellation, cleans up
// resistant descendants, reaps the worker, and commits a terminal transition
// with a finalization outbox row.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/yingliang-zhang/ananke/internal/supervisor"
)

func main() {
	var (
		storePath      = flag.String("store", "", "path to the SQLite store")
		runID          = flag.String("run", "", "run id")
		workerPath     = flag.String("worker", "", "worker binary path")
		workerArgs     = flag.String("worker-args", "", "comma-separated worker args")
		identityPath   = flag.String("identity", "", "identity file path")
		socketPath     = flag.String("socket", "", "unix socket path")
		transcriptPath = flag.String("transcript", "", "transcript file path")
		token          = flag.String("token", "", "auth token")
	)
	flag.Parse()

	if *storePath == "" || *runID == "" || *workerPath == "" || *socketPath == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "supervisor: --store, --run, --worker, --socket, --token are required")
		os.Exit(2)
	}

	var args []string
	if *workerArgs != "" {
		args = strings.Split(*workerArgs, ",")
	}

	cfg := supervisor.Config{
		StorePath:      *storePath,
		RunID:          *runID,
		WorkerPath:     *workerPath,
		WorkerArgs:     args,
		IdentityPath:   *identityPath,
		SocketPath:     *socketPath,
		TranscriptPath: *transcriptPath,
		Token:          *token,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := supervisor.New(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "supervisor: %v\n", err)
		os.Exit(1)
	}
	terminal, err := s.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "supervisor: %v\n", err)
		os.Exit(1)
	}
	_ = terminal
}
