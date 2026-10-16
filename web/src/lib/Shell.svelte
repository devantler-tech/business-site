<script lang="ts">
  import type { Snippet } from "svelte";
  import themeScript from "../../../src/components/business/theme.js?url";
  let {
    locale,
    title,
    description,
    page = "about",
    journalPage = 1,
    journalSlug = "",
    children,
  }: { locale: "en" | "da"; title: string; description: string; page?: "home" | "about" | "projects" | "journal"; journalPage?:number; journalSlug?:string; children: Snippet } = $props();
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
  let home = $derived(locale === "da" ? "/da/" : "/");
  let publishedHome = $derived(`https://devantler.tech${home}`);
  let suffix = $derived(page === "home" ? "" : page === "journal" ? `blog/${journalSlug ? `${journalSlug}/` : journalPage > 1 ? `${journalPage}/` : ""}` : `${page}/`);
</script>

<svelte:head>
  <title>{title} | Devantler Tech</title><meta name="description" content={description} />
  {#if !journalSlug}<meta property="og:description" content={description} />{/if}
  <link rel="canonical" href={`${publishedHome}${suffix}`} />
  <link rel="alternate" hreflang="en" href={`https://devantler.tech/${suffix}`} />
  {#if page !== "journal"}<link rel="alternate" hreflang="da" href={`https://devantler.tech/da/${suffix}`} />{/if}
  <link rel="alternate" hreflang="x-default" href={`https://devantler.tech/${suffix}`} />
  <script src={themeScript}></script>
</svelte:head>

<a class="skip" href="#main">{t.skip}</a>
<header>
  <div class="business-header container">
    <a href={home} class="brand" aria-label="Devantler Tech"
      ><svg class="brand-mark" viewBox="0 0 32 32" aria-hidden="true"
        ><path d="M8 8h8a8 8 0 0 1 0 16H8V8Zm5 5v6h3a3 3 0 0 0 0-6h-3Z" fill="currentColor" /></svg
      ><span>Devantler<span class="brand-tech"> Tech</span></span></a
    >
    <nav aria-label={t.navigation}>
      <a href={`${home}#services`}>{t.services}</a><a href={`${home}projects/`} aria-current={page === "projects" ? "page" : undefined}>{t.projects}</a><a
        href={locale === "da" ? "/da/about/" : "/about/"}
        aria-current={page === "about" ? "page" : undefined}>{t.about}</a
      ><a href="/blog/" aria-current={page === "journal" ? "page" : undefined}>{t.journal}</a><a href={`${home}#contact`}>{t.contact}</a>
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
        ><a href={`/da/${page === "journal" ? "" : suffix}`} lang="da" aria-label={page === "journal" ? "Danish business homepage" : undefined} aria-current={locale === "da" ? "page" : undefined}>DA</a>
      </div>
    </div>
  </div>
</header>
{@render children()}
<footer class="container">
  <a class="brand" href={home}>Devantler Tech</a>
  <p>{t.identity}</p>
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
  .brand-tech {
    color: var(--accent);
  }
  .brand-mark {
    width: 34px;
    height: 34px;
    margin-right: 0.45rem;
    background: var(--accent);
    color: var(--accent-ink);
    border-radius: 8px;
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
    padding-block: 2rem;
    border-top: 1px solid var(--line);
  }
  footer p {
    font-size: 0.875rem;
    margin-top: 0.5rem;
  }
  @media (max-width: 1050px) {
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
    .brand {
      font-size: 1.1rem;
    }
    .brand-mark {
      width: 28px;
      height: 28px;
    }
    .controls {
      gap: 0.6rem;
    }
  }
</style>
