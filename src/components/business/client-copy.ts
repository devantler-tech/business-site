import { publishedOffer } from './published-offer.ts';

/**
 * Build shared bilingual quality/portfolio copy without framework configuration.
 * Use published offer wording unless the caller explicitly enables its revision.
 */
export function createClientCopy(revisedOffer: boolean) {
const previewClientCopy = {
  en: {
    qualityEyebrow: 'Care built in', qualityTitle: 'Useful software. Built with care.',
    qualityIntro: 'Every project deserves careful attention to quality and security.',
    quality: [
      { title: 'Quality you can try', text: 'Important journeys are tested before delivery. You get to try the result, so we can check that it does the job you agreed to.' },
      { title: 'Security from the start', text: 'Automated checks help spot risky changes and outdated software. Access and data needs are considered as part of the project—not left until the end.' },
      { title: 'A responsible handover', text: 'Changes are reviewed before they ship. You know who built the work, how to use it and what ongoing care has been agreed.' },
    ],
    qualityScope: 'Checks are chosen for each project; they are not a certification or a promise of flawless software. Hosted projects include optimization, accessibility improvements, bug fixes and routine upkeep in the agreed monthly price. New features and larger iterations need a new agreement; capacity, backups and human support terms are specified in your quote.',
    portfolioBridge: 'Websites, web apps, reusable tools and earlier research. Explore the full range of work behind Devantler Tech.',
    projectsIntro: 'Different projects. The same care for the people who use them.',
    familyLabel: '01 / Websites & web apps', catalogueLabel: '02 / The wider portfolio', researchLabel: '03 / Earlier research',
    catalogueTitle: 'Tools that make other work possible.',
    catalogueIntro: 'Alongside websites and web apps, I build tools for developing, checking and running software. Here is the complete public collection, including work still in development.',
    reuse: 'Availability does not grant unrestricted reuse. Each project has its own licence; the labels below identify special restrictions. No paid product subscription is available.',
    qualityBridge: 'How the work is built', qualityAction: 'Quality & security on Home',
    platformTitle: 'The hosting behind my own apps',
    platformText: 'A separate project for running and maintaining my own applications. It is not a hosting guarantee; client capacity, support and running costs are agreed individually.',
    researchText: 'My master’s research looked at how organisations could share useful data while keeping responsibility for their own part. It is background research, not a current service.',
    details: 'Research background & diagram (English)',
    researchBackground: 'Data products & data spaces',
    researchBackgroundText: 'The work explored independent data products working together in a data space, with a prototype built in .NET. The thesis sets out the investigation and its trade-offs.',
    researchDiagram: 'How independent data products can fit together',
    productVisit: 'Explore KSail', journal: 'Stories behind the work (English)',
  },
  da: {
    qualityEyebrow: 'Omtanke fra starten', qualityTitle: 'Nyttig software. Bygget med omtanke.',
    qualityIntro: 'Hvert projekt fortjener omtanke for kvalitet og sikkerhed.',
    quality: [
      { title: 'Kvalitet, du kan afprøve', text: 'Vigtige brugerforløb testes før levering. Du får mulighed for at afprøve resultatet, så vi kan kontrollere, at det løser den aftalte opgave.' },
      { title: 'Sikkerhed fra starten', text: 'Automatiske kontroller hjælper med at opdage risikable ændringer og forældet software. Adgang og databehov tænkes ind i projektet—ikke først til sidst.' },
      { title: 'En ansvarlig overdragelse', text: 'Ændringer gennemgås før udgivelse. Du ved, hvem der har bygget løsningen, hvordan den bruges, og hvilken løbende vedligeholdelse der er aftalt.' },
    ],
    qualityScope: 'Kontrollen vælges til hvert projekt; den er ikke en certificering eller et løfte om fejlfri software. Hostede projekter har optimering, forbedret tilgængelighed, fejlrettelser og almindelig vedligeholdelse med i den aftalte månedspris. Nye funktioner og større videreudvikling kræver en ny aftale; kapacitet, backup og menneskelig support aftales i tilbuddet.',
    portfolioBridge: 'Hjemmesider, webapps, genbrugelige værktøjer og tidligere forskning. Se bredden i arbejdet bag Devantler Tech.',
    projectsIntro: 'Forskellige projekter. Samme omtanke for dem, der bruger dem.',
    familyLabel: '01 / Hjemmesider og webapps', catalogueLabel: '02 / Den bredere portefølje', researchLabel: '03 / Tidligere forskning',
    catalogueTitle: 'Værktøjer, der gør andet arbejde muligt.',
    catalogueIntro: 'Ud over hjemmesider og webapps bygger jeg værktøjer til at udvikle, kontrollere og drive software. Her er hele den offentlige samling, også projekter under udvikling.',
    reuse: 'Tilgængelighed giver ikke ubegrænset ret til genbrug. Hvert projekt har sin egen licens; mærkaterne nedenfor viser særlige begrænsninger. Der tilbydes ingen betalte produktabonnementer.',
    qualityBridge: 'Sådan bliver arbejdet bygget', qualityAction: 'Kvalitet og sikkerhed på forsiden',
    platformTitle: 'Hostingen bag mine egne apps',
    platformText: 'Et særskilt projekt til at drive og vedligeholde mine egne applikationer. Det er ikke en hostinggaranti; kapacitet, support og driftsudgifter aftales for hvert kundeprojekt.',
    researchText: 'Mit speciale undersøgte, hvordan organisationer kunne dele nyttige data og stadig have ansvaret for deres egen del. Det er baggrundsforskning, ikke en aktuel ydelse.',
    details: 'Forskningsbaggrund og diagram (engelsk)',
    researchBackground: 'Data products & data spaces',
    researchBackgroundText: 'The work explored independent data products working together in a data space, with a prototype built in .NET. The thesis sets out the investigation and its trade-offs.',
    researchDiagram: 'How independent data products can fit together',
    productVisit: 'Se mere om KSail', journal: 'Historierne bag arbejdet (engelsk)',
  },
} as const;

return revisedOffer ? previewClientCopy : {
  en: { ...previewClientCopy.en, ...publishedOffer.clientCopy.en },
  da: { ...previewClientCopy.da, ...publishedOffer.clientCopy.da },
} as const;
}

