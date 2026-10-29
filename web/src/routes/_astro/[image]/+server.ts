import { error } from "@sveltejs/kit";
import feed from "virtual:journal-feed";
import discovery from "virtual:public-discovery";

export const prerender = true;

/** Only the captured feed dependency closure becomes static image routes. */
export function entries() {
  return feed ? [...new Set([...feed.assets,...(discovery ?? [])].filter(asset => asset.filename.startsWith("_astro/")).map(asset => asset.filename.slice("_astro/".length)))].map(image => ({image})) : [];
}

export function GET({params}:{params:{image:string}}) {
  if (!feed) error(404);
  const asset = [...feed.assets,...(discovery ?? [])].find(item => item.filename === `_astro/${params.image}`);
  if (!asset) error(404);
  const type = asset.filename.endsWith(".webp") ? "image/webp" : asset.filename.endsWith(".png") ? "image/png" : "image/jpeg";
  return new Response(new Uint8Array(Buffer.from(asset.source,"base64")),{headers:{"Content-Type":type}});
}
