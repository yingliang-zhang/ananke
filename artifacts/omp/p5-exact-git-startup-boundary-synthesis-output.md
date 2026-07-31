Working...
## Status

Not accepted. The exact Git startup boundary remains RED; no canary success is claimed.

### RED

```text
go test ./internal/trustedsupervisor -run '^TestDarwinDirectOMPSandboxExecutesPinnedGitWithoutParentRepositoryDiscovery$' -count=1
```

Expected RED reproduced:

```text
FAIL
fake OMP exit code: 70
stdout: empty
stderr: empty
package result: FAIL
```

### Post-repair tight failure

The same command still fails with fake OMP exit code `70`, now with bounded test-only telemetry:

```text
stderr: git-startup-stage-05PE
```

`05PE` establishes:

- fixed Git environment validation passed;
- private-home global-config fixture creation passed;
- `/usr/bin/git rev-parse --show-toplevel` started;
- Git exited `1` with output classified as `Operation not permitted`;
- the ceiling exactly matched the physical working directory;
- later config, mutation, and arbitrary-executable runtime probes were not reached.

### Host finding

Verified:

- `/usr/bin/git` is a Mach-O executable.
- It links `/usr/lib/libxcselect.dylib`.
- `/usr/bin/git --exec-path` resolves to:
  `/Applications/Xcode-beta.app/Contents/Developer/usr/libexec/git-core`
- The sandbox grants exact literal read/map/exec authority to `/usr/bin/git`, not `/usr/bin` or the Xcode developer tree.
- `rev-parse` exits `1` with `Operation not permitted` under that profile.

[INFERENCE] `/usr/bin/git` is an xcselect-mediated launcher on this host and repository operations transfer execution or loading to the selected Xcode Git implementation outside the single admitted `/usr/bin/git` literal. Therefore the requested lone literal is insufficient here. Adding Xcode paths without a new pinned identity contract would violate the narrow authority requirement.

### Production changes already made

`internal/trustedsupervisor/audit_executor.go` now:

- Adds only `entry.GitExecutable.Path` beside the pinned OMP path to exact literal:
  - `file-read*` / `file-test-existence`
  - `file-map-executable`
  - `process-exec`
- Adds no `/usr/bin` or `/bin` subpath authority.
- Adds fixed child environment:
  - `GIT_CEILING_DIRECTORIES=<invocation.WorkDir>`
  - `GIT_CONFIG_GLOBAL=/dev/null`
  - `GIT_CONFIG_NOSYSTEM=1`
- Bumps the command descriptor to:
  `ananke.local-trusted-supervisor-command-descriptor.v6`
- Binds:
  - the pinned Git executable identity;
  - the fixed Git environment values;
  - `exact_workdir_ceiling_no_global_or_system_config_v1`.
- Preserves direct pinned-OMP launch and existing runtime ancestor, native denial, transport, timeout, and credential-projection boundaries.

### Passing focused checks

This unaffected focused suite passed once:

```text
go test ./internal/trustedsupervisor -run '^(TestDarwinDirectOMPSandboxEnforcesReadonlySnapshotAndRootIsolation|TestDarwinAuditSandboxDeniesPrivateAuthorityParentAndLocalEndpoints|TestAuditInvocationUsesExactArgvMinimalEnvironmentAndFourRoots|TestAuditInvocationMaterializesPrivateStateAndBoundModels|TestAuditInvocationEnvironmentPinsStateConfigSessionAndOMPPath|TestAuditInvocationEnvironmentProjectsPreferredSudoCredentialToRuntimeName|TestP5DirectInvocationDerivesExactOracleOMPArgv|TestP5DirectRouteIsExactClosedSealedDeclaration|TestP5DirectCommandUsesPinnedOMPTargetWithoutShellTransport|TestP5ProductionCommandHasNoStdinOrInheritedFiles|TestDarwinP5SandboxGrantsNoSharedTempHeredocAuthority)$' -count=1
```

Result:

```text
go test: 1 packages ok
```

Scoped gofmt verification produced no diff:

```text
gofmt -d internal/trustedsupervisor/audit_executor.go internal/trustedsupervisor/audit_executor_test.go internal/trustedsupervisor/audit_direct_runtime_test.go internal/trustedsupervisor/audit_wrapper_compatibility_test.go internal/trustedsupervisor/testdata/fakeomp/main.go
```

Scoped whitespace check passed with no output:

```text
git diff --check -- internal/trustedsupervisor/audit_executor.go internal/trustedsupervisor/audit_executor_test.go internal/trustedsupervisor/audit_direct_runtime_test.go internal/trustedsupervisor/audit_wrapper_compatibility_test.go internal/trustedsupervisor/testdata/fakeomp/main.go
```

### Gates not run

- Required focused GREEN: not achieved because the tight test remains at `05PE`.
- Focused `-count=10`: not run because the first GREEN did not pass.
- Race, full package, vet, tagged gates: not run per the requested narrow workflow and because the prerequisite focused GREEN failed.
- Real-provider canary: prohibited and not run.
- No acceptance or end-to-end provider/audit success is inferred.

### Changed files

- `internal/trustedsupervisor/audit_executor.go`
- `internal/trustedsupervisor/audit_executor_test.go`
- `internal/trustedsupervisor/audit_direct_runtime_test.go`
- `internal/trustedsupervisor/audit_wrapper_compatibility_test.go`
- `internal/trustedsupervisor/testdata/fakeomp/main.go`

### Architecture decision required

1. **Pin the resolved concrete Git implementation.** Replace or extend the policy’s Git identity with the selected Xcode Git executable and grant only its verified literal/runtime chain. This changes the current fixed `/usr/bin/git` policy contract.

2. **Model `/usr/bin/git` as a pinned launcher chain.** Keep `/usr/bin/git`, but add a closed, independently identity-pinned xcselect-selected Git/runtime authority and revalidate the selection at every effect boundary. This is broader and more complex than the requested single-executable hypothesis.

Neither option should be implemented speculatively without orchestrator/user selection.
