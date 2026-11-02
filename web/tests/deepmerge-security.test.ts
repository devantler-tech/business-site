import { createRequire } from "node:module";
import { registerMergeSecurityTests } from "./helpers/deepmerge-security.ts";

registerMergeSecurityTests(createRequire(import.meta.resolve("@sveltejs/vite-plugin-svelte")));
