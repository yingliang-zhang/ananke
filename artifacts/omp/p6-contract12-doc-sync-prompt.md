Update only `docs/experiments/p6-controlled-repair-supervisor-contract.md` in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` so it exactly matches the current compiled Contract Slices 1–2 release and tests. No code edits. No commit.

Source of truth:
- `internal/repaircontract/release_artifacts.go`
- `internal/repaircontract/contract.go`
- `internal/repaircontract/acceptance_registry_test.go`
- `internal/repaircontract/testdata/release-v1/release-manifest.json`
- round-2 report `artifacts/omp/p6a/contract-slices1-2-closure-review-2.md`

Required documentation corrections:
1. Eight DER certificate/SPKI files; fourteen explicit embedded public artifacts; thirteen ordered manifest entries.
2. Current hashes exactly:
   - release pins `sha256:04deac3392b77cda2393895008ac3ea90c5f07a5b3a0501b4142d3988639df0a`
   - trust bundle `sha256:28519f0022034ed6c47b011351ce80cb0f42594f4df2ebe5f4a820678d169786`
   - release manifest `sha256:09918769192b68de8f9f9e13c7281f37ecc24231fbce89b9bd3dce9f5fb0c095`
   - rotation policy `sha256:069e8492a9b2bd1ac9ac6f62f2e4f9946a5004e5236349b32aa334a153d75a24`
3. Include approver root/leaf certificate and SPKI hashes, root ID, fixed key ID, role, and signature domain from code.
4. Exact manifest order from the round-2 report/current JSON.
5. Document both independent Ed25519 X.509 chains, certificate semantics, SPKI distinction, and fixed future approval signer identity. Keep V1 `no_successor_authorized` and no active signature instance.
6. Replace the 79-vector inventory with all 91 exact ordered IDs from `canonicalAcceptanceVectorIDs`; renumber consistently and state the executable registry test name.
7. Remove every stale pre-B2 count/hash/claim. Do not add runtime/storage claims.

Verification:
- compare every documented artifact count/hash/order to source;
- compare vector list exactly to `canonicalAcceptanceVectorIDs` with a script or test;
- run `go test ./internal/repaircontract -count=1`, `go vet ./internal/repaircontract`, and `git diff --check`.
Return exact results. Do not create cron jobs.
