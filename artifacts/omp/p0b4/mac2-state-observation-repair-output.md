Working...
Repaired Mac2 state observation without changing stable accessibility IDs.

- `tests/mac2/lib/harness.mjs`
  - Locates `ananke-daemon-health` / `ananke-selected-run-state` by static `accessibility id`.
  - Resolves exactly one relative `XCUIElementTypeStaticText` descendant through WDA, then reads that child’s `GET /text`.
  - Existing polling re-locates the parent on every attempt, tolerating renderer redraws.

- `tests/mac2/lib/wda.mjs`
  - Added `findOnlyDescendantByClassName`; fails closed unless WDA returns exactly one descendant.

- `tests/mac2/test/harness.test.mjs`
  - Regression mock returns static IDs for parent text and dynamic visible state only for the child.
  - Verifies the fixed relative `class name` query and child text reads for online/running/cancelled transitions.

- `docs/experiments/mac-native-e2e-contract.md`
  - Documents the Mac2 mapping.

**Mapping**

`aria-label="ananke-daemon-health"` maps to an `XCUIElementTypeGroup`; WDA `GET /text` on that group returns the static accessible name. Its visible state is the sole `XCUIElementTypeStaticText` descendant. The harness therefore uses:

1. `accessibility id` → stable nonvisual parent locator.
2. Relative `class name: XCUIElementTypeStaticText` → fixed structural descendant.
3. `GET /text` on that descendant → visible state, e.g. `● daemon online`.

No visible-copy locator, predicate, or XPath is used.

**Evidence**

- Live WDA probe at `http://127.0.0.1:10100` returned `● daemon online` through the new parent/descendant mapping. No launch or cancel control was clicked.
- `npm --prefix tests/mac2 test` — 5/5 passed.
- Preflight only; no full E2E run:
  - Passed evidence: `/var/folders/fh/7dlfvrsn5938lw_4z6_pg_th0000gn/T/ananke-mac2-preflight-final-HHTI3t/preflight.json`
  - Verified WDA readiness, session creation, all five static IDs, and screenshot capture.
