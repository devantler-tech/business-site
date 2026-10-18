import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import assets from "virtual:public-discovery";

/** Serve only the complete, input-bound app-owned asset inventory. */
export function discoveryResponse(filename:string) {
  const asset = FEATURE_SVELTE_FRONTEND && assets?.find(item => item.filename === filename);
  if (!asset) error(404);
  const type = filename.endsWith(".pdf") ? "application/pdf" : filename.endsWith(".png") ? "image/png" : "text/plain; charset=utf-8";
  return new Response(new Uint8Array(Buffer.from(asset.source,"base64")),{headers:{"Content-Type":type}});
}
