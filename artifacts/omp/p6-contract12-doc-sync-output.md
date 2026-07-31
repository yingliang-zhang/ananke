Working...
Updated only `docs/experiments/p6-controlled-repair-supervisor-contract.md`. No code edits. No commit.

Corrections include:

- 14 explicit embedded artifacts: 8 DER/SPKI files and 6 JSON declarations.
- 13 exact manifest entries, in canonical order with matching hashes.
- Current release-pins, trust-bundle, manifest, and rotation-policy hashes.
- Complete fixed rotation-approver identity and certificate/SPKI hashes.
- Both independent Ed25519 X.509 chains, certificate semantics, SPKI separation, and V1 `no_successor_authorized` state with no active signature instance.
- All 91 ordered `canonicalAcceptanceVectorIDs`.
- Executable registry test name: `TestP6A2ExecutableAcceptanceVectorRegistry`.
- Stale pre-B2 hashes, counts, role, and claims removed.

Verification:

```text
artifact comparison: PASS — 14 explicit embeds (8 DER/SPKI + 6 JSON), 13 ordered manifest entries
artifact hashes: PASS — all 13 manifest hashes match source bytes and documentation
release pins: PASS — documented pin/mode values match canonical fixture, including required four hashes and approver identity
manifest order: PASS — documentation exactly matches release-manifest.json
vector comparison: PASS — 91/91 IDs exactly match canonicalAcceptanceVectorIDs in order
```

Stale pre-B2 search: no matches.

```text
$ go test ./internal/repaircontract -count=1
ok  	github.com/yingliang-zhang/ananke/internal/repaircontract	1.278s
```

```text
$ go vet ./internal/repaircontract
```

No output; exit success.

```text
$ git diff --check
```

No output; exit success.
