import { defineEnvVars } from "@sveltejs/kit/env";
import { parseFrontendRelease } from "./release-gate.ts";

export const variables = defineEnvVars({
  FEATURE_SVELTE_FRONTEND: { static: true, schema: parseFrontendRelease },
  FEATURE_COMPANY_IDENTITY: { static: true, schema: parseFrontendRelease },
  FEATURE_OFFER_COPY: { static: true, schema: parseFrontendRelease },
  FEATURE_CUSTOMER_SCOPE: { static: true, schema: parseFrontendRelease },
  COMPANY_POSTAL_ADDRESS: { static: true, schema: (value) => value },
  PUBLIC_CONTENT_PROFILE: { static: true, schema: (value) => {
    if (value === undefined || value === "default") return "default";
    if (value === "production-template") return value;
    throw new Error("PUBLIC_CONTENT_PROFILE must be default or production-template");
  } },
});
