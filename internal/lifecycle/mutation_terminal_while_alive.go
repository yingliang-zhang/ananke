//go:build mutation_terminal_while_alive

package lifecycle

func init() {
	mutationHooks.terminalWhileAlive = true
}
