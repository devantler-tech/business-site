import { loadGuide } from "#lib/guide-loader.server.ts";
export const load = ({params}:{params:{guide?:string}}) => loadGuide(`/templates/${params.guide ? `${params.guide}/` : ""}`);
