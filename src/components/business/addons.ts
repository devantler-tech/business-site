// The standard add-ons, at fixed VAT-inclusive prices in whole kroner. The
// portal sells exactly these (server/internal/identity/offerings.json `addons`);
// scripts/proposal-offerings.test.mjs fails when the two differ. Anything else
// gets a fixed-price quote. The launch discount never applies to an add-on.
export type AddOnText = { title: string; description: string };
export type AddOn = {
  id: string;
  kind: 'one-time' | 'monthly';
  price: number;
  offerings: readonly string[];
  max?: number;
  tier?: 'priority' | 'express';
  text: { en: AddOnText; da: AddOnText };
};

export const standardAddons: readonly AddOn[] = [
  {
    id: 'priority', kind: 'monthly', price: 400, offerings: ['website', 'app', 'service'], tier: 'priority',
    text: {
      en: { title: 'Priority feature requests', description: 'The target is to pick up your feature requests about once a week instead of in the monthly round.' },
      da: { title: 'Prioriterede ønsker', description: 'Målet er at tage dine ønsker til nye funktioner op cirka en gang om ugen i stedet for i den månedlige runde.' },
    },
  },
  {
    id: 'express', kind: 'monthly', price: 1000, offerings: ['website', 'app', 'service'], tier: 'express',
    text: {
      en: { title: 'Express feature requests', description: 'The target is to start work on your feature request within 1 business day, with replies within 1 business day.' },
      da: { title: 'Ekspres-ønsker', description: 'Målet er, at arbejdet på dit ønske begynder inden for 1 arbejdsdag, og at du får svar inden for 1 arbejdsdag.' },
    },
  },
  {
    id: 'statistics', kind: 'monthly', price: 75, offerings: ['website', 'app'],
    text: {
      en: { title: 'Visitor statistics', description: 'Privacy-friendly visitor statistics without cookies, on the Devantler Tech platform.' },
      da: { title: 'Besøgsstatistik', description: 'Privatlivsvenlig besøgsstatistik uden cookies på Devantler Tech-platformen.' },
    },
  },
  {
    id: 'email', kind: 'monthly', price: 100, offerings: ['website', 'app', 'service'],
    text: {
      en: { title: 'Email sending', description: "Your project sends email, such as receipts and notifications, through the platform's mail sender." },
      da: { title: 'Afsendelse af e-mail', description: 'Dit projekt sender e-mail, fx kvitteringer og beskeder, gennem platformens afsender.' },
    },
  },
  {
    id: 'language', kind: 'one-time', price: 1500, offerings: ['website', 'app'], max: 5,
    text: {
      en: { title: 'Extra language', description: 'Your project in one more language, per language. You supply the translated text.' },
      da: { title: 'Ekstra sprog', description: 'Dit projekt på ét sprog mere, pr. sprog. Du leverer den oversatte tekst.' },
    },
  },
  {
    id: 'contact-form', kind: 'one-time', price: 1500, offerings: ['website', 'app'],
    text: {
      en: { title: 'Contact form', description: "A form that sends your visitors' messages to your email." },
      da: { title: 'Kontaktformular', description: 'En formular, der sender dine besøgendes beskeder til din e-mail.' },
    },
  },
  {
    id: 'payments', kind: 'one-time', price: 3000, offerings: ['website', 'app'],
    text: {
      en: { title: 'Online payments', description: "Card and MobilePay payments through Stripe. Stripe's own fees come on top." },
      da: { title: 'Onlinebetaling', description: 'Betaling med kort og MobilePay via Stripe. Stripes egne gebyrer kommer oveni.' },
    },
  },
  {
    id: 'news', kind: 'one-time', price: 2500, offerings: ['website'],
    text: {
      en: { title: 'News section', description: 'A news section you can update yourself.' },
      da: { title: 'Nyhedssektion', description: 'En nyhedssektion, du selv kan opdatere.' },
    },
  },
  {
    id: 'accounts', kind: 'one-time', price: 4000, offerings: ['app'],
    text: {
      en: { title: 'User accounts and sign-in', description: 'Your users sign in to your app with their own accounts.' },
      da: { title: 'Brugerkonti og login', description: 'Dine brugere logger ind i din app med deres egne konti.' },
    },
  },
  {
    id: 'migration', kind: 'one-time', price: 1500, offerings: ['website'],
    text: {
      en: { title: 'Content move', description: 'The content of your old site is moved to the new one.' },
      da: { title: 'Flytning af indhold', description: 'Indholdet fra din gamle side flyttes til den nye.' },
    },
  },
  {
    id: 'seo', kind: 'one-time', price: 1000, offerings: ['website', 'app'],
    text: {
      en: { title: 'Search engine setup', description: 'Your project is registered with Google and Bing, with a sitemap and basic search settings.' },
      da: { title: 'Opsætning til søgemaskiner', description: 'Dit projekt registreres hos Google og Bing med sitemap og grundlæggende indstillinger til søgning.' },
    },
  },
];

export const addonsCopy = {
  en: {
    title: 'Add-ons',
    intro: 'Standard extras at fixed prices, chosen when you order. Monthly add-ons need hosting, and the launch discount does not apply to add-ons. Priority and Express follow the fair-use rule in the terms. Anything else gets a fixed-price quote.',
    perMonth: 'a month',
    each: 'each',
  },
  da: {
    title: 'Tilvalg',
    intro: 'Faste tilvalg til faste priser, som du vælger, når du bestiller. Månedlige tilvalg kræver hosting, og introduktionsrabatten gælder ikke tilvalg. Prioritet og Ekspres følger reglen om rimelig brug i betingelserne. Alt andet får et fastpristilbud.',
    perMonth: 'om måneden',
    each: 'pr. stk.',
  },
} as const;
