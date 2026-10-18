import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import feed from "virtual:journal-feed";

export const prerender = true;

/** Only the captured feed dependency closure becomes static image routes. */
export function entries() {
  return FEATURE_SVELTE_FRONTEND && feed ? feed.assets.map(asset => ({image:asset.filename.slice("_astro/".length)})) : [];
}

export function GET({params}:{params:{image:string}}) {
  if (!FEATURE_SVELTE_FRONTEND || !feed) error(404);
  const asset = feed.assets.find(item => item.filename === `_astro/${params.image}`);
  if (!asset) error(404);
  const type = asset.filename.endsWith(".webp") ? "image/webp" : asset.filename.endsWith(".png") ? "image/png" : "image/jpeg";
  return new Response(new Uint8Array(Buffer.from(asset.source,"base64")),{headers:{"Content-Type":type}});
}
