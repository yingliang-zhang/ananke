//go:build mutation_signal_after_reap

package supervisor

func init() {
	mutationHooks.signalAfterReap = true
}
