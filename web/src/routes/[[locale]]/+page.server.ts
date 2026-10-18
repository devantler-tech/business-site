import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import { business } from "#lib/business.server.ts";
import { professionalWork } from "../../../../src/components/business/professional-work";

export const load = ({ params }: { params: { locale?: string } }) => {
  if (!FEATURE_SVELTE_FRONTEND || (params.locale !== undefined && params.locale !== "da")) error(404);
  const locale: "en" | "da" = params.locale === "da" ? "da" : "en";
  const t = business.copy[locale];
  const c = business.client[locale];
  return {
    locale,
    title: t.title.replace(/ \| Devantler Tech$/, ""),
    description: t.description,
    home: {
      eyebrow: t.eyebrow, headline: t.headline, intro: t.intro, primary: t.primary, secondary: t.secondary,
      availability: t.availability, developerRole: t.developerRole, developerNote: t.developerNote, profile: t.profile,
      promise: t.promise, servicesTitle: t.servicesTitle, servicesIntro: t.servicesIntro,
      starting: t.starting, monthly: t.monthly, perMonth: t.perMonth, currency: t.currency,
      offers: business.offers.map((offer, i) => ({ ...offer, ...t.offerCopy[i] })),
      vatMarker: business.vatMarker(locale),
      priceNote: t.priceNote, hostingTitle: t.hostingTitle, hostingText: t.hostingText, fitTitle: t.fitTitle, fitText: t.fitText,
      qualityEyebrow: c.qualityEyebrow, qualityTitle: c.qualityTitle, qualityIntro: c.qualityIntro,
      quality: c.quality, qualityScope: c.qualityScope,
      work: professionalWork[locale], workPreviewAlt: t.workPreviewAlt, workPreviewAction: t.workPreviewAction,
      workVisitAction: t.workVisitAction, portfolioBridge: c.portfolioBridge, portfolioLink: t.portfolioLink,
      processTitle: t.processTitle, steps: t.steps, contact: t.contact, contactTitle: t.contactTitle,
      contactText: t.contactText, contactAction: t.contactAction, contactNote: t.contactNote, contactAlternative: t.contactAlternative,
    },
    inquiryUrl: business.inquiryUrl,
    companyEnabled: business.companyEnabled,
  };
};
