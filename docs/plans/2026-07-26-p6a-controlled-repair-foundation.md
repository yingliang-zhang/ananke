# P6 Controlled Repair Supervisor Contract-First Plan

> **For Hermes:** Freeze and independently review every contract slice before implementing storage or runtime code. Use strict RED→GREEN TDD after contract ACCEPT.

**Goal:** Produce a machine-verifiable, at-most-once controlled repair flow that lets Ananke request code changes while only a dedicated trusted supervisor may create worktrees, run an adapter/tests, and sign review evidence.

**Architecture:** Ananke owns durable GUI authorization, dispatch outbox, public trust pins, verification, and human review state. A separate `ananke-controlled-repair-supervisor` owns a FULL-sync effect journal, dedicated repair-attestor key, sandbox/UID leases, detached worktree effects, disposable test sandboxes, and signed canonical attestations. The adapter and project tests are untrusted. Ananke holds no repair private key and exposes no process/effect authority.

**Tech Stack:** Go, SQLite FULL/fullfsync journal, authenticated Unix sockets, Ed25519 detached signatures, Darwin Seatbelt, dedicated runtime UID leases, pinned Go toolchain/module-cache manifests, RFC 8785/JCS canonical records.

---

## Status and supersession

The first in-process candidate is rejected and archived at:

- `docs/plans/rejected/2026-07-26-p6a-controlled-repair-foundation-first-candidate.md`
- `artifacts/omp/p6a/first-hard-review-report.md`
- `artifacts/omp/p6a/design-rereview-report.md`

No task in the archived plan is actionable. Before any commit, remove or rewrite the uncommitted `internal/repairrunner` implementation and unreleased unsigned P6 review-evidence schema.

## Frozen trust boundaries

| Component | Trusted authority | Explicitly absent authority |
|---|---|---|
| Local GUI / Ananke | fresh human authorization, durable request/outbox, release-pinned public verifier, signed-attestation verification, human accept/reject | repair private key, adapter/test process launch, commit/ref/push/merge |
| Repair supervisor | FULL-sync phase claims, sandbox launch, terminal process proof, candidate/test verification, dedicated repair signature | human approval fabrication, commit/ref/push/merge, automatic crash resume |
| OMP adapter | proposed source edits inside one retained worktree | common `.git`, network except separately reviewed provider broker, credentials, journal/key paths, evidence authority |
| Project tests | execution inside disposable candidate copy | `.git`, original/retained worktree, network, external files/refs, supervisor state/keys |
| SQLite | immutable mirrored facts | trust bootstrap, caller-injected verifier, unsigned review-state authority |

The guarantee is **at-most-once automatic phase launch**, never exactly-once effects. Any crash after a claim may mean zero or partial effects and becomes signed `waiting_for_human`; it is never automatically resumed.

## Contract freeze sequence — no runtime/storage implementation before ACCEPT

### Slice 1: Trust bootstrap, role separation, and rotation

**Objective:** Make the repair signature verifier non-substitutable.

**Artifacts:**
- Create `docs/experiments/p6-controlled-repair-supervisor-contract.md`
- Create `internal/repaircontract/contract.go`
- Create `internal/repaircontract/canonical.go`
- Create `internal/repaircontract/contract_test.go`
- Create `internal/repaircontract/testdata/p6-contract-v1.json`

**Freeze:**
- release-pinned repair trust-bundle hash and repair-attestor leaf SPKI outside SQLite;
- exact bundle file identity and no-follow open;
- distinct role `controlled_repair_review_attestor`, leaf key, certificate, socket, journal, policy, and runtime identity;
- signature domain `ananke.controlled-repair.review-attestation.v1`;
- no TOFU, no database trust-root initialization/replacement, no caller-injected verifier;
- explicit cross-signed, release-approved rotation only;
- Ananke contains only public repair certificate/bundle; private key custody/zeroization remains in the dedicated service.

**Mandatory negative vectors:** attacker-generated self-consistent root, swapped bundle path/inode, protocol-adapter key used as repair key, wrong leaf role/SPKI, stale/revoked root, permissive verifier injection, database-only trust-root replacement.

### Slice 2: Authorization, dispatch outbox, and effect-time freshness

**Freeze:**
- authenticated GUI provenance and exact local operator identity;
- approval maximum age, maximum lifetime, and `approved_at <= dispatch/effect < not_after` checks;
- full non-projected P4 fact/fence/repository/base/path/test-profile/route/attempt binding;
- immutable outbox request and authenticated Unix channel/peer identities;
- attempt 1 and attempt 2 each require a new human authorization;
- no process/filesystem effect from Ananke.

**Mandatory vectors:** year-old approval, long future expiry, admitted-then-expired request, swapped P4/fence/base/profile/channel, duplicate/conflicting dispatch, restart replay.

### Slice 3: Supervisor intent, phase claims, and recovery

**Freeze three unique claims in the supervisor journal:**
1. `materialization_claim` before common-`.git`/worktree mutation;
2. `adapter_claim` before adapter spawn;
3. `test_claim` before test-root creation/spawn.

Each FULL/fullfsync claim binds authorization/policy/P4/fence/request/channel, predecessor claim/event, supervisor boot epoch, repository/executable/sandbox/namespace identities, sequence, and unique `(attempt_hash, phase)`.

**Semantics:** only the live executor receiving commit confirmation may launch; duplicate callers perform zero effects; no recovery sweep launches a pending/claimed intent; prior-epoch nonterminal claims become signed `waiting_for_human`; attempt 2 is a new chain.

**Crash matrix:** before/after every claim commit, before/after every launch, before/after terminal proof, before/after attestation signature, and before/after response persistence.

### Slice 4: Repository, common-`.git`, and retained worktree authority

**Freeze:**
- descriptor/device/inode/owner/mode identity for repository top-level, common `.git`, worktree parent/target/admin subtree, `.git` file, pinned Git executable and immutable ancestry;
- pre-effect hashes for local config, HEAD, refs, object inventory, index, hooks, attributes/config inputs, and existing worktree registry;
- reject includes, credentials, protocol helpers, filters, fsmonitor, hooks, external diff/textconv, and `extensions.worktreeConfig`;
- sole permitted common-`.git` delta is claim-derived `worktrees/<name>` from fixed `worktree add --detach --no-checkout`;
- no ref/branch/config/index/object delta;
- adapter cannot write common `.git`, worktree `.git`, config/admin/hook/attribute surfaces;
- ignored and ordinary untracked entries are reconciled with filesystem, porcelain-v2, raw, numstat, and patch views;
- retained worktree/admin subtree are never automatically removed or pruned; ambiguous partial state is retained for human intervention.

**Mandatory vectors:** ignored payload, worktree config/fsmonitor injection, common-`.git` A→B→A, config/include mutation, ref/object/index mutation, malformed/quoted/Unicode/NUL path, symlink/gitlink/binary/mode/rename/delete/untracked changes.

### Slice 5: Adapter sandbox and UID terminal proof

**Freeze:**
- adapter is a child process only; no production in-process interface;
- dedicated attempt UID or exclusive lease from a closed release-provisioned UID pool;
- journaled UID lease before spawn and no concurrent attempt sharing it;
- Seatbelt profile identities and exact write/read/network/exec capabilities;
- provider credentials only through the separately reviewed broker channel, never child argv/raw policy/evidence;
- on exit/timeout: reap leader, TERM/KILL original PGID, enumerate/kill every leased-UID process including new sessions, repeat until UID-empty, close descriptors, freeze roots, persist terminal proof, then and only then continue;
- terminal proof binds UID lease, leader, PGID, UID-empty observation, sandbox hash, root identities, descriptor closure, and cleanup result.

**Mandatory vectors:** ignored context, double-fork, `setsid`, closed stdio, delayed write/ref update, restart with child alive, UID reuse/contention, stale PID/epoch, broker/network escape.

**Operational prerequisite:** provision dedicated macOS repair runtime users/UID pool. This requires one manual administrator-authenticated step because passwordless sudo is unavailable. No production activation before it succeeds.

### Slice 6: Closed offline Go test profile

Ananke authorizes only ordered profile IDs/hashes. It supplies no executable path, argv, env, cwd, or cache path.

**Freeze one P6a profile:**
- release-pinned root-owned Go toolchain manifest and executable;
- fixed `go test ./... -count=1 -mod=readonly` command;
- fixed timeout/output/resource bounds;
- `CGO_ENABLED=0`, `GOENV=off`, `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off`, `GOVCS=*:off`, `GOWORK=off`;
- private HOME/TMPDIR/GOCACHE and release-pinned read-only module cache;
- fresh candidate copy/archive with no `.git`, remotes, credentials, original repo, retained worktree, journal, or key paths;
- no network; writes only in disposable build/cache/temp roots; exec only pinned toolchain and binaries produced there;
- same exclusive UID terminal proof as adapter; disposable root scrubbed and proven absent before signing.

**P6a non-goals:** cgo, network/integration/service tests, mutable external caches, Git metadata, missing downloads, custom caller commands.

**Mandatory vectors:** test tries git push/local ref write/network/external write/original-worktree mutation/arbitrary exec/fork escape/setsid/delayed mutation; missing module; cache drift; toolchain replacement.

### Slice 7: Canonical repair-review attestation

Freeze exact closed RFC 8785/JCS schema containing:
- schema/domain, attestation ID/hash/issued_at;
- release trust-bundle hash, repair certificate hash/root/leaf SPKI;
- transport request/response nonce/channel hashes;
- full P4 fact/fence, authorization/policy/approval/attempt, all phase-claim hashes;
- effect-time approval-validation timestamp;
- repository/common-`.git`/Git executable identities;
- worktree parent/target/admin/descriptor hashes;
- adapter profile/result/terminal-proof hashes;
- patch hash/size, ordered paths, status/raw/numstat/ignored/filesystem-scan hashes;
- ordered test profile/command/result/output hashes/sizes;
- test sandbox/terminal/cleanup proof hashes;
- supervisor journal head/predecessor hashes;
- state exactly `waiting_for_review`.

The Ed25519 signature covers the entire canonical attestation excluding only the signature field and includes explicit domain separation.

### Slice 8: Ananke verification and persistence

**Freeze:**
- internally loaded release-pinned verifier only;
- full durable P4/authorization/attempt/head reconstruction;
- signer role/certificate/root validity/revocation at issuance, signature, delivery freshness, channel/request/current-head checks;
- exact rejection of foreign request/attempt/claim/worktree/policy/signer/bundle;
- atomic immutable attestation bytes + sole `waiting_for_review` event;
- exact replay returns same row with no transport/effect;
- every read re-verifies signature against release pins; raw DB insertion cannot produce an accepted state;
- no unsigned/generic transition API.

### Slice 9: Pre-release schema/API cutover

Because v15/v16 and `internal/repairrunner` are uncommitted and unreleased:
- remove rejected exported effect/evidence APIs;
- squash/replace unreleased P6 migrations rather than preserve an obsolete unsigned schema;
- reject any populated local rejected-schema DB during migration instead of interpreting it;
- prove production binaries contain no in-process adapter, arbitrary-test runner, rejected API marker, or reused P5 protocol key.

## Contract acceptance gate

Before implementation:
1. canonical fixture and verifier tests pass at boundaries N−1/N/N+1 where counts are bounded;
2. all negative vectors mutate and consistently rehash linked data, then fail semantically;
3. frozen docs, Go types, canonical fixture, and verifier inventory agree;
4. independent fresh hard review returns `DESIGN ACCEPT`;
5. no storage migration, process launch, or production command is added before ACCEPT.

## Post-ACCEPT implementation order

1. Extract shared public cryptographic/Unix-transport primitives without changing P5 semantics.
2. Implement release-pinned repair verifier and dedicated key-role provisioning.
3. Replace unreleased P6 schema with signed attestation + outbox mirror contract.
4. Implement dedicated supervisor FULL-sync journal and at-most-once claim/recovery matrix.
5. Implement descriptor-bound worktree materialization and ignored/filesystem diff closure.
6. Implement sandboxed provider-free fake adapter with UID terminal proof.
7. Implement closed offline Go profile in disposable sandbox with the same terminal proof.
8. Implement signed attestation and Ananke atomic verification/persistence.
9. Run provider-free E2E ending only in verified `waiting_for_review`.
10. Independent implementation hard review; repair/resume until ACCEPT.
11. Only then add the real route-aware OMP repair adapter and repeat independent review.
12. Only after all gates wire GUI submission/status/evidence/accept-reject. No automatic commit/push/merge.

## Verification commands after implementation

Run focused RED/GREEN tests after each tracer bullet, then final gates:

- `go test ./internal/repaircontract -count=10`
- `go test -race ./internal/repaircontract -count=3`
- `go test ./internal/store ./internal/repairsupervisor -count=10`
- `go test -race ./internal/store ./internal/repairsupervisor -count=3`
- `go test ./... -count=1`
- `go test -race ./internal/store ./internal/repairsupervisor ./internal/trustedsupervisor -count=1`
- `go vet ./...`
- `git diff --check`

## Residual non-goals through P6a

- exactly-once effects;
- automatic crash resume/retry;
- real OMP/provider repair;
- cgo/network/integration/service tests;
- commit, ref/branch update, push, merge, rollback, prune, or worktree deletion;
- human accept/merge execution;
- future immutability of a retained worktree without revalidation at human acceptance.
