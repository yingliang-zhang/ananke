import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { spawn } from "node:child_process";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const repositoryRoot = resolve(scriptDirectory, "../..");
const outputDirectory = resolve(repositoryRoot, ".ananke/bin");
const go = process.env.GO ?? "go";
const targets = [
  ["ananke", "./cmd/ananke"],
  ["ananke-supervisor", "./cmd/ananke-supervisor"],
  ["ananke-fakeworker", "./cmd/ananke-fakeworker"],
];

await mkdir(outputDirectory, { recursive: true, mode: 0o700 });
for (const [name, source] of targets) {
  const output = resolve(outputDirectory, name);
  await new Promise((resolveBuild, rejectBuild) => {
    const child = spawn(go, ["build", "-o", output, source], {
      cwd: repositoryRoot,
      stdio: "inherit",
    });
    child.once("error", rejectBuild);
    child.once("exit", (code) => {
      if (code === 0) {
        resolveBuild();
        return;
      }
      rejectBuild(new Error(`${go} build ${source} exited with ${code}`));
    });
  });
}
