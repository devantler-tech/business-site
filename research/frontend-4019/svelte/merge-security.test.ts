import { createRequire } from "node:module";
import { registerMergeSecurityTests } from "../../../web/tests/helpers/deepmerge-security.ts";

const project = createRequire(import.meta.url);
registerMergeSecurityTests(createRequire(project.resolve("@sveltejs/vite-plugin-svelte")));
