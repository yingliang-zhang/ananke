Working...
Implemented P6 Slice-3 Repair B. Status remains **Repair A+B candidate pending independent frozen-source review**—no Slice ACCEPT, full-P6, runtime, or durability implementation claim.

### Completed

- Replaced the normative machine-contract JSON with the exact compiled manifest:
  - recovery schema `ananke.controlled-repair-supervisor-recovery-record.v1`
  - actions `status_only`, `replay_response`
  - exact deterministic five-record recovery fixture hashes
  - exact 99-vector ordered registry
  - unchanged 91-vector Slices 1–2 registry
- Updated normative prose for:
  - canonical RFC 8785/JCS self-hashed recovery records
  - opaque, private, integrity-rechecked `VerifiedSupervisorRecoverySnapshot`
  - verified complete/unambiguous empty-journal snapshots
  - no production durability-capability constructor or decoder
  - response replay only for the exact five-record prefix
  - `EffectAllowed=false` for every successful classification
  - explicit exclusion of runtime, store, filesystem durability, launch, attestation signing, and response persistence

### RED evidence

Previously captured blocker reproduction:

```text
go test ./internal/repaircontract \
  -run '^TestP6Slice3RepairBRejectsCallerForgedRecoveryEvidence$' \
  -count=1 -v -timeout 120s
```

```text
caller-forged recovery evidence classified as durable: result={DurableObservation:response_persisted Disposition:response_replay EffectAllowed:false NextRequirement:no_further_effect_permitted} err=<nil>
FAIL
```

The new API probes also initially failed to compile with representative errors:

```text
undefined: VerifiedSupervisorRecoverySnapshot
undefined: DecodeSupervisorRecoveryRecord
cannot use RecoveryReplayResponse ... as SupervisorRecoveryObservation
```

### Final verification

All requested gates passed:

```text
TestP6Slice3NormativeDocumentMatchesTypesFixturesAndInventory
PASS
ok github.com/yingliang-zhang/ananke/internal/repaircontract 0.761s
```

```text
go test ./internal/repaircontract \
  -run '^(TestP6Slice3RepairB|TestP6Slice3ExecutableVectorRegistry)$' \
  -count=1 -v -timeout 120s

PASS
ok github.com/yingliang-zhang/ananke/internal/repaircontract 4.475s
```

```text
go test ./internal/repaircontract -run '^TestP6Slice3' -count=1 -v -timeout 180s

PASS
ok github.com/yingliang-zhang/ananke/internal/repaircontract 5.937s
```

```text
go test ./internal/repaircontract -count=1 -timeout 180s
ok github.com/yingliang-zhang/ananke/internal/repaircontract 6.573s
```

```text
go test ./internal/repaircontract -count=10 -timeout 300s
ok github.com/yingliang-zhang/ananke/internal/repaircontract 67.147s
```

```text
go test -race ./internal/repaircontract -count=3 -timeout 300s
ok github.com/yingliang-zhang/ananke/internal/repaircontract 167.557s
```

Passed with no output:

```text
go vet ./internal/repaircontract
gofmt -d <changed Go files>
git diff --check -- internal/repaircontract docs/experiments/p6-controlled-repair-supervisor-intent.md
```

### Changed files

```text
docs/experiments/p6-controlled-repair-supervisor-intent.md
internal/repaircontract/supervisor_intent.go
internal/repaircontract/supervisor_intent_document_test.go
internal/repaircontract/supervisor_intent_registry_test.go
internal/repaircontract/supervisor_intent_repair_a_test.go
internal/repaircontract/supervisor_intent_test.go
internal/repaircontract/supervisor_recovery_repair_b_test.go
```

No commit, cron job, provider call, or next-slice work was performed.
