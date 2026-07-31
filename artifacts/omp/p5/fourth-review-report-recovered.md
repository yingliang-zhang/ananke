# CHANGES REQUESTED

Base reviewed: `1bbc880576173913d62f13200ea54b25d46f4393`

The current source closes important parts of the third review—bounded owned-process termination, Ed25519-authenticated policy-aware history, and a passing provider-free installed-OMP preflight—but does not satisfy the complete P5 security and durability contract.

## 1. High — Physical root overlap is checked lexically, not by Darwin filesystem identity

**Location**

- `internal/trustedsupervisor/execution_policy.go:306-327`
- `internal/trustedsupervisor/execution_policy.go:808-818`, `pathsOverlap`
- Physical aliases are otherwise recognized by `internal/trustedsupervisor/audit_executor.go:1287-1303`, `sandboxPathVariants`

**Exploit/failure mechanism**

`pathsOverlap` uses only `filepath.Clean` and `filepath.Rel`. On Darwin, `/var` resolves to `/private/var`. A policy can therefore present apparently separate paths such as:

```text
/var/ananke-root
/private/var/ananke-root/child
```

The lexical comparison reports no overlap, while the physical paths are nested:

```text
lexical_common= /
real_a= /private/var/ananke-root
real_b= /private/var/ananke-root/child
real_common= /private/var/ananke-root
```

This bypasses separation among prompt, output, session, work, temporary, repository, native-addon, or executable authorities. Cleanup of one nominal root can remove another root’s data; write or read authority can cross the intended boundary.

**Why existing tests miss it**

- `internal/trustedsupervisor/execution_policy_test.go:57` tests only lexically identical overlapping roots.
- `internal/trustedsupervisor/audit_executor_test.go:98-108` verifies `/var` and `/private/var` sandbox variants for native write denial, not policy-root overlap.
- No policy-loading test supplies alias-equivalent or physically nested roots.

**Required production change**

Resolve and pin physical directory identity before containment checks, or compare descriptor-backed ancestry. Reject alias-equivalent and physically nested roots before any process, snapshot, or cleanup effect.

**Required regression test**

Add Darwin policy tests covering both `/var → /private/var` directions for:

- every pair among the five invocation roots;
- repository versus each invocation root;
- native addon versus repository and invocation roots;
- allowed-test executable roots.

Every case must fail during policy loading.

---

## 2. High — `localhost:<port>` sandbox authority permits an uncontrolled IPv6 listener

**Location**

- IPv4-only gateway listener: `internal/trustedsupervisor/audit_connect_broker.go:100`
- IPv4 address converted to `localhost`: `internal/trustedsupervisor/audit_executor.go:899-906`, `auditSandboxBrokerAddress`
- Sandbox network rule: `internal/trustedsupervisor/audit_executor.go:1268-1270`

**Exploit/failure mechanism**

The trusted gateway owns only `127.0.0.1:<port>`, but the sandbox rule allows:

```scheme
(allow network-outbound (remote tcp "localhost:<port>"))
```

A direct Darwin probe proved that this rule also permits `::1:<port>`. An unrelated same-UID process can bind the IPv6 address on the same port and receive traffic from the sandboxed audit process while that process holds `SUDO_API_KEY`.

The generated model registry currently specifies IPv4, but confinement cannot rely on the invoked executable voluntarily honoring that configuration.

**Why existing tests miss it**

- `internal/trustedsupervisor/audit_executor_test.go:425-449` asserts that the profile contains the `localhost` string; it does not exercise IPv6.
- The installed preflight verifies the expected IPv4 request but does not run a competing `::1` listener.
- No test asserts that every sandbox-permitted socket is owned by the gateway.

**Required production change**

Constrain the sandbox to exact IPv4 `127.0.0.1:<port>`, or have the trusted gateway own every address admitted by the rule. Do not use the ambiguous `localhost` authority.

**Required regression test**

Start:

1. the production IPv4 gateway;
2. an independent IPv6 listener on `::1` using the same port.

From the sandboxed subprocess, prove the IPv4 gateway is reachable and the IPv6 listener is not. Assert the fake credential never reaches the IPv6 listener.

---

## 3. High — The upstream HTTP client follows redirects beyond the policy-pinned route

**Location**

- Client construction without `CheckRedirect`: `internal/trustedsupervisor/audit_connect_broker.go:126-138`
- Initial exact request construction: `internal/trustedsupervisor/audit_connect_broker.go:272-280`
- Pinned hostname/port dialer: `internal/trustedsupervisor/audit_connect_broker.go:525-544`
- Current broker tests: `internal/trustedsupervisor/audit_connect_broker_test.go:12-79`

**Exploit/failure mechanism**

A nil `CheckRedirect` enables Go’s default redirect behavior. A same-authority `307` or `308` can issue a second credential-bearing POST to a path other than `/v1/responses`. `301`, `302`, or `303` can alter method semantics. The dialer constrains hostname and port, but it does not constrain the redirected path.

This violates the closed contract of one policy-pinned `POST /v1/responses`.

**Why existing tests miss it**

The broker tests cover unsafe DNS resolution and close/reap behavior only. They contain no redirect response or second-request assertion.

**Required production change**

Set `CheckRedirect` to reject every redirect, or return the first redirect response without following it.

**Required regression test**

Exercise same-authority `301`, `302`, `303`, `307`, and `308` responses. For each case, assert:

- exactly one upstream request occurred;
- its method/path were `POST /v1/responses`;
- no authorization header reached the redirect target.

---

## 4. High — Installed OMP and native-addon validation is not atomic with execution/loading

**Location**

- OMP root check: `internal/trustedsupervisor/execution_policy.go:509-518`
- OMP path read/hash validation: `internal/trustedsupervisor/execution_policy.go:521-536`
- Native path read/hash validation: `internal/trustedsupervisor/execution_policy.go:548-595`
- Final pre-launch revalidation: `internal/trustedsupervisor/audit_executor.go:568-572`
- Sandboxed Bash launch: `internal/trustedsupervisor/audit_executor.go:584-618`
- Copied native validation: `internal/trustedsupervisor/audit_executor.go:409-425`
- Installed wrapper resolves `omp` through `PATH`: `/Users/yingliangzhang/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh:1027-1031`

Observed authority:

```text
yingliangzhang admin drwxr-xr-x /opt/homebrew/Cellar/omp/17.1.3/bin
yingliangzhang staff drwxr-xr-x /Users/yingliangzhang/.omp/natives/17.1.3
yingliangzhang staff -r-xr-xr-x /opt/homebrew/Cellar/omp/17.1.3/bin/omp
yingliangzhang staff -rwxr-xr-x /Users/yingliangzhang/.omp/natives/17.1.3/pi_natives.darwin-arm64.node
```

**Exploit/failure mechanism**

The executable and native are validated through descriptors, but those descriptors are closed before use. The installed wrapper later resolves `omp` by path. A concurrent same-UID actor can rename-replace the executable after validation but before resolution.

The copied native is mode `0400`, but its parent hierarchy is supervisor-UID-owned. A same-UID actor can replace and restore it around dynamic loading. Post-run A→B→A validation cannot detect that substitution.

**Why existing tests miss it**

- Wrapper tests freeze wrapper bytes through a private pipe, but OMP itself is not executed from a frozen descriptor or private immutable copy.
- Existing mutation tests exercise policy and wrapper drift, not replacement between the final validation and OMP path resolution.
- The installed preflight validates native bytes before and after execution; it does not interpose a replacement during loading.

**Required production change**

Materialize OMP into a supervisor-owned sealed execution hierarchy or execute it through descriptor-stable authority. Make the copied native’s complete parent hierarchy unavailable for replacement after sealing, and preserve descriptor-backed identity through dynamic loading where feasible.

**Required regression test**

Use deterministic start gates to:

- rename-replace `omp` after final validation but before wrapper resolution;
- rename-replace and restore the copied native while OMP starts;
- prove substituted executable/native bytes never execute or load.

Cover rename replacement, not only in-place writes.

---

## 5. High — `completed` becomes durable before cleanup is complete

**Location**

- Completed event persistence: `internal/trustedsupervisor/audit_runtime.go:494-513`
- Cleanup after persistence: `internal/trustedsupervisor/audit_runtime.go:515-520`
- Completed is terminal: `internal/trustedsupervisor/audit_journal.go:461-464`
- Callback cleanup check: `internal/trustedsupervisor/audit_evidence.go:277-293`, `validateAuditCallbackEvidence`

**Exploit/failure mechanism**

The executor appends an authoritative `completed` event before scrubbing owned roots. If cleanup then fails or the server crashes, completed cannot transition to `artifact_cleanup_failed`.

Reconciliation checks only whether the output still matches or its output directory is absent. It does not require prompt, session, temporary, work, snapshot, and transport roots to be absent. A signed completed callback can therefore be returned while sensitive or writable owned artifacts remain.

**Why existing tests miss it**

- Normal cleanup tests verify the success path.
- `internal/trustedsupervisor/audit_server_test.go:140-171` pauses after completed persistence but mutates the output; rejection is caused by output mismatch, not by incomplete cleanup.
- No test reconciles an unchanged completed output while cleanup is paused.
- No crash/restart test stops execution between completed persistence and cleanup.

**Required production change**

Complete and verify cleanup before appending `completed`, or introduce a durable pre-completion/finalization state whose cleanup resumes after restart. A completed callback must not be exposed until every owned root is verified absent.

**Required regression test**

- Pause immediately after evidence verification but before cleanup; reconciliation must remain nonterminal.
- Inject cleanup failure independently for each owned root; no completed callback may be produced.
- Crash after evidence persistence but before cleanup, restart, and prove cleanup resumes before completed authority is exposed.

---

## 6. Medium — Populated legacy V2 journals have no explicit authenticated-history migration behavior

**Location**

- V2 contains audit tables: `internal/trustedsupervisor/server_journal_validation.go:215-237`
- V2→V3 schema migration: `internal/trustedsupervisor/server_journal.go:407-477`
- Current event signature verification: `internal/trustedsupervisor/audit_journal.go:365-381`
- Startup authority binding validates existing rows: `internal/trustedsupervisor/audit_authority.go:143-168`
- Migration fixtures create empty schemas: `internal/trustedsupervisor/audit_journal_test.go:94-114,164-212`

**Exploit/failure mechanism**

Current code does not silently elevate unsigned rows: authority binding rejects them. However, migration changes the schema without converting or explicitly rejecting populated legacy audit history before migration completion. A previously accepted V2 journal containing pre-authentication events can migrate structurally and then prevent server startup when authority binding encounters unsigned rows.

This is a fail-closed availability and migration-contract defect, not a silent signature bypass.

**Why existing tests miss it**

The V1/V2 migration tests create schemas with no audit intents or events. They never exercise populated legacy history.

**Required production change**

Define one explicit safe behavior:

- reject nonempty legacy audit history before committing migration and provide an operator-visible remediation path; or
- migrate only through separately authenticated provenance.

Never sign merely rehashed legacy rows.

**Required regression test**

Create a populated pre-authentication V2 journal. Prove the selected behavior is atomic, deterministic, restart-safe, and cannot turn correlated rehash/reseal edits into signed authority.

---

## 7. Medium — CLI/runbook/ledger claims are contradictory and one schema version is stale

**Location**

- Installed provider-free OMP claim: `docs/local-trusted-supervisor-transport-runbook.md:11`
- Fake-wrapper-only/no-real-OMP-canary claim: `docs/local-trusted-supervisor-transport-runbook.md:159-160`
- Documented execution-policy schema `v1`: `docs/local-trusted-supervisor-transport-runbook.md:61`
- Actual schema `v4`: `internal/trustedsupervisor/execution_policy.go:20-21`
- Accurate CLI distinction: `cmd/ananke-trusted-supervisor/main.go:7-8`
- Ledger installed-OMP claims: `docs/experiment-ledger.md:2196-2209`

**Exploit/failure mechanism**

The runbook first says installed OMP v17.1.3 ran under the real sandbox, then says P5 tests use fake wrappers only and that no real OMP/model canary occurred. The installed preflight is itself a P5 Go test using real installed OMP, so the statements cannot both be true as written.

The stale `v1` policy-schema instruction can cause operators to construct documents production rejects as anything other than `v4`.

**Why existing tests miss it**

- No documentation consistency check compares operational claims with the installed-preflight test inventory.
- No documentation check compares the runbook schema string with `executionPolicySchemaVersion`.

**Required production change**

Correct the documentation to distinguish:

1. fake route-aware wrappers used by most executor tests;
2. the separate provider-free installed OMP v17.1.3 preflight;
3. absence of any real model/provider API canary;
4. continued prohibition on such a canary before acceptance.

Change the documented execution-policy schema to `v4` and describe populated V2 journal behavior accurately.

**Required regression test**

Add a documentation/constant consistency check for the execution-policy schema and a checked operational-gate section that names the installed preflight separately from fake-wrapper tests.

---

## 8. High — The required repeated supervisor-termination matrix is flaky

**Location**

- Failing test: `internal/trustedsupervisor/supervisor_test_termination_test.go:128-154`
- Marker wait with one-second deadline: `internal/trustedsupervisor/supervisor_test_termination_test.go:387-399`

Observed failure:

```text
--- FAIL: TestSupervisorAuditTestContextShutdownUsesOwnedTermination (1.50s)
    supervisor_test_termination_test.go:142:
    supervisor test did not publish marker .../supervisor_tests/ready
FAIL
FAIL github.com/yingliang-zhang/ananke/internal/trustedsupervisor 173.037s
```

**Exploit/failure mechanism**

Under the repeated matrix, the fixture assumes the sandboxed child will schedule, execute, and publish its marker within one second. Cross-suite load violated that assumption. The required verification gate is therefore nondeterministic and cannot establish closure of supervisor-owned shutdown behavior.

**Why existing tests miss it**

The same case passed ten isolated repetitions. Isolation removes the scheduling contention that triggers the failure; it does not prove the complete repeated matrix is stable.

**Required production change**

No production defect was conclusively established by this failure. Replace the fixture’s wall-clock scheduling assumption with bounded process-aware synchronization that also reports early child exit. If that instrumentation reveals a production startup failure, repair the production path rather than extending the timeout blindly.

**Required regression test**

Run the complete focused matrix at least five times and the focused race matrix. Both must pass without retries or isolated substitution.

## Gate status

| Gate | Status | Observed result |
|---|---:|---|
| Base HEAD | PASS | `1bbc880576173913d62f13200ea54b25d46f4393` |
| `go test ./internal/trustedsupervisor -count=1 -timeout 600s` | PASS | Package `78.979s`; wall approximately `80.24s` |
| Expanded focused adversarial matrix, `-count=3` | **FAIL** | `TestSupervisorAuditTestContextShutdownUsesOwnedTermination`; package `173.037s` |
| Isolated termination test, `-count=10` | PASS | `5.62s`; diagnostic only, not a substitute for the failed matrix |
| Required focused matrix, `-count=5` | UNOBSERVED | Launched; no terminal result observed before review synthesis |
| Focused `go test -race` matrix | NOT RUN | No current result |
| `go test ./... -count=1 -timeout 600s` | NOT RUN | Historical ledger result not substituted |
| `go test -race ./... -count=1 -timeout 600s` | NOT RUN | Historical ledger result not substituted |
| `go vet ./...` | NOT RUN | No current result |
| P3d verifier + self-test | PASS | Both completed |
| P3f verifier + self-test | PASS | Both completed |
| P4 verifier + self-test | PASS | Both completed |
| Scoped `git diff --check` against base | PASS | No output |
| Installed OMP provider-free preflight, exact fixture paths, `-count=3` | PASS | `9.67s`; fake credential/local deterministic rejection only |
| Darwin `localhost` IPv6 confinement probe | **FAILS SECURITY EXPECTATION** | `localhost:<port>` successfully reached `::1:<port>` |

## Workspace evidence

Last observed state:

- staged files: `0`
- unstaged tracked files: `17`
- untracked files: `377`
- scoped untracked files under `internal/trustedsupervisor`, `cmd/ananke-trusted-supervisor`, and `docs`: `24`
- tracked diff against base: `17 files changed, 1256 insertions, 150 deletions`

No review edits were made.

A real model/provider canary remains **prohibited**. The passing installed-OMP preflight used only the prescribed fixture paths, a fake credential, and local deterministic rejection; no provider API was contacted. Commit and push also remain **prohibited** until the blockers are repaired and independently accepted.
