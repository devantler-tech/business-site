export const portfolioText = {
  da: {
  pageIntro: 'Udvalgt arbejde — fra en offentlig hjemmeside til værktøjer, der løser konkrete problemer.',
  sourceAvailable: 'Kildekode tilgængelig — se projektets licens.',
  reuse: 'Hvert projekt har sin egen licens. Se projektets vilkår før genbrug.',
  work: 'Udvalgt arbejde', intro: 'En offentlig hjemmeside, du kan besøge og udforske.',
  tools: 'Værktøjer med offentlig kildekode', toolsIntro: 'Seks udvalgte, vedligeholdte værktøjer. Rækkefølgen følger antallet af GitHub-stjerner, ikke en vurdering af kvalitet.',
  checked: 'Rangering kontrolleret', wider: 'Udforsk resten af mit offentlige arbejde', project: 'Udforsk projektet',
},
  en: {
  pageIntro: 'Selected work — from a public website to tools that solve practical problems.',
  sourceAvailable: 'Source-available — see the project’s licence.',
  reuse: 'Each project has its own licence. Check its terms before reuse.',
  work: 'Selected work', intro: 'A public website you can visit and explore.',
  tools: 'Tools with public source', toolsIntro: 'Six selected, maintained tools. Their order follows GitHub stars, not a judgement of quality.',
  checked: 'Ranking checked', wider: 'Explore the rest of my public work', project: 'Explore the project',
},
} as const;

export const ksailStories = {
  da: {
  lead: 'Fra den første opsætning til den daglige drift.',
  steps: [
    { title: 'Opret', body: 'Skab et ensartet fundament til dine applikationer.' },
    { title: 'Udgiv', body: 'Flyt ændringer i applikationerne ud i miljøet.' },
    { title: 'Driv', body: 'Se dine applikationer og administrer miljøet.' },
  ],
  detail: 'Se desktopprogrammet', newTab: 'åbner i en ny fane',
  privacy: 'Klyngeoversigt med private oplysninger skjult.',
},
  en: {
  lead: 'From first setup to everyday operation.',
  steps: [
    { title: 'Set up', body: 'Create a consistent foundation for your applications.' },
    { title: 'Deploy', body: 'Move application changes into the environment.' },
    { title: 'Operate', body: 'Inspect your applications and manage the environment.' },
  ],
  detail: 'View the desktop interface', newTab: 'opens in a new tab',
  privacy: 'Cluster overview with private details masked.',
},
} as const;

export const legacyAnchors: Record<string, string[]> = {
  ksail: ['️-ksail---'], khoist: ['data-product-controller', '-data-product-controller--'],
  'world-at-ruin': ['️-world-at-ruin--'], actions: ['-reusable-workflows-', '-actions-'],
  'agent-skills': ['-agent-skills--'], 'agent-plugins': ['-agent-plugins---'],
  'provider-upjet-unifi': ['-unifi-provider---'], 'kyverno-policies': ['️-kyverno-policies--'],
};
