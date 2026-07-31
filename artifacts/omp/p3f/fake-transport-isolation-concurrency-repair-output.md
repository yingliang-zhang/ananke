Working...
Implemented P3f fake-transport review repairs.

- **Receipt persistence boundary**
  - Added `Store.DeliverAndPersistExternalSupervisorReceipt`.
  - Holds the immediate SQLite transaction through fake delivery, receipt authentication, exact receipt binding, insert, and commit.
  - Runtime now uses this atomic path; a durable receipt suppresses subsequent fake delivery callbacks.

- **Deterministic concurrent regression**
  - Added `TestP3FExternalSupervisorConcurrentSubmitAndRecoverPersistReceiptBeforeDuplicateFakeDelivery`.
  - Uses separate SQLite handles plus concurrent submit/recover goroutines and a receipt-authentication gate.
  - Asserts one fake attempt/delivery, exact receipt identity, no callback/cancellation/reconciliation inference, normalized fail-closed output, and no local Run.
  - RED reproduced: `attempts=2 deliveries=1`; GREEN passes after the atomic boundary change.

- **Production isolation guard**
  - Replaced the single-file guard with `TestP3FExternalSupervisorProductionCoreIsolatesInterfaceAndAuthenticator`.
  - Parses every compiler-selected non-test lifecycle source.
  - Requires exact transport signatures and exact runtime fields/types.
  - Rejects production concrete transport/authenticator implementations.
  - Rejects endpoint/process/network imports and authority-bearing identifiers in external-supervisor production sources.
  - Requires `p3fInProcessFakeSupervisor` to remain in `_test.go`.

- **Cleanup**
  - Removed duplicated P3f fixture staging setup.
  - Recorded TDD, implementation, and verification evidence in `docs/experiment-ledger.md`.

Verification passed:

```text
go test ./internal/store ./internal/lifecycle -run 'ExternalSupervisor' -count=1 -timeout=120s
go test ./... -count=1 -timeout=300s
go test -race ./... -count=1 -timeout=360s
go vet ./...
node --check contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs && node contracts/p3d/verify.mjs --self-test &&
node --check contracts/p3f/verify.mjs && node contracts/p3f/verify.mjs && node contracts/p3f/verify.mjs --self-test
```
