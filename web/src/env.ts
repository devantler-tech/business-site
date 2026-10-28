import { defineEnvVars } from "@sveltejs/kit/env";
import { parseBooleanRelease } from "./public-routes.ts";

export const variables = defineEnvVars({
  FEATURE_COMPANY_IDENTITY: { static: true, schema: parseBooleanRelease },
  FEATURE_OFFER_COPY: { static: true, schema: parseBooleanRelease },
  COMPANY_POSTAL_ADDRESS: { static: true, schema: (value) => value },
  PUBLIC_CONTENT_PROFILE: { static: true, schema: (value) => {
    if (value === undefined || value === "default") return "default";
    if (value === "production-template") return value;
    throw new Error("PUBLIC_CONTENT_PROFILE must be default or production-template");
  } },
});
