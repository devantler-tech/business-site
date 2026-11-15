import type { Locale } from './business-copy';
import company from '../../data/company.json' with { type: 'json' };

// Privacy notice and customer terms (#19). Plain statements of what the business
// does and of the statutory rights, not a compliance claim. Keep the street
// address out: these pages link to the company-information page instead, and
// each proposal carries the complete withdrawal form with the address filled in.

/** A paragraph, a bullet list, or a list of links. */
export type LegalBlock = string | { list: readonly string[] } | { links: readonly { href: string; label: string }[] };
export type LegalSection = { title: string; blocks: readonly LegalBlock[] };
export type LegalPage = { title: string; description: string; intro: string; updated: string; sections: readonly LegalSection[] };

const updated = { en: 'Last updated 15 November 2026.', da: 'Senest opdateret 15. november 2026.' } as const;
const companyPath = { en: '/company/', da: '/da/company/' } as const;

/** The trader line of the terms; the phone number appears once company.json has one. */
export function traderLine(locale: Locale, facts: { cvr: string; owner: string; email: string; phone?: string }): string {
  const phone = facts.phone?.trim();
  return locale === 'da'
    ? `Devantler Tech, CVR ${facts.cvr}, en enkeltmandsvirksomhed drevet af ${facts.owner}. E-mail: ${facts.email}.${phone ? ` Telefon: ${phone}.` : ''}`
    : `Devantler Tech, CVR ${facts.cvr}, a sole proprietorship run by ${facts.owner}. Email: ${facts.email}.${phone ? ` Phone: ${phone}.` : ''}`;
}

// Portal-only text is unreleased content: it renders only with FEATURE_PORTAL_PRIVACY (default off).
const privacy = (portal: boolean) => ({
  en: {
    title: 'Privacy and cookies',
    description: 'How Devantler Tech handles personal data, what this website stores on your device, and your rights.',
    intro: `This page explains which personal data Devantler Tech handles, why, for how long, and what your rights are. It covers this website, email enquiries${portal ? ', customer agreements and the customer portal' : ' and customer agreements'}.`,
    updated: updated.en,
    sections: [
      { title: 'Who is responsible', blocks: [
        `Devantler Tech (CVR ${company.cvr}), the sole proprietorship of ${company.owner}, is responsible for your personal data. Write to ${company.email} about anything on this page.`,
        { links: [{ href: companyPath.en, label: 'Company information and postal address' }] },
      ] },
      { title: 'Cookies and storage on your device', blocks: [
        'This website sets no cookies, uses no analytics or advertising tracking and loads no content from other websites.',
        'If you choose an appearance (System, Light or Dark), your browser saves that choice on your device so the next page uses it. The choice is never sent to me. You can remove it by clearing this site’s data in your browser.',
        ...(portal ? ['The customer portal and its sign-in pages use cookies only to keep you signed in and to protect forms against forgery. The portal’s session cookie ends when you close your browser and stops working after 30 minutes; its form-protection cookie expires after 30 minutes. The sign-in service at accounts.devantler.tech sets a browser identifier that ties your sign-in sessions to this browser, kept for one year, and a form-protection cookie that expires after 12 hours. None of these cookies is used for anything else.'] : []),
      ] },
      { title: 'Visiting the website', blocks: [
        'Like every website, the server that delivers a page receives your IP address, browser details and the address of the page you ask for. This is used only to deliver the website and keep it secure, not to track you. The legal basis is my legitimate interest in running a secure website (GDPR Article 6(1)(f)).',
        'The website runs on my own platform on Hetzner servers in the EU and reaches you through Cloudflare’s network. These technical logs are kept only as long as they are needed for operation and security.',
      ] },
      { title: 'When you email me', blocks: [
        'I receive your name, email address, what you write and anything you attach. I use it to answer you and, if you wish, to prepare a proposal.',
        'The legal basis is that the processing is needed for steps you ask for before an agreement (GDPR Article 6(1)(b)). If you write on behalf of a business, it is my legitimate interest in answering business enquiries (Article 6(1)(f)).',
        'If no agreement follows, I delete the correspondence once six months have passed since our last contact. I check for such mail every month. If we make an agreement, the correspondence becomes part of the customer records described below.',
        'Please do not send passwords or sensitive personal data, such as health information, by email.',
      ] },
      { title: 'Customers', blocks: [
        'About customers I keep name and contact details, company details if you buy as a business, the agreement, invoices and payments. I use them to deliver the agreed work and hosting, to invoice and to keep accounts.',
        'The legal basis is our agreement (GDPR Article 6(1)(b)) and my legal duties to keep accounts and report VAT (Article 6(1)(c)).',
        'Accounting records, including invoices, are kept for five years from the end of the financial year they belong to, as the Danish Bookkeeping Act requires. Other customer correspondence is kept while the agreement runs and afterwards only as long as it is needed to handle any claims.',
        'When I host a project that holds personal data about other people, I process that data on your behalf under a data processing agreement (GDPR Article 28) that is part of our agreement.',
      ] },
      ...(portal ? [{ title: 'The customer portal', blocks: [
        'Customers can be invited to the customer portal at portal.devantler.tech to send project requests, accept proposals, place orders and follow payments. Accounts are created only by invitation.',
        'For a portal account I keep your name and email address, your sign-in details, the contact details you save, and your project requests, proposals, acceptances, orders and payment status. Your password is stored only in hashed form, and password sign-in requires two-step verification. The sign-in service logs each sign-in.',
        'Sign-in runs on my own services, ZITADEL at accounts.devantler.tech and Dex at login.devantler.tech, on the same EU platform as the portal. Emails about sign-in and the portal are sent through Amazon Web Services (Amazon SES) in Frankfurt. When you place an order, the portal sends what billing needs to Numina for an invoice, or to Stripe for card or MobilePay. When you pay by card or MobilePay, Stripe collects your billing address and, for a business, its name and VAT number, to issue your invoice.',
        'You can choose to sign in with GitHub instead. It never creates an account: it links to the account you were invited to, either automatically when your GitHub email address matches the invitation, or after one sign-in with your password and two-step verification. With GitHub sign-in, GitHub’s own login protects your account. ZITADEL receives your GitHub user ID and username, your public GitHub profile and your email addresses. It keeps only the link to your GitHub account (user ID and username), and uses your email address only to find your invited account. GitHub learns that you sign in to Devantler Tech. You can remove the link in your account, or ask me to. If you do not use GitHub sign-in, GitHub receives nothing about you.',
        'The legal basis is our agreement, and the steps you ask for before one (GDPR Article 6(1)(b)). Your account and portal records are kept while you are a customer and afterwards only as long as they are needed to handle any claims; accounting records are kept for five years, as described above. Backups of the portal and sign-in databases are stored with Cloudflare and kept for 30 days.',
      ] }] : []),
      { title: 'Who else receives the data', blocks: [
        'I do not sell personal data or share it for marketing. These providers receive data:',
        { list: [
          'hosting and network providers: Hetzner and Cloudflare;',
          portal ? 'my email provider, and Amazon Web Services for sign-in and portal emails;' : 'my email provider;',
          'Numina, for accounting and invoices;',
          'Stripe, if you pay by card or MobilePay. Your card details go to Stripe and never reach me. Stripe also uses payment data for its own fraud prevention and legal duties' + (portal ? ';' : '.'),
          ...(portal ? ['GitHub, only if you choose to sign in with GitHub.'] : []),
        ] },
        'Public authorities, such as the Danish Tax Agency, receive data when the law requires it.',
        `Some of these providers, including ${portal ? 'Cloudflare, Stripe, Amazon Web Services and GitHub' : 'Cloudflare and Stripe'}, belong to groups based in the USA, so data may be processed there. Such transfers rely on the safeguards in each provider’s data protection terms, such as the EU standard contractual clauses or the EU–US Data Privacy Framework. Write to me if you want details.`,
      ] },
      { title: 'Your rights', blocks: [
        'You can ask to see the personal data I hold about you, and to have it corrected or deleted. You can ask me to limit its use, object to its use, and receive the data you gave me in a structured, commonly used and machine-readable format. Write to me and I will answer within one month. Some data, such as accounting records, I must keep by law.',
        'You do not have to give me personal data, but I need your contact details to answer you and to make an agreement. I make no automated decisions about you and do no profiling.',
        'You can complain to the Danish Data Protection Agency (Datatilsynet).',
        { links: [{ href: 'https://www.datatilsynet.dk/', label: 'Datatilsynet' }] },
      ] },
    ],
  },
  da: {
    title: 'Privatliv og cookies',
    description: 'Sådan behandler Devantler Tech personoplysninger, hvad hjemmesiden gemmer på din enhed, og dine rettigheder.',
    intro: `Her kan du læse, hvilke personoplysninger Devantler Tech behandler, hvorfor, hvor længe, og hvilke rettigheder du har. Det gælder hjemmesiden, henvendelser på e-mail${portal ? ', kundeaftaler og kundeportalen' : ' og kundeaftaler'}.`,
    updated: updated.da,
    sections: [
      { title: 'Hvem er ansvarlig', blocks: [
        `Devantler Tech (CVR ${company.cvr}), ${company.owner}s enkeltmandsvirksomhed, er dataansvarlig for dine personoplysninger. Skriv til ${company.email}, hvis du har spørgsmål til denne side.`,
        { links: [{ href: companyPath.da, label: 'Virksomhedsoplysninger og postadresse' }] },
      ] },
      { title: 'Cookies og lagring på din enhed', blocks: [
        'Hjemmesiden sætter ingen cookies, bruger ingen statistik- eller reklamesporing og henter intet indhold fra andre hjemmesider.',
        'Hvis du vælger et udseende (System, Lys eller Mørk), gemmer din browser valget på din enhed, så næste side bruger det. Valget sendes aldrig til mig. Du kan fjerne det ved at slette hjemmesidens data i din browser.',
        ...(portal ? ['Kundeportalen og dens login-sider bruger kun cookies til at holde dig logget ind og til at beskytte formularer mod forfalskning. Portalens sessionscookie ophører, når du lukker din browser, og holder op med at virke efter 30 minutter; dens formularbeskyttelse udløber efter 30 minutter. Login-tjenesten på accounts.devantler.tech sætter et browser-id, der knytter dine login-sessioner til denne browser og gemmes i et år, og en formularbeskyttelse, der udløber efter 12 timer. Ingen af disse cookies bruges til andet.'] : []),
      ] },
      { title: 'Når du besøger hjemmesiden', blocks: [
        'Som ved alle hjemmesider modtager den server, der leverer siden, din IP-adresse, oplysninger om din browser og adressen på den side, du beder om. Det bruges kun til at levere hjemmesiden og holde den sikker, ikke til at spore dig. Behandlingsgrundlaget er min legitime interesse i at drive en sikker hjemmeside (databeskyttelsesforordningens artikel 6, stk. 1, litra f).',
        'Hjemmesiden kører på min egen platform på Hetzner-servere i EU og når frem til dig gennem Cloudflares netværk. Disse tekniske logs gemmes kun, så længe de er nødvendige for drift og sikkerhed.',
      ] },
      { title: 'Når du skriver til mig', blocks: [
        'Jeg modtager dit navn, din e-mailadresse, det, du skriver, og eventuelle vedhæftede filer. Jeg bruger oplysningerne til at svare dig og, hvis du ønsker det, til at lave et tilbud.',
        'Behandlingsgrundlaget er, at behandlingen er nødvendig for at gennemføre tiltag, du har bedt om, inden en aftale indgås (artikel 6, stk. 1, litra b). Skriver du på vegne af en virksomhed, er grundlaget min legitime interesse i at besvare henvendelser fra virksomheder (artikel 6, stk. 1, litra f).',
        'Hvis der ikke bliver indgået en aftale, sletter jeg korrespondancen, når der er gået seks måneder siden vores seneste kontakt. Jeg gennemgår den slags e-mails hver måned. Indgår vi en aftale, bliver korrespondancen en del af kundeoplysningerne nedenfor.',
        'Send venligst ikke adgangskoder eller følsomme personoplysninger, fx helbredsoplysninger, på e-mail.',
      ] },
      { title: 'Kunder', blocks: [
        'Om kunder gemmer jeg navn og kontaktoplysninger, virksomhedsoplysninger, hvis du køber som virksomhed, aftalen, fakturaer og betalinger. Jeg bruger dem til at levere det aftalte arbejde og den aftalte hosting, til fakturering og til regnskab.',
        'Behandlingsgrundlaget er vores aftale (artikel 6, stk. 1, litra b) og mine retlige forpligtelser til at føre regnskab og indberette moms (artikel 6, stk. 1, litra c).',
        'Regnskabsmateriale, herunder fakturaer, opbevares i fem år fra udgangen af det regnskabsår, det vedrører, som bogføringsloven kræver. Anden korrespondance med kunder gemmes, mens aftalen løber, og derefter kun så længe, det er nødvendigt for at håndtere eventuelle krav.',
        'Når jeg hoster et projekt med personoplysninger om andre, behandler jeg dem på dine vegne efter en databehandleraftale (databeskyttelsesforordningens artikel 28), som er en del af vores aftale.',
      ] },
      ...(portal ? [{ title: 'Kundeportalen', blocks: [
        'Kunder kan blive inviteret til kundeportalen på portal.devantler.tech, hvor de kan sende projektforespørgsler, acceptere tilbud, afgive ordrer og følge betalinger. Konti oprettes kun efter invitation.',
        'Til en portalkonto gemmer jeg dit navn og din e-mailadresse, dine loginoplysninger, de kontaktoplysninger, du gemmer, og dine projektforespørgsler, tilbud, accepter, ordrer og betalingsstatus. Din adgangskode gemmes kun i hashet form, og login med adgangskode kræver totrinsbekræftelse. Login-tjenesten registrerer hvert login.',
        'Login kører på mine egne tjenester, ZITADEL på accounts.devantler.tech og Dex på login.devantler.tech, på samme EU-platform som portalen. E-mails om login og portalen sendes gennem Amazon Web Services (Amazon SES) i Frankfurt. Når du afgiver en ordre, sender portalen det, faktureringen kræver, til Numina ved betaling via faktura eller til Stripe ved betaling med kort eller MobilePay. Betaler du med kort eller MobilePay, indsamler Stripe din faktureringsadresse og, for en virksomhed, dens navn og momsnummer, så Stripe kan udstede din faktura.',
        'Du kan i stedet vælge at logge ind med GitHub. Det opretter aldrig en konto, men kobles til den konto, du er inviteret til: automatisk, når din e-mailadresse hos GitHub svarer til invitationen, eller efter ét login med din adgangskode og totrinsbekræftelse. Ved login med GitHub beskytter GitHubs eget login din konto. ZITADEL modtager dit GitHub-bruger-id og brugernavn, din offentlige GitHub-profil og dine e-mailadresser. Den gemmer kun koblingen til din GitHub-konto (bruger-id og brugernavn) og bruger kun din e-mailadresse til at finde din inviterede konto. GitHub får at vide, at du logger ind hos Devantler Tech. Du kan fjerne koblingen i din konto eller bede mig om det. Bruger du ikke login med GitHub, modtager GitHub intet om dig.',
        'Behandlingsgrundlaget er vores aftale og de tiltag, du beder om, inden en aftale indgås (artikel 6, stk. 1, litra b). Din konto og dine portaloplysninger gemmes, mens du er kunde, og derefter kun så længe, det er nødvendigt for at håndtere eventuelle krav; regnskabsmateriale gemmes i fem år som beskrevet ovenfor. Sikkerhedskopier af portalens og logintjenesternes databaser opbevares hos Cloudflare i 30 dage.',
      ] }] : []),
      { title: 'Hvem modtager ellers oplysningerne', blocks: [
        'Jeg sælger ikke personoplysninger og deler dem ikke til markedsføring. Disse leverandører modtager oplysninger:',
        { list: [
          'hosting- og netværksleverandører: Hetzner og Cloudflare,',
          portal ? 'min e-mailudbyder og Amazon Web Services til e-mails om login og portalen,' : 'min e-mailudbyder,',
          'Numina til regnskab og fakturaer,',
          'Stripe, hvis du betaler med kort eller MobilePay. Dine kortoplysninger går til Stripe og kommer aldrig frem til mig. Stripe bruger også betalingsoplysninger til egen forebyggelse af svindel og egne lovkrav' + (portal ? ',' : '.'),
          ...(portal ? ['GitHub, kun hvis du vælger at logge ind med GitHub.'] : []),
        ] },
        'Offentlige myndigheder, fx Skattestyrelsen, modtager oplysninger, når loven kræver det.',
        `Nogle af leverandørerne, herunder ${portal ? 'Cloudflare, Stripe, Amazon Web Services og GitHub' : 'Cloudflare og Stripe'}, hører til koncerner med hovedsæde i USA, så oplysninger kan blive behandlet dér. Overførslerne bygger på garantierne i hver leverandørs databeskyttelsesvilkår, fx EU’s standardkontraktbestemmelser eller EU-US Data Privacy Framework. Skriv til mig, hvis du vil have flere oplysninger.`,
      ] },
      { title: 'Dine rettigheder', blocks: [
        'Du kan bede om at se de personoplysninger, jeg har om dig, og om at få dem rettet eller slettet. Du kan bede mig begrænse brugen, gøre indsigelse mod brugen og få de oplysninger, du selv har givet mig, i et struktureret, almindeligt anvendt og maskinlæsbart format. Skriv til mig, så svarer jeg inden for en måned. Nogle oplysninger, fx regnskabsmateriale, skal jeg gemme efter loven.',
        'Du har ikke pligt til at give mig personoplysninger, men jeg skal bruge dine kontaktoplysninger for at kunne svare dig og indgå en aftale. Jeg træffer ingen automatiske afgørelser om dig og laver ingen profilering.',
        'Du kan klage til Datatilsynet.',
        { links: [{ href: 'https://www.datatilsynet.dk/', label: 'Datatilsynet' }] },
      ] },
    ],
  },
}) satisfies Record<Locale, LegalPage>;

const terms = {
  en: {
    title: 'Terms for projects and hosting',
    description: 'How agreements with Devantler Tech work: proposals, prices, payment, your right of withdrawal and ending hosting.',
    intro: 'These terms apply when you agree a project or hosting with Devantler Tech. Your written proposal gives the details of your project. Where the proposal and these terms differ, the proposal applies, but no proposal reduces your rights as a private customer.',
    updated: updated.en,
    sections: [
      { title: 'Who you agree with', blocks: [
        traderLine('en', company),
        { links: [{ href: companyPath.en, label: 'Company information and postal address' }] },
      ] },
      { title: 'Proposal and agreement', blocks: [
        'You receive a written proposal by email. It describes what will be built or hosted, the total price, any monthly price, the schedule and the payment terms. These terms and the withdrawal form, with my postal address filled in, come with the proposal.',
        'The agreement is made when you accept the proposal in writing. I then confirm the agreement by email, with these terms attached.',
      ] },
      { title: 'Prices', blocks: [
        'Prices are in Danish kroner. For private customers and Danish businesses they include 25% Danish VAT. The prices on this website are starting prices; your proposal states the total price for your project, any monthly price and the VAT that applies to you.',
        'Domains, external services and usage-based charges are not part of the starting prices. Your proposal states any such costs, or how they are calculated, before you accept.',
      ] },
      { title: 'Payment and delivery', blocks: [
        'Your proposal states when to pay. You pay by invoice, or by card or MobilePay where the proposal offers it. Invoices are issued through Numina; card and MobilePay payments go through Stripe.',
        'Your proposal states the schedule. You can try the result before it goes live.',
      ] },
      { title: 'Your right of withdrawal (private customers)', blocks: [
        'As a private customer, you can withdraw from an agreement made by email or online within 14 days, without giving a reason. The 14 days run from the day the agreement is made. If you receive these terms in writing only later, they run from that day instead. If the last day is a Saturday, Sunday, public holiday, 5 June, 24 December or 31 December, you have until the next working day.',
        `To withdraw, tell me clearly before the deadline, for example by email to ${company.email}. You can use the form below, but you do not have to. It is enough to send your message before the deadline.`,
        'I only start work within the 14 days if you expressly ask me to in writing. If you then withdraw, you pay for the work done until you told me, in proportion to the whole agreement. If you asked me to start early and confirmed that you lose the right of withdrawal once the work is completely finished, the right ends when the work is completely finished.',
        'If you withdraw, I refund what you have paid, minus any payment for work already done, without undue delay and no later than 14 days after I receive your message. I use the same payment method you used, unless you expressly agree to another, and the refund costs you nothing.',
      ] },
      { title: 'Withdrawal form', blocks: [
        'Complete and return this form only if you wish to withdraw from the agreement. The copy that comes with your proposal has my postal address filled in.',
        { list: [
          `To: Devantler Tech, [postal address on the company information page], ${company.email}`,
          'I/We (*) hereby give notice that I/We (*) withdraw from my/our (*) contract for the provision of the following service (*):',
          'Ordered on (*)/received on (*):',
          'Name of consumer(s):',
          'Address of consumer(s):',
          'Signature of consumer(s) (only if this form is notified on paper):',
          'Date:',
        ] },
        '(*) Delete as appropriate.',
      ] },
      { title: 'Hosting and ending it', blocks: [
        'Hosting is paid monthly and continues until it is ended. You can end it by email with one month’s notice to the end of a month, unless your proposal states a minimum period. Your proposal also states the notice I give if I end hosting.',
        'As a private customer you can in any case end hosting with one month’s notice to the end of a month once five months have passed since the agreement. If you have paid for a period after the end date, I refund that amount immediately.',
        'When hosting ends, I hand over your content and code as described in your proposal.',
      ] },
      { title: 'Faults and complaints', blocks: [
        `If something delivered does not match what we agreed, write to ${company.email} within a reasonable time after you notice it. Then we will find a solution. If you are a private customer, the rules on defects in the Danish Sale of Goods Act (købeloven) may apply.`,
        'If we cannot agree, a private customer can complain to Nævnenes Hus, Toldboden 2, 8800 Viborg. If you live in another EU country, the European Consumer Centre in Denmark (Forbruger Europa) can help.',
        { links: [{ href: 'https://naevneneshus.dk/', label: 'Nævnenes Hus' }, { href: 'https://forbrugereuropa.dk/', label: 'Forbruger Europa' }] },
      ] },
      { title: 'Business customers and law', blocks: [
        'If you buy as a business, the sections for private customers do not apply.',
        'The agreement is governed by Danish law. If you are a private customer living in another EU country, you keep the protection your own country’s mandatory consumer law gives you.',
      ] },
    ],
  },
  da: {
    title: 'Handelsbetingelser for projekter og hosting',
    description: 'Sådan fungerer aftaler med Devantler Tech: tilbud, priser, betaling, din fortrydelsesret og opsigelse af hosting.',
    intro: 'Disse betingelser gælder, når du aftaler et projekt eller hosting med Devantler Tech. Dit skriftlige tilbud beskriver detaljerne i dit projekt. Er der uoverensstemmelse mellem tilbuddet og betingelserne, gælder tilbuddet, men intet tilbud forringer dine rettigheder som privatkunde.',
    updated: updated.da,
    sections: [
      { title: 'Hvem du indgår aftale med', blocks: [
        traderLine('da', company),
        { links: [{ href: companyPath.da, label: 'Virksomhedsoplysninger og postadresse' }] },
      ] },
      { title: 'Tilbud og aftale', blocks: [
        'Du får et skriftligt tilbud på e-mail. Det beskriver, hvad der skal bygges eller hostes, den samlede pris, en eventuel månedspris, tidsplanen og betalingsbetingelserne. Disse betingelser og fortrydelsesformularen, med min postadresse udfyldt, følger med tilbuddet.',
        'Aftalen er indgået, når du skriftligt accepterer tilbuddet. Derefter bekræfter jeg aftalen på e-mail med disse betingelser vedhæftet.',
      ] },
      { title: 'Priser', blocks: [
        'Priserne er i danske kroner. For privatkunder og danske virksomheder er de inkl. 25 % moms. Priserne på hjemmesiden er startpriser; dit tilbud oplyser den samlede pris for dit projekt, en eventuel månedspris og den moms, der gælder for dig.',
        'Domæner, eksterne tjenester og forbrugsafhængige gebyrer indgår ikke i startpriserne. Dit tilbud oplyser sådanne udgifter, eller hvordan de beregnes, før du accepterer.',
      ] },
      { title: 'Betaling og levering', blocks: [
        'Dit tilbud oplyser, hvornår du skal betale. Du betaler via faktura eller med kort eller MobilePay, hvis tilbuddet giver mulighed for det. Fakturaer udstedes gennem Numina; betaling med kort og MobilePay sker gennem Stripe.',
        'Dit tilbud oplyser tidsplanen. Du kan afprøve resultatet, før det går i luften.',
      ] },
      { title: 'Din fortrydelsesret (privatkunder)', blocks: [
        'Som privatkunde kan du fortryde en aftale, der er indgået på e-mail eller online, inden for 14 dage uden at give en begrundelse. De 14 dage regnes fra den dag, aftalen indgås. Får du først disse betingelser på skrift senere, regnes de i stedet fra den dag. Falder den sidste dag på en lørdag, søndag, helligdag, grundlovsdag, juleaftensdag eller nytårsaftensdag, har du frist til den følgende hverdag.',
        `Hvis du vil fortryde, skal du give mig en tydelig besked inden fristen, fx på e-mail til ${company.email}. Du kan bruge formularen nedenfor, men det er ikke et krav. Det er nok, at du sender beskeden inden fristens udløb.`,
        'Jeg begynder kun arbejdet inden for de 14 dage, hvis du udtrykkeligt og skriftligt beder om det. Fortryder du derefter, betaler du et beløb, der står i forhold til det arbejde, der er udført, indtil du fortrød, sammenlignet med hele aftalen. Har du udtrykkeligt bedt mig begynde inden fortrydelsesfristens udløb og anerkendt, at du mister fortrydelsesretten, når arbejdet er helt udført, ophører fortrydelsesretten, når arbejdet er helt udført.',
        'Fortryder du, tilbagebetaler jeg det, du har betalt, med fradrag af en eventuel betaling for udført arbejde, uden unødig forsinkelse og senest 14 dage efter, at jeg har modtaget din besked. Jeg bruger samme betalingsmiddel, som du brugte, medmindre du udtrykkeligt accepterer et andet, og tilbagebetalingen koster dig intet.',
      ] },
      { title: 'Standardfortrydelsesformular', blocks: [
        'Denne formular udfyldes og returneres kun, hvis fortrydelsesretten gøres gældende. Den udgave, der følger med dit tilbud, har min postadresse udfyldt.',
        { list: [
          `Til: Devantler Tech, [postadressen under virksomhedsoplysninger], ${company.email}`,
          'Jeg/vi (*) meddeler herved, at jeg/vi (*) ønsker at gøre fortrydelsesretten gældende i forbindelse med min/vores (*) aftale om levering af følgende tjenesteydelser (*):',
          'Bestilt den (*)/modtaget den (*):',
          'Forbrugerens navn (Forbrugernes navne):',
          'Forbrugerens adresse (Forbrugernes adresse):',
          'Forbrugerens underskrift (Forbrugernes underskrifter) (kun hvis formularens indhold meddeles på papir):',
          'Dato:',
        ] },
        '(*) Det ikke relevante udstreges.',
      ] },
      { title: 'Hosting og opsigelse', blocks: [
        'Hosting betales månedligt og fortsætter, indtil den opsiges. Du kan opsige den på e-mail med en måneds varsel til udgangen af en måned, medmindre dit tilbud angiver en bindingsperiode. Dit tilbud oplyser også, med hvilket varsel jeg kan opsige hostingen.',
        'Som privatkunde kan du under alle omstændigheder opsige hostingen med en måneds varsel til udgangen af en måned, når der er gået fem måneder efter aftalens indgåelse. Har du betalt for en periode efter ophøret, tilbagebetaler jeg straks beløbet for den periode.',
        'Når hostingen ophører, overdrager jeg dit indhold og din kode som beskrevet i dit tilbud.',
      ] },
      { title: 'Fejl og klager', blocks: [
        `Hvis det leverede ikke svarer til det aftalte, så skriv til ${company.email} inden rimelig tid efter, at du har opdaget det. Så finder vi en løsning. Er du privatkunde, kan købelovens mangelsregler finde anvendelse.`,
        'Kan vi ikke blive enige, kan en privatkunde klage til Nævnenes Hus, Toldboden 2, 8800 Viborg. Bor du i et andet EU-land, kan Forbruger Europa hjælpe.',
        { links: [{ href: 'https://naevneneshus.dk/', label: 'Nævnenes Hus' }, { href: 'https://forbrugereuropa.dk/', label: 'Forbruger Europa' }] },
      ] },
      { title: 'Erhvervskunder og lovvalg', blocks: [
        'Køber du som virksomhed, gælder afsnittene for privatkunder ikke.',
        'Aftalen er underlagt dansk ret. Er du privatkunde med bopæl i et andet EU-land, bevarer du den beskyttelse, som dit eget lands ufravigelige forbrugerregler giver dig.',
      ] },
    ],
  },
} as const satisfies Record<Locale, LegalPage>;

export type LegalKind = 'privacy' | 'terms';

/** One legal page; portal-only privacy text appears only when `portal` is true. */
export function legalPage(kind: LegalKind, locale: Locale, portal: boolean): LegalPage {
  return kind === 'privacy' ? privacy(portal)[locale] : terms[locale];
}
