<script lang="ts">
  import Shell from "#lib/Shell.svelte";
  import revealScript from "../../../../../src/components/business/project-reveal.js?url";
  import desktop from "../../../../../src/assets/ksail-desktop.png?enhanced";
  import coachingOriginal from "../../../../../src/assets/ascoaching-homepage.jpg?url";
  import type { load } from "./+page.server";
  let { data }: { data: ReturnType<typeof load> } = $props();
  let home = $derived(data.locale === "da" ? "/da/" : "/");
  let t = $derived(data.labels);
</script>

<svelte:head><script src={revealScript} defer></script></svelte:head>
<Shell locale={data.locale} title={data.title} description={data.description} page="projects">
  <main id="main" class="container projects-page">
    <div class="opening"><h1>{data.title}</h1><p>{data.text.pageIntro}</p></div>
    <div data-client-portfolio data-featured-portfolio>
      <nav class="project-index" aria-label={data.projectIndex}><a href="#open-title">{data.text.tools}</a><a href="#family-title">{data.text.work}</a><a href="#research">{data.researchTitle}</a></nav>
      <section aria-labelledby="open-title">
        <span class="legacy-anchor" id="technical-title" aria-hidden="true"></span><span class="legacy-anchor" id="technical-projects" aria-hidden="true"></span><span class="legacy-anchor" id="more-public-products" aria-hidden="true"></span>
        <div class="catalogue-heading"><div class="section-heading"><p class="eyebrow">01</p><h2 id="open-title">{data.text.tools}</h2><p>{data.text.toolsIntro}</p><p class="checked">{data.text.checked}: <time datetime={data.checkedAt}>{data.checkedAt.replace("T", " ").replace("Z", " UTC")}</time></p></div><enhanced:img src="../../../../../src/assets/editorial/workflows.webp?w=440" alt="" /></div>
        {#each data.hiddenAnchors as id}<span class="legacy-anchor" {id} aria-hidden="true"></span>{/each}
        <div class="collection">
          {#each data.products as product, i}
            <article class="product" class:featured={product.repository === "ksail"} data-public-product={product.repository} data-stars={product.stars} data-ksail-presentation={product.repository === "ksail" || undefined}>
              {#each product.anchors as id}<span class="legacy-anchor" {id} aria-hidden="true"></span>{/each}
              <div class="product-copy">
                <span class="number" aria-hidden="true">{String(i + 1).padStart(2, "0")}</span><h3>{product.name}</h3><p>{product.description}</p>
                {#if product.repository === "ksail"}<p class="terms">{data.text.sourceAvailable}</p>{/if}
                {#if product.terms}<p class="terms">{product.terms}</p>{/if}
                {#if product.repository === "ksail"}<a href="https://ksail.devantler.tech">{t.productVisit}<span aria-hidden="true">↗</span></a>{/if}
                <a href={`https://github.com/devantler-tech/${product.repository}`}>{data.text.project}: {product.name}<span aria-hidden="true">↗</span></a>
              </div>
              {#if product.repository === "ksail"}
                <div class="ksail-story"><p class="workflow-lead">{data.ksail.lead}</p><ol data-ksail-workflow>{#each data.ksail.steps as step}<li><h4>{step.title}</h4><p>{step.body}</p></li>{/each}</ol>
                  <a data-ksail-detail href={desktop.img.src} target="_blank" rel="noopener">{data.ksail.detail}<span aria-hidden="true">↗</span><span class="sr-only"> ({data.ksail.newTab})</span></a><p class="interface-note">{data.ksail.privacy}</p>
                </div>
              {/if}
            </article>
          {/each}
        </div>
        <p class="scope">{data.text.reuse}</p><a href="/blog/">{t.journal}<span aria-hidden="true">→</span></a>
        <p><a href="https://github.com/orgs/devantler-tech/repositories">{data.text.wider}<span aria-hidden="true">↗</span></a></p>
      </section>
      <section aria-labelledby="family-title">
        <span class="legacy-anchor" id="-self-hosted-personal-apps" aria-hidden="true"></span>
        <div class="section-heading"><p class="eyebrow">02</p><h2 id="family-title">{data.text.work}</h2><p>{data.text.intro}</p></div>
        <article class="work-example">
          <div><p class="eyebrow">{data.work.kind}</p><h3>AS Coaching og Vaner</h3><p class="description">{data.work.text}</p></div>
          <a class="work-preview" href={coachingOriginal} target="_blank" rel="noopener"><enhanced:img src="../../../../../src/assets/ascoaching-homepage.jpg?w=240;480" alt={data.workPreviewAlt} sizes="(max-width: 520px) 96px, 240px" loading="lazy" /><span>{data.workPreviewAction}<span aria-hidden="true">↗</span></span></a>
          <a class="visit" href="https://ascoachingogvaner.dk/">{data.workVisitAction}<span aria-hidden="true">↗</span></a>
        </article>
        <article data-hosting-project class="hosting"><span class="legacy-anchor" id="️-platform---" aria-hidden="true"></span><h3>{t.platformTitle}</h3><p>{t.platformText}</p></article>
      </section>
      <aside id="engineering-checks" class="quality-bridge"><p>{t.qualityBridge}</p><a href={`${home}#quality`}>{t.qualityAction}<span aria-hidden="true">↗</span></a></aside>
      <section id="research" aria-labelledby="research-title">
        <div class="research-heading"><div class="section-heading"><p class="eyebrow">{t.researchLabel}</p><h2 id="research-title">{data.researchTitle}</h2><p>{t.researchText}</p><a href="https://devantler.tech/pdfs/thesis.pdf">{data.thesis}<span aria-hidden="true">↗</span></a></div><enhanced:img src="../../../../../src/assets/editorial/data-research.webp?w=480;760" alt="" /></div>
        <details><summary>{t.details}</summary><div class="research-detail" lang="en"><h3 id="-data-product-">{t.researchBackground}</h3><p>{t.researchBackgroundText}</p><h3 id="-exploration-of-state-of-the-art-technology-architectures-and-tools-to-create-future-proof-data-spaces">{t.researchDiagram}</h3><enhanced:img src="../../../../../src/assets/data-space-as-a-data-mesh.png?w=480;960" alt="Data Space as a Data Mesh" /></div></details>
      </section>
    </div>
    <a class="contact-link" href={`${home}#contact`}>{data.contact}<span aria-hidden="true">↗</span></a>
  </main>
</Shell>

<style>
  .projects-page { padding-block: 4rem; }
  .opening { max-width: 820px; padding-bottom: 3rem; }
  h1 { font-size: clamp(2.3rem, 5vw, 4rem); }
  .opening p { font-size: 1.2rem; margin-top: 1.5rem; max-width: 650px; }
  .project-index { display: flex; flex-wrap: wrap; gap: .5rem 2rem; padding-bottom: 2rem; }
  a { display: inline-flex; align-items: center; min-height: 44px; gap: .7rem; color: var(--accent); text-underline-offset: .2em; }
  section { padding-block: 4rem; border-top: 1px solid var(--line); scroll-margin-top: 1.5rem; }
  .section-heading { max-width: 710px; }
  h2 { font-size: clamp(1.8rem, 3vw, 2.65rem); } h3 { font-size: 1.3rem; }
  .section-heading > p:not(.eyebrow) { margin-top: 1rem; }
  .catalogue-heading { display: grid; grid-template-columns: minmax(0, 1fr) 190px; gap: 3rem; align-items: center; }
  .catalogue-heading :global(img), .research-heading :global(img) { width: 100%; height: auto; border-radius: 8px; }
  .collection { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 3rem; margin-top: 2.5rem; }
  .product { min-width: 0; padding-block: 1.5rem; border-top: 1px solid var(--line); }
  .product .number { display: block; font-size: .8rem; color: var(--accent); margin-bottom: .6rem; }
  .product h3 { overflow-wrap: anywhere; font-size: 1.2rem; }
  .product p { margin-top: .7rem; font-size: .95rem; }
  .product .terms, .scope { font-size: .8rem; }
  .product a { margin-right: 1.25rem; }
  .checked { font-size: .8rem; margin-top: 1rem; }
  .featured { grid-column: 1 / -1; display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 0 5rem; align-items: center; padding-block: 2.75rem; }
  .featured > .legacy-anchor { grid-column: 1 / -1; }
  .featured h3 { font-size: clamp(2.5rem, 4vw, 3.5rem); letter-spacing: -.05em; }
  .featured .product-copy > p:first-of-type { max-width: 38ch; font-size: 1.05rem; line-height: 1.7; margin-block: 1rem; }
  .ksail-story { min-width: 0; padding-left: 2rem; border-left: 2px solid var(--accent); }
  .product .workflow-lead { margin: 0; color: var(--ink); font-size: 1.35rem; font-weight: 600; line-height: 1.35; letter-spacing: -.025em; max-width: 30ch; }
  .ksail-story ol { list-style: none; margin: 1.5rem 0 .75rem; padding: 0; }
  .ksail-story li { display: grid; grid-template-columns: 5rem minmax(0, 1fr); gap: 1rem; padding-block: .85rem; border-top: 1px solid var(--line); }
  .ksail-story h4 { margin: 0; font-size: .95rem; font-weight: 600; line-height: 1.65; }
  .ksail-story li p { margin: 0; font-size: .9rem; line-height: 1.65; }
  .product .interface-note { margin: .2rem 0 0; font-size: .75rem; }
  .scope { margin-top: 1rem; max-width: 900px; }
  .quality-bridge { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: .5rem 2rem; background: var(--paper); border: 1px solid var(--line); border-radius: 6px; padding: 1rem 1.5rem; margin-bottom: 4rem; scroll-margin-top: 2rem; }
  .quality-bridge p { color: var(--ink); font-weight: 600; }
  .research-heading { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 3rem; align-items: center; }
  details { margin-top: 2rem; border-block: 1px solid var(--line); }
  summary { min-height: 44px; padding-block: 1rem; cursor: pointer; font-weight: 600; }
  summary:focus-visible { outline: 3px solid var(--accent); outline-offset: 4px; }
  .research-detail { max-width: 900px; padding-block: 1rem 2rem; }
  .research-detail h3 { margin-block: 1.5rem 1rem; scroll-margin-top: 2rem; }
  .research-detail :global(img) { width: 100%; height: auto; margin-top: 1rem; background: white; border-radius: 5px; }
  .legacy-anchor { display: block; scroll-margin-top: 1.5rem; }
  .work-example { display: grid; grid-template-columns: minmax(0, 1fr) 240px; gap: 0 2rem; margin-top: 1.5rem; padding-block: 1.5rem; border-block: 1px solid var(--line); }
  .work-example .eyebrow { margin-bottom: .6rem; }
  .description { font-size: .9rem; margin-top: 1rem; max-width: 640px; }
  .work-preview { grid-column: 2; grid-row: 1 / span 2; color: var(--muted); font-size: .8rem; text-decoration: none; cursor: zoom-in; display: block; }
  .work-preview :global(img) { display: block; width: 100%; height: auto; border: 1px solid var(--line); border-radius: 8px; }
  .work-preview > span { display: flex; align-items: center; gap: .5rem; min-height: 44px; }
  .visit { grid-column: 1; grid-row: 2; justify-self: start; align-self: start; margin-top: .5rem; }
  .hosting { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 2fr); gap: 0 3rem; margin-top: 2.5rem; padding: 1.5rem 0 0; border-top: 1px solid var(--line); }
  .hosting .legacy-anchor { grid-column: 1 / -1; }
  .hosting h3 { font-size: 1.15rem; } .hosting p { font-size: .9rem; }
  .contact-link { margin-top: 3rem; }
  @media (max-width: 760px) { .projects-page { padding-block: 2.5rem; } section { padding-block: 2.5rem; } .collection, .featured, .research-heading, .hosting { grid-template-columns: 1fr; gap: 1.5rem; } .catalogue-heading { grid-template-columns: 1fr; gap: 0; } .catalogue-heading :global(picture), .catalogue-heading :global(img) { display: none; } .quality-bridge { padding: 1rem; margin-bottom: 2.5rem; } .collection { gap: 0; } .featured { gap: 2rem; padding-block: 2rem; } .ksail-story { padding-left: 1.25rem; } }
  @media (max-width: 520px) { .work-example { grid-template-columns: minmax(0, 1fr) 96px; gap: 0 1rem; } .work-example h3 { font-size: 1.1rem; } .work-preview > span { font-size: .7rem; } }
</style>
