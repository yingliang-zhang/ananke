//go:build mutation_cancel_parent_only

package supervisor

func init() {
	mutationHooks.cancelParentOnly = true
}
