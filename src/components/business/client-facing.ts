import { FEATURE_OFFER_COPY } from 'astro:env/server';
import { createClientCopy } from './client-copy';
import type { Locale } from './content';

// Business descriptions complement the technical catalogue without changing
// its repository set, ranking, licence dispositions or technical routes.
export const clientProducts: Record<string, Record<Locale, string>> = {
  ksail: { en: 'One tool for setting up and running software environments, with an optional AI assistant.', da: 'Ét værktøj til at oprette og drive softwaremiljøer med en valgfri AI-assistent.' },
  'world-at-ruin': { en: 'An online fantasy game in early development. A creative project, not a finished game; reuse and hosting are restricted by its licence.', da: 'Et online fantasyspil i tidlig udvikling. Et kreativt projekt, ikke et færdigt spil; licensen begrænser genbrug og hosting.' },
  'data-product-controller': { en: 'Tools for describing, combining and publishing reusable data products.', da: 'Værktøjer til at beskrive, kombinere og udgive genbrugelige dataprodukter.' },
  'agent-plugins': { en: 'Installable bundles that give AI coding assistants a consistent way of working.', da: 'Pakker, der giver AI-kodeassistenter en ensartet måde at arbejde på.' },
  'agent-skills': { en: 'Focused instructions that help AI assistants carry out development tasks with care.', da: 'Afgrænsede instruktioner, der hjælper AI-assistenter med at løse udviklingsopgaver med omtanke.' },
  'provider-upjet-unifi': { en: 'A reusable connection for managing supported UniFi network equipment through an infrastructure platform.', da: 'En genbrugelig forbindelse til at administrere understøttet UniFi-netværksudstyr via en infrastrukturplatform.' },
  '.github': { en: 'Shared automation for building, checking and releasing software across the portfolio.', da: 'Fælles automatisering til at bygge, kontrollere og udgive software på tværs af porteføljen.' },
  actions: { en: 'The earlier automation collection, retained for existing users. New projects use the current .github collection.', da: 'Den tidligere samling af automatisering, bevaret for eksisterende brugere. Nye projekter bruger den aktuelle .github-samling.' },
  'kyverno-policies': { en: 'Tested rules that help keep application hosting consistent and controlled.', da: 'Testede regler, der hjælper med at holde applikationshosting ensartet og kontrolleret.' },
  'go-template': { en: 'A prepared starting point for a new software project built in Go.', da: 'Et forberedt udgangspunkt for et nyt softwareprojekt bygget i Go.' },
  'dotnet-template': { en: 'A prepared starting point for .NET software, with conventions and automatic checks.', da: 'Et forberedt udgangspunkt for .NET-software med fælles rammer og automatiske kontroller.' },
  'platform-template': { en: 'A reusable starting point for building an application-hosting platform—not a copy of a live installation.', da: 'En genbrugelig startpakke til en platform for applikationshosting, ikke en kopi af en aktiv installation.' },
  'platform-tenant-template': { en: 'A reusable starting point for putting an application on a hosting platform.', da: 'Et genbrugeligt udgangspunkt for at lægge en applikation på en hostingplatform.' },
};

export const clientCopy = createClientCopy(FEATURE_OFFER_COPY);
