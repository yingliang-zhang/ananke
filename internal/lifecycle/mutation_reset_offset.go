//go:build mutation_reset_offset

package lifecycle

func init() {
	mutationHooks.resetOffset = true
}
