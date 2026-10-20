import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import feed from "virtual:journal-feed";

export const prerender = true;

export function GET() {
  if (!FEATURE_SVELTE_FRONTEND || !feed) error(404);
  return new Response(feed.xml, {headers:{"Content-Type":"application/rss+xml; charset=utf-8"}});
}
