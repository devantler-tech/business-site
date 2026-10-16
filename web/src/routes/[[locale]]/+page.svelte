<script lang="ts">
  import Shell from "#lib/Shell.svelte";
  import coachingOriginal from "../../../../src/assets/ascoaching-homepage.jpg?url";
  import type { load } from "./+page.server";
  let { data }: { data: ReturnType<typeof load> } = $props();
  let t = $derived(data.home);
  let home = $derived(data.locale === "da" ? "/da/" : "/");
  const format = (value: number) => new Intl.NumberFormat(data.locale === "da" ? "da-DK" : "en-GB").format(value);
</script>

<Shell locale={data.locale} title={data.title} description={data.description} page="home">
  <main id="main">
    <section class="hero-scene" aria-labelledby="hero-title">
      <div class="hero-art" aria-hidden="true">
        <enhanced:img src="../../../../src/assets/matrix.png?w=640;1280;1920" alt="" sizes="100vw" fetchpriority="high" />
      </div>
      <div class="hero container">
        <div class="hero-copy">
          <p class="eyebrow">{t.eyebrow}</p>
          <h1 id="hero-title">{#each t.headline.split("\n") as line}<span>{line}</span>{/each}</h1>
          <p class="hero-intro">{t.intro}</p>
          <div class="actions"><a class="button" href="#contact">{t.primary}<span aria-hidden="true">↗</span></a><a class="text-link" href="#services">{t.secondary}<span aria-hidden="true">↓</span></a></div>
          <p class="availability"><span aria-hidden="true"></span>{t.availability}</p>
        </div>
        <aside class="developer" aria-labelledby="developer-name">
          <figure>
            <enhanced:img src="../../../../src/assets/profile.jpg?w=224;320;600" alt="Nikolai Emil Damm" sizes="(max-width: 760px) 112px, 320px" />
            <figcaption><h2 id="developer-name">Nikolai Emil Damm</h2><p>{t.developerRole}</p></figcaption>
          </figure>
          <p class="developer-note">{t.developerNote}</p>
          <a class="text-link" href={`${home}about/`}>{t.profile}</a>
        </aside>
      </div>
    </section>
    <div class="promise container"><ul>{#each t.promise as item}<li>{item}</li>{/each}</ul></div>
    <section id="quality" data-client-quality data-engineering-quality class="quality container" aria-labelledby="quality-title">
      <div class="section-heading"><p class="eyebrow">{t.qualityEyebrow}</p><h2 id="quality-title">{t.qualityTitle}</h2><p>{t.qualityIntro}</p></div>
      <div class="quality-columns">{#each t.quality as item}<article><h3>{item.title}</h3><p>{item.text}</p></article>{/each}</div>
      <p class="quality-scope">{t.qualityScope}</p>
    </section>
    <section id="services" class="section container" aria-labelledby="services-title">
      <div class="section-heading"><h2 id="services-title">{t.servicesTitle}</h2><p>{t.servicesIntro}</p></div>
      <div class="offers">{#each t.offers as offer}<article class="offer" data-offer={offer.id} data-setup={offer.setup} data-monthly={offer.monthly}>
        <h3>{offer.title}</h3><p class="offer-description">{offer.description}</p>
        <div class="price"><span>{t.starting}</span><strong>{data.locale === "en" ? `${t.currency} ` : ""}{format(offer.setup)}{data.locale === "da" ? ` ${t.currency}` : ""}</strong></div>
        <p class="hosting-price">{t.monthly} <b>{format(offer.monthly)} {t.currency}</b> {t.perMonth}</p>
        <ul>{#each offer.includes as item}<li>{item}</li>{/each}</ul>
        <p class="boundary">{offer.boundary}</p><a class="text-link" href="#contact">{t.contact}<span aria-hidden="true">↗</span></a>
      </article>{/each}</div>
      <p class="price-note">{t.priceNote}</p>
      <div class="scope-notes"><div><h3>{t.hostingTitle}</h3><p>{t.hostingText}</p></div><div><h3>{t.fitTitle}</h3><p>{t.fitText}</p></div></div>
    </section>
    <section id="work" class="work-band" aria-labelledby="work-title">
      <div class="container section">
        <div class="section-heading"><h2 id="work-title">{t.work.heading}</h2><p>{t.work.intro}</p></div>
        <article class="work-example" data-work-example="coaching" data-compact-work>
          <div><p class="eyebrow">{t.work.kind}</p><h3>AS Coaching og Vaner</h3><p class="description">{t.work.text}</p></div>
          <a class="work-preview" href={coachingOriginal} target="_blank" rel="noopener">
            <enhanced:img src="../../../../src/assets/ascoaching-homepage.jpg?w=160;240;320;480" alt={t.workPreviewAlt} sizes="(max-width: 520px) 96px, 240px" loading="lazy" />
            <span>{t.workPreviewAction}<span aria-hidden="true">↗</span></span>
          </a>
          <a class="text-link visit" href="https://ascoachingogvaner.dk/">{t.workVisitAction}<span aria-hidden="true">↗</span></a>
        </article>
        <div id="products" class="portfolio-bridge"><p>{t.portfolioBridge}</p><a class="text-link" href={`https://devantler.tech${home}projects/`}>{t.portfolioLink}<span aria-hidden="true">↗</span></a></div>
      </div>
    </section>
    <section id="process" class="section container process" aria-labelledby="process-title">
      <div class="section-heading"><h2 id="process-title">{t.processTitle}</h2></div>
      <ol>{#each t.steps as step}<li><h3>{step.title}</h3><p>{step.text}</p></li>{/each}</ol>
    </section>
    <section id="contact" class="contact-band" aria-labelledby="contact-title">
      <div class="container"><h2 id="contact-title">{t.contactTitle}</h2><p>{t.contactText}</p><a class="button" href={data.inquiryUrl}>{t.contactAction}<span aria-hidden="true">↗</span></a><p class="contact-note">{t.contactNote}</p></div>
    </section>
  </main>
</Shell>

<style>
  h2 { font-size: clamp(1.8rem, 3vw, 2.65rem); }
  h3 { font-size: 1.3rem; letter-spacing: -.025em; }
  .hero-scene { position: relative; isolation: isolate; overflow: hidden; background: var(--canvas); }
  .hero-art { position: absolute; inset: 0; z-index: -2; opacity: var(--art-opacity); }
  .hero-art :global(img) { width: 100%; height: 100%; object-fit: cover; }
  .hero-scene::before { content: ''; position: absolute; inset: 0; z-index: -1; background: linear-gradient(90deg, var(--hero-shield) 10%, transparent 90%), linear-gradient(0deg, var(--canvas), transparent 30%); }
  .hero { display: grid; grid-template-columns: minmax(0, 1.65fr) minmax(0, 1fr); gap: 5rem; padding-block: 4.5rem 3.5rem; align-items: center; }
  .eyebrow { font-size: .8rem; font-weight: 600; color: var(--accent); margin-bottom: 1.2rem; }
  h1 { font-size: clamp(2.7rem, 4.5vw, 4.15rem); line-height: 1.09; }
  h1 span { display: block; }
  .hero-intro { max-width: 550px; font-size: 1.1rem; margin-top: 1.6rem; }
  .actions { display: flex; align-items: center; gap: 1.7rem; flex-wrap: wrap; margin-top: 2rem; }
  .button { display: inline-flex; align-items: center; justify-content: center; gap: 1.5rem; min-height: 48px; padding: .85rem 1.25rem; background: var(--accent); border: 1px solid var(--accent); border-radius: 6px; color: var(--accent-ink); font-size: .92rem; font-weight: 600; text-decoration: none; }
  .button:hover { background: var(--accent-hover); border-color: var(--accent-hover); color: var(--accent-ink); }
  .text-link { display: inline-flex; align-items: center; gap: .7rem; min-height: 44px; font-size: .9rem; font-weight: 600; color: var(--accent); text-decoration: none; }
  .text-link:hover { text-decoration: underline; }
  .availability { display: flex; align-items: center; gap: .6rem; font-size: .8rem; margin-top: 1.25rem; }
  .availability span { width: 6px; height: 6px; background: var(--accent); border-radius: 50%; }
  .developer { max-width: 320px; justify-self: center; }
  .developer figure { margin: 0; }
  .developer :global(img) { display: block; width: 100%; height: auto; aspect-ratio: 1; object-fit: cover; object-position: center 35%; border: 1px solid var(--line); border-radius: 8px; }
  .developer figcaption { margin-top: 1rem; }
  .developer h2 { font-size: 1.15rem; }
  .developer figcaption p { font-size: .75rem; margin-top: .35rem; }
  .developer-note { font-size: .85rem; margin-top: 1rem; }
  .promise { padding-block: 1.4rem; border-block: 1px solid var(--line); }
  .promise ul { display: flex; justify-content: space-between; gap: 1rem; padding: 0; margin: 0; list-style: none; font-size: .85rem; color: var(--muted); }
  .section { padding-block: 5rem; }
  .section-heading { max-width: 730px; margin-bottom: 2.5rem; }
  .section-heading p { margin-top: 1rem; }
  .quality { padding-block: 4rem; }
  .quality-columns { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 2.5rem; }
  .quality-columns article { padding-top: 1.2rem; border-top: 1px solid var(--line); }
  .quality-columns h3 { font-size: 1.2rem; }
  .quality-columns p { margin-top: .8rem; font-size: .95rem; }
  .quality-scope { max-width: 860px; font-size: .8rem; margin-top: 2rem; }
  .offers { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 1.25rem; }
  .offer { display: flex; flex-direction: column; border: 1px solid var(--line); border-radius: 6px; padding: 1.65rem; background: var(--card); }
  .offer-description { font-size: .9rem; margin-top: .8rem; min-height: 4.5em; }
  .price { display: flex; flex-direction: column; margin-top: 1.5rem; }
  .price > span { font-size: .8rem; color: var(--muted); }
  .price strong { font-size: 2rem; font-weight: 600; letter-spacing: -.05em; color: var(--ink); }
  .hosting-price { font-size: .8rem; border-bottom: 1px solid var(--line); padding-bottom: 1.3rem; }
  .offer ul { padding-left: 1.1rem; font-size: .86rem; margin-block: 1.35rem; }
  .offer li { padding-left: .15rem; margin-block: .45rem; }
  .offer li::marker { color: var(--accent); }
  .boundary { font-size: .79rem; margin-bottom: 1rem; }
  .offer .text-link { margin-top: auto; }
  .price-note { font-size: .8rem; margin-top: 1.3rem; max-width: 1000px; }
  .scope-notes { display: grid; grid-template-columns: 1fr 1fr; gap: 4rem; margin-top: 3rem; padding-top: 3rem; border-top: 1px solid var(--line); }
  .scope-notes h3 { font-size: 1.2rem; }
  .scope-notes p { font-size: .9rem; margin-top: .9rem; }
  .work-band { background: var(--paper); border-block: 1px solid var(--line); }
  .work-example { display: grid; grid-template-columns: minmax(0, 1fr) 240px; gap: 0 2rem; margin-top: 1.5rem; padding-block: 1.5rem; border-block: 1px solid var(--line); }
  .work-example .eyebrow { margin-bottom: .6rem; }
  .description { font-size: .9rem; margin-top: 1rem; max-width: 640px; }
  .work-preview { grid-column: 2; grid-row: 1 / span 2; color: var(--muted); font-size: .8rem; text-decoration: none; cursor: zoom-in; }
  .work-preview :global(img) { display: block; width: 100%; height: auto; border: 1px solid var(--line); border-radius: 8px; }
  .work-preview > span { display: flex; align-items: center; gap: .5rem; min-height: 44px; }
  .work-preview:hover > span { color: var(--accent); text-decoration: underline; }
  .visit { grid-column: 1; grid-row: 2; justify-self: start; align-self: start; margin-top: .5rem; }
  .portfolio-bridge { display: flex; align-items: center; justify-content: space-between; gap: 1rem 3rem; padding-top: 1.5rem; scroll-margin-top: 1.5rem; }
  .portfolio-bridge p { max-width: 650px; font-size: .9rem; }
  .portfolio-bridge a { flex-shrink: 0; }
  .process ol { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 3rem; list-style: none; counter-reset: steps; margin: 0; padding: 0; }
  .process li { border-top: 1px solid var(--line); padding-top: 1.3rem; counter-increment: steps; }
  .process li::before { content: counter(steps); display: block; color: var(--accent); font-size: .9rem; margin-bottom: 1rem; }
  .process h3 { font-size: 1.15rem; }
  .process p { font-size: .9rem; margin-top: .9rem; }
  .contact-band { background: var(--contact); border-top: 1px solid var(--line); padding-block: 4.5rem; }
  .contact-band h2 { max-width: 700px; }
  .contact-band p { max-width: 650px; margin-top: 1.3rem; }
  .contact-band .button { margin-top: 1.6rem; }
  .contact-band .contact-note { font-size: .75rem; max-width: 700px; }
  @media (max-width: 1050px) { .hero { gap: 2rem; } .offers { gap: .75rem; } .offer { padding: 1.2rem; } }
  @media (max-width: 760px) {
    .hero { grid-template-columns: 1fr; gap: 2rem; padding-block: 2rem; }
    .hero-scene::before { background: linear-gradient(90deg, var(--hero-shield), transparent 170%), linear-gradient(0deg, var(--canvas), transparent 35%); }
    .developer { width: 100%; max-width: 540px; }
    .developer figure { display: grid; grid-template-columns: 112px minmax(0, 1fr); gap: 1rem; align-items: center; }
    .developer figcaption { margin-top: 0; }
    h1 { font-size: clamp(2.2rem, 8vw, 3.4rem); }
    .promise ul { flex-direction: column; gap: .5rem; }
    .section { padding-block: 3rem; }
    .offers, .scope-notes, .quality-columns, .process ol { grid-template-columns: 1fr; gap: 1.5rem; }
    .portfolio-bridge { flex-direction: column; align-items: flex-start; gap: .5rem; }
    .quality { padding-block: 2.5rem; }
    .offer { padding: 1.5rem; }
    .offer-description { min-height: 0; }
    .scope-notes { margin-top: 2rem; padding-top: 2rem; }
    .contact-band { padding-block: 3rem; }
  }
  @media (max-width: 520px) { .work-example { grid-template-columns: minmax(0, 1fr) 96px; gap: 0 1rem; } .work-example h3 { font-size: 1.1rem; } .work-preview > span { font-size: .7rem; } }
</style>
