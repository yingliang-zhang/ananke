Resume exact immutable-runtime implementation session. Continue using tools and finish the implementation; do not merely synthesize. Current build errors from real `go test`:
- execution_policy.go:292 undefined validateExecutionPolicyAtomicRuntimeAuthority
- execution_policy.go:979 undefined entry
- atomic_runtime_authority_test.go:54 ServerConfig missing testAtomicRuntimeAuthorityVerifier
- atomic_runtime_authority_test.go:100 auditInvocationHooks missing CredentialLookup
- atomic_runtime_authority_test.go:303 invalid struct comparison because slices

Inspect current partial files before editing. Fix root causes and complete typed fail-closed + immutable root-owned authority integration. Ensure production verifier is wired at NewServer/policy admission and runAuditInvocation before gateway/credential/child. Unexported test verifier must be injectable only through test-only package state/config that production callers cannot enable; prefer function dependency on internal structs rather than exported ServerConfig. Do not leave dead undefined stubs.

Then run gofmt, go test focused atomic/runtime count=10, race=3, critical Darwin sandbox/evidence tests count=3, full internal package once, and git diff --check. Current user-owned installed OMP preflight must expect typed unsupported with zero gateway/provider/child; no real provider. Report exact remaining provisioning step. No commit/push.