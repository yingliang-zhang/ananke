# Mac-native E2E harness contract

**Status:** implemented; execution remains explicitly authorized by the Mac host owner.

## Goal

Turn the verified Appium Mac2 accessibility probe into a reproducible,
explicitly-authorized Mac-native E2E harness for Ananke.

## Requirements

- Add stable accessibility ids to the renderer controls needed for bootstrap,
  refresh, launch fixture, cancel/reconnect state observation.
- Keep visible copy independent from test selectors.
- Store Mac2 test tooling under a dedicated test directory; do not add Appium to
  GUI production dependencies.
- The harness may only attach to a caller-provided, already-authorized WDA URL;
  it must never enable Automation Mode, click permission dialogs, or modify host
  security policy.
- Document the Xcode 27 Mac2 WDA deployment-target workaround and exact
  authorization prerequisite.
- Add a preflight that fails clearly when WDA endpoint/app bundle/accessibility
  ids are unavailable.
- No daemon transport, private DTO, P0b schema semantics, commit, or push.

## Implementation

The isolated harness is in [`tests/mac2`](../../tests/mac2). It uses Node 22's
built-in `fetch`, `node:test`, and filesystem APIs only; it does not install or
depend on Appium, XCTest, or any GUI production dependency. It is a direct WDA
client, so `--wda-url` is required and has no default.

The renderer exposes stable DOM ids as accessibility identifiers. The harness
locates every state surface with the WDA `accessibility id` locator; it never
locates a state surface by visible copy, predicate, or XPath.

| Identifier | Purpose |
| --- | --- |
| `ananke-bootstrap-state` | Bootstrap completion surface |
| `ananke-daemon-health` | Online/reconnect state observation |
| `ananke-refresh` | Explicit refresh action |
| `ananke-run-list` | Run-list bootstrap surface |
| `ananke-launch-fixture` | Fixture launch action |
| `ananke-selected-run-id` | Selected-run identity |
| `ananke-selected-run-state` | Running/cancelled state observation |
| `ananke-cancel-run` | Durable cancellation action |

The identifiers are independent of button labels and displayed state copy.

State observation has a separate mapping. Mac2 maps an `aria-label` state
surface to an `XCUIElementTypeGroup`, whose `GET /text` result is the static
accessible name. Its visual state is the group's single
`XCUIElementTypeStaticText` descendant. After locating the stable parent id,
the harness uses WDA's relative `POST /session/{sessionId}/element/{parentId}/elements`
query with `{ "using": "class name", "value": "XCUIElementTypeStaticText" }`,
requires exactly one descendant, then reads that descendant with `GET /text`.
The relative class query is fixed structure, not a visible-copy locator.

## Required authorization boundary

Before either command is run, the caller must manually start the exact
WebDriverAgentMac endpoint supplied in `--wda-url` and complete its macOS
authorization in the active GUI login session. In particular, the WDA/XCTest
runner (and the parent that launched it) must already be allowed to use the
required Accessibility and Automation controls for Ananke. If macOS presents
an Automation Mode or privacy dialog, stop the harness; authorize it manually,
then start WDA again before retrying.

The harness does not start Appium, WDA, `xcodebuild`, or XCTest. It does not
invoke `automationmodetool`, `tccutil`, System Settings, or any permission
dialog control. Its only session capability requests are `appPath`, an optional
matching `bundleId`, `noReset: true`, and `skipAppKill: true`; the latter two
avoid resetting or terminating a caller-owned Ananke app.

### Xcode 27 Mac2 WDA deployment target

The verified Mac2 4.0.4 WDA source declares
`MACOSX_DEPLOYMENT_TARGET = 12.0` for both Debug and Release in
`WebDriverAgentMac/WebDriverAgentMac.xcodeproj/project.pbxproj`. When Xcode 27
rejects an older Mac2 WDA deployment target, the host owner must manually set
both of those WDA build settings to `12.0`, rebuild WDA, and start that endpoint
outside this harness. Recheck the installed driver source after every driver
upgrade; the harness neither patches that project nor starts its build.

## Commands and preflight

Build the Ananke app through the normal project build path, manually prepare an
authorized WDA endpoint, and choose new empty evidence directories:

```sh
APP="$PWD/gui/src-tauri/target/release/bundle/macos/Ananke.app"
WDA_URL="http://127.0.0.1:10100"
EVIDENCE_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/ananke-mac2.XXXXXX")"

npm --prefix tests/mac2 run preflight -- \
  --wda-url "$WDA_URL" --app "$APP" --evidence-dir "$EVIDENCE_ROOT/preflight"

npm --prefix tests/mac2 run e2e -- \
  --wda-url "$WDA_URL" --app "$APP" --evidence-dir "$EVIDENCE_ROOT/e2e"
```

For the cancellable workflow, build the debug app and use its bundle instead:

```sh
npm --prefix gui run tauri:build -- --debug
APP="$PWD/gui/src-tauri/target/debug/bundle/macos/Ananke.app"
```

Only debug builds configure the existing fakeworker fixture to emit its normal
six events and then remain active for its 30-second pre-exit fixture hold. That
window exceeds the harness's 15-second observation deadline without adding a
harness retry or changing the release fixture's short demonstration lifecycle.

`preflight` rejects an unavailable/non-`.app` bundle, missing `Info.plist`, an
unreachable or `ready=false` WDA endpoint, and any missing static bootstrap,
health, refresh, run-list, or launch identifier. The E2E command repeats that
preflight, then verifies the dynamic selected-run and cancel identifiers while
driving refresh → launch fixture → cancel → refresh. A fixture that becomes
terminal before cancellation is an explicit test failure rather than a hidden
retry.

Each evidence directory must be empty. `preflight` writes `preflight.json` and
`preflight.png`; E2E writes `result.json` plus `preflight.png`, `running.png`,
and `cancelled.png`. A failed run writes its structured error/timeline and
attempts `failure.png` before closing only its WDA session. The Node contract
tests use a local mock WDA server; they never attach to a real WDA endpoint or
launch a native test runner.
