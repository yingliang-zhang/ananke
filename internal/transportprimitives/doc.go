// Package transportprimitives provides shared public cryptographic and
// Unix-transport primitives extracted from the P5 trusted-supervisor
// protocol. Both the P5 trusted-supervisor and the P6 controlled-repair
// runtime import these primitives to avoid code duplication while
// preserving identical canonical-JSON, Ed25519, framing, file-security,
// and Unix-peer-credential semantics.
package transportprimitives
