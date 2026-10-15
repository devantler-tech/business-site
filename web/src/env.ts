import { defineEnvVars } from "@sveltejs/kit/env";
import { parseFrontendRelease } from "./release-gate.ts";

export const variables = defineEnvVars({
  FEATURE_SVELTE_FRONTEND: { static: true, schema: parseFrontendRelease },
});
