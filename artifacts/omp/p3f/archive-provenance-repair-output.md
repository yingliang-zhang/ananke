Working...
Implemented P3f archive provenance repair.

- `p3fTrackedSourceManifest` now binds `ArchiveSHA256`; its canonical self-hash includes that field.
- Activation and launch request independently carry the same archive digest; identity checks reject drift at initial and final fence boundaries.
- `stagePinnedArchive` SHA-256s the duplicated inherited archive descriptor **before** creating the staging directory. It requires equality with both activation and manifest bindings.
- PAX `comment` commit parsing remains, but runs only after byte-digest validation as consistency metadata—not archive authentication.
- Added `TestP3FRejectsForgedPAXArchiveBeforeFakeChild`:
  - hand-builds a non-Git tar,
  - supplies the expected PAX commit,
  - supplies exact expected member names and content hashes,
  - proves its archive bytes differ,
  - requires rejection at `tracked_archive` before sandbox/fake-child execution.
- Added archive-hash drift cases to initial and final-boundary identity tests.
- Updated `docs/experiment-ledger.md` with RED/GREEN evidence and boundary rationale.

No static `contracts/p3f` digest was fabricated: those declaration-only fixtures contain no archive bytes to authenticate. The new binding is on each temporary synthetic Git archive’s test-only canonical manifest.

Verification passed:

- RED: forged-PAX test previously reached `sandbox` instead of failing at `tracked_archive`.
- Focused: `go test -v ./internal/lifecycle -run '^TestP3F' -count=1 -timeout=120s`
- Full: `go test ./... -count=1 -timeout=300s`
- Race: `go test -race ./... -count=1 -timeout=360s`
- Vet: `go vet ./...`
- Contracts: syntax, normal, and self-test verification for P1a, P1c, P2a, P2c, P3a, P3d, and P3f.

The only Git activity remains temporary synthetic repositories inside the test suite; no real OMP, wrapper, source target, or repository commit was used.
