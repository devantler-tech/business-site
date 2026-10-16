import { defineConfig, loadEnv } from "vite";
import { sveltekit } from "@sveltejs/kit/vite";
import adapter from "@sveltejs/adapter-static";
import { enhancedImages } from "@sveltejs/enhanced-img";
import { frontendEntries, checkUnseenRoutes, parseFrontendRelease } from "./src/release-gate.ts";
import { readJournal } from "../src/data/journal.mjs";

export default defineConfig(({ mode }) => ({
  build: { assetsInlineLimit: 0 },
  plugins: [
    {
      name: "authored-journal",
      resolveId(id) { if (id === "virtual:journal") return "\0virtual:journal"; },
      load(id) {
        if (id === "\0virtual:journal") {
          const enabled = parseFrontendRelease(loadEnv(mode, process.cwd(), "FEATURE_SVELTE_FRONTEND").FEATURE_SVELTE_FRONTEND);
          return `export default ${JSON.stringify(enabled ? readJournal() : [])};`;
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
