import { ACCESSIBILITY_IDS, PREFLIGHT_ACCESSIBILITY_IDS } from "./ids.mjs";
import { createEvidenceWriter } from "./evidence.mjs";
import { WdaClient } from "./wda.mjs";

export class HarnessError extends Error {
  constructor(message, options = {}) {
    super(message, options.cause == null ? undefined : { cause: options.cause });
    this.name = "HarnessError";
  }
}

const delay = (milliseconds) => new Promise((resolve) => setTimeout(resolve, milliseconds));
const STATE_TEXT_CLASS_NAME = "XCUIElementTypeStaticText";

function now() {
  return new Date().toISOString();
}

function errorRecord(error) {
  return {
    name: error?.name ?? "Error",
    message: error instanceof Error ? error.message : String(error),
    status: typeof error?.status === "number" ? error.status : undefined,
  };
}

async function waitFor(operation, config, description) {
  const deadline = Date.now() + config.timeoutMs;
  let lastError;
  while (Date.now() <= deadline) {
    try {
      const value = await operation();
      if (value !== false && value != null) return value;
    } catch (error) {
      lastError = error;
    }
    await delay(config.pollIntervalMs);
  }
  const suffix = lastError instanceof Error ? ` Last response: ${lastError.message}` : "";
  throw new HarnessError(`${description} within ${config.timeoutMs}ms.${suffix}`, { cause: lastError });
}

async function requireAccessibilityId(client, sessionId, identifier, config) {
  try {
    return await waitFor(
      () => client.findByAccessibilityId(sessionId, identifier),
      config,
      `Accessibility identifier unavailable: ${identifier}`,
    );
  } catch (error) {
    if (error instanceof HarnessError) throw error;
    throw new HarnessError(`Accessibility identifier unavailable: ${identifier}`, { cause: error });
  }
}
async function requireEnabledAccessibilityId(client, sessionId, identifier, config) {
  return waitFor(async () => {
    const elementId = await client.findByAccessibilityId(sessionId, identifier);
    return (await client.isEnabled(sessionId, elementId)) === true ? elementId : false;
  }, config, `Accessibility control did not become enabled: ${identifier}`);
}

async function verifyPreflightIds(client, sessionId, config) {
  const found = [];
  for (const identifier of PREFLIGHT_ACCESSIBILITY_IDS) {
    await requireAccessibilityId(client, sessionId, identifier, config);
    found.push(identifier);
  }
  return found;
}

async function waitForStateText(client, sessionId, identifier, expected, config, description) {
  return waitFor(async () => {
    const stateSurfaceId = await client.findByAccessibilityId(sessionId, identifier);
    const stateTextId = await client.findOnlyDescendantByClassName(
      sessionId,
      stateSurfaceId,
      STATE_TEXT_CLASS_NAME,
    );
    const text = String(await client.text(sessionId, stateTextId));
    if (expected.test(text)) return text;
    throw new HarnessError(`Observed ${identifier} descendant text ${JSON.stringify(text)}.`);
  }, config, description);
}

async function captureScreenshot(client, sessionId, evidence, result, name) {
  try {
    await evidence.screenshot(`${name}.png`, await client.screenshot(sessionId));
    result.screenshots.push(`${name}.png`);
  } catch (error) {
    result.evidenceWarnings.push(`Could not capture ${name}.png: ${errorRecord(error).message}`);
  }
}

async function closeSession(client, sessionId, result) {
  if (sessionId == null) return;
  try {
    await client.deleteSession(sessionId);
  } catch (error) {
    result.evidenceWarnings.push(`Could not close WDA session: ${errorRecord(error).message}`);
  }
}

function makeResult(kind, config) {
  return {
    schemaVersion: 1,
    kind,
    status: "running",
    startedAt: now(),
    endpoint: config.wdaUrl,
    appPath: config.appPath,
    bundleId: config.bundleId ?? null,
    preflightAccessibilityIds: [],
    timeline: [],
    screenshots: [],
    evidenceWarnings: [],
  };
}

function addTimeline(result, step, detail = {}) {
  result.timeline.push({ at: now(), step, ...detail });
}

async function requireOnlineHealth(client, sessionId, config) {
  return waitForStateText(
    client,
    sessionId,
    ACCESSIBILITY_IDS.daemonHealth,
    /\bonline\b/i,
    config,
    "Daemon health did not report online",
  );
}

async function requireRunState(client, sessionId, expectedState, config) {
  return waitForStateText(
    client,
    sessionId,
    ACCESSIBILITY_IDS.selectedRunState,
    new RegExp(`\\b${expectedState}\\b`, "i"),
    config,
    `Selected run did not reach ${expectedState}`,
  );
}

async function runWorkflow(config, kind) {
  const evidence = await createEvidenceWriter(config.evidenceDir);
  const client = new WdaClient(config.wdaUrl);
  const result = makeResult(kind, config);
  let sessionId;
  let failure;

  try {
    const status = await client.status();
    result.wdaReady = status?.ready ?? true;
    addTimeline(result, "wda-ready");

    sessionId = await client.createSession(config);
    addTimeline(result, "session-created");

    result.preflightAccessibilityIds = await verifyPreflightIds(client, sessionId, config);
    addTimeline(result, "accessibility-ids-verified", {
      count: result.preflightAccessibilityIds.length,
    });
    await captureScreenshot(client, sessionId, evidence, result, "preflight");

    if (kind === "preflight") {
      result.status = "passed";
    } else {
      const refresh = await requireAccessibilityId(
        client,
        sessionId,
        ACCESSIBILITY_IDS.refresh,
        config,
      );
      await client.click(sessionId, refresh);
      const onlineBeforeLaunch = await requireOnlineHealth(client, sessionId, config);
      addTimeline(result, "refresh-verified", { healthText: onlineBeforeLaunch });

      const launch = await requireAccessibilityId(
        client,
        sessionId,
        ACCESSIBILITY_IDS.launchFixture,
        config,
      );
      await client.click(sessionId, launch);
      const running = await requireRunState(client, sessionId, "running", config);
      addTimeline(result, "fixture-running", { stateText: running });

      const cancel = await requireEnabledAccessibilityId(
        client,
        sessionId,
        ACCESSIBILITY_IDS.cancelRun,
        config,
      );
      await captureScreenshot(client, sessionId, evidence, result, "running");

      await client.click(sessionId, cancel);
      const cancelled = await requireRunState(client, sessionId, "cancelled", config);
      addTimeline(result, "fixture-cancelled", { stateText: cancelled });

      const refreshAfterCancel = await requireAccessibilityId(
        client,
        sessionId,
        ACCESSIBILITY_IDS.refresh,
        config,
      );
      await client.click(sessionId, refreshAfterCancel);
      const onlineAfterCancel = await requireOnlineHealth(client, sessionId, config);
      const persistedState = await requireRunState(client, sessionId, "cancelled", config);
      addTimeline(result, "refresh-after-cancel-verified", {
        healthText: onlineAfterCancel,
        stateText: persistedState,
      });
      await captureScreenshot(client, sessionId, evidence, result, "cancelled");
      result.status = "passed";
    }
  } catch (error) {
    failure = error;
    result.status = "failed";
    result.error = errorRecord(error);
    addTimeline(result, "failed", result.error);
    if (sessionId != null) {
      await captureScreenshot(client, sessionId, evidence, result, "failure");
    }
  } finally {
    await closeSession(client, sessionId, result);
    result.finishedAt = now();
    await evidence.json(kind === "preflight" ? "preflight.json" : "result.json", result);
  }

  if (failure != null) {
    throw new HarnessError(`${errorRecord(failure).message}\nEvidence: ${evidence.directory}`, {
      cause: failure,
    });
  }
  return { ...result, evidenceDir: evidence.directory };
}

export function runPreflight(config) {
  return runWorkflow(config, "preflight");
}

export function runE2E(config) {
  return runWorkflow(config, "e2e");
}
