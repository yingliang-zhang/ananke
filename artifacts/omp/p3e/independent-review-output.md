Working...
## Verdict: **CHANGES REQUESTED**

No files edited or committed.

### Confirmed

- **Current candidate callsites are fake-only.** The sole `p3eExecAdapter` construction is test code: it re-execs `os.Args[0]` with `-test.run=^TestP3EFakeAdapterExecutable$`, fixed fake-mode environment, and a temporary synthetic root (`internal/lifecycle/omp_adapter_test.go:280-335`). No `cmd/`, GUI, daemon, or public lifecycle callsite constructs the runtime.
- Route, forbidden writes, provider/model, P3a materialization hash/nonce, active full fence, P3c `retry_process_admission`, materialization identity, and run intent are checked before launch (`omp_adapter.go:417-443`). The runtime only reads the launch boundary; it does not write P3e outbox/terminal/evidence facts or invoke `CreateRun`.
- The transcript projection is bounded to three ordered event kinds, scanner-limited to 64 KiB, and unknown/malformed/order-invalid input clears the prefix into `waiting_for_human` (`omp_adapter.go:579-654`).
- Normal completion, unknown transcript, crash, reconnect, cancellation, timeout, pre-start stale-fence, traversal, and pre-write directory replacement tests exist and pass.

### Blocking findings

1. **P1 — P3d’s complete sealed-materialization contract is not represented or validated.**  
   P3d’s canonical HostSpec and request require `payload_hash` and `seal_fingerprint` in `sealed_materialization`. `p3eHostSpec` and `p3eMaterialization` omit both; `validateRequest` checks only fixed materialization hash, nonce, and a caller-supplied `SourceHash` (`omp_adapter.go:173-255`, `417-422`).  
   A caller can change `Files` and recompute `SourceHash`; the request passes and arbitrary bytes are materialized. The existing “bad source” test changes bytes **without** recomputing the hash, so it does not cover this bypass (`omp_adapter_test.go:171-173`). This contradicts the ledger’s claim of “P3d’s exact closed HostSpec” validation.

   **Required:** bind and validate the complete P3d sealed-materialization tuple, including payload hash and seal fingerprint; do not accept caller-self-authenticated source bytes as the seal.

2. **P1 — Launch reopens a mutable pathname after sealing; partial materializations are not cleaned up.**  
   `materialize` initially uses descriptor-relative `O_NOFOLLOW` operations and rechecks before writing, but `start` passes the mutable `sealed.path` string directly to `adapter.Start` without revalidating the sealed directory identity (`omp_adapter.go:379-396`). `p3eExecAdapter.Start` assigns that string to `cmd.Dir` (`omp_adapter.go:72-90`), so a post-seal replacement is resolved by pathname at exec time. The existing TOCTOU test only replaces the directory **before** writes (`omp_adapter_test.go:252-277`).

   Separately, a duplicate valid relative path is allowed. The first write succeeds, the second fails `O_EXCL`, `materialize` returns an empty `p3eSealedRoot`, and `start` has no descriptor/identity with which to clean the partially created tree (`omp_adapter.go:446-498`, `501-544`). The claimed cleanup proof covers successful and terminal paths, not partial materialization failure.

   **Required:** revalidate/bind the sealed directory immediately at the launch boundary without falling back to a mutable lexical path; make materialization transactional and descriptor-owned so every post-`Mkdirat` failure cleans only the exact created tree. Add deterministic post-seal/pre-exec replacement and duplicate-path/partial-write cleanup tests.

3. **P1 — Cancellation recovery does not implement P3d’s required action.**  
   P3d mandates `retry_bounded_cancellation` when cancellation was requested before the terminal event. P3e defines only `retry_adapter_admission` and `reconnect_transcript_source` (`omp_adapter.go:161-166`); `recover` returns reconnect for every extant session, including one already rejected by `cancel` (`omp_adapter.go:696-711`). The ledger explicitly records this incompatible two-action policy (`experiment-ledger.md:1579`).

   **Required:** model the cancellation-requested recovery boundary distinctly and return the exact P3d action with an empty fail-closed state. Add cancellation-then-recover-before-terminal coverage.

4. **P2 — “Exact” transcript shape validation accepts duplicate JSON member names.**  
   `normalizeP3eTranscript` unmarshals into `map[string]string` (`omp_adapter.go:638-653`). Duplicate JSON keys collapse to the last value, so an otherwise malformed/ambiguous transcript shape can be accepted if its final values are canonical. P3d requires unknown event shapes to fail closed.

   **Required:** reject duplicate members while decoding and add an adversarial duplicate-key vector.

### Additional verification gap

- Stale-fence rejection is covered only when the fence is reclaimed before `start` begins. There remains a launch window after the final `validateFence` and before `adapter.Start` (`omp_adapter.go:383-393`). Add a deterministic interleaving proving a reclaimed fence cannot produce a fake invocation.

### Verification run

Passed:

```text
go test ./internal/lifecycle -run '^TestP3E' -count=1 -timeout 60s
go test ./... -count=1 -timeout 300s
go test -race ./... -count=1 -timeout 360s
go vet ./...
node --check contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs
node contracts/p3d/verify.mjs --self-test
```

The green suite establishes the implemented fake-only paths, but does not cover the missing seal fields, post-seal launch TOCTOU, partial-materialization cleanup, or P3d cancellation-recovery action.
