Resume exact session `019f9ecd-0128-7000-bf2c-7eb157a5c5d4` in `/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen`. Do not redo research or redesign completed A2 code.

Independent gates already pass: single 3.639s, count=10 17.254s, race count=3 32.903s, vet and diff-check.

One explicit A2 requirement remains unmet and blocks rereview:
- `assertExecutedVectorOrder` is defined but unused.
- There is no single acceptance-vector registry keyed by exact canonical vector ID.
- `TestP6A2AcceptanceVectorsAreExecutableNotFixtureClaims` only proves decorative fixture fields were removed; it does not make deletion/renaming of an actual vector test fail.

Fix only this evidence gap:
1. Create one ordered executable registry keyed by the complete canonical Slice 1–2 vector IDs documented for this contract.
2. Every registry entry must run a real positive/negative probe or a specific mutation with its expected outcome; semantic negative entries must consistently recompute all linked hashes where applicable.
3. One test must execute every entry, record actual ID/order, compare against the separately frozen canonical ID inventory, fail on missing/duplicate/renamed/unexecuted IDs, and verify each expected result.
4. Do not satisfy this by setting booleans, listing IDs without execution, or making every ID call one unrelated broad test. Shared probe helpers are fine, but each ID must map to behavior that proves its named condition.
5. Ensure prior attacker-root rehash covers `Dispatch.ReleasePinsHash` and all linked values before semantic rejection.
6. Update docs to state the registry is executable and name its test.
7. Keep all A1/A2 architecture unchanged. Scope remains `internal/repaircontract/**` and the experiment doc. No commit.
8. Run focused registry test, then package single, count=10, race count=3, vet, gofmt, diff-check. Return exact results. Do not call more unrelated research tools.
