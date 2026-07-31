Working...
Implemented P5 Option A without touching the ledger, external Hermes wrapper, or P6 files.

## Contract changes

- Trusted Git: `/Library/Developer/CommandLineTools/usr/bin/git`
- Child `PATH`: `/Library/Developer/CommandLineTools/usr/bin:/usr/bin:/bin`
- Git discovery ceiling: exact snapshot parent above the `source` working directory
- Git configuration:
  - `GIT_CONFIG_GLOBAL=/dev/null`
  - `GIT_CONFIG_NOSYSTEM=1`
  - ambient command configuration omitted
- Sandbox executable/read-map authority remains exact literals for pinned OMP and CLT Git. No `/usr/bin`, `/bin`, Xcode, shell, environment launcher, other CLT executable, or test-executable authority was added.
- Fake OMP resolves command-name `git` through the fixed `PATH`, checks the result against an embedded policy-selected path, and rejects caller mismatch.
- Existing owner/device/inode/hash/mode checks and every-effect revalidation remain active.

Schema cutover:

- Execution-policy document: `ananke.local-trusted-supervisor-execution-policy.v7`
- Execution-policy entry: `ananke.local-trusted-supervisor-execution-policy-entry.v7`
- Command descriptor: `ananke.local-trusted-supervisor-command-descriptor.v7`
- Git discovery policy: `exact_snapshot_parent_ceiling_no_global_or_system_config_v1`

Stale v6 policy documents and entries fail closed.

## Files changed

- `docs/local-trusted-supervisor-transport-runbook.md`
- `internal/trustedsupervisor/execution_policy.go`
- `internal/trustedsupervisor/execution_policy_test.go`
- `internal/trustedsupervisor/audit_executor.go`
- `internal/trustedsupervisor/audit_executor_test.go`
- `internal/trustedsupervisor/audit_direct_runtime_test.go`
- `internal/trustedsupervisor/audit_wrapper_compatibility_test.go`
- `internal/trustedsupervisor/audit_snapshot_test.go`
- `internal/trustedsupervisor/testdata/fakeomp/main.go`

Remaining `/usr/bin/git` and Xcode literals are confined to negative policy tests, sandbox-denial assertions, fake blocked probes, and runbook rejection prose.

## Required RED

```sh
go test ./internal/trustedsupervisor \
  -run '^TestExecutionPolicyRequiresExactRootOwnedCommandLineToolsGit$' \
  -count=1
```

Expected failure:

```text
production Git path = "/usr/bin/git", want exact CLT Git "/Library/Developer/CommandLineTools/usr/bin/git"
exact root-owned CLT Git rejected
xcselect_forwarding_Git: rejected Git identity error = <nil>
FAIL github.com/yingliang-zhang/ananke/internal/trustedsupervisor 0.864s
```

Command exited 1; wall time `3.17s`.

The same command after implementation passed with exit 0; wall time `2.99s`.

## Tight startup test

```sh
go test ./internal/trustedsupervisor \
  -run '^TestDarwinDirectOMPSandboxExecutesPinnedGitWithoutParentRepositoryDiscovery$' \
  -count=1
```

Result: PASS, exit 0; wall time `1.76s`.

The completed probe reached its final `git-startup-isolated` marker after checking:

- parent repository non-discovery;
- global, system, ambient command configuration isolation;
- immutable snapshot source and parent `HEAD`;
- denial of `/usr/bin/git`, `/bin/sh`, `/bin/bash`, `/usr/bin/env`;
- denial of an executable copied into the writable temporary root;
- denial of Xcode/Xcode-beta Git;
- denial of another CLT executable.

An initial CLT run exposed real Git ceiling semantics: setting the ceiling to the working directory still inspected the repository above it. The contract was narrowed to the exact snapshot parent. A subsequent failure found the copied arbitrary-executable probe in the artifact scanner; the fixture now removes that probe before successful completion.

## Comprehensive focused verification

Single pass:

```sh
/usr/bin/time -p go test -tags ananke_real_provider_canary \
  ./internal/trustedsupervisor \
  -run '^(TestExecutionPolicy.*|TestRunbookMatchesExecutionPolicySchemaAndNamesInstalledPreflightSeparately|TestAuditInvocationMaterializesPrivateStateAndBoundModels|TestAuditInvocationEnvironment.*|TestAuditInvocationUsesExactArgvMinimalEnvironmentAndFourRoots|TestP5.*|TestDarwinP5.*|TestDarwinDirectOMP.*|TestDarwinAuditSandbox.*|TestNativeFakeAuditOMPRejectsCallerSelectedGitPath|TestMaterializeAuditSnapshot.*|TestCanonicalGitArchive.*|TestRealProviderCanary.*)$' \
  -count=1
```

Result:

```text
ok github.com/yingliang-zhang/ananke/internal/trustedsupervisor 9.250s
real 11.26
user 7.74
sys 3.88
```

Repeated set:

```sh
/usr/bin/time -p go test -tags ananke_real_provider_canary \
  ./internal/trustedsupervisor \
  -run '^(TestExecutionPolicy.*|TestRunbookMatchesExecutionPolicySchemaAndNamesInstalledPreflightSeparately|TestAuditInvocationMaterializesPrivateStateAndBoundModels|TestAuditInvocationEnvironment.*|TestAuditInvocationUsesExactArgvMinimalEnvironmentAndFourRoots|TestP5.*|TestDarwinP5.*|TestDarwinDirectOMP.*|TestDarwinAuditSandbox.*|TestNativeFakeAuditOMPRejectsCallerSelectedGitPath|TestMaterializeAuditSnapshot.*|TestCanonicalGitArchive.*|TestRealProviderCanary.*)$' \
  -count=10
```

Result:

```text
ok github.com/yingliang-zhang/ananke/internal/trustedsupervisor 82.125s
real 83.45
user 39.08
sys 31.16
```

The build tag compiled the real-provider canary fixture and ran only `TestRealProviderCanary.*` unit tests. `TestAuditRealProviderCanary` did not match the regex and was not executed.

Formatting:

```sh
/usr/bin/time -p gofmt -d \
  internal/trustedsupervisor/execution_policy.go \
  internal/trustedsupervisor/execution_policy_test.go \
  internal/trustedsupervisor/audit_executor.go \
  internal/trustedsupervisor/audit_executor_test.go \
  internal/trustedsupervisor/audit_direct_runtime_test.go \
  internal/trustedsupervisor/audit_wrapper_compatibility_test.go \
  internal/trustedsupervisor/audit_snapshot_test.go \
  internal/trustedsupervisor/audit_real_provider_canary_test.go \
  internal/trustedsupervisor/testdata/fakeomp/main.go
```

Result: PASS, zero diff output; real time `0.02s`.

Whitespace:

```sh
/usr/bin/time -p git diff --check
```

Result: PASS, no output; real time `0.03s`.

## Residual risks

- No real-provider canary, provider request, or production audit was executed. No provider/audit success is claimed.
- Race, full-package, vet, and full tagged canary execution were intentionally not run.
- CLT replacement or upgrade changes device/inode/hash/size metadata; existing policies then fail closed and require operator resealing.
- Git subcommands requiring separate `git-core` helper executables will fail closed because only the concrete Git executable is authorized. The exercised repository commands are supported by the exact single-executable boundary.

