// Package supervisor: mutation hooks. These variables default to false in
// production. Build-tag-controlled mutation files set them to true to alter
// behavior for mutation testing.
package supervisor

// mutationHooks holds flags set by build-tag mutation files.
var mutationHooks = struct {
	reapBeforeCleanup bool
	signalAfterReap   bool
	cancelParentOnly  bool
}{}
