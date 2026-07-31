Implement a focused TDD fix in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`: the fixed `readOnlyAuditPromptTemplate` in `internal/trustedsupervisor/execution_policy.go` does not tell a real model the exact closed canonical JSON schema that `decodeAuditModelReport` requires. No commit and no docs/ledger.

Requirements:
- Preserve read-only/no repair/no Run/no unlogged-test-claim instructions.
- Explicitly require exactly one RFC-8785/JCS canonical JSON object and no prose/markdown/code fence/trailing bytes.
- Give exact top-level fields in canonical lexicographic order: findings, schema_version, summary, verdict. Schema version exact `ananke.local-trusted-supervisor-model-audit-report.v1`.
- Verdict approved iff findings is empty; rejected iff 1..256 findings.
- Finding fields exact canonical order code,line,message,path,severity. Severity enum blocker/high/medium/low/info; code regex semantics; repository-relative clean slash path; line bounds; no absolute paths/newlines/NUL; summary/message bounds and trimmed one-line semantics.
- Findings must be strictly sorted by the decoder's exact ordering: severity rank, code, path, line, message.
- Include compact canonical approved and rejected examples that themselves pass decodeAuditModelReport once the dynamic authority leak check is given a neutral invocation.
- Do not include machine-specific paths or credentials in prompt.
- Add tests in existing execution_policy/audit_evidence tests that fail before the template change and verify: required instructions present; embedded examples extract and decode; exact template hash changes are consumed dynamically; no raw authority.
- Run focused tests count=10, race count=3, full trustedsupervisor package, gofmt, vet, diff-check. Keep changes only to execution_policy.go and relevant tests.
Return changed files and exact test results.