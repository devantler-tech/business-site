import { discoveryResponse } from "#lib/public-discovery.server.ts";

export const prerender = true;
export const GET = () => discoveryResponse("robots.txt");
