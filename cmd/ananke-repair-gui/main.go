// Command ananke-repair-gui starts a web GUI for controlled repairs.
// Default model: kimi-k3 via OMP wrapper.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/yingliang-zhang/ananke/internal/gui"
	"github.com/yingliang-zhang/ananke/internal/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8420", "listen address")
	storePath := flag.String("store", "ananke-repair.sqlite", "path to SQLite store")
	wrapper := flag.String("wrapper", "", "OMP wrapper script path (default: ~/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh)")
	provider := flag.String("provider", "custom:sudo-kimi-k3", "OMP provider")
	model := flag.String("model", "t9s/kimi-k3", "OMP model")
	timeout := flag.Int("timeout", 300, "adapter timeout in seconds")
	flag.Parse()

	if *wrapper == "" {
		home, _ := os.UserHomeDir()
		*wrapper = home + "/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh"
	}

	s, err := store.Open(*storePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ananke-repair-gui: open store: %v\n", err)
		os.Exit(1)
	}
	defer s.Close()

	cfg := gui.RepairConfig{
		WrapperPath: *wrapper,
		Provider:    *provider,
		Model:       *model,
		Timeout:     *timeout,
	}

	api := gui.NewAPI(s, *addr, cfg)
	if err := api.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "ananke-repair-gui: start: %v\n", err)
		os.Exit(1)
	}
	defer api.Stop()

	fmt.Printf("Ananke Repair GUI\n")
	fmt.Printf("  URL:      http://%s\n", *addr)
	fmt.Printf("  Model:    %s (%s)\n", *model, *provider)
	fmt.Printf("  Wrapper:  %s\n", *wrapper)
	fmt.Printf("  Store:    %s\n", *storePath)
	fmt.Printf("  Timeout:  %ds\n\n", *timeout)
	fmt.Printf("Open http://%s in your browser to submit repairs.\n", *addr)
	fmt.Printf("Press Ctrl+C to stop.\n")

	// Wait for SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
