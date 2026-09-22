import { readdir, readFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const frontendDirectory = dirname(fileURLToPath(import.meta.url));

async function filesBelow(directory) {
  const entries = await readdir(directory, { withFileTypes: true });
  const files = await Promise.all(entries.map(async (entry) => {
    const path = join(directory, entry.name);
    return entry.isDirectory() ? filesBelow(path) : [path];
  }));
  return files.flat().sort();
}

function runNode(arguments_, options = {}) {
  const result = spawnSync(process.execPath, arguments_, {
    cwd: frontendDirectory,
    stdio: "inherit",
    ...options,
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

const files = await filesBelow(frontendDirectory);
const modules = files.filter((path) => path.endsWith(".js"));
const tests = files.filter((path) => path.endsWith(".test.cjs"));

for (const modulePath of modules) {
  const source = await readFile(modulePath);
  runNode(["--input-type=module", "--check"], { input: source, stdio: ["pipe", "inherit", "inherit"] });
}

if (tests.length === 0) {
  throw new Error("frontend test discovery returned no files");
}
runNode(["--test", ...tests]);
