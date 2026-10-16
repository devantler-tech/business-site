import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import reading from "virtual:journal-reading";

export const load = () => {
  if (!FEATURE_SVELTE_FRONTEND || !reading) error(404);
  return reading;
};
