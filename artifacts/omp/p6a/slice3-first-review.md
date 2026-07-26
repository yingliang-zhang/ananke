Working...
# SLICE CHANGES REQUESTED

Slice 3 is not frozen enough for runtime journal work. Three authority/recovery defects allow callers to manufacture evidence the contract describes as journal-verified. No direct effect or launch result was observed, but the forged capabilities/statuses would become unsafe foundations for later runtime code.

**Blocking defects**

1. **HIGH — Caller-constructed slot authority can mint committed capabilities and violate slot uniqueness.**

   Locations: `internal/repaircontract/supervisor_intent.go:146-185,210-229,241-275`; ineffective coverage at `internal/repaircontract/supervisor_intent_registry_test.go:90-92,283-291`; contrary claims at `docs/experiments/p6-controlled-repair-supervisor-intent.md:13-23,65-73`.

   `SupervisorClaimSlotAuthority` and `SupervisorIntentAuthority` are exported structs with public `Status`, tuple, slot ID, and committed canonical bytes. `EvaluateSupervisorIntentClaim` treats those caller-provided values as commit evidence and returns an opaque `VerifiedSupervisorIntentClaim`.

   Reproduced sequence:

   1. Start with the valid attempt-1 `VerifiedAuthorization`.
   2. Construct a second committed materialization authority with the same `(attempt_hash, phase)` but a different `claim_id` and `slot_id`.
   3. Rehash matching claim bytes and place them in public `CommittedClaimCanonical`.
   4. Evaluation returns `exact_replay` and a non-nil committed capability.
   5. Construct adapter authority reusing the materialization slot ID, supply the valid predecessor, and rehash matching adapter bytes.
   6. Evaluation again returns `exact_replay` and a non-nil capability.

   This accepts both a second slot for one tuple and one slot ID aliased across phases. `duplicateSupervisorSlotProbe` only sets the caller-controlled status to `duplicate`; it does not test an actual alias. `PredecessorTerminalEventHash` has the same problem: it is only a public hash string, not verified terminal-event evidence.

   Required correction: make journal-slot/commit authority an opaque capability minted by the trusted journal verifier, with privately retained and integrity-checked tuple, slot ID, journal head, committed bytes, and uniqueness result. Require opaque verified terminal-event evidence for phases 2–3. Add negative probes for two committed slots on one tuple, one slot ID across tuples/phases, and an invented terminal-event hash.

2. **HIGH — Stale committed replay reconstructs a predecessor capability.**

   Location: `internal/repaircontract/supervisor_intent.go:210-229,300-305`.

   Line 212 enables freshness checking only for an empty slot. A committed slot follows the clock-independent path at line 304 and still returns a new `VerifiedSupervisorIntentClaim`.

   Reproduced sequence:

   1. Use the canonical committed materialization claim and valid authorization.
   2. Set `now` exactly to the claim’s exclusive `not_after`.
   3. Call `EvaluateSupervisorIntentClaim`.
   4. Result: `Disposition=exact_replay`, `EffectAllowed=false`, no error, and a non-nil predecessor capability.

   Required correction: stale claim, authorization, dispatch, or release state must not mint a new capability. Historical bytes may remain classifiable as exact replay, but capability creation must require live freshness or an opaque commit proof retained from the original live commit path.

3. **HIGH — Recovery durability is represented by unbound hashes and caller booleans.**

   Locations: `internal/repaircontract/supervisor_intent.go:434-458,475-518`; overclaim at `docs/experiments/p6-controlled-repair-supervisor-intent.md:77-87`.

   Recovery records contain only `Kind`, `RecordHash`, `BootEpochHash`, and caller-supplied `Complete`. The authority is an array of caller-supplied kinds/hashes. There are no schema versions, canonical bytes, self-hash recomputation, attempt/phase/slot/journal-head binding, predecessor links, or durability evidence.

   Reproduced sequence:

   1. Invent current and recorded boot hashes.
   2. Invent five unrelated hashes for the five record kinds.
   3. Put the same hashes into `ExpectedRecords`.
   4. Submit matching records with `Complete=true`.
   5. Request response replay at `after_response_persistence`.
   6. Result: no error and `{DurableObservation:response_persisted, Disposition:response_replay, EffectAllowed:false}`.

   The classifier therefore validates agreement between two caller declarations, not durable journal records. No effect is exposed, but the returned durable status is forgeable.

   Required correction: consume opaque verified journal-record observations, or closed canonical record schemas whose hashes are recomputed and bind attempt, phase, claim, slot, boot epoch, journal head, predecessor record, durability policy, and record order. `Complete` and `Ambiguous` must be verifier results, not public trust assertions.

4. **MEDIUM — Unsafe negative API constants and decorative/nondeterministic probes.**

   - `RecoveryLaunch` and `RecoveryReplayResponseAndEffect` at `internal/repaircontract/supervisor_intent.go:45-46` are permanently rejected by `ClassifySupervisorRecovery` at lines 495–500. They are distinct from recovery dispositions, are never returned, and did not authorize an effect. They nevertheless advertise impossible operations as exported production actions. Remove them from the public surface; construct invalid values only in tests.
   - `duplicate_phase` at `supervisor_intent_registry_test.go:90` duplicates a JSON member and overlaps generic duplicate-key coverage. It does not submit two phase claims.
   - `duplicate_slot` only supplies `Status=duplicate`; it misses the accepted alias sequence above.
   - `priorEpochWaitingForHumanProbe` iterates a map at `supervisor_intent_registry_test.go:403`, making its internal probe order nondeterministic. Use an ordered table.

**Behavior that held**

- Exactly three phase constants and sequences are enforced.
- The claim validator binds authorization, approval, policy, all five P4 hashes, fence and claim token, request/dispatch/channel/peer, boot epoch, journal head/slot, repository/base, executable/sandbox/namespace/root, durability, timestamps, predecessors, and claim hash.
- Attempt 2 has distinct authorization, approval, request, dispatch, attempt hash, claims, slots, and predecessors; attempt-1 reuse probes reject.
- Empty valid slots return only `awaiting_journal_commit`, no capability, and `effect_allowed=false`.
- Changed committed bytes return `ErrSupervisorClaimConflict`; explicit duplicate/invalid statuses return `ErrInvalidSupervisorIntent`.
- Deep copies isolate caller mutation of raw and committed canonical bytes.
- Claim lifetime and freshness boundaries pass at $N-1\text{ns}$, $N$, and $N+1\text{ns}$ for the empty-slot path.
- Trailing JSON, invalid UTF-8, lone surrogates, Unicode noncharacters, unknown members, duplicate members, and noncanonical JSON map to stable supervisor sentinel errors.
- All successful recovery results currently have `effect_allowed=false`; no launch disposition exists.
- All ten cut points executed: before/after claim commit, phase launch, terminal proof, attestation signature, and response persistence.
- Code inventory, registry, and normative document contain the same 64 unique vector IDs in the same order; all 64 registry entries have executable probes. The semantic gaps above remain despite that mechanical completeness.
- The 24 accepted Slice 1–2 manifest entries are byte-identical to the accepted R3 baseline. The Slice-3 manifest adds only the four scoped Go files and normative intent document. No shared helper changed, and no new Slice 1–2 regression was reproduced.
- No production store, filesystem, transport, process, sandbox, UID, signing, or runtime effect exists in the Slice-3 code.

**Commands and actual results**

```text
go test ./internal/repaircontract -run '^TestP6Slice3' -count=1
ok github.com/yingliang-zhang/ananke/internal/repaircontract 1.674s

go test ./internal/repaircontract -count=1
ok github.com/yingliang-zhang/ananke/internal/repaircontract 1.792s

go test ./internal/repaircontract -count=10
ok github.com/yingliang-zhang/ananke/internal/repaircontract 15.735s

go test -race ./internal/repaircontract -count=3
ok github.com/yingliang-zhang/ananke/internal/repaircontract 46.299s
```

```text
go test ./internal/repaircontract \
  -run '^(TestP6Slice3CrashRecoveryMatrix|TestP6Slice3ExecutableVectorRegistry)$' \
  -count=1 -v

PASS — all 64 registry subtests and all 10 crash-cut subtests executed
ok github.com/yingliang-zhang/ananke/internal/repaircontract 1.656s
```

```text
go vet ./internal/repaircontract
PASS — no output

git diff --check -- \
  internal/repaircontract/supervisor_intent.go \
  internal/repaircontract/supervisor_intent_test.go \
  internal/repaircontract/supervisor_intent_registry_test.go \
  internal/repaircontract/supervisor_intent_document_test.go \
  docs/experiments/p6-controlled-repair-supervisor-intent.md
PASS — no output

gofmt -d internal/repaircontract/supervisor_intent*.go
PASS — no output
```

Read-only `/tmp` overlay boundary probes passed in `0.788s`, including lifetime/freshness $N-1/N/N+1$ and canonical envelope failures.

The three adversarial overlay probes reproduced the blockers:

```text
go test -overlay=/tmp/ananke_slice3_hard_review_overlay.json \
  ./internal/repaircontract \
  -run '^TestHardReview(CommittedReplay|CallerConstructed|RecoveryAccepts)' \
  -count=1 -v
```

```text
FAIL stale exact replay minted predecessor capability at exclusive not_after
FAIL caller-constructed authorities minted capabilities for both a second slot
     on one tuple and one slot ID aliased across phases
FAIL unbound caller hashes and Complete booleans classified as a durable response
FAIL github.com/yingliang-zhang/ananke/internal/repaircontract 0.370s
```

**Final manifest integrity**

Before and after review:

```text
sha256:8d99981593745eaf614e3609fbe993433176e7d09746ebcc38474d3c675c9816
```

- 29 entries.
- Every listed size and SHA-256 matched before review.
- Every listed size and SHA-256 matched after review.
- 388,644 total listed bytes.
- Manifest bytes unchanged.
- No per-file review drift.
- No repository file was edited; only `/tmp` overlay files were created.
- No cron job was created.

**Later-slice obligations, not additional Slice-3 defects**

- Implement the actual `FULL+fullfsync` journal, atomic unique `(attempt_hash, phase)` constraint, and live commit-confirmation capability.
- Persist and verify boot epochs, launch/terminal records, signed `waiting_for_human`, attestation signatures, and replayable responses.
- Implement process/worktree/test-root/sandbox effects behind the live commit gate, with recovery permanently unable to launch.
- Independently review runtime, storage, signing, and effect behavior.

This verdict covers only the frozen Slice-3 contract candidate. It is not runtime, storage, full-P6, or `DESIGN ACCEPT` approval.
