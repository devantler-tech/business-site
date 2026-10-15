export const handle = async ({ event, resolve }) =>
  resolve(event, {
    transformPageChunk: ({ html }) => html.replace("%lang%", event.url.pathname.startsWith("/da") ? "da" : "en"),
  });
