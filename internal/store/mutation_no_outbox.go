//go:build mutation_no_outbox

package store

func init() {
	mutationHooks.noOutbox = true
}
