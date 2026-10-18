import { defineConfig, loadEnv } from "vite";
import { sveltekit } from "@sveltejs/kit/vite";
import adapter from "@sveltejs/adapter-static";
import { enhancedImages } from "@sveltejs/enhanced-img";
import { readFileSync, writeFileSync, rmSync } from "node:fs";
import { capturePublicArtifact } from "../scripts/public-build.mjs";
import { frontendEntries, checkUnseenRoutes, parseFrontendRelease } from "./src/release-gate.ts";
import { readJournal, journalArchives } from "../src/data/journal.mjs";
import { readingPaths, readReadingReference } from "../src/data/journal-reading.mjs";
import { readJournalFeedReference } from "../src/data/journal-feed-reference.mjs";
import { readPublicDiscoveryReference } from "../src/data/public-discovery-reference.mjs";
import { writeDiscovery } from "./discovery.mjs";

export default defineConfig(({ mode }) => ({
  build: { assetsInlineLimit: 0 },
  plugins: [
    {
      name: "authored-journal",
      resolveId(id) { if (["virtual:journal", "virtual:journal-archives", "virtual:journal-reading", "virtual:technical-guides", "virtual:journal-feed", "virtual:business-sharing", "virtual:public-discovery"].includes(id)) return `\0${id}`; },
      load(id) {
        const enabled = parseFrontendRelease(loadEnv(mode, process.cwd(), "FEATURE_SVELTE_FRONTEND").FEATURE_SVELTE_FRONTEND);
        if (id === "\0virtual:business-sharing") return `export default ${JSON.stringify(enabled ? readPublicDiscoveryReference().sharingImage : null)};`;
        if (id === "\0virtual:public-discovery") return `export default ${JSON.stringify(enabled ? readPublicDiscoveryReference().assets : null)};`;
        if (id === "\0virtual:technical-guides") return `export default ${JSON.stringify(enabled ? readReadingReference().guides : null)};`;
        if (id === "\0virtual:journal-feed") {
          const reference = enabled ? readJournalFeedReference() : null;
          return `export default ${JSON.stringify(reference ? {xml:reference.xml,assets:reference.assets} : null)};`;
        }
        if (id === "\0virtual:journal-reading") {
          if (!enabled) return "export default null;";
          return `export default ${JSON.stringify(readReadingReference().readings)};`;
        }
        if (["\0virtual:journal", "\0virtual:journal-archives"].includes(id)) {
          const admitted = enabled ? new Set(readingPaths()) : new Set();
          const posts = enabled ? readJournal().map(post => ({...post,
            href:admitted.has(new URL(post.href).pathname) ? new URL(post.href).pathname : post.href,
            tags:post.tags.map(tag => ({...tag,href:new URL(tag.href).pathname})),
          })) : [];
          return `export default ${JSON.stringify(id === "\0virtual:journal" ? posts : journalArchives(posts))};`;
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
      adapter: {
        ...adapter(),
        async adapt(builder) {
          await adapter().adapt(builder);
          if (parseFrontendRelease(loadEnv(mode,process.cwd(),"FEATURE_SVELTE_FRONTEND").FEATURE_SVELTE_FRONTEND)) {
            writeDiscovery("build");
            writeFileSync("build/404.html",readFileSync(new URL("./not-found.html",import.meta.url)));
            capturePublicArtifact("build","../.astro/selected-public-artifact.json",process.env.SVELTE_PUBLIC_BUILD_NONCE);
          } else rmSync("../.astro/selected-public-artifact.json",{force:true});
        },
      },
      prerender: {
        entries: frontendEntries(loadEnv(mode, process.cwd(), "FEATURE_").FEATURE_SVELTE_FRONTEND, loadEnv(mode, process.cwd(), "FEATURE_").FEATURE_COMPANY_IDENTITY),
        crawl: false,
        handleUnseenRoutes: ({ routes }) =>
          checkUnseenRoutes(loadEnv(mode, process.cwd(), "FEATURE_").FEATURE_SVELTE_FRONTEND, routes, loadEnv(mode, process.cwd(), "FEATURE_").FEATURE_COMPANY_IDENTITY),
      },
    }),
  ],
}));
