Finish only the incomplete parts of fourth-review Batch 1. Do not redo physical-root overlap or IPv4 sandbox work; their focused tests pass. No provider/model, no commit.

A) audit_connect_broker.go: set http.Client.CheckRedirect to reject all redirects. Add table tests for 301/302/303/307/308 proving exactly one upstream request, exact initial POST /v1/responses, no second request, and no Authorization at redirect target. Preserve response bounds and pinned dialer.

B) docs/runbook: change execution policy schema text from v1 to exact v4. Rewrite operational-gate caveat: most executor tests use fake route-aware wrappers; a separate provider-free installed OMP v17.1.3 preflight uses fixed fake credential/local deterministic rejection; no real model/provider API canary; no repair/source-write/Run. Add a Go consistency test that checks runbook schema equals executionPolicySchemaVersion and operational section names installed preflight separately.

C) supervisor_test_termination_test.go: replace waitForSupervisorTestMarker one-second wall-clock-only polling. Build bounded process-aware synchronization using the production AfterStart identity/process waiter or system Inspect, reporting early child exit and retaining a bounded deadline. Do not merely increase timeout. Update all five callers. Ensure no goroutine leak.

Run redirect/docs/termination focused count=10 and race=3, then required focused matrix count=5 and focused race once. Full package if time. Report exact results.