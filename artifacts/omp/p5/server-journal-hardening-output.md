Working...
Implemented only production-server findings **#1 and #2**.

### Changes

- `internal/trustedsupervisor/server_journal.go`
  - Rejects absolute journal paths containing `?`, `&`, `%`, or `#` before `unix.Open` or SQLite sees the path.
  - Validates all journal content before signing, replaying, or loading.
  - Recomputes both byte hashes before returning durable bytes.
  - Preserves exact replay bytes and transactional replay insertion.

- `internal/trustedsupervisor/server_journal_validation.go`
  - Pins exact `sqlite_master` inventory and SQL for:
    - tables;
    - implicit indexes;
    - immutability triggers;
    - `sqlite_sequence`.
  - Detects extra/altered indexes and same-name no-op triggers.
  - Validates migration version, ID, timestamp, future versions, and gapped history.
  - Validates request/response hashes, exact nonce inventory and roles, and replay fingerprints against their parent durable request.

- `internal/trustedsupervisor/server_journal_security_test.go`
  - Covers URI-metacharacter filenames without creating literal or redirected databases.
  - Covers symlink and live path replacement fail-closed behavior.
  - Covers mutated request bytes, response bytes, and both hash columns.
  - Covers startup, replay, load, and pre-signing validation.
  - Covers transitive nonce/replay fingerprint corruption.
  - Covers integrity-check-clean semantic corruption.
  - Covers altered table/index/trigger definitions and same-name no-op trigger.
  - Covers future and gapped migrations.
  - Existing exact-restart replay test remains byte-identical.

### TDD evidence

**RED:** New suite failed against the original implementation:

- URI-significant paths were accepted.
- Corrupted replay/load bytes and hash columns were returned or treated only as replay conflicts.
- Integrity-clean corrupt journals reopened successfully.
- Corrupted nonce/replay fingerprints reopened successfully.
- Altered tables, extra indexes, and no-op immutability triggers reopened successfully.

**GREEN:**

- Focused journal suite, `-count=10`: PASS
- Focused journal race suite, `-race -count=3`: PASS
- Full `internal/trustedsupervisor` package: PASS
- `go vet ./internal/trustedsupervisor`: PASS

### Residual finding #3

**Unchanged and still P1:** the wire request lacks the complete sealed predecessor projection. The server therefore cannot independently validate all predecessor and cancellation bindings enforced by the client.

### Residual finding #4

**Unchanged and still P1:** `Server.Close` can race accepted-connection admission between `AcceptUnix`, connection tracking, `WaitGroup.Add`, and shutdown resource release.

No wire protocol, server Close/admission, key loader, docs, or OMP changes. No commit or push performed.
