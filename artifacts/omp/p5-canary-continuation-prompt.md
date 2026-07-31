# P5 Real-Provider Canary — Continued Session

You are the Hermes Orchestrator working on the Ananke project. This is a continuation from a previous session that ended at a stable checkpoint.

## Project

- **Repo**: `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`
- **Branch**: `feat/task-proposal-core` (HEAD `2df447790b89`, matched upstream)
- **SSH remote**: `git@github.com:yingliang-zhang/ananke.git`
- **Go**: 1.26.5 darwin/arm64
- **OMP**: 17.1.4 at `/opt/homebrew/Cellar/omp/17.1.4/bin/omp`, native addon at `/Users/yingliangzhang/.omp/natives/17.1.4/pi_natives.darwin-arm64.node`
- **Workspace is intentionally dirty** — do NOT `git reset`, `git clean`, `git checkout`, or broad `restore`. ~30 modified tracked files, ~280 untracked paths.
- **First action**: read `docs/experiment-ledger.md` (especially the final ~150 lines, offset 2400+) to recover full state.

## Current Task: P5 Real-Provider Canary

The P5 read-only audit executor and release foundation passed 8 rounds of independent hard review (8th = ACCEPT). The remaining gate is a **real-provider canary** that exercises the full path: sealed HOME → sandboxed direct OMP → trusted provider gateway → typed model evidence → immutable repository proof.

### What's Already Done (This Session)

1. **Sealed HOME layout**: `.omp` directory sealed to `0500` (read-only), `.omp/run` writable `0700`. `PI_CONFIG_DIR=.omp/run` and `XDG_CACHE_HOME=<HomeRunDir>` added to invocation environment to redirect OMP's config/cache writes into the writable run directory.
2. **Relocated OMP executable**: The installed-OMP preflight test now copies the OMP executable into a private `bin/` directory (mode `0500`) and runs it from there, proving the sandbox allows relocated OMP execution.
3. **Real audit prompt + xhigh thinking**: The preflight now uses `readOnlyAuditPromptTemplate` (the real frozen audit prompt) with `--thinking xhigh`, matching production invocation.
4. **Read-only non-empty worktree**: The preflight populates the work directory with `go.mod` and `main.go` (mode `0444`) then seals the work directory to `0555`, proving OMP can start in a read-only non-empty Go worktree.
5. **OMP log diagnostic**: Added `classifyRealProviderCanaryOMPLogs` that walks `HomeRunDir/logs/` before artifact scan, extracts safe JSON log labels (level/message/provider/step), and reports them via `t.Logf("omp_log_diagnostic=%s", ...)`. A `BeforeArtifactScan` hook was added to `auditInvocationHooks` to capture logs before cleanup.
6. **Ancestor-secret probe**: `audit_executor_test.go` writes an `ancestor-secret` file in the snapshot parent directory; fakeomp `probeSealedHome` attempts to read it and fails if accessible.

### Tests That PASS

- Focused sealed-home tests: `go test ./internal/trustedsupervisor -run '^(TestDarwinDirectOMPSandboxSealsHomeStateAndAllowsOnlyRunWrites|TestAuditInvocationEnvironmentPinsStateConfigSessionAndOMPPath)$' -count=3` → PASS
- Installed OMP preflight: `env ANANKE_PINNED_OMP_FIXTURE=/opt/homebrew/Cellar/omp/17.1.4/bin/omp ANANKE_PINNED_OMP_NATIVE_FIXTURE=/Users/yingliangzhang/.omp/natives/17.1.4/pi_natives.darwin-arm64.node go test ./internal/trustedsupervisor -run '^TestAuditInstalledOMPProviderFreeTransportPreflight$' -count=1` → PASS
- Build-tagged canary diagnostics: `go test -tags ananke_real_provider_canary ./internal/trustedsupervisor -run '^TestRealProviderCanary' -count=1` → PASS
- `gofmt -d`, `git diff --check`, `go vet ./internal/trustedsupervisor` → all PASS

### The Real-Provider Canary — Current Blocker

**Command**:
```
env ANANKE_REAL_PROVIDER_CANARY=1 \
  ANANKE_REAL_CANARY_REPOSITORY=/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen \
  ANANKE_REAL_CANARY_WRAPPER=/Users/yingliangzhang/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh \
  ANANKE_PINNED_OMP_FIXTURE=/opt/homebrew/Cellar/omp/17.1.4/bin/omp \
  ANANKE_PINNED_OMP_NATIVE_FIXTURE=/Users/yingliangzhang/.omp/natives/17.1.4/pi_natives.darwin-arm64.node \
  go test -tags ananke_real_provider_canary ./internal/trustedsupervisor \
  -run '^TestAuditRealProviderCanary$' -count=1 -v
```

**Result**: FAIL (exit 1, ~12–24s)

**Diagnostic output**:
```
provider_resolution_transport=transparent_fake_ip_transport_alias
provider_transport lookups=1 dial_attempts=0 dial_successes=0
wrapper_diagnostic=working_only
omp_log_diagnostic=warn.failed_to_acquire_macos_power_assertion,warn.model_discovery_failed_for_provider.ollama,warn.model_discovery_failed_for_provider.llama.cpp,warn.model_discovery_failed_for_provider.lm-studio
real-provider canary safe timeline=[
  {"attempt":1,"sequence":1,"state":"prepared"},
  {"attempt":1,"sequence":2,"state":"running"},
  {"attempt":1,"sequence":3,"state":"failed","failure_class":"artifact_scan_temporary_authority_home_work_root"}
]
real-provider canary failed closed: terminal_state
```

### Root Cause Analysis So Far

1. **`dial_attempts=0`** — OMP never reached the trusted provider gateway. It starts, writes logs to `.omp/logs/`, but exits before making any provider request.
2. **`wrapper_diagnostic=working_only`** — OMP's stdout contains only `working...` (the text-print mode initial output), no error message.
3. **`omp_log_diagnostic`** shows two warning classes:
   - `warn.failed_to_acquire_macos_power_assertion` — OMP tries to acquire a macOS PowerManagement mach port, which the sandbox denies.
   - `warn.model_discovery_failed_for_provider.{ollama,llama.cpp,lm-studio}` — OMP tries to discover local model providers (Ollama on port 11434, llama.cpp on port 8080, LM Studio on port 1234) and fails because the sandbox denies those network connections.
4. **Sandbox deny logs** (from `/usr/bin/log show`) show OMP being denied: `network-outbound remote:*:11434`, `network-outbound remote:*:8080`, `network-outbound remote:*:1234`, `mach-lookup com.apple.PowerManagement.control`, `sysctl-read hw.model`, `sysctl-read kern.osrelease`, `sysctl-read kern.version`, `file-read-metadata /etc`, `file-read-data /Library/Preferences/com.apple.networkd.plist`.
5. The **installed-OMP preflight test** (which uses a local HTTP server and a dummy provider) **PASSes** because it uses a different sandbox profile (`auditInstalledOMPProbeSandboxProfile`) that only allows `network-outbound` to the single test gateway address, and does not need sysctl/mach-lookup for model discovery.
6. The **real canary** uses the production sandbox profile (`writeDirectOMPSandboxProfile`) which allows `network-outbound` only to the trusted gateway address. OMP's model discovery code tries to probe local providers *before* using the configured `sudo` provider, and those probe attempts are denied by the sandbox, causing OMP to exit before reaching the real provider.

### Next Diagnostic Step

Compare the two sandbox profiles:
- **Passing**: `auditInstalledOMPProbeSandboxProfile` in `audit_wrapper_compatibility_test.go` (line ~1131)
- **Failing**: the production sandbox profile in `audit_executor.go` (function starting around line ~1570, `writeDirectOMPSandboxProfile` or similar)

The key difference is likely:
1. The preflight profile allows `sysctl-read` for specific names and `mach-lookup` for specific services that OMP needs during initialization.
2. The production profile has a more restrictive `sysctl-read` allowlist (only `security.mac.lockdown_mode_state`, `kern.bootargs`, `kern.osproductversion`, `kern.iossupportversion`, `kern.osvariant_status`, `hw.ephemeral_storage`, `hw.pagesize_compat`).

The production sandbox needs to allow the minimal additional sysctl-read and mach-lookup permissions that OMP 17.1.4 requires for initialization, **without** allowing the local-provider network probes (ports 11434/8080/1234). The model discovery failures for ollama/llama.cpp/lm-studio are *expected* (we don't want those providers) — the question is whether OMP treats those discovery failures as fatal or continues to the configured `sudo` provider.

**Hypothesis**: OMP's model discovery failures for local providers are *warnings*, not fatal. The real blocker may be one of:
- (a) `warn.failed_to_acquire_macos_power_assertion` — if OMP treats this as fatal and exits
- (b) A denied `sysctl-read` (hw.model, kern.osrelease, kern.version) that OMP needs and treats as fatal
- (c) A denied `mach-lookup` (com.apple.PowerManagement.control) that OMP needs
- (d) OMP exits because it cannot write to `.omp/gpu_cache.json` or `.omp/logs/` (but sealed HOME should have fixed this via `.omp/run`)

### Key Files

- `internal/trustedsupervisor/audit_executor.go` — sandbox profile, invocation environment, `BeforeArtifactScan` hook
- `internal/trustedsupervisor/audit_wrapper_compatibility_test.go` — `auditInstalledOMPProbeSandboxProfile`, installed OMP preflight test
- `internal/trustedsupervisor/audit_real_provider_canary_test.go` — real-provider canary test, log diagnostic, wrapper diagnostic
- `internal/trustedsupervisor/audit_executor_test.go` — sealed-home test, ancestor-secret probe
- `internal/trustedsupervisor/testdata/fakeomp/main.go` — fake OMP binary, `probeSealedHome`
- `internal/trustedsupervisor/namespace_authority.go` — `mkdirAndCaptureOwnedChild`, directory sealing
- `internal/trustedsupervisor/execution_policy.go` — pinned OMP version, Git executable path
- `docs/experiment-ledger.md` — full experiment history (2543 lines)

### Constraints

- **Do NOT** `git reset`, `git clean`, `git checkout`, or broad `restore` — the workspace is intentionally dirty.
- **Do NOT** commit/push without explicit approval.
- **Do NOT** run `git merge` to main.
- **Do NOT** weaken the sandbox to allow arbitrary network/sysctl/mach-lookup — only add minimal exact permissions that OMP 17.1.4 needs for initialization.
- **Do NOT** add broad artifact allowlist — the scanner must remain strict.
- **Do NOT** run the real-provider canary until focused tests pass after any sandbox profile change.
- Preserve all existing passing tests — no regressions.
- All code, comments, commit messages in English.
- Report to user in Chinese.
- Use `terminal` for builds/tests, `patch`/`write_file` for edits, `read_file`/`search_files` for inspection.
- Background long-running commands with `notify_on_complete=true`.
- The OMP wrapper is `/Users/yingliangzhang/.hermes/profiles/orchestrator/scripts/omp_with_timeout.sh`.

### Task List

1. **Diagnose**: Read the production sandbox profile (`writeDirectOMPSandboxProfile` or equivalent in `audit_executor.go`) and compare its sysctl-read/mach-lookup allowlists with what OMP 17.1.4 actually needs (from the deny logs). Determine whether the model-discovery warnings are fatal or the power-assertion/sysctl denials are the real blocker.
2. **Repair**: Add the minimal exact sysctl-read and/or mach-lookup permissions that OMP 17.1.4 needs for initialization, without allowing local-provider network probes. If OMP treats model-discovery failures as fatal, investigate whether OMP can be configured to skip local provider discovery (e.g., via `PI_CONFIG_DIR` or `models.yml` settings).
3. **Verify**: Focused tests PASS → installed OMP preflight PASS → real-provider canary PASS (or at least progresses past model discovery to `dial_attempts≥1`).
4. **If canary PASSes**: Update `docs/experiment-ledger.md` with the result, then proceed to `p5-review-commit` (freeze, independent review, commit, push).
5. **If canary still fails**: Iterate the diagnosis — the next diagnostic step is to add more granular OMP log parsing or a bounded pre-cleanup snapshot of the sandbox deny logs to identify the exact fatal denial.

### Success Criteria

- Real-provider canary returns typed model evidence (approved/rejected report with findings, verdict, evidence hash).
- `dial_attempts ≥ 1` and `dial_successes ≥ 1` (proving the trusted provider gateway was reached).
- Repository immutability proof passes (pre/post source-status SHA-256 match).
- No credential disclosure in sanitized summary.
- All existing focused/full/race tests continue to PASS.
