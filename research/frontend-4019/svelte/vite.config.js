import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";
import adapter from "@sveltejs/adapter-node";
export default defineConfig({
  plugins: [sveltekit({ adapter: adapter(), paths: { origin: "http://127.0.0.1:5882" } })],
  resolve: { alias: { "#sample": new URL("../shared", import.meta.url).pathname } },
});
