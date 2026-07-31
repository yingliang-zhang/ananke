# P6 Contract Slices 1–6 Fix Prompt

## Repository
`/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen` on branch `feat/task-proposal-core`.

## Context
An independent K3 hard audit of P6 Slices 1–6 found the following findings that need fixing. Fix each finding with a RED→implement→GREEN cycle.

## Findings from audit
<!-- AUDIT FINDINGS WILL BE PASTED HERE -->

## Fix rules
1. Fix one logical group per commit
2. Write a RED test first that demonstrates the defect
3. Implement the minimal fix
4. Run the focused test to confirm GREEN
5. Do NOT refactor beyond the findings
6. Do NOT run the full test suite mid-campaign
7. Do NOT commit, push, or edit docs/artifacts
8. Follow existing code conventions (canonical self-hashed records, closed enums, opaque capabilities)
9. Run `go build ./...` and `go vet ./...` after all fixes
10. Run `gofmt -w` on all modified files

## Verification after fixes
```bash
go build ./... && echo "BUILD_PASS"
go vet ./... && echo "VET_PASS"
go test ./internal/repaircontract -run '^TestP6Slice' -count=1 -timeout 120s
gofmt -d internal/repaircontract/*.go
```
