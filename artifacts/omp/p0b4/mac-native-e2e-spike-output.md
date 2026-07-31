# Mac-nativCompleted the spike. Findings are in:

`artifacts/omp/p0b4/mac-native-e2e-spike-output.md`

Evidence-backed result:

- **Feasible conditionally:** Appium **3.5.2** + Mac2 **4.0.4** launched the debug-built Ananke app through a real XCTest-backed session.
- Retrieved the native accessibility tree; Ananke’s WebView exposed **Refresh** and **Launch fixture** as enabled `XCUIElementTypeButton` elements with stable `title` values, but empty native identifiers.
- Located **Refresh** via Appium `accessibility id` and clicked it successfully: find and click both returned HTTP 200. Post-click tree remained daemon-online with zero runs.
- **Host limitation:** stock Mac2 WDA bootstrap fails on Xcode 27 because its `10.15` deployment target is unsupported; an isolated WDA-only target override to `12.0` was required. The Appium-owned WDA path also hit `LocalAuthentication Code=-2 "Canceled by user"` without interacting with any prompt. Manually starting the isolated XCTest WDA and attaching Appium via `appium:webDriverAgentMacUrl` worked.
- Temporary Appium/XCTest services were stopped. No production P0b code, ledger, commits, or pushes were changed.
 The [capabilities reference](https://appium.github.io/appium-mac2-driver/latest/reference/capabilities/) documents `platformName: mac`, `appium:automationName: mac2`, `appium:appPath`, and `appium:webDriverAgentMacUrl` used below. The official setup also requires Accessibility permission for Xcode Helper and notes that Automation Mode may require interactive authentication.

## Isolated setup

All npm/Appium state lives under `artifacts/omp/p0b4/mac-native-e2e-tooling/`; `gui/package.json` was untouched.

```console
$ /Users/yingliangzhang/.hermes/node/bin/npm --prefix artifacts/omp/p0b4/mac-native-e2e-tooling install --no-save appium@3.5.2
added 296 packages in 1s

$ APPIUM_HOME=artifacts/omp/p0b4/mac-native-e2e-tooling/appium-home \
  artifacts/omp/p0b4/mac-native-e2e-tooling/node_modules/.bin/appium driver install mac2
i Driver mac2@4.0.4 successfully installed

$ …/appium driver doctor mac2
✔ xCode is installed at '/Applications/Xcode-beta.app/Contents/Developer'
✔ xcodebuild is installed and has a matching version number (27.0 >= 13)
WARN ✖ Automation Mode requires user authentication
… 0 required fixes needed, 1 optional fix possible.
```

The suggested `automationmodetool enable-automationmode-without-authentication` was **not** run: it changes host automation policy and the spike was not authorized to interact with authentication/permission prompts.

## Debug app build and launch

```console
$ DEVELOPER_DIR=/Applications/Xcode-beta.app/Contents/Developer \
  /Users/yingliangzhang/.hermes/node/bin/npm run tauri:build -- --debug
…
Finished `dev` profile [unoptimized + debuginfo] target(s) in 15.59s
Built application at: …/gui/src-tauri/target/debug/ananke-gui
Bundling Ananke.app (…/gui/src-tauri/target/debug/bundle/macos/Ananke.app)
Finished 1 bundle at:
…/gui/src-tauri/target/debug/bundle/macos/Ananke.app
```

App under test:

```text
/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/gui/src-tauri/target/debug/bundle/macos/Ananke.app
```

`codesign -dv --verbose=4` reported an arm64 ad-hoc debug signature (`Signature=adhoc`, no TeamIdentifier). This was not a runtime blocker: Mac2 launched the exact app path and returned its live UI tree.

## Mac2 server/session probe

The Appium server started successfully with the isolated home:

```console
$ APPIUM_HOME=…/mac-native-e2e-tooling/appium-home \
  DEVELOPER_DIR=/Applications/Xcode-beta.app/Contents/Developer \
  …/node_modules/.bin/appium --address 127.0.0.1 --port 4723
[Appium] Welcome to Appium v3.5.2
[Appium] Mac2Driver has been successfully loaded …
[Appium] Appium REST http interface listener started on http://127.0.0.1:4723
[Appium]   - mac2@4.0.4 (automationName 'Mac2')
```

### Xcode 27 compatibility and authentication limitation

The unmodified Mac2 4.0.4 WebDriverAgent project specifies `MACOSX_DEPLOYMENT_TARGET = 10.15`. Xcode 27 rejects that value:

```text
error: The macOS deployment target 'MACOSX_DEPLOYMENT_TARGET' is set to 10.15,
but the range of supported deployment target versions is 12.0 to 27.0.x.
```

Changing only the installed WDA copy under `artifacts/…/appium-home/node_modules/appium-mac2-driver/` to `12.0` made `xcodebuild build-for-testing test-without-building` succeed. The normal Appium-managed WDA lifecycle then reached XCTest but failed without user interaction:

```text
WebDriverAgentRunner … Failed to initialize for UI testing:
Error Domain=com.apple.LocalAuthentication Code=-2 "Canceled by user."
… Mac2Driver host process has exited with code 65
```

No dialog was clicked. This is an environment/setup limitation—not an Ananke signing/build failure. It makes the stock, Appium-owned WDA launch path unreliable on this Xcode 27 beta host until the user grants/configures the documented automation authorization (or the driver/Xcode compatibility is updated).

For the non-interactive probe, the same isolated, target-12 WDA was launched explicitly, without changing production files:

```console
$ USE_HOST=127.0.0.1 USE_PORT=10101 \
  DEVELOPER_DIR=/Applications/Xcode-beta.app/Contents/Developer \
  xcodebuild build-for-testing test-without-building \
    -project …/appium-mac2-driver/WebDriverAgentMac/WebDriverAgentMac.xcodeproj \
    -scheme WebDriverAgentRunner COMPILER_INDEX_STORE_ENABLE=NO
ServerURLHere->http://localhost:10101<-ServerURLHere

GET http://127.0.0.1:10101/status (Node `fetch` probe) → HTTP 200
{"value":{"message":"WebDriverAgent is ready to accept commands",…"ready":true,…}}
```

Appium attached to that running XCTest server and launched Ananke via this W3C payload:

```json
{
  "platformName": "mac",
  "appium:automationName": "mac2",
  "appium:appPath": "/Users/yingliangzhang/Projects/ananke-p0a-schema-codegen/gui/src-tauri/target/debug/bundle/macos/Ananke.app",
  "appium:webDriverAgentMacUrl": "http://127.0.0.1:10101",
  "appium:serverStartupTimeout": 30000
}
```

```console
POST /session … HTTP 200
{"value":{"capabilities":{"platformName":"mac","automationName":"mac2",…},
"sessionId":"070438a6-d3db-4ce8-aa95-049762dbe3e9"}}
```

The session was explicitly terminated with `DELETE /session/070438a6-d3db-4ce8-aa95-049762dbe3e9`, returning HTTP 200. Both temporary servers were stopped afterward.

## Accessibility findings and safe interaction

`GET /session/{id}/source` returned HTTP 200 and a 43,727-character XCTest tree. Relevant app-owned subtree (selected fields only):

```xml
<XCUIElementTypeWebView label="Ananke" …>
  <XCUIElementTypeStaticText value="ANANKE" …/>
  <XCUIElementTypeStaticText value="● daemon online" …/>
  <XCUIElementTypeStaticText value="0 active · 0 settled" …/>
  <XCUIElementTypeButton identifier="" label="" title="Refresh" enabled="true" …/>
  <XCUIElementTypeButton identifier="" label="" title="Launch fixture" enabled="true" …/>
  <XCUIElementTypeStaticText value="No runs yet." …/>
</XCUIElementTypeWebView>
```

The raw tree is deliberately not preserved here because the macOS menu hierarchy contains unrelated host menu/recents data. The selected UI evidence above is sufficient for the Ananke assessment.

| Control | XCTest exposure | Automation result |
| --- | --- | --- |
| Refresh | `XCUIElementTypeButton`, `title="Refresh"`, enabled; empty native `identifier`/`label` | `POST /element` with `{ "using": "accessibility id", "value": "Refresh" }` returned HTTP 200; `POST /click` returned HTTP 200 `{ "value": null }` |
| Launch fixture | `XCUIElementTypeButton`, `title="Launch fixture"`, enabled; empty native `identifier`/`label` | Located through the same `accessibility id` strategy (HTTP 200); intentionally not clicked because it creates a run |
| Status and empty-state text | `XCUIElementTypeStaticText` values | Re-read after Refresh: `● daemon online`, `0 active · 0 settled`, `No runs yet.`, and `Launch the real fixture.` all remained present |

**Conclusion on selectors:** current WebView controls are accessible to Mac2 by their rendered title, and the safe Refresh action is automated end-to-end. They are not exposed with explicit, test-owned accessibility identifiers; the native `identifier` fields are empty. Title-based locators are currently usable but couple tests to visible copy. A durable production E2E suite should use stable explicit accessibility ids/names before relying on nontrivial flows.

## Go/no-go

- **Go for a constrained Mac-native E2E harness:** debug app build, isolated Appium 3/Mac2 tooling, a real session, accessibility enumeration, and a safe interaction are proven.
- **No-go for unattended stock Mac2 startup on this host today:** Xcode 27 rejects Mac2 4.0.4's 10.15 deployment target, and XCTest automation authentication was canceled without permitted user interaction. Resolve the documented Xcode Helper/Automation Mode authorization and use a driver release compatible with Xcode 27 (or maintain the isolated WDA target override) before CI-like unattended execution.
- **No Ananke app build/signing blocker observed.** The limitation is Mac2/Xcode host setup; no production source change was needed for this spike.

