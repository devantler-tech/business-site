import { legalPageLoad } from "#lib/legal.server.ts";

export const load = ({ params }: { params: { locale?: string } }) => legalPageLoad("terms", params.locale);
