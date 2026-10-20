import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import { business } from "#lib/business.server.ts";

export const load = ({ params }: { params: { locale?: string } }) => {
  if (!FEATURE_SVELTE_FRONTEND || (params.locale !== undefined && params.locale !== "da")) error(404);
  const locale: "en" | "da" = params.locale === "da" ? "da" : "en";
  const t = business.supporting[locale];
  return {
    locale,
    about: {
      title: t.aboutTitle,
      description: t.aboutDescription,
      intro: t.aboutIntro,
      founderRole: t.founderRole,
      founderText: t.founderText,
      founderNote: t.founderNote,
      approachTitle: t.approachTitle,
      principles: t.principles,
      backgroundTitle: t.backgroundTitle,
      backgroundText: t.backgroundText,
      cv: t.cv,
      contact: t.contact,
    },
  };
};
