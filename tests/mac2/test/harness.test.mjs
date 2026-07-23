import assert from "node:assert/strict";
import { createServer } from "node:http";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { createConfig } from "../lib/config.mjs";
import { runE2E, runPreflight } from "../lib/harness.mjs";
import { ACCESSIBILITY_IDS, PREFLIGHT_ACCESSIBILITY_IDS, WEB_ACCESSIBILITY_SELECTOR_ATTRIBUTE } from "../lib/ids.mjs";

const ELEMENT_KEY = "element-6066-11e4-a52e-4f735466cecf";
const ONE_PIXEL_PNG =
  "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScL8nAAAAABJRU5ErkJggg==";

async function requestBody(request) {
  let body = "";
  for await (const chunk of request) body += chunk;
  return body === "" ? undefined : JSON.parse(body);
}

function respond(response, status, value) {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(JSON.stringify({ value }));
}

async function startWdaMock({ missingIdentifier, cancelEnabledSequence } = {}) {
  const state = {
    clicks: [],
    locators: [],
    deletedSessions: 0,
    runState: undefined,
    sessionCapabilities: undefined,
    childLocators: [],
    textRequests: [],
    cancelEnabledChecks: [],
  };
  const staticIdentifiers = new Set(PREFLIGHT_ACCESSIBILITY_IDS);

  const server = createServer(async (request, response) => {
    const url = new URL(request.url, "http://wda.test");
    const method = request.method;
    const pathname = url.pathname;

    if (method === "GET" && pathname === "/status") {
      respond(response, 200, { ready: true });
      return;
    }
    if (method === "POST" && pathname === "/session") {
      state.sessionCapabilities = (await requestBody(request)).capabilities.alwaysMatch;
      respond(response, 200, { sessionId: "mock-session", capabilities: {} });
      return;
    }
    if (method === "DELETE" && pathname === "/session/mock-session") {
      state.deletedSessions += 1;
      respond(response, 200, null);
      return;
    }
    if (method === "POST" && pathname === "/session/mock-session/element") {
      const { using, value } = await requestBody(request);
      state.locators.push({ using, value });
      const available =
        staticIdentifiers.has(value) ||
        (state.runState != null &&
          (value === ACCESSIBILITY_IDS.selectedRunState || value === ACCESSIBILITY_IDS.cancelRun));
      if (using !== "accessibility id" || value === missingIdentifier || !available) {
        respond(response, 404, { error: "no such element", message: `missing ${value}` });
        return;
      }
      respond(response, 200, { [ELEMENT_KEY]: `element:${value}` });
      return;
    }
    if (method === "GET" && pathname === "/session/mock-session/screenshot") {
      respond(response, 200, ONE_PIXEL_PNG);
      return;
    }

    const childMatch = pathname.match(/^\/session\/mock-session\/element\/(.+)\/elements$/);
    if (childMatch && method === "POST") {
      const parent = decodeURIComponent(childMatch[1]).replace(/^element:/, "");
      const { using, value } = await requestBody(request);
      state.childLocators.push({ parent, using, value });
      const available =
        parent === ACCESSIBILITY_IDS.daemonHealth || parent === ACCESSIBILITY_IDS.selectedRunState;
      if (using !== "class name" || value !== "XCUIElementTypeStaticText" || !available) {
        respond(response, 404, { error: "no such element", message: `missing child for ${parent}` });
        return;
      }
      respond(response, 200, [{ [ELEMENT_KEY]: `state-text:${parent}` }]);
      return;
    }

    const elementMatch = pathname.match(/^\/session\/mock-session\/element\/(.+)\/(click|enabled|text)$/);
    if (elementMatch) {
      const identifier = decodeURIComponent(elementMatch[1]).replace(/^element:/, "");
      const command = elementMatch[2];
      if (command === "click" && method === "POST") {
        await requestBody(request);
        state.clicks.push(identifier);
        if (identifier === ACCESSIBILITY_IDS.launchFixture) state.runState = "running";
        if (identifier === ACCESSIBILITY_IDS.cancelRun) state.runState = "cancelled";
        respond(response, 200, null);
        return;
      }
      if (command === "enabled" && method === "GET") {
        const enabled =
          identifier !== ACCESSIBILITY_IDS.cancelRun
            ? true
            : cancelEnabledSequence == null
              ? state.runState === "running"
              : cancelEnabledSequence[Math.min(state.cancelEnabledChecks.length, cancelEnabledSequence.length - 1)];
        if (identifier === ACCESSIBILITY_IDS.cancelRun) state.cancelEnabledChecks.push(enabled);
        respond(response, 200, enabled);
        return;
      }
      if (command === "text" && method === "GET") {
        state.textRequests.push(identifier);
        const parent = identifier.replace(/^state-text:/, "");
        const text =
          parent === ACCESSIBILITY_IDS.daemonHealth
            ? "● daemon online"
            : parent === ACCESSIBILITY_IDS.selectedRunState
              ? `● ${state.runState}`
              : identifier;
        respond(response, 200, text);
        return;
      }
    }

    respond(response, 404, { error: "unknown command", message: `${method} ${pathname}` });
  });

  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  return {
    endpoint: `http://127.0.0.1:${address.port}`,
    state,
    async close() {
      await new Promise((resolve, reject) => server.close((error) => (error ? reject(error) : resolve())));
    },
  };
}

async function createAppBundle(root) {
  const appPath = join(root, "Ananke.app");
  await mkdir(join(appPath, "Contents"), { recursive: true });
  await writeFile(
    join(appPath, "Contents", "Info.plist"),
    "<plist><dict><key>CFBundleIdentifier</key><string>com.yingliangzhang.ananke</string></dict></plist>",
  );
  return appPath;
}

async function withFixture(callback, mockOptions) {
  const root = await mkdtemp(join(tmpdir(), "ananke-mac2-test-"));
  const mock = await startWdaMock(mockOptions);
  try {
    const appPath = await createAppBundle(root);
    await callback({
      appPath,
      mock,
      config: (evidenceName) =>
        createConfig({
          wdaUrl: mock.endpoint,
          appPath,
          evidenceDir: join(root, evidenceName),
          timeoutMs: 100,
          pollIntervalMs: 1,
        }),
      root,
    });
  } finally {
    await mock.close();
    await rm(root, { recursive: true, force: true });
  }
}
test("Mac2 selector contract uses explicit non-visual ARIA labels", () => {
  assert.equal(WEB_ACCESSIBILITY_SELECTOR_ATTRIBUTE, "aria-label");
  assert.deepEqual(ACCESSIBILITY_IDS, {
    bootstrapState: "ananke-bootstrap-state",
    daemonHealth: "ananke-daemon-health",
    refresh: "ananke-refresh",
    runList: "ananke-run-list",
    launchFixture: "ananke-launch-fixture",
    selectedRunId: "ananke-selected-run-id",
    selectedRunState: "ananke-selected-run-state",
    cancelRun: "ananke-cancel-run",
    grillReview: "ananke-grill-review",
  });
  assert.deepEqual(PREFLIGHT_ACCESSIBILITY_IDS, [
    ACCESSIBILITY_IDS.bootstrapState,
    ACCESSIBILITY_IDS.daemonHealth,
    ACCESSIBILITY_IDS.refresh,
    ACCESSIBILITY_IDS.runList,
    ACCESSIBILITY_IDS.launchFixture,
    ACCESSIBILITY_IDS.grillReview,
  ]);
});

test("preflight verifies the caller WDA endpoint, app bundle, and static accessibility ids", async () => {
  await withFixture(async ({ config, mock, root }) => {
    const result = await runPreflight(await config("preflight-evidence"));

    assert.equal(result.status, "passed");
    assert.deepEqual(result.preflightAccessibilityIds, PREFLIGHT_ACCESSIBILITY_IDS);
    assert.equal(mock.state.deletedSessions, 1);
    assert.deepEqual(mock.state.locators.map(({ using }) => using), mock.state.locators.map(() => "accessibility id"));
    assert.deepEqual(
      new Set(mock.state.locators.map(({ value }) => value)),
      new Set(PREFLIGHT_ACCESSIBILITY_IDS),
    );
    const evidence = JSON.parse(await readFile(join(root, "preflight-evidence", "preflight.json"), "utf8"));
    assert.equal(evidence.status, "passed");
    assert.deepEqual(evidence.screenshots, ["preflight.png"]);
  });
});

test("preflight reports an unavailable accessibility id and retains failure evidence", async () => {
  await withFixture(async ({ config, root }) => {
    await assert.rejects(runPreflight(await config("missing-id-evidence")), /Accessibility identifier unavailable: ananke-refresh/);

    const evidence = JSON.parse(await readFile(join(root, "missing-id-evidence", "preflight.json"), "utf8"));
    assert.equal(evidence.status, "failed");
    assert.match(evidence.error.message, /ananke-refresh/);
  }, { missingIdentifier: ACCESSIBILITY_IDS.refresh });
});

test("E2E observes dynamic health and run state through WDA text descendants while locating by accessibility id", async () => {
  await withFixture(async ({ config, mock, root }) => {
    const result = await runE2E(await config("e2e-evidence"));

    assert.equal(result.status, "passed");
    assert.deepEqual(mock.state.clicks, [
      ACCESSIBILITY_IDS.refresh,
      ACCESSIBILITY_IDS.launchFixture,
      ACCESSIBILITY_IDS.cancelRun,
      ACCESSIBILITY_IDS.refresh,
    ]);
    assert.equal(mock.state.sessionCapabilities.skipAppKill, true);
    assert.equal(mock.state.sessionCapabilities.noReset, true);
    const evidence = JSON.parse(await readFile(join(root, "e2e-evidence", "result.json"), "utf8"));
    assert.equal(evidence.status, "passed");
    assert.deepEqual(evidence.screenshots, ["preflight.png", "running.png", "cancelled.png"]);
    assert.deepEqual(mock.state.childLocators, [
      { parent: ACCESSIBILITY_IDS.daemonHealth, using: "class name", value: "XCUIElementTypeStaticText" },
      { parent: ACCESSIBILITY_IDS.selectedRunState, using: "class name", value: "XCUIElementTypeStaticText" },
      { parent: ACCESSIBILITY_IDS.selectedRunState, using: "class name", value: "XCUIElementTypeStaticText" },
      { parent: ACCESSIBILITY_IDS.daemonHealth, using: "class name", value: "XCUIElementTypeStaticText" },
      { parent: ACCESSIBILITY_IDS.selectedRunState, using: "class name", value: "XCUIElementTypeStaticText" },
    ]);
    assert.deepEqual(mock.state.textRequests, [
      `state-text:${ACCESSIBILITY_IDS.daemonHealth}`,
      `state-text:${ACCESSIBILITY_IDS.selectedRunState}`,
      `state-text:${ACCESSIBILITY_IDS.selectedRunState}`,
      `state-text:${ACCESSIBILITY_IDS.daemonHealth}`,
      `state-text:${ACCESSIBILITY_IDS.selectedRunState}`,
    ]);
  });
});

test("E2E re-resolves a transiently disabled cancel control before clicking", async () => {
  await withFixture(async ({ config, mock }) => {
    const result = await runE2E(await config("transient-disabled-cancel-evidence"));

    assert.equal(result.status, "passed");
    assert.deepEqual(mock.state.cancelEnabledChecks, [false, true]);
    assert.equal(
      mock.state.locators.filter(({ value }) => value === ACCESSIBILITY_IDS.cancelRun).length,
      2,
    );
    assert.deepEqual(mock.state.clicks, [
      ACCESSIBILITY_IDS.refresh,
      ACCESSIBILITY_IDS.launchFixture,
      ACCESSIBILITY_IDS.cancelRun,
      ACCESSIBILITY_IDS.refresh,
    ]);
  }, { cancelEnabledSequence: [false, true] });
});

test("E2E never clicks a cancel control WDA continues to report disabled", async () => {
  await withFixture(async ({ config, mock }) => {
    await assert.rejects(
      runE2E(await config("disabled-cancel-evidence")),
      /Accessibility control did not become enabled: ananke-cancel-run/,
    );

    assert.ok(mock.state.cancelEnabledChecks.length > 0);
    assert.ok(mock.state.cancelEnabledChecks.every((enabled) => enabled === false));
    assert.deepEqual(mock.state.clicks, [ACCESSIBILITY_IDS.refresh, ACCESSIBILITY_IDS.launchFixture]);
  }, { cancelEnabledSequence: [false] });
});

test("configuration rejects an unavailable app bundle before connecting to WDA", async () => {
  const root = await mkdtemp(join(tmpdir(), "ananke-mac2-config-"));
  try {
    await assert.rejects(
      createConfig({
        wdaUrl: "http://127.0.0.1:10100",
        appPath: join(root, "missing.app"),
        evidenceDir: join(root, "evidence"),
      }),
      /App bundle is unavailable/,
    );
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
