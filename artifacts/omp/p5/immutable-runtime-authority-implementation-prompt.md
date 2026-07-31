Implement blocker-4 safe production boundary and viable immutable-hierarchy activation. Read artifacts/omp/p5/descriptor-stable-omp-native-synthesis-recovered.md. Strict TDD, no provider/model, no commit.

A) Typed fail-closed:
- Add ErrUnsupportedAtomicRuntimeBoundary and typed closed-enum component/reason/failure_class=unsupported_atomic_omp_runtime_boundary.
- Production policy/NewServer and runAuditInvocation must reject unsafe OMP/native before gateway creation, credential lookup, StartGate, wrapper/child. No env/test flag/caller bypass.

B) Immutable hierarchy authority:
- Accept real OMP only when executable, native, and every physical path ancestor are non-symlink, root-owned (UID 0; keep this MVP narrower than configurable operator UID), and not writable by supervisor effective UID/supplementary groups/ACL. Use descriptor/openat O_NOFOLLOW identity traversal plus effective access checks; require publish-once regular artifacts with no write bits. Retain opened artifact/directory descriptors CLOEXEC in parent through process-group exit; never inherit them.
- Add authority schema/hash binding for ordered ancestor identities, artifact path/device/inode/owner/mode/size/hash, bootstrap hash, framed stream hash, and explicit non-inherited FD policy. Bump execution policy schema only if serialized fields change; update docs consistency.
- Native: do not copy into supervisor-owned HOME. Require immutable layout `<XDG_DATA_HOME>/omp/natives/17.1.3/pi_natives.darwin-arm64.node`, set XDG_DATA_HOME to policy-pinned root, deny all fallback native roots, validate the exact selected file.
- Executable: wrapper bytes remain frozen. Prepend trusted fixed bootstrap defining readonly function `omp(){ exec '<immutable absolute path>' "$@"; }`; wrapper bytes unchanged. Bind/hash framed bootstrap+wrapper. Remove OMP path from PATH. Prove background `$!` points to OMP process and timeout/resume semantics remain.

C) Tests:
- Current actual user-owned Homebrew/~/.omp fixtures: policy/production returns typed unsupported before any credential/gateway/child; zero provider request.
- Root-owned behavior unit tests via injected unexported authority verifier/test dependency only; production cannot enable bypass. Validate all ancestor rejection cases including ACL/effective write, symlink, rename A→B→A, wrong owner/mode.
- Preserve /dev/fd EACCES and native-loader-no-FD probes as feasibility evidence.
- Provider-free local OMP preflight should skip or expect typed unsupported for current layout; do not claim real activation until `/Library/Ananke` is provisioned.

Run focused count=10/race=3, package tests. Keep blockers 5/6 untouched. Report exact operator provisioning layout/permissions needed under /Library/Ananke.