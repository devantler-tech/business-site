import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import { previewSupporting } from "../../../../../src/components/business/supporting-copy";
import { publishedOffer } from "../../../../../src/components/business/published-offer";

export const load = ({ params }: { params: { locale?: string } }) => {
  if (!FEATURE_SVELTE_FRONTEND || (params.locale !== undefined && params.locale !== "da")) error(404);
  const locale: "en" | "da" = params.locale === "da" ? "da" : "en";
  const t = { ...previewSupporting[locale], ...publishedOffer.supporting[locale] };
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
