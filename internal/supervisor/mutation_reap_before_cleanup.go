//go:build mutation_reap_before_cleanup

package supervisor

func init() {
	mutationHooks.reapBeforeCleanup = true
}
