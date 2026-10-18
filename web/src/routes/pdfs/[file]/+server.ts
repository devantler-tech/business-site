import { discoveryResponse } from "#lib/public-discovery.server.ts";

export const prerender = true;
export const GET = ({params}:{params:{file:string}}) => discoveryResponse(`pdfs/${params.file}`);
