// Package repairverifier implements the release-pinned repair verifier and
// dedicated key-role provisioning for the P6 controlled-repair runtime.
//
// The verifier uses frozen release pins and trust bundle from the
// repaircontract package to verify Ed25519 signatures on repair-review
// attestations. The key provisioning module loads the repair attestor's
// private key from an owner-only file and verifies it matches the public
// key pinned in the frozen trust bundle.
package repairverifier
