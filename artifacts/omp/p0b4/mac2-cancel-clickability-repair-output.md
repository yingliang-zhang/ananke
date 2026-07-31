Working...
Repaired `tests/mac2/lib/harness.mjs`; no commit/push.

**Cause:** renderer redraws replace the accessibility tree (`app.innerHTML` on refresh). The harness observed `● running`, then treated one subsequent WDA `enabled=false` snapshot as terminal instead of re-resolving the cancel control. The failure occurred 175 ms after the running observation.

**WDA proof**
- Raw `/source` reports a disabled cancel control as `enabled="false"`.
- For that same control: `/enabled → false`; `/attribute/enabled → "false"`.
- A normal Refresh control reported `enabled="true"`, `/enabled → true`, and `/attribute/enabled → "true"`.
- The disabled Cancel control was still `hittable="true"`: hittability is not a safe substitute for enabled state.
- A raw lookup also produced a WDA stale-element response after renderer turnover. Thus this is not an inverted WDA enabled convention; element snapshots are transient across redraws.

**Fix**
- `requireEnabledAccessibilityId()` now re-finds `ananke-cancel-run` on every poll and accepts it only when WDA returns the boolean `true` from `/enabled`.
- The harness never clicks after `false`, a missing element, or a stale-element response. It times out fail-closed instead.

**Regression coverage**
- Retries a transient `false → true` enabled sequence, re-locates the control, then clicks once.
- Keeps `false` indefinitely and proves no Cancel click is issued.

**Verification**
- `npm --prefix tests/mac2 test` — 7/7 passed.
- Focused transient/disabled cancel tests — 2/2 passed.

No live fixture was launched or cancelled during investigation; WDA was used only for session creation, source/attribute inspection, selection, and observation.
