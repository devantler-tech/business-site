import type { Locale } from './business-copy';
import company from '../../data/company.json' with { type: 'json' };

// The published disclosure and the migration candidate share one translation source.
export const companyCopy = {
  da: {
    title: 'Virksomhed og kontakt', description: 'Registrering og kontaktoplysninger for Devantler Tech.',
    intro: 'Devantler Tech er en enkeltmandsvirksomhed drevet af Nikolai Emil Damm.',
    name: 'Virksomhed', registration: 'Virksomhedsform', type: 'Enkeltmandsvirksomhed',
    owner: 'Indehaver', address: 'Registreret virksomhedsadresse', email: 'E-mail',
    contact: 'Har du et projekt i tankerne? Skriv til mig, så finder vi sammen det næste skridt.',
  },
  en: {
    title: 'Company information', description: 'Registration and contact information for Devantler Tech.',
    intro: 'Devantler Tech is a sole proprietorship run by Nikolai Emil Damm.',
    name: 'Business', registration: 'Business type', type: 'Sole proprietorship',
    owner: 'Owner', address: 'Registered business address', email: 'Email',
    contact: 'Have a project in mind? Email me and we can work out the next step together.',
  },
} satisfies Record<Locale, Record<string, string>>;

export const companyCopyFor = () => companyCopy;
export const companyFacts = () => company;
