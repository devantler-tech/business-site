import type { Handle } from "@sveltejs/kit/hooks";

export const handle: Handle = async ({ event, resolve }) =>
  resolve(event, {
    transformPageChunk: ({ html }) => html.replace("%locale%", event.url.pathname.startsWith("/da/") ? "da" : "en"),
  });
