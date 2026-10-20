import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
export const load = ({ params }: { params: { legacy: string } }) => {
  if (!FEATURE_SVELTE_FRONTEND || (params.legacy !== "active" && params.legacy !== "completed")) error(404);
  return { fallback: params.legacy === "active" ? "#open-title" : "#research" };
};
