const ELEMENT_KEY = "element-6066-11e4-a52e-4f735466cecf";

export class WdaError extends Error {
  constructor(message, details = {}) {
    super(message, details.cause == null ? undefined : { cause: details.cause });
    this.name = "WdaError";
    this.status = details.status;
    this.payload = details.payload;
  }
}

function errorMessage(payload, fallback) {
  if (payload != null && typeof payload === "object") {
    const value = payload.value;
    if (value != null && typeof value === "object" && typeof value.message === "string") {
      return value.message;
    }
    if (typeof payload.message === "string") return payload.message;
  }
  return fallback;
}

function requiredElementId(value, description) {
  const elementId = value?.[ELEMENT_KEY] ?? value?.ELEMENT;
  if (typeof elementId !== "string" || elementId === "") {
    throw new WdaError(`WDA returned no element id for ${description}.`, { payload: value });
  }
  return elementId;
}

export class WdaClient {
  constructor(endpoint, fetchImpl = fetch) {
    this.endpoint = endpoint.replace(/\/$/, "");
    this.fetch = fetchImpl;
  }

  async request(path, { method = "GET", body } = {}) {
    let response;
    try {
      response = await this.fetch(`${this.endpoint}${path}`, {
        method,
        headers: body == null ? undefined : { "content-type": "application/json" },
        body: body == null ? undefined : JSON.stringify(body),
      });
    } catch (cause) {
      throw new WdaError(`WDA endpoint is unavailable at ${this.endpoint}: ${cause.message}`, { cause });
    }

    const raw = await response.text();
    let payload;
    try {
      payload = raw === "" ? undefined : JSON.parse(raw);
    } catch {
      payload = { raw };
    }
    const w3cError = payload?.value != null && typeof payload.value === "object" && payload.value.error;
    if (!response.ok || w3cError) {
      throw new WdaError(
        `WDA ${method} ${path} failed (${response.status}): ${errorMessage(payload, raw || response.statusText)}`,
        { status: response.status, payload },
      );
    }
    return payload?.value;
  }

  async status() {
    const status = await this.request("/status");
    if (status?.ready === false) {
      throw new WdaError("WDA endpoint reported ready=false.", { payload: status });
    }
    return status;
  }

  async createSession({ appPath, bundleId }) {
    const alwaysMatch = {
      platformName: "mac",
      appPath,
      noReset: true,
      skipAppKill: true,
    };
    if (bundleId) alwaysMatch.bundleId = bundleId;
    const value = await this.request("/session", {
      method: "POST",
      body: { capabilities: { alwaysMatch, firstMatch: [{}] } },
    });
    const sessionId = value?.sessionId;
    if (typeof sessionId !== "string" || sessionId === "") {
      throw new WdaError("WDA did not return a session id.", { payload: value });
    }
    return sessionId;
  }

  async deleteSession(sessionId) {
    await this.request(`/session/${encodeURIComponent(sessionId)}`, { method: "DELETE" });
  }

  async findByAccessibilityId(sessionId, identifier) {
    const value = await this.request(`/session/${encodeURIComponent(sessionId)}/element`, {
      method: "POST",
      body: { using: "accessibility id", value: identifier },
    });
    return requiredElementId(value, `accessibility id ${identifier}`);
  }

  async findOnlyDescendantByClassName(sessionId, ancestorId, className) {
    const value = await this.request(
      `/session/${encodeURIComponent(sessionId)}/element/${encodeURIComponent(ancestorId)}/elements`,
      { method: "POST", body: { using: "class name", value: className } },
    );
    if (!Array.isArray(value) || value.length !== 1) {
      throw new WdaError(`WDA returned ${Array.isArray(value) ? value.length : "no"} ${className} descendants.`, {
        payload: value,
      });
    }
    return requiredElementId(value[0], `class name ${className}`);
  }

  async click(sessionId, elementId) {
    await this.request(
      `/session/${encodeURIComponent(sessionId)}/element/${encodeURIComponent(elementId)}/click`,
      { method: "POST", body: {} },
    );
  }

  async isEnabled(sessionId, elementId) {
    return this.request(
      `/session/${encodeURIComponent(sessionId)}/element/${encodeURIComponent(elementId)}/enabled`,
    );
  }

  async text(sessionId, elementId) {
    return this.request(
      `/session/${encodeURIComponent(sessionId)}/element/${encodeURIComponent(elementId)}/text`,
    );
  }

  async screenshot(sessionId) {
    return this.request(`/session/${encodeURIComponent(sessionId)}/screenshot`);
  }
}
