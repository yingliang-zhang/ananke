// Command ananke-supervisor owns one Ananke run while remaining outside the
// worker process group. It launches a paused group-leading trampoline,
// publishes authority, releases the real worker, performs group cleanup before
// exact reap, and commits terminal finalization.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/yingliang-zhang/ananke/internal/lifecycle"
	"github.com/yingliang-zhang/ananke/internal/supervisor"
)

func main() {
	if lifecycle.WorkerTrampolineRequested() {
		if err := lifecycle.RunWorkerTrampoline(); err != nil {
			fmt.Fprintf(os.Stderr, "worker trampoline: %v\n", err)
			os.Exit(125)
		}
		return
	}
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
