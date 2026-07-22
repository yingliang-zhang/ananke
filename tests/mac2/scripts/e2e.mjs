import { configFromArguments, usage } from "../lib/config.mjs";
import { runE2E } from "../lib/harness.mjs";

const parsed = await configFromArguments(process.argv.slice(2));
if (parsed.help) {
  console.log(usage);
} else {
  try {
    const result = await runE2E(parsed.config);
    console.log(JSON.stringify({ status: result.status, evidenceDir: result.evidenceDir }));
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
