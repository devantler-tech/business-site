// Confirmed public invitation only (#149), not server-side order authorization.
// Outside-EU obligations and portal enforcement remain open under #147 and #146.
export const customerScope = {
  en: {
    title: 'Who I work with',
    audience: 'Private customers in Denmark and the rest of the EU, and businesses in Denmark.',
    boundary: 'I cannot currently take on projects for businesses in other EU countries under the PMV setup. Work outside the EU is not open while its requirements are being confirmed.',
  },
  da: {
    title: 'Hvem jeg samarbejder med',
    audience: 'Privatkunder i Danmark og resten af EU samt virksomheder i Danmark.',
    boundary: 'Under PMV-ordningen kan jeg foreløbig ikke påtage mig projekter for virksomheder i andre EU-lande. Opgaver uden for EU er ikke åbne, mens kravene afklares.',
  },
} as const;

// Only selected by an explicitly enabled release preview or activation.
export const customerScopeCopy = {
  en: {
    title: 'Websites & useful software | Devantler Tech',
    description: 'Websites, web apps and services for private customers in the EU and businesses in Denmark. Agreed prices and step-by-step development from Devantler Tech.',
    intro: 'Devantler Tech is my independent software business on Funen, Denmark. I build websites, web apps and services for private customers and Danish businesses—and I’m the person you’ll talk to about your project.',
    contactText: 'Send me a few lines about your project and what you’d like to make easier. A rough budget and timing help, but you don’t need to arrive with a technical specification.',
    footer: 'Websites and software for private customers in the EU and businesses in Denmark.',
    websiteDescription: 'Present what you do with a clear, professional website.',
    firstStep: 'Share what you want to achieve, the problem you want solved, your budget and your preferred timing.',
    principle: 'You get a chance to try the result and a practical handover. The technical choices serve what you need to get done.',
  },
  da: {
    title: 'Hjemmesider og nyttig software | Devantler Tech',
    description: 'Hjemmesider, webapps og services til privatkunder i EU og virksomheder i Danmark. Aftalte priser og udvikling i etaper hos Devantler Tech.',
    intro: 'Devantler Tech er min uafhængige softwarevirksomhed på Fyn. Jeg bygger hjemmesider, webapps og services til privatkunder og danske virksomheder—og det er mig, du taler med om dit projekt.',
    contactText: 'Send mig et par linjer om dit projekt og det, du gerne vil gøre lettere. Et omtrentligt budget og en tidsplan hjælper, men du behøver ikke en teknisk specifikation.',
    footer: 'Hjemmesider og software til privatkunder i EU og virksomheder i Danmark.',
    websiteDescription: 'Præsenter det, du laver, med en klar og professionel hjemmeside.',
    firstStep: 'Fortæl, hvad du vil opnå, hvilket problem du vil løse, dit budget og din ønskede tidsplan.',
    principle: 'Du får mulighed for at afprøve resultatet og en praktisk overdragelse. De tekniske valg skal understøtte det, du har brug for at få gjort.',
  },
} as const;
