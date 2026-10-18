import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import guides from "virtual:technical-guides";

export function loadGuide(path:string) {
  if (!FEATURE_SVELTE_FRONTEND) error(404);
  const reading = guides?.find(guide => guide.path === path);
  if (!reading) error(404);
  return {reading,guides:(guides ?? []).map(({path,title}) => ({path,title}))};
}
