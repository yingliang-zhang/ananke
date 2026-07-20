package lifecycle

import (
	"os"
	"os/signal"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// runHelper executes a helper-subprocess mode selected by the ANANKE_HELPER env
// var. Each mode exercises a slice of the lifecycle backend in a real process
// so the parent test can observe process-group behavior directly.
func runHelper(mode string) {
	switch mode {
	case "worker":
		helperWorker()
	case "resistant":
		helperResistant()
	default:
		os.Exit(2)
	}
}

// helperWorker reports {pid, pgid} then sleeps and exits with a configured code.
// Env: ANANKE_SLEEP_MS (default 2000), ANANKE_EXIT (default 0).
func helperWorker() {
	writeResultFile(os.Getenv(resultEnv), map[string]any{
		"pid":  os.Getpid(),
		"pgid": pgidOf(0),
	})
	sleep := envInt("ANANKE_SLEEP_MS", 2000)
	if sleep > 0 {
		time.Sleep(time.Duration(sleep) * time.Millisecond)
	}
	os.Exit(envInt("ANANKE_EXIT", 0))
}

// helperResistant ignores SIGTERM and sleeps until SIGKILL. Reports {pid, pgid}.
func helperResistant() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, unix.SIGTERM)
	go func() {
		for range ch {
			// swallow SIGTERM
		}
	}()
	writeResultFile(os.Getenv(resultEnv), map[string]any{
		"pid":  os.Getpid(),
		"pgid": pgidOf(0),
	})
	time.Sleep(5 * time.Minute)
	os.Exit(0)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func pgidOf(pid int) int {
	g, err := unix.Getpgid(pid)
	if err != nil {
		return 0
	}
	return g
}
