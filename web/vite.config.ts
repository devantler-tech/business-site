import { defineConfig, loadEnv } from "vite";
import { sveltekit } from "@sveltejs/kit/vite";
import adapter from "@sveltejs/adapter-static";
import { enhancedImages } from "@sveltejs/enhanced-img";
import { frontendEntries, checkUnseenRoutes, parseFrontendRelease } from "./src/release-gate.ts";
import { readJournal } from "../src/data/journal.mjs";
import { readingPath, readReadingReference } from "../src/data/journal-reading.mjs";

export default defineConfig(({ mode }) => ({
  build: { assetsInlineLimit: 0 },
  plugins: [
    {
      name: "authored-journal",
      resolveId(id) { if (["virtual:journal", "virtual:journal-reading"].includes(id)) return `\0${id}`; },
      load(id) {
        const enabled = parseFrontendRelease(loadEnv(mode, process.cwd(), "FEATURE_SVELTE_FRONTEND").FEATURE_SVELTE_FRONTEND);
        if (id === "\0virtual:journal-reading") {
          if (!enabled) return "export default null;";
          const { assets, ...reading } = readReadingReference();
          return `export default ${JSON.stringify(reading)};`;
        }
        if (id === "\0virtual:journal") {
          const posts = enabled ? readJournal().map(post => ({...post,href:post.href === `https://devantler.tech${readingPath}` ? readingPath : post.href})) : [];
          return `export default ${JSON.stringify(posts)};`;
        }
      },
      generateBundle() {
        if (!parseFrontendRelease(loadEnv(mode, process.cwd(), "FEATURE_SVELTE_FRONTEND").FEATURE_SVELTE_FRONTEND)) return;
        for (const asset of readReadingReference().assets) {
          this.emitFile({type:"asset",fileName:asset.filename,source:Buffer.from(asset.source, "base64")});
        }
      },
    },
    enhancedImages(),
    sveltekit({
      adapter: adapter(),
      prerender: {
        entries: frontendEntries(loadEnv(mode, process.cwd(), "FEATURE_SVELTE_FRONTEND").FEATURE_SVELTE_FRONTEND),
        crawl: false,
        handleUnseenRoutes: ({ routes }) =>
          checkUnseenRoutes(loadEnv(mode, process.cwd(), "FEATURE_SVELTE_FRONTEND").FEATURE_SVELTE_FRONTEND, routes),
      },
    }),
  ],
}));
