Working...
Implemented only `internal/trustedsupervisor/process_e2e_test.go`.

Changes:
- Impostor mode now uses a distinct deterministic Ed25519 seed namespace and distinct trust-root IDs.
- Strict-mode fixture seeds, root identities, and frozen predecessor envelope values remain unchanged.
- Added precondition assertions that legitimate/impostor:
  - peer public keys differ;
  - peer SPKI hashes differ;
  - root identities differ;
  - trust-bundle hashes differ;
  - public trust bundles contain no private data.
- Client still loads `legitimate.bundlePath` while expecting `impostor.pid`; rejection therefore proves failure to possess the legitimate private key, not PID mismatch.
- Added the frozen-envelope assertion to the path-replacement test.

TDD evidence:
- Before fix: exact regression failed because the impostor became durable authority.
- After fix:
  - Exact regression `-count=10`: PASS
  - Exact regression `-race -count=3`: PASS
  - Production server tests: PASS
  - Signed production command/server paths: PASS
  - Full `internal/trustedsupervisor` package: PASS
  - `gofmt -d`: clean

No production code, documentation, or other packages modified. No commit or push.
