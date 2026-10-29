import { error } from "@sveltejs/kit";
import feed from "virtual:journal-feed";

export const prerender = true;

export function GET() {
  if (!feed) error(404);
  return new Response(feed.xml, {headers:{"Content-Type":"application/rss+xml; charset=utf-8"}});
}
