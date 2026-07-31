Working...
Implemented the P1b migration-integrity repair. No commit or push.

- `migrateV7` now invokes immutable `createTaskProposalSchemaV7`.
- `migrateV8` invokes distinct composite-identity `createTaskProposalSchemaV8`.
- `migrateV9` retains `createTaskProposalSchemaV9` for the reciprocal Approval/Lifecycle FK.
- Removed the duplicate historical-v7 DDL test helper.
- `TestProposalV7DataUpgradesToCompositeIdentityForeignKeys` now:
  - applies `migrations[:7]`,
  - asserts historical v7 foreign-key shapes before seeding,
  - seeds a valid v7 chain,
  - migrates through v8/v9 via `Open`,
  - verifies replay, v8/v9 history rows, orphan-Approval rejection, and clean FK checks.
- Added factual RED/GREEN/verification evidence to `docs/experiment-ledger.md`.

**TDD evidence**

- RED: the focused upgrade test initially failed because `migrations[:7]` leaked the v8 full identity FK into `task_proposal_idempotency`.
- GREEN: focused v7→v8→v9 upgrade test passed.

**Verification passed**

```text
go test ./internal/store -run '^(TestSchemaVersionMigrationFromV1Fixture|TestSchemaVersionMigrationFromV2AddsOutboxDiagnostic|TestOpenRejectsInvalidSchemaVersionHistory|TestOpenMigratesValidOldSchemaHistoryToHead|TestProposalSchemaMigrationFromV6Fixture|TestProposalV7DataUpgradesToCompositeIdentityForeignKeys)$' -count=1 -timeout=90s

go test ./internal/store -count=1 -timeout=180s

go test -race ./internal/store -count=1 -timeout=180s

node contracts/p1a/verify.mjs
```

The removed duplicate helper `createHistoricalV7TaskProposalSchema` has no remaining references.
