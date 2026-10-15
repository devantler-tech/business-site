import { readFile, readdir } from "node:fs/promises";
import { gzipSync } from "node:zlib";
import { createHash } from "node:crypto";
const roots = { astro: "dist/client", svelte: "build/client" };
async function files(dir) {
  let all = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = dir + "/" + entry.name;
    if (entry.isDirectory()) all.push(...(await files(path)));
    else all.push(path);
  }
  return all;
}
for (const [name, root] of Object.entries(roots)) {
  let bytes = 0,
    gzip = 0;
  for (const path of await files(name + "/" + root)) {
    if (!path.endsWith(".js")) continue;
    const data = await readFile(path);
    bytes += data.length;
    gzip += gzipSync(data).length;
  }
  const lock = await readFile(name + "/package-lock.json");
  const times = [];
  for (const i of [1, 2, 3])
    times.push(Number((await readFile(name + "/time-" + i + ".txt", "utf8")).match(/^real ([0-9.]+)$/m)[1]));
  console.log(
    JSON.stringify({
      sample: name,
      node: "24.19.0",
      npm: "11.19.1",
      lockSha256: createHash("sha256").update(lock).digest("hex"),
      buildSeconds: times,
      allEmittedJsBytes: bytes,
      allEmittedJsGzipBytes: gzip,
      dependencyNodes: Object.keys(JSON.parse(lock).packages).length - 1,
    }),
  );
}
