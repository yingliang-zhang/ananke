// Package store: mutation hooks. These variables default to false in
// production. Build-tag-controlled mutation files set them to true to alter
// behavior for mutation testing.
package store

// mutationHooks holds flags set by build-tag mutation files.
var mutationHooks = struct {
	noOutbox                          bool
	resetOffset                       bool
	allowIncompleteTerminalTranscript bool
}{}
