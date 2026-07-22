import { mkdir, readdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

export class EvidenceError extends Error {
  constructor(message) {
    super(message);
    this.name = "EvidenceError";
  }
}

export async function createEvidenceWriter(directory) {
  const resolvedDirectory = resolve(directory);
  await mkdir(resolvedDirectory, { recursive: true });
  const existing = await readdir(resolvedDirectory);
  if (existing.length > 0) {
    throw new EvidenceError(
      `Evidence directory must be empty to prevent overwriting prior evidence: ${resolvedDirectory}`,
    );
  }

  return Object.freeze({
    directory: resolvedDirectory,
    async json(name, value) {
      await writeFile(
        resolve(resolvedDirectory, name),
        `${JSON.stringify(value, null, 2)}\n`,
        "utf8",
      );
    },
    async screenshot(name, base64Png) {
      if (typeof base64Png !== "string" || base64Png.length === 0) {
        throw new EvidenceError("WDA returned an empty screenshot payload.");
      }
      await writeFile(resolve(resolvedDirectory, name), Buffer.from(base64Png, "base64"));
    },
  });
}
