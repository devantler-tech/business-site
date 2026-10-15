import { defineConfig, loadEnv } from "vite";
import { sveltekit } from "@sveltejs/kit/vite";
import adapter from "@sveltejs/adapter-static";
import { enhancedImages } from "@sveltejs/enhanced-img";
import { frontendEntries, checkUnseenRoutes } from "./src/release-gate.ts";

export default defineConfig(({ mode }) => ({
  build: { assetsInlineLimit: 0 },
  plugins: [
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
