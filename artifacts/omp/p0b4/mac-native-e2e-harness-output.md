# Mac-nativCompleted. Evidence written to:

- `artifacts/omp/p0b4/mac-native-e2e-harness-output.md`

Implemented:

- Stable renderer accessibility IDs for bootstrap, health, refresh, launch, selected run/state, and cancellation.
- Isolated `tests/mac2` direct-WDA harness with no GUI production dependency additions.
- WDA/app/accessibility preflight, JSON/PNG evidence output, and refresh → launch → cancel → refresh workflow.
- Mock-WDA contract coverage and operational/Xcode 27 authorization documentation.

Verified:

- `npm --prefix tests/mac2 test` — 4/4 passed.
- `npm --prefix gui run typecheck` — passed.
- `npm --prefix gui run build` — Go bridge and Vite production build passed.
- `cargo test --manifest-path gui/src-tauri/Cargo.toml` — 18 passed.

No Appium, WDA, XCTest, or native authorization flow was launched. Native execution still requires the caller’s already-running, manually authorized WDA endpoint. No commit or push was performed.
uction dependencies.

- `tests/mac2/lib/ids.mjs`
  - Canonical selector contract and static preflight selector set.

- `tests/mac2/lib/config.mjs`
  - Requires a caller-supplied HTTP(S) WDA URL, validates the `.app` bundle and
    readable `Contents/Info.plist`, validates an optional matching bundle ID,
    and requires an evidence directory.

- `tests/mac2/lib/wda.mjs`
  - Zero-dependency direct WDA WebDriver client using Node built-ins.
  - Every element lookup uses the `accessibility id` strategy.

- `tests/mac2/lib/evidence.mjs`
  - Requires an empty evidence directory and writes JSON/PNG evidence without
    overwriting prior output.

- `tests/mac2/lib/harness.mjs`
  - Preflight validates WDA readiness and static bootstrap selectors.
  - E2E validates `refresh → online → launch fixture → running → cancel →
    cancelled → refresh → online + cancelled`.
  - WDA sessions request `noReset: true` and `skipAppKill: true`; the harness
    closes its WDA session without terminating the app.
  - Captures structured result/timeline data and best-effort screenshots.

- `tests/mac2/scripts/preflight.mjs`
- `tests/mac2/scripts/e2e.mjs`
  - Isolated command-line entrypoints.

- `tests/mac2/test/harness.test.mjs`
  - Local mock-WDA contract tests for successful preflight, missing-selector
    failure evidence, full launch/cancel/refresh flow, and unavailable bundle
    rejection before WDA contact.

- `docs/experiments/mac-native-e2e-contract.md`
  - Documents the selector contract, authorization boundary, Xcode 27 Mac2
    WDA deployment-target workaround, commands, preflight, and evidence files.

## Verification

Passed checks:

```text
npm --prefix tests/mac2 test
# 4/4 Node mock-WDA contract tests passed

npm --prefix gui run typecheck
# TypeScript project check passed

npm --prefix gui run build
# Go bridge build and Vite production build passed

cargo test --manifest-path gui/src-tauri/Cargo.toml
# 18 tests passed across 3 suites
```

The mock-WDA E2E test proved that the harness uses accessibility-id selectors,
performs the expected refresh/launch/cancel/refresh order, verifies a persisted
`cancelled` state after refresh, records screenshots, and deletes its WDA
session. The GUI typecheck and production frontend build accepted the renderer
identifier changes; the full Tauri Rust test suite passed.

## Remaining user authorization prerequisite

Before a native run, the host owner must manually:

1. Build and start the exact WDA endpoint supplied by `--wda-url`.
2. Complete Automation Mode and any macOS privacy authorization in the active
   GUI login session.
3. Grant the WDA/XCTest runner and its launcher the required Accessibility and
   Automation access to control Ananke.
4. For the examined Mac2 4.0.4 WDA source, retain
   `MACOSX_DEPLOYMENT_TARGET = 12.0` in both Debug and Release WDA build
   settings when Xcode 27 rejects an older deployment target; rebuild and start
   WDA manually after that external-tool change.

If macOS shows an authorization dialog, the harness must be stopped. The caller
must authorize manually and restart WDA before rerunning. The harness never
invokes `automationmodetool`, `tccutil`, System Settings, or permission-dialog
controls.

Native AX/WDA interaction remains unexecuted until the caller supplies that
already-authorized endpoint; the provided preflight is the evidence-producing
check for that host-specific prerequisite.
