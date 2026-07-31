Working...
Implemented P3f durable external-supervisor fake transport integration.

- Production `externalSupervisorHandoffTransport` is now a private three-method interface; `externalSupervisorHandoffRuntime` holds only that interface. Static test rejects endpoint imports and non-interface wiring.
- Test-only in-process fake:
  - rejects unsealed envelopes;
  - issues/verifies identity-only fake receipts and callbacks;
  - supports withheld delivery responses;
  - records actual delivery invocation count, proving a durable receipt suppresses duplicate transport delivery.
- Added integration coverage for:
  - delivery admission and exact duplicate suppression;
  - callback-before-durable-receipt rejection;
  - authenticated trust-root, receipt, envelope, and attempt drift;
  - cancellation, stale-fence recovery, no-response recovery;
  - exact `waiting_for_human` projection and no inferred local Run outcome.
- Recorded RED→GREEN evidence and final gates in `docs/experiment-ledger.md`.

Verification passed:

```sh
go test ./internal/store ./internal/lifecycle -run 'ExternalSupervisor' -count=1 -timeout=120s
go test ./internal/lifecycle -run '^TestP3FExternalSupervisor' -count=1 -timeout=120s
go test ./... -count=1 -timeout=300s
go test -race ./... -count=1 -timeout=360s
go vet ./...
node --check contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs --self-test
node --check contracts/p3f/verify.mjs && node contracts/p3f/verify.mjs && node contracts/p3f/verify.mjs --self-test
```

No production network/RPC endpoint, concrete transport, P3f OMP integration, credential channel, raw source/path/evidence channel, commit, or push was added.
