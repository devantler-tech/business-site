import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";

export const load = () => {
  if (!FEATURE_SVELTE_FRONTEND) error(404);
  return {};
};
