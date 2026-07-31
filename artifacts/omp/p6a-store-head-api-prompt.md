Add one missing read-only P6 API in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`, strict TDD, scope only `internal/store/p6_repair.go` and `internal/store/p6_repair_test.go`. No commit/docs.

Implement `GetP6RepairEventHead(ctx context.Context, attemptHash string) (P6RepairEvent, error)`:
- validate exact sha256 grammar;
- require the attempt exists;
- load the highest-sequence event through the existing private loader;
- revalidate canonical event JSON/hash, attempt hash, authorization binding, sequence/predecessor/state grammar;
- return ErrP6RepairNotFound for unknown attempts and fail closed on malformed/tampered/multiple-invalid state;
- no writes/effects; concurrent readers deterministic.
Add tests for prepared head, running head after append, terminal head, unknown/invalid hash, raw SQL tamper attempts blocked by insert-only design, and concurrent reads. Run focused count=10, race count=3, full store, gofmt, vet, diff-check. Do not alter admission/transition semantics.