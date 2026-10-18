import { loadEnv } from "vite";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";
import { parseFrontendRelease } from "./src/release-gate.ts";

/** Resolve the exact same production-mode setting as the candidate renderer. */
export function selectedFrontend(directory = fileURLToPath(new URL("./",import.meta.url))): "astro" | "svelte" {
  const flags = loadEnv("production",directory,"FEATURE_");
  if (!parseFrontendRelease(flags.FEATURE_SVELTE_FRONTEND)) return "astro";
  // These older presentation variants are not implemented by the candidate.
  // Fail closed instead of quietly publishing a different flag combination.
  if (flags.FEATURE_PREVIEW_BANNER !== undefined && flags.FEATURE_PREVIEW_BANNER !== "false" ||
      flags.FEATURE_CLIENT_PORTFOLIO !== undefined && flags.FEATURE_CLIENT_PORTFOLIO !== "true" ||
      flags.FEATURE_FEATURED_PORTFOLIO !== undefined && flags.FEATURE_FEATURED_PORTFOLIO !== "true") throw new Error("Svelte publication profile has unsupported presentation settings");
  return "svelte";
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) console.log(selectedFrontend());
