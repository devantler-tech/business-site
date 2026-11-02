import { defineConfig, loadEnv } from "vite";
import { sveltekit } from "@sveltejs/kit/vite";
import adapter from "@sveltejs/adapter-static";
import { enhancedImages } from "@sveltejs/enhanced-img";
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";
import { capturePublicArtifact } from "../scripts/public-build.mjs";
import { frontendEntries, checkUnseenRoutes } from "./src/public-routes.ts";
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
      configureServer(server) {
        // The same closed asset inventory serves local development; never expose
        // the generated working directory or arbitrary repository files.
        const inventory = readReadingReference().assets as {filename:string;source:string}[];
        const assets = new Map(inventory.map(asset=>[`/${asset.filename}`,asset]));
        server.middlewares.use((request,response,next)=>{
          const asset = assets.get(new URL(request.url ?? "/","http://localhost").pathname);
          if (!asset) return next();
          const type = asset.filename.endsWith(".css") ? "text/css" : asset.filename.endsWith(".js") ? "text/javascript" : asset.filename.endsWith(".png") ? "image/png" : asset.filename.endsWith(".jpg") ? "image/jpeg" : "image/webp";
          response.setHeader("Content-Type",type);
          response.setHeader("Cache-Control","no-store");
          response.end(Buffer.from(asset.source,"base64"));
        });
      },
      resolveId(id) { if (["virtual:journal", "virtual:journal-archives", "virtual:journal-reading", "virtual:technical-guides", "virtual:journal-feed", "virtual:business-sharing", "virtual:public-discovery"].includes(id)) return `\0${id}`; },
      load(id) {
        if (id === "\0virtual:business-sharing") return `export default ${JSON.stringify(readPublicDiscoveryReference().sharingImage)};`;
        if (id === "\0virtual:public-discovery") return `export default ${JSON.stringify(readPublicDiscoveryReference().assets)};`;
        if (id === "\0virtual:technical-guides") return `export default ${JSON.stringify(readReadingReference().guides)};`;
        if (id === "\0virtual:journal-feed") {
          const reference = readJournalFeedReference();
          return `export default ${JSON.stringify(reference ? {xml:reference.xml,assets:reference.assets} : null)};`;
        }
        if (id === "\0virtual:journal-reading") {
          return `export default ${JSON.stringify(readReadingReference().readings)};`;
        }
        if (["\0virtual:journal", "\0virtual:journal-archives"].includes(id)) {
          const admitted = new Set(readingPaths());
          const posts = readJournal().map(post => ({...post,
            href:admitted.has(new URL(post.href).pathname) ? new URL(post.href).pathname : post.href,
            tags:post.tags.map(tag => ({...tag,href:new URL(tag.href).pathname})),
          }));
          return `export default ${JSON.stringify(id === "\0virtual:journal" ? posts : journalArchives(posts))};`;
        }
      },
    },
    enhancedImages(),
    sveltekit({
      adapter: {
        ...adapter(),
        async adapt(builder) {
          // These literal HTML/module dependencies are content assets, not
          // application imports. Populate the client directory before adaptation
          // so both framework preview and static publication serve the same closure.
          for (const asset of readReadingReference().assets) {
            const file = `${builder.getClientDirectory()}/${asset.filename}`;
            mkdirSync(dirname(file),{recursive:true});
            writeFileSync(file,Buffer.from(asset.source,"base64"));
          }
          await adapter().adapt(builder);
          writeDiscovery("build");
          writeFileSync("build/404.html",readFileSync(new URL("./not-found.html",import.meta.url)));
          capturePublicArtifact("build","../.generated/selected-public-artifact.json",process.env.SVELTE_PUBLIC_BUILD_NONCE);
        },
      },
      prerender: {
        entries: frontendEntries(loadEnv(mode, process.cwd(), "FEATURE_").FEATURE_COMPANY_IDENTITY),
        crawl: false,
        handleUnseenRoutes: ({ routes }) =>
          checkUnseenRoutes(routes, loadEnv(mode, process.cwd(), "FEATURE_").FEATURE_COMPANY_IDENTITY),
      },
    }),
  ],
}));
