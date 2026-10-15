import { defineConfig } from "astro/config";
import node from "@astrojs/node";
export default defineConfig({
  output: "server",
  adapter: node({ mode: "standalone" }),
  security: { allowedDomains: [{ hostname: "127.0.0.1", port: "5881", protocol: "http" }] },
  vite: { resolve: { alias: { "#sample": new URL("../shared", import.meta.url).pathname } } },
});
