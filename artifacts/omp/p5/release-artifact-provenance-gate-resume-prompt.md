Resume exact release-artifact session 019f9d22-e713-7000-ae84-de9a3b115d7f. Continue with tools/edits; do not merely synthesize. Existing internal/releaseartifact tests pass, but no production/operator entrypoint calls BuildAndPublishTrustedSupervisor or VerifyTrustedSupervisor.

Add a minimal Go CLI (English code/docs) for the release workflow, e.g. `cmd/ananke-trusted-supervisor-release`, with strict explicit absolute paths and closed arguments. It must support:
- build/publish from an explicit repository root to explicit output using BuildAndPublishTrustedSupervisor;
- verify an exact existing artifact using VerifyTrustedSupervisor;
- no default output, no repository-root production name, no env/tag bypass.
Add CLI tests for argument closure and tagged artifact rejection through verify mode. Update runbook with exact commands and state that successful verification applies only to the exact output path. Ensure build workflow is actually operator-callable.

Then run gofmt, release focused count=10, race=3, CLI package tests, full go test, vet, diff check; ensure root `ananke-trusted-supervisor` absent. Do not touch cleanup authority blocker; no commit.