import { configFromArguments, usage } from "../lib/config.mjs";
import { runPreflight } from "../lib/harness.mjs";

const parsed = await configFromArguments(process.argv.slice(2));
if (parsed.help) {
  console.log(usage);
} else {
  try {
    const result = await runPreflight(parsed.config);
    console.log(JSON.stringify({ status: result.status, evidenceDir: result.evidenceDir }));
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}
