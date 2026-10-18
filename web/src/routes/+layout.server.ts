import { businessFrame } from "#lib/business.server.ts";

export const load = ({ params }: { params: { locale?: string } }) => ({
  businessFrame: businessFrame(params.locale === "da" ? "da" : "en"),
});
