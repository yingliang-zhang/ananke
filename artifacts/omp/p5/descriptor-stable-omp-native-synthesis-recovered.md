## Feasibility verdict

No working direct descriptor-stable activation exists for installed OMP 17.1.3 on this Darwin host.

### Executable descriptor

Observed probe:

- Opened the verified OMP Mach-O with `O_RDONLY|O_CLOEXEC|O_NOFOLLOW`.
- Inherited it as FD 3 through `exec.Cmd.ExtraFiles`.
- Renamed the pathname and installed a replacement executable.
- Ran a frozen Bash stream containing `/dev/fd/3 --version`.

Darwin returned:

```text
/bin/bash: line 2: /dev/fd/3: Permission denied
```

The replacement marker was absent, so neither the descriptor-held original nor the replacement executed. Darwin’s `/dev/fd/N` cannot provide the required executable authority here. No `fexecve`/`execveat` interface was found in the available Darwin APIs.

### Native loader

OMP 17.1.3’s tagged loader has no supported exact native-file or inherited-FD override.

For a compiled binary, the arm64 native candidate is:

```text
pi_natives.darwin-arm64.node
```

The loader derives its versioned directory as follows:

1. If `$XDG_DATA_HOME/omp` already exists:

   ```text
   $XDG_DATA_HOME/omp/natives/17.1.3/pi_natives.darwin-arm64.node
   ```

2. Otherwise:

   ```text
   $HOME/.omp/natives/17.1.3/pi_natives.darwin-arm64.node
   ```

Additional compiled-binary fallback candidates include the user-data, executable, and packaged-native directories. `PI_NATIVE_VARIANT` only selects x64 ISA variants; it is not a path override.

The native probe:

- Inherited a verified native descriptor.
- Put a same-size invalid native at the XDG versioned candidate.
- Triggered a native-dependent `omp grep` command.
- OMP failed to load the native.
- Diagnostics did not include `/dev/fd/3`.

Therefore the installed loader does not select an inherited native descriptor. This does not prove Darwin `dlopen("/dev/fd/N")` itself is impossible; it proves OMP 17.1.3 exposes no supported way to request it.

## Safest current production boundary

Fail closed before real-OMP activation.

The current installation is outside the safe boundary:

- OMP executable hierarchy: supervisor-UID-owned Homebrew Cellar.
- Native hierarchy: supervisor-UID-owned `$HOME/.omp/natives/17.1.3`.
- Both remain rename-replaceable by the threatened same-UID actor.
- A→B→A restoration defeats post-run path revalidation.

Return a precise typed error, for example:

```go
var ErrUnsupportedAtomicRuntimeBoundary = errors.New(
    "installed OMP runtime lacks atomic executable/native authority",
)

type AtomicRuntimeBoundaryError struct {
    Component string // "executable", "native", or "ancestor"
    Reason    string // closed enum; never untrusted text
}
```

Recommended failure class:

```text
unsupported_atomic_omp_runtime_boundary
```

Enforce it:

1. At execution-policy loading or `NewServer`, so an unsafe production policy cannot activate.
2. Again in `runAuditInvocation`, before gateway creation, credential lookup, wrapper startup, or child creation.
3. In the installed provider-free preflight.

No production environment variable, test flag, or caller-supplied configuration may bypass it.

## Immutable-hierarchy alternative

An equivalently non-replaceable path-based mechanism is feasible, but was not implemented or end-to-end proven in this session.

Require both artifacts and every physical ancestor to be:

- Root-owned, or owned by a configured distinct trusted operator UID.
- Not writable by the supervisor UID.
- Not writable through any supplementary group of the supervisor.
- Not writable through Darwin ACL entries.
- Non-symlink components, traversed component-by-component.
- In versioned, publish-once paths that the trusted owner never mutates in place.

Example layout:

```text
/Library/Ananke/omp/17.1.3/bin/omp
/Library/Ananke/omp-data/omp/natives/17.1.3/pi_natives.darwin-arm64.node
```

Set:

```text
XDG_DATA_HOME=/Library/Ananke/omp-data
```

The `omp` directory must already exist because OMP only selects the XDG native root when `$XDG_DATA_HOME/omp` exists.

Do not copy the native into supervisor-owned `HOME` or an invocation temporary directory. Load directly from the immutable XDG hierarchy.

This closes the stated same-UID replacement threat because that actor cannot unlink, rename, replace, or recreate either artifact or any ancestor. Root or the distinct trusted operator remains in the trusted computing base.

### Parent-side descriptor handling

Even with immutable paths:

1. Traverse ancestors with directory FDs and `openat`/`O_NOFOLLOW`.
2. Open executable and native with:

   ```text
   O_RDONLY | O_CLOEXEC | O_NOFOLLOW
   ```

3. Require regular files and validate owner, mode, device, inode, size, and ACL boundary.
4. Hash each artifact from its opened descriptor.
5. Compare `fstat` before and after hashing.
6. Retain artifact and relevant directory descriptors until the OMP process group exits. This conservatively spans child startup and native loading.
7. Do not pass these FDs through `ExtraFiles`; the child does not use them, and unrelated descendants must not inherit them.
8. Close all descriptors deterministically on every error, timeout, cancellation, and normal exit.

## Invoking OMP without PATH

Even under an immutable hierarchy, avoid the wrapper’s existing plain `omp` lookup.

Because `/dev/fd/N` execution is unavailable, prepend a trusted, fixed bootstrap to the frozen wrapper stream:

```bash
omp() {
    exec '/Library/Ananke/omp/17.1.3/bin/omp' "$@"
}
readonly -f omp
```

The `exec` is important. The installed wrapper backgrounds `omp` and treats `$!` as the OMP PID; the function’s background subshell must become the OMP process rather than remain an intermediate shell.

Required bindings:

- Preserve the installed wrapper bytes unchanged and verify their existing SHA-256.
- Hash the fixed bootstrap separately.
- Hash a length-framed `bootstrap || wrapper` stream.
- Bind the immutable executable path and authority record into the command descriptor.
- Embed the path as a supervisor-generated shell literal; never read it from environment or caller input.
- Prove the wrapper cannot redefine or unset the function.
- Remove the OMP Cellar directory from `PATH`; retain only required immutable system-tool paths.

This is path-based execution, not descriptor execution. It is acceptable only when the complete path hierarchy has passed the non-replaceable trusted-owner boundary.

## Hash and evidence bindings

The command descriptor and evidence should bind:

- Executable SHA-256, device, inode, owner, mode, size, and immutable absolute path.
- Native SHA-256, device, inode, owner, mode, size, and immutable XDG path.
- Full ordered ancestor identity chains for both.
- Trusted owner UID and authority-policy version.
- Wrapper SHA-256.
- Bootstrap SHA-256.
- Final framed wrapper-stream SHA-256.
- Explicit FD policy: parent-retained only; no inherited runtime artifact FDs.
- XDG native-root selection and the complete set of denied fallback roots.

Sandbox rules must allow read/map only for the exact immutable native and executable, and deny OMP’s alternative native candidates. Otherwise loader fallback could select a different file after the preferred candidate fails.

## Regression recommendations

1. **Darwin executable feasibility**
   - Preserve the `/dev/fd/3` probe.
   - Assert `EACCES`.
   - Assert replacement marker absent.

2. **Native override feasibility**
   - Inherit verified native FD.
   - Put same-size invalid bytes at the XDG candidate.
   - Run native-dependent `omp grep`, not `omp --version`.
   - Assert native loading fails and `/dev/fd/N` is never a candidate.

3. **Current installation fail-closed**
   - Use the actual supervisor-owned Homebrew executable and `$HOME/.omp` native.
   - Assert typed `ErrUnsupportedAtomicRuntimeBoundary`.
   - Assert zero child PID, zero gateway request, and no credential lookup.

4. **Executable A→B→A gate**
   - Gate after final validation and before wrapper release.
   - Rename A away, install B, then restore A.
   - Under the current installation, activation must already have failed closed.
   - Under an immutable fixture, the rename must fail with permission denial and B’s marker must remain absent.

5. **Native A→B→A gate**
   - Gate during native-dependent startup.
   - Attempt replace-and-restore of the immutable XDG native.
   - Replacement must fail; B must never load.
   - With a mutable hierarchy, authority admission must reject before startup.

6. **Bootstrap semantics**
   - Prove all wrapper arguments, redirects, exit status, timeout behavior, job-control behavior, and `$!` PID semantics match the original wrapper.
   - Prove no caller/environment control over the OMP path.
   - Prove neither PATH nor a shell alias can shadow OMP.

7. **Descriptor lifecycle**
   - Assert exact opened FD count.
   - Assert `CLOEXEC` remains set.
   - Assert no runtime artifact FD appears in the child.
   - Assert deterministic closure on start failure, success, timeout, and cancellation.

8. **Fake-wrapper compatibility**
   - Keep fake-wrapper coverage through an explicit unexported test dependency.
   - Label it non-production evidence.
   - Never infer test mode from a shebang, path, environment variable, or fake native contents.
   - Add a test proving production server configuration cannot enable the bypass.

9. **Installed preflight**
   - On the current installation, run three times and expect the typed unsupported-boundary error with zero provider requests.
   - Enable the existing provider-free local rejection preflight only when both artifacts are provisioned in the immutable hierarchy.

10. **Stress gates**
    - Run executable/native focused tests with `-count=10`.
    - Run them under `-race -count=3`.

No production-safe real-OMP activation can be claimed for the currently installed user-owned executable/native layout. The correct immediate repair is typed fail-closed admission; immutable root/distinct-operator provisioning is the viable guarded activation path.
