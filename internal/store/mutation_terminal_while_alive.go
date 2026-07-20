//go:build mutation_terminal_while_alive

package store

func init() {
	mutationHooks.allowIncompleteTerminalTranscript = true
}
