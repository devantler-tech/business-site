import { readSync, writeSync } from "node:fs";
import { renderContact } from "./entry.mjs";

/** Read an already bounded size; only a boundary EOF is an ordinary shutdown.
 * @param {number} size @param {boolean} boundary
 */
function read(size, boundary = false) {
  const value = Buffer.alloc(size);
  let offset = 0;
  while (offset < size) {
    const count = readSync(0, value, offset, size - offset, null);
    if (count === 0) {
      if (boundary && offset === 0) return null;
      throw new Error("unavailable");
    }
    offset += count;
  }
  return value;
}

/** Only the supervisor's validated display projection may enter this worker.
 * The worker is not an HTTP, identity, authorization or general request parser.
 */
function serve() {
  if (!process.versions.node.startsWith("24.")) throw new Error("unavailable");
  const decoder = new TextDecoder("utf-8", { fatal: true });
  for (;;) {
    const prefix = read(4, true);
    if (prefix === null) return;
    const size = prefix.readUInt32BE();
    if (size === 0 || size > 8192 - 4) throw new Error("unavailable");
    const payload = read(size);
    if (payload === null) throw new Error("unavailable");
    const request = JSON.parse(decoder.decode(payload));
    if (
      request === null ||
      typeof request !== "object" ||
      Array.isArray(request) ||
      Object.keys(request).sort().join(",") !== "contact,id,version" ||
      request.version !== 1 ||
      typeof request.id !== "string" ||
      !/^[a-f0-9]{32}$/u.test(request.id) ||
      request.contact === null ||
      typeof request.contact !== "object" ||
      Array.isArray(request.contact)
    ) {
      throw new Error("unavailable");
    }
    const body = renderContact(request.contact);
    const response = Buffer.from(JSON.stringify({ version: 1, id: request.id, body }));
    if (response.length === 0 || response.length > 65536 - 4) throw new Error("unavailable");
    const header = Buffer.alloc(4);
    header.writeUInt32BE(response.length);
    const frame = Buffer.concat([header, response]);
    let offset = 0;
    while (offset < frame.length) offset += writeSync(1, frame, offset, frame.length - offset);
  }
}

// This is a separate compiler entry from the passive contact.mjs export.
// It serves only anonymous pipes: never listen on a port or emit diagnostics.
try {
  serve();
} catch {
  process.exitCode = 1;
}
