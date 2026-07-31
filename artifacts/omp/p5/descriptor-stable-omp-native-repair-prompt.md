Repair fourth-review blocker 4 only: installed OMP executable/native validation must be atomic with execution/loading. Read artifacts/omp/p5/fourth-review-report-recovered.md section 4 and current execution_policy.go/audit_executor.go/wrapper compatibility tests. Strict TDD, no provider/model, no commit.

Threat: concurrent same-UID rename replacement after final validation; A→B→A evades post-validation. Existing route wrapper launches plain `omp`; native loader opens HOME/.omp/natives/17.1.3/pi_natives.darwin-arm64.node by path.

First create deterministic local feasibility tests/probes (no network/provider) for Darwin descriptor-stable execution: opened verified OMP FD invoked via /dev/fd/N from the frozen Bash wrapper stream; inspect installed OMP 17.1.3 loader code/config for a supported native path override or prove dlopen through inherited /dev/fd can be selected without mutable path. Do not assume APIs.

Implement a production-safe mechanism if feasible:
- Parent opens and hashes pinned executable/native descriptors with O_NOFOLLOW and retains them through child startup/load.
- The frozen wrapper stream must invoke the verified executable descriptor, never PATH/plain omp. A trusted bootstrap/function may wrap frozen bytes only if exact wrapper semantics and hash remain bound and tests prove no caller control.
- Native must load from descriptor-stable authority or an equivalently non-replaceable parent-held object. Every required FD must be explicit, minimal, CLOEXEC adjusted only as needed, closed deterministically, bound into command/evidence hashes, and denied to unrelated descendants where possible.
- Deterministic gates rename-replace OMP after final validation and native during startup, then restore A; B must never execute/load and credential/provider is absent.

If installed OMP 17.1.3 cannot load the native descriptor-stably on Darwin, do NOT weaken the threat model. Make production real-OMP activation fail closed with a precise typed unsupported-boundary error while preserving fake-wrapper tests/provider-free compatibility only as clearly non-production evidence, and report the exact blocker. Prefer a working guarded implementation if technically possible.

Run executable/native focused tests count=10/race=3, installed provider-free preflight count=3 if enabled, and full package if time. Do not touch blockers 5/6.