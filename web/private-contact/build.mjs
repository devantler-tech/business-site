import { copyFileSync, mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";

/** Build only an owned private artifact, never the website's output directory. */
export async function buildContactRenderer() {
  if (!process.versions.node.startsWith("24.")) throw new Error("Node 24 is required");
  const directory = mkdtempSync(join(tmpdir(), "business-private-contact-"));
  const source = dirname(fileURLToPath(import.meta.url));
  try {
    await build({
      configFile: false,
      root: source,
      logLevel: "error",
      plugins: [svelte({ configFile: false })],
      ssr: { noExternal: true },
      build: {
        ssr: join(source, "entry.mjs"),
        outDir: directory,
        emptyOutDir: false,
        minify: false,
        rollupOptions: { output: { entryFileNames: "contact.mjs" } },
      },
    });
    // Preserve upstream notices with the copied server bundle. esm-env's
    // constants can be folded away, but its notice remains with its consumer.
    for (const [name, license] of [["svelte", "LICENSE.md"], ["clsx", "license"], ["esm-env", "LICENSE"]]) {
      const notices = join(directory, "notices", name);
      mkdirSync(notices, { recursive: true });
      copyFileSync(join(source, "..", "node_modules", name, license), join(notices, license));
    }
    return { directory, entry: join(directory, "contact.mjs") };
  } catch (error) {
    rmSync(directory, { recursive: true });
    throw error;
  }
}
