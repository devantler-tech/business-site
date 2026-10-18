import type { Locale } from './business-copy';

// The published disclosure and the migration candidate share one translation source.
export const companyCopy = {
  da: {
    title: 'Virksomhed og kontakt', description: 'Registrering og kontaktoplysninger for Devantler Tech.',
    intro: 'Devantler Tech er en personligt ejet mindre virksomhed drevet af Nikolai Emil Damm.',
    name: 'Virksomhed', registration: 'Virksomhedsform', type: 'Personligt ejet mindre virksomhed (PMV)',
    owner: 'Indehaver', address: 'Registreret virksomhedsadresse', email: 'E-mail',
    contact: 'Har du et projekt i tankerne? Skriv til mig, så finder vi sammen det næste skridt.',
  },
  en: {
    title: 'Company information', description: 'Registration and contact information for Devantler Tech.',
    intro: 'Devantler Tech is a personally owned small business run by Nikolai Emil Damm.',
    name: 'Business', registration: 'Business type', type: 'Personally owned small business (PMV)',
    owner: 'Owner', address: 'Registered business address', email: 'Email',
    contact: 'Have a project in mind? Email me and we can work out the next step together.',
  },
} satisfies Record<Locale, Record<string, string>>;
