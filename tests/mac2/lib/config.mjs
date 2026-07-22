import { access, readFile, stat } from "node:fs/promises";
import { extname, resolve } from "node:path";

export class ConfigError extends Error {
  constructor(message) {
    super(message);
    this.name = "ConfigError";
  }
}

const flagNames = new Set([
  "--wda-url",
  "--app",
  "--bundle-id",
  "--evidence-dir",
  "--timeout-ms",
  "--poll-interval-ms",
]);

function stringOption(value, name) {
  if (typeof value !== "string" || value.trim() === "") {
    throw new ConfigError(`${name} must be a non-empty value.`);
  }
  return value.trim();
}

function milliseconds(value, name, fallback) {
  if (value == null) return fallback;
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    throw new ConfigError(`${name} must be a positive integer number of milliseconds.`);
  }
  return parsed;
}

function bundleIdentifierFromXml(plist) {
  const match = plist.match(/<key>CFBundleIdentifier<\/key>\s*<string>([^<]+)<\/string>/);
  return match?.[1]?.trim();
}

export function parseArguments(argv) {
  const options = {};
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (argument === "--help" || argument === "-h") {
      return { help: true };
    }

    const [flag, inlineValue] = argument.split("=", 2);
    if (!flagNames.has(flag)) {
      throw new ConfigError(`Unknown option: ${argument}`);
    }
    if (options[flag] != null) {
      throw new ConfigError(`Option may be supplied once: ${flag}`);
    }

    const value = inlineValue ?? argv[++index];
    if (value == null || value.startsWith("--")) {
      throw new ConfigError(`Missing value for ${flag}.`);
    }
    options[flag] = value;
  }
  return { help: false, options };
}

export async function createConfig(input) {
  const wdaUrl = stringOption(input.wdaUrl, "--wda-url");
  let endpoint;
  try {
    endpoint = new URL(wdaUrl);
  } catch {
    throw new ConfigError(`--wda-url must be an absolute HTTP(S) URL: ${wdaUrl}`);
  }
  if (endpoint.protocol !== "http:" && endpoint.protocol !== "https:") {
    throw new ConfigError(`--wda-url must use HTTP(S), not ${endpoint.protocol}`);
  }
  if (endpoint.search || endpoint.hash) {
    throw new ConfigError("--wda-url must not include a query string or fragment.");
  }

  const appPath = resolve(stringOption(input.appPath, "--app"));
  if (extname(appPath).toLowerCase() !== ".app") {
    throw new ConfigError(`--app must name a macOS .app bundle: ${appPath}`);
  }
  try {
    if (!(await stat(appPath)).isDirectory()) {
      throw new Error("not a directory");
    }
  } catch {
    throw new ConfigError(`App bundle is unavailable: ${appPath}`);
  }

  const infoPlist = resolve(appPath, "Contents", "Info.plist");
  let plist;
  try {
    await access(infoPlist);
    plist = await readFile(infoPlist, "utf8");
  } catch {
    throw new ConfigError(`App bundle is unavailable: missing readable ${infoPlist}`);
  }

  const suppliedBundleId = input.bundleId == null ? undefined : stringOption(input.bundleId, "--bundle-id");
  const discoveredBundleId = bundleIdentifierFromXml(plist);
  if (suppliedBundleId && discoveredBundleId && suppliedBundleId !== discoveredBundleId) {
    throw new ConfigError(
      `--bundle-id (${suppliedBundleId}) does not match ${infoPlist} (${discoveredBundleId}).`,
    );
  }

  const evidenceDir = resolve(stringOption(input.evidenceDir, "--evidence-dir"));
  return Object.freeze({
    wdaUrl: endpoint.href.replace(/\/$/, ""),
    appPath,
    bundleId: suppliedBundleId ?? discoveredBundleId,
    evidenceDir,
    timeoutMs: milliseconds(input.timeoutMs, "--timeout-ms", 15_000),
    pollIntervalMs: milliseconds(input.pollIntervalMs, "--poll-interval-ms", 150),
  });
}

export async function configFromArguments(argv) {
  const parsed = parseArguments(argv);
  if (parsed.help) return parsed;
  return {
    help: false,
    config: await createConfig({
      wdaUrl: parsed.options["--wda-url"],
      appPath: parsed.options["--app"],
      bundleId: parsed.options["--bundle-id"],
      evidenceDir: parsed.options["--evidence-dir"],
      timeoutMs: parsed.options["--timeout-ms"],
      pollIntervalMs: parsed.options["--poll-interval-ms"],
    }),
  };
}

export const usage = `Usage:\n  npm --prefix tests/mac2 run preflight -- --wda-url <already-running-wda-url> --app <Ananke.app> --evidence-dir <empty-directory> [--bundle-id <id>]\n  npm --prefix tests/mac2 run e2e -- --wda-url <already-running-wda-url> --app <Ananke.app> --evidence-dir <empty-directory> [--bundle-id <id>]\n\nThe harness never starts Appium or WebDriverAgent and never changes macOS authorization.`;
