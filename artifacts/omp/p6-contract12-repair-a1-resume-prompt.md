Resume exact session `019f9eaf-d30e-7000-aaa2-40882d261596` in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. Do not redo research.

All intended A1 public artifacts appear on disk. I added the single missing test import `crypto/x509/pkix` and ran gofmt. The focused test now compiles but every fixture-based test fails at `DecodeFixture(testdata/p6-contract-v1.json)` with the same generic `controlled repair contract is invalid` error. This is the current RED/root cause to fix.

Tasks:
1. Diagnose why the generated canonical fixture is rejected by the new strict decoder/embedded real-artifact contract.
2. Fix the artifact generator/fixture/types/verifier minimally; do not weaken strict decoding, release-pin verification, X.509 role/domain/chain checks, or attacker substitutions.
3. Confirm real public bundle/manifest/policy/profile hashes are actual bytes and no private material/path is present.
4. Run package single, count=10, race count=3, package vet, gofmt, git diff --check.
5. Keep scope to `internal/repaircontract/**` and its experiment doc. No commit. Do not read or reference `$HOME/.ananke/controlled-repair/v1/`.

Return the exact cause and GREEN outputs. If blocked, leave a precise failing test rather than fabricating success.
