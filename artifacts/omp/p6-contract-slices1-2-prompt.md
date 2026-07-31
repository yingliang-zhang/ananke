Implement only P6 contract-freeze Slices 1 and 2 in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` using strict RED→GREEN TDD. Read the current `docs/plans/2026-07-26-p6a-controlled-repair-foundation.md`, especially Slice 1, Slice 2, and Contract acceptance gate. Scope is limited to NEW pure contract artifacts:
- `internal/repaircontract/contract.go`
- `internal/repaircontract/canonical.go`
- `internal/repaircontract/contract_test.go`
- `internal/repaircontract/testdata/p6-contract-v1.json`
- `docs/experiments/p6-controlled-repair-supervisor-contract.md`
Do not edit store, repairrunner, trusted-supervisor, command binaries, migrations, go.mod, or any runtime code. No process launch, DB, network, filesystem effects beyond writing these contract artifacts. No commit.

Goal: freeze machine-verifiable canonical contracts for (1) repair trust bootstrap/role separation/rotation and (2) GUI authorization + immutable dispatch.

Requirements:
- Closed typed schemas with exact key sets and schema versions. RFC 8785/JCS canonical bytes and `sha256:` lowercase hashes excluding each record's own hash field. Reject duplicate keys, unknown keys at every nesting level, trailing data, invalid UTF-8, lone Unicode surrogates/noncharacters where the existing project contract rejects them, malformed/noncanonical hashes, semantically invalid UTC timestamps.
- Freeze release-pinned fields outside DB: repair trust-bundle hash, bundle file identity hash, distinct repair-attestor certificate hash/root ID/leaf SPKI, release manifest/build identity hash, exact signature domain `ananke.controlled-repair.review-attestation.v1`. Explicitly prohibit TOFU/runtime install/database replacement/caller-injected verifier. Freeze rotation as a future-only cross-signed release-approved record; V1 normal requests cannot rotate.
- Distinct role exactly `controlled_repair_review_attestor`; reject the existing protocol-adapter role/key, wrong role/SPKI/root/bundle, self-consistent attacker root, mutable/unpinned bundle identity, permissive-verifier selection.
- Freeze authorization: local authenticated GUI operator identity/provenance, approved_at/not_after, max approval age and max lifetime constants, P4 input/bundle/admission + complete fence tuple, repository/base commit/tree, exact ordered writable paths, ordered supervisor-installed test profile IDs/hashes (no executable/argv/env/path), route/profile identity, attempt number/cap, policy/approval/authorization hashes.
- Freeze immutable dispatch/outbox: authorization/attempt, request ID/hash, channel binding hash, expected Unix peer identity, selected release-pinned repair supervisor policy/profile hashes, created_at/dispatch_not_after. Ananke has no process/effect/private-key fields.
- Semantic validator takes explicit `now` and validates admission and effect-time freshness; exact max bounds include N-1/N/N+1 tests. Use small explicit constants suitable for local GUI approval, document them.
- Cross-record verifier validates every identity/hash relationship, exact ordered lists, no duplicate paths/profiles, attempt 1..2 cap=2, full P4/fence nonprojection, dispatch within authorization validity, and no raw paths/keys/credentials/argv/env/private key/process/socket path in canonical fixture.
- Fixture includes valid records and a complete acceptance-vector inventory. Tests must mutate/recompute linked hashes consistently to prove semantic rejection, not only stale-hash rejection.
- Mandatory negative vectors: self-consistent attacker trust bundle/root; release pin mismatch; protocol leaf reused; wrong repair role/SPKI; revoked/stale/future cert; TOFU/database/runtime/permissive-verifier mode; year-old approval; overlong lifetime; admitted then expired at dispatch/effect; swapped P4/fence/repo/base/profile/channel/peer/policy; duplicate/conflicting dispatch; restart exact replay identity vs conflict; unknown secret-looking field and no leaked diagnostic.
- Docs must clearly state these are pure contract/fixture/verifier artifacts and claim no storage/runtime/sandbox implementation.
- Run focused tests count=10, race count=3, gofmt, vet package, diff-check. Return RED evidence, files, exact results, and gaps.
