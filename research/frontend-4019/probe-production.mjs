import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { setTimeout } from "node:timers/promises";
import { fileURLToPath } from "node:url";

const samples = [
  ["astro", 5881, "dist/server/entry.mjs"],
  ["svelte", 5882, "build/index.js"],
];

async function requireFreePort(port) {
  const listener = createServer();
  await new Promise((resolve, reject) => {
    listener.once("error", reject);
    listener.listen(port, "127.0.0.1", resolve);
  });
  await new Promise((resolve, reject) => listener.close((error) => (error ? reject(error) : resolve())));
}

for (const [sample, port, entry] of samples) {
  await requireFreePort(port);
  const cwd = fileURLToPath(new URL(`./${sample}/`, import.meta.url));
  const child = spawn(process.execPath, [entry], {
    cwd,
    env: { ...process.env, HOST: "127.0.0.1", PORT: String(port) },
    stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  let startError;
  child.stdout.on("data", (chunk) => {
    output = (output + chunk).slice(-4096);
  });
  child.stderr.on("data", (chunk) => {
    output = (output + chunk).slice(-4096);
  });
  child.on("error", (error) => {
    startError = error;
  });
  const finished = new Promise((resolve) => child.once("close", resolve));
  const base = `http://127.0.0.1:${port}`;
  try {
    let ready = false;
    for (let attempt = 0; attempt < 100; attempt++) {
      if (startError || child.exitCode !== null) throw new Error(`${sample} failed to start: ${startError || output}`);
      try {
        ready = (await fetch(base, { signal: AbortSignal.timeout(1000) })).status === 200;
      } catch {
        /* Still starting. */
      }
      if (ready) break;
      await setTimeout(100);
    }
    if (!ready) throw new Error(`${sample} did not become ready`);
    const probe = spawn(process.execPath, ["--test", fileURLToPath(new URL("./probe.test.mjs", import.meta.url))], {
      env: { ...process.env, SAMPLE_URL: base },
      stdio: "inherit",
    });
    const status = await new Promise((resolve, reject) => {
      probe.once("error", reject);
      probe.once("close", resolve);
    });
    if (status !== 0) throw new Error(`${sample} production probes failed`);
  } finally {
    child.kill("SIGTERM");
    const stopped = await Promise.race([finished.then(() => true), setTimeout(5000).then(() => false)]);
    if (!stopped) {
      child.kill("SIGKILL");
      await finished;
    }
  }
}
