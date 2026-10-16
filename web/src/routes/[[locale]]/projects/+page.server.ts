import { error } from "@sveltejs/kit";
import { FEATURE_SVELTE_FRONTEND } from "$app/env/private";
import { createBusinessCopy } from "../../../../../src/components/business/business-copy";
import { createClientCopy } from "../../../../../src/components/business/client-copy";
import { clientProducts } from "../../../../../src/components/business/client-products";
import { previewSupporting } from "../../../../../src/components/business/supporting-copy";
import { publishedOffer } from "../../../../../src/components/business/published-offer";
import { professionalWork } from "../../../../../src/components/business/professional-work";
import { portfolioText, ksailStories, legacyAnchors } from "../../../../../src/components/business/portfolio-content";
import { publicStarSnapshot, publicProducts, selectPublicTools } from "../../../../../src/data/public-products";
import catalogue from "../../../../../src/data/public-products.json";

export const load = ({ params }: { params: { locale?: string } }) => {
  if (!FEATURE_SVELTE_FRONTEND || (params.locale !== undefined && params.locale !== "da")) error(404);
  const locale: "en" | "da" = params.locale === "da" ? "da" : "en";
  const t = createClientCopy(false)[locale];
  const s = { ...previewSupporting[locale], ...publishedOffer.supporting[locale] };
  for (const product of publicProducts) {
    if (!clientProducts[product.repository]?.[locale]) throw new Error(`Missing business description for ${product.repository} (${locale})`);
  }
  const products = selectPublicTools(catalogue, publicStarSnapshot);
  return {
    locale, title: s.projectsTitle, description: portfolioText[locale].pageIntro,
    products: products.map(product => ({
      repository: product.repository, name: product.name, stars: product.stars,
      description: clientProducts[product.repository][locale], terms: product.terms?.[locale],
      anchors: legacyAnchors[product.repository] ?? [],
    })),
    hiddenAnchors: publicProducts.filter(product => !products.some(item => item.repository === product.repository))
      .flatMap(product => legacyAnchors[product.repository] ?? []),
    checkedAt: publicStarSnapshot.fetchedAt!,
    text: portfolioText[locale], ksail: ksailStories[locale],
    projectIndex: s.projectIndex, researchTitle: s.researchTitle, thesis: s.thesis, contact: s.contact,
    labels: {
      productVisit: t.productVisit, journal: t.journal, platformTitle: t.platformTitle, platformText: t.platformText,
      qualityBridge: t.qualityBridge, qualityAction: t.qualityAction, researchLabel: t.researchLabel,
      researchText: t.researchText, details: t.details, researchBackground: t.researchBackground,
      researchBackgroundText: t.researchBackgroundText, researchDiagram: t.researchDiagram,
    },
    work: professionalWork[locale],
    workPreviewAlt: createBusinessCopy(false, false, true)[locale].workPreviewAlt,
    workPreviewAction: createBusinessCopy(false, false, true)[locale].workPreviewAction,
    workVisitAction: createBusinessCopy(false, false, true)[locale].workVisitAction,
  };
};
