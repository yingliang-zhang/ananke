Working...
Implemented P6 Slice-3 Repair A. No commit. Repair B was not started.

### Implemented

- Removed caller-constructible journal slot status and committed-byte authority.
- Added opaque, private, integrity-checked:
  - `VerifiedSupervisorClaimSlotCommit`
  - `VerifiedSupervisorTerminalEvent`
  - `VerifiedSupervisorIntentClaim`
- Enforced both uniqueness dimensions:
  - one slot per `(attempt_hash, phase)`;
  - one slot ID cannot serve another tuple or phase.
- Empty admission returns only `awaiting_journal_commit`; no capability or effect.
- Historical exact replay remains classifiable, but stale claim/authorization/dispatch/release state returns no predecessor capability.
- Authority validation now proves `not_after - 1ns` passes complete effect-time freshness.
- Canonical fixture `not_after` is the exclusive intersection of:
  - `created_at + MaxSupervisorIntentLifetime`;
  - dispatch expiry;
  - authorization expiry;
  - `approved_at + MaxApprovalAge + 1ns`.
- Added exhaustive opaque slot/terminal mutation, alias, and stale-boundary coverage.
- Replaced decorative duplicate vectors with real conflicting opaque-proof sequences.
- Registry and normative machine contract now contain 73 ordered vectors.
- Documentation explicitly leaves recovery-record authority and durability to Repair B.

## RED evidence

Initial opaque-capability probes:

```text
gofmt -w internal/repaircontract/supervisor_intent_repair_a_test.go &&
go test ./internal/repaircontract \
  -run '^TestP6Slice3RepairA' \
  -count=1 -timeout 120s
```

Result: expected build failure. Missing new API/types included:

```text
first.slotCommits undefined
authority.JournalSlotID undefined
undefined: VerifiedSupervisorClaimSlotCommit
undefined: VerifiedSupervisorTerminalEvent
too many arguments in call to EvaluateSupervisorIntentClaim
FAIL
```

Continuation reproduction:

```text
go test ./internal/repaircontract \
  -run '^(TestP6Slice3RepairAStaleExactReplayCapabilityBoundary|TestP6Slice3NormativeDocumentMatchesTypesFixturesAndInventory)$' \
  -count=1 -v -timeout 120s
```

Result: expected failure:

```text
TestP6Slice3RepairAStaleExactReplayCapabilityBoundary/N-1ns:
capability present=false, want true at 2026-07-26T12:05:09.999999999Z

TestP6Slice3NormativeDocumentMatchesTypesFixturesAndInventory:
normative machine contract drifted

FAIL
```

## GREEN evidence

Freshness boundary and stale replay vectors:

```text
go test ./internal/repaircontract \
  -run '^(TestP6Slice3RepairAStaleExactReplayCapabilityBoundary|TestP6Slice3ExecutableVectorRegistry/(exact_replay_capability_n_minus_1ns|exact_replay_no_capability_n|exact_replay_no_capability_n_plus_1ns|stale_authorization_exact_replay_no_capability|stale_dispatch_exact_replay_no_capability|expired_release_exact_replay_no_capability))$' \
  -count=1 -v -timeout 120s
```

Result:

```text
PASS
ok github.com/yingliang-zhang/ananke/internal/repaircontract 0.789s
```

Normative document synchronization:

```text
go test ./internal/repaircontract \
  -run '^TestP6Slice3NormativeDocumentMatchesTypesFixturesAndInventory$' \
  -count=1 -v -timeout 120s
```

Result:

```text
PASS
ok github.com/yingliang-zhang/ananke/internal/repaircontract 0.792s
```

All Slice-3 tests and all 73 registry vectors:

```text
go test ./internal/repaircontract \
  -run '^TestP6Slice3' \
  -count=1 -v -timeout 120s
```

Result:

```text
PASS
ok github.com/yingliang-zhang/ananke/internal/repaircontract 2.796s
```

Opaque proof mutation and alias suite:

```text
go test ./internal/repaircontract \
  -run '^TestP6Slice3RepairA(OpaqueProofMutationIsolation|RejectsConflictingOpaqueSlotProofs|RequiresOpaquePredecessorTerminalEvent|StaleExactReplayCapabilityBoundary)$' \
  -count=1 -v -timeout 120s
```

Result:

```text
PASS
ok github.com/yingliang-zhang/ananke/internal/repaircontract 1.071s
```

Package verification:

```text
go test ./internal/repaircontract -count=1 -timeout 120s
```

```text
ok github.com/yingliang-zhang/ananke/internal/repaircontract 3.830s
```

```text
go test ./internal/repaircontract -count=10 -timeout 300s
```

```text
ok github.com/yingliang-zhang/ananke/internal/repaircontract 33.736s
```

```text
go test -race ./internal/repaircontract -count=3 -timeout 300s
```

```text
ok github.com/yingliang-zhang/ananke/internal/repaircontract 95.220s
```

Static and formatting checks:

```text
go vet ./internal/repaircontract
```

```text
PASS — no output
```

```text
gofmt -d internal/repaircontract/supervisor_intent.go \
  internal/repaircontract/supervisor_intent_test.go \
  internal/repaircontract/supervisor_intent_repair_a_test.go \
  internal/repaircontract/supervisor_intent_registry_test.go \
  internal/repaircontract/supervisor_intent_document_test.go
```

```text
PASS — no output
```

```text
git diff --check -- internal/repaircontract \
  docs/experiments/p6-controlled-repair-supervisor-intent.md
```

```text
PASS — no output
```

Removed-surface scans found no remaining public slot status/bytes types, unsafe recovery action constants, or production capability constructors.

## Changed files

- `internal/repaircontract/supervisor_intent.go`
- `internal/repaircontract/supervisor_intent_test.go`
- `internal/repaircontract/supervisor_intent_repair_a_test.go`
- `internal/repaircontract/supervisor_intent_registry_test.go`
- `internal/repaircontract/supervisor_intent_document_test.go`
- `docs/experiments/p6-controlled-repair-supervisor-intent.md`

## Open obligations

- Repair B: opaque or canonical self-hashed recovery records, real journal durability evidence, lineage binding, and non-caller-controlled completeness/ambiguity.
- Future reviewed journal implementation or seam for minting opaque commit and terminal-event capabilities.
- Runtime/store/process/effect implementation remains out of scope.
- Independent frozen-source review remains required. No Slice ACCEPT claim.
