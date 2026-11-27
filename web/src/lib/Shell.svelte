<script lang="ts">
  import type { Snippet } from "svelte";
  import { getContext } from "svelte";
  import { businessFrameContext, type BusinessFrame } from "./business-frame";
  import TrustpilotLink from "./TrustpilotLink.svelte";
  import themeScript from "../../../src/components/business/theme.js?url";
  import favicon from "../../../public/favicon.png?url";
  import logoDark from "../../../src/assets/brand/devantler-tech-logo-dark.svg?url";
  import logoLight from "../../../src/assets/brand/devantler-tech-logo-light.svg?url";
  import sharingImage from "virtual:business-sharing";
  import { portalCopy } from "../../../src/components/business/portal-copy";
  let {
    locale,
    title,
    description,
    page = "about",
    journalPage = 1,
    journalSlug = "",
    customSocialMetadata = false,
    children,
  }: { locale: "en" | "da"; title: string; description: string; page?: "home" | "about" | "projects" | "company" | "privacy" | "terms" | "journal"; journalPage?:number; journalSlug?:string; customSocialMetadata?:boolean; children: Snippet } = $props();
  const frame = getContext<() => BusinessFrame>(businessFrameContext);
  let business = $derived(frame());
  const words = {
    en: {
      skip: "Skip to content",
      navigation: "Main navigation",
      services: "Services & prices",
      projects: "Projects",
      about: "About",
      journal: "Journal",
      contact: "Get in touch",
      appearance: "Appearance",
      system: "System",
      light: "Light",
      dark: "Dark",
      identity: "Nikolai Emil Damm’s one-person software business on Funen, Denmark.",
    },
    da: {
      skip: "Spring til indhold",
      navigation: "Hovednavigation",
      services: "Ydelser og priser",
      projects: "Projekter",
      about: "Om virksomheden",
      journal: "Journal (engelsk)",
      contact: "Kontakt",
      appearance: "Udseende",
      system: "System",
      light: "Lys",
      dark: "Mørk",
      identity: "En softwarevirksomhed på Fyn, drevet af Nikolai Emil Damm.",
    },
  };
  let t = $derived(words[locale]);
  let portal = $derived(portalCopy[locale]);
  let home = $derived(locale === "da" ? "/da/" : "/");
  let publishedHome = $derived(`https://devantler.tech${home}`);
  let englishOnly = $derived(page === "journal");
  let suffix = $derived(page === "home" ? "" : page === "journal" ? `blog/${journalSlug ? `${journalSlug}/` : journalPage > 1 ? `${journalPage}/` : ""}` : `${page}/`);
</script>

<svelte:head>
  <title>{title} | Devantler Tech</title><meta name="description" content={description} />
  {#if !customSocialMetadata}
    <meta property="og:title" content={`${title} | Devantler Tech`} /><meta property="og:description" content={description} />
    <meta property="og:type" content="website" /><meta property="og:url" content={`${publishedHome}${suffix}`} />
    <meta property="og:locale" content={locale === "da" ? "da_DK" : "en_GB"} />
    <meta property="og:image" content={sharingImage ?? ""} /><meta property="og:image:alt" content="Nikolai Emil Damm" />
    <meta name="twitter:card" content="summary" />
  {/if}
  <link rel="canonical" href={`${publishedHome}${suffix}`} />
  <link rel="alternate" hreflang="en" href={`https://devantler.tech/${suffix}`} />
  {#if !englishOnly}<link rel="alternate" hreflang="da" href={`https://devantler.tech/da/${suffix}`} />{/if}
  <link rel="alternate" hreflang="x-default" href={`https://devantler.tech/${suffix}`} />
  <link rel="sitemap" href="/sitemap-index.xml" /><link rel="icon" type="image/png" href={favicon} />
  <script src={themeScript}></script>
</svelte:head>

<a class="skip" href="#main">{t.skip}</a>
<header>
  <div class="business-header container">
    <a href={home} class="brand" aria-label="Devantler Tech"
      ><img class="brand-logo brand-logo-dark" src={logoDark} alt="" width="573" height="108" /><img
        class="brand-logo brand-logo-light" src={logoLight} alt="" width="573" height="108" /></a
    >
    <nav aria-label={t.navigation}>
      <a href={`${home}#services`}>{t.services}</a><a href={`${home}projects/`} aria-current={page === "projects" ? "page" : undefined}>{t.projects}</a><a
        href={locale === "da" ? "/da/about/" : "/about/"}
        aria-current={page === "about" ? "page" : undefined}>{t.about}</a
      ><a href="/blog/" aria-current={page === "journal" ? "page" : undefined}>{t.journal}</a><a href={`${home}#contact`}>{t.contact}</a>
      <a class="client-login" href={portal.href}>{portal.login}</a>
    </nav>
    <div class="controls">
      <div id="theme-control" hidden>
        <label class="sr-only" for="theme-select">{t.appearance}</label><select id="theme-select"
          ><option value="auto">{t.system}</option><option value="light">{t.light}</option><option value="dark"
            >{t.dark}</option
          ></select
        >
      </div>
      <div class="languages" aria-label={locale === "da" ? "Vælg sprog" : "Choose language"}>
        <a href={`/${suffix}`} lang="en" aria-current={locale === "en" ? "page" : undefined}>EN</a><span aria-hidden="true"
          >/</span
        ><a href={`/da/${englishOnly ? "" : suffix}`} lang="da" aria-label={englishOnly ? "Danish business homepage" : undefined} aria-current={locale === "da" ? "page" : undefined}>DA</a>
      </div>
    </div>
  </div>
</header>
{@render children()}
<footer data-business-footer class="container">
  <div><a class="brand" href={home}>Devantler Tech</a>
    <p>{business.footer.identity}</p><p>{business.footer.note}</p>
    {#if business.identity}<p>CVR {business.identity.cvr} · {business.footer.registration}</p><a class="company-email" href={business.inquiryUrl}>{business.identity.email}</a>{/if}
  </div>
  <div class="footer-actions">
    <div class="footer-links"><a href={`${home}about/`}>{business.footer.about}</a><a href={`${home}projects/`}>{business.footer.projects}</a>{#if business.identity}<a href={`${home}company/`}>{locale === "da" ? "Virksomhedsoplysninger" : "Company information"}</a><a href={`${home}terms/`}>{locale === "da" ? "Handelsbetingelser" : "Terms"}</a><a href={`${home}privacy/`}>{locale === "da" ? "Privatliv og cookies" : "Privacy and cookies"}</a>{/if}<a href={`${home}#contact`}>{business.footer.contact}</a><a href={portal.href}>{portal.login}</a></div>
    {#if business.identity}<TrustpilotLink {locale} />{/if}
  </div>
</footer>

<style>
  header {
    border-bottom: 1px solid var(--line);
    background: var(--canvas);
  }
  .business-header {
    display: flex;
    align-items: center;
    gap: 1.5rem;
    min-height: 90px;
  }
  .brand {
    display: inline-flex;
    align-items: center;
    flex-shrink: 0;
    font-size: 1.25rem;
    font-weight: 600;
    letter-spacing: -0.04em;
    text-decoration: none;
    color: var(--ink);
  }
  /* The Devantler Tech wordmark: the dark-background version by default, the
     light-background one when the page is light, following theme.css. */
  .brand-logo {
    display: block;
    width: auto;
    height: 32px;
  }
  .brand-logo-light {
    display: none;
  }
  :global(:root[data-theme="light"]) .brand-logo-dark {
    display: none;
  }
  :global(:root[data-theme="light"]) .brand-logo-light {
    display: block;
  }
  @media (prefers-color-scheme: light) {
    :global(:root:not([data-theme])) .brand-logo-dark {
      display: none;
    }
    :global(:root:not([data-theme])) .brand-logo-light {
      display: block;
    }
  }
  nav {
    display: flex;
    justify-content: flex-end;
    flex: 1;
    gap: 1.1rem;
  }
  nav a {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    font-size: 0.875rem;
    text-decoration: none;
    color: var(--ink);
    white-space: nowrap;
  }
  .client-login {
    border: 1px solid var(--line);
    border-radius: 6px;
    padding-inline: 0.85rem;
    font-weight: 600;
  }
  .client-login:hover {
    border-color: var(--accent);
  }
  a:hover,
  a[aria-current] {
    color: var(--accent);
  }
  .controls {
    display: flex;
    align-items: center;
    gap: 0.9rem;
    flex-shrink: 0;
  }
  select {
    min-height: 44px;
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 0.4rem 0.35rem 0.4rem 0.65rem;
    background: var(--paper);
    color: var(--ink);
    font: inherit;
    font-size: 0.8rem;
    cursor: pointer;
  }
  .languages {
    display: flex;
    gap: 0.5rem;
    font-size: 0.8rem;
    color: var(--muted);
  }
  .languages a {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    text-decoration: none;
    color: inherit;
  }
  .languages [aria-current] {
    color: var(--accent);
    font-weight: 600;
  }
  .languages span {
    align-self: center;
  }
  .skip {
    position: fixed;
    top: -100%;
    left: 1rem;
    z-index: 100;
    padding: 0.6rem 1rem;
    background: var(--accent);
    color: var(--accent-ink);
  }
  .skip:focus {
    top: 1rem;
  }
  footer {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 2rem;
    padding-block: 2rem;
    border-top: 1px solid var(--line);
  }
  footer p {
    font-size: 0.875rem;
    margin-top: 0.5rem;
  }
  .company-email, .footer-links a { display: inline-flex; align-items: center; min-height: 44px; font-size: .8rem; color: var(--ink); text-underline-offset: .2em; overflow-wrap: anywhere; }
  .footer-actions { min-inline-size: 0; }
  .footer-links { display: flex; flex-wrap: wrap; gap: 1.25rem; }
  @media (max-width: 1200px) {
    .business-header {
      gap: 0.5rem 1rem;
      flex-wrap: wrap;
      padding-block: 0.75rem;
    }
    nav {
      order: 3;
      flex-basis: 100%;
      justify-content: flex-start;
      flex-wrap: wrap;
      gap: 0.1rem 1.25rem;
    }
    .controls {
      margin-left: auto;
    }
  }
  @media (max-width: 760px) {
    footer { flex-direction: column; gap: .75rem; }
    .brand {
      font-size: 1.1rem;
    }
    .brand-logo {
      height: 26px;
    }
    .controls {
      gap: 0.6rem;
    }
  }
  @media (max-width: 520px) {
    nav {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 0.25rem 1rem;
    }
    .client-login {
      justify-content: center;
    }
  }
</style>
