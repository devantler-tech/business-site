<script lang="ts">
  import Shell from "#lib/Shell.svelte";
  import copyScript from "#lib/journal-copy.js?url";
  import type readings from "virtual:journal-reading";
  let { data }: { data:NonNullable<typeof readings>[number] } = $props();
</script>

<svelte:head>
  <link rel="alternate" type="application/rss+xml" title="Devantler Tech Journal" href="https://devantler.tech/blog/rss.xml" />
  {#each data.metadata as item}<meta name={item.name} property={item.property} content={item.content} />{/each}
  {#each data.structuredData as json}{@html `<script type="application/ld+json">${json}</script>`}{/each}
  <script type="module" src={copyScript}></script>
  {#if data.diagramScript}<script type="module" src={data.diagramScript}></script>{/if}
</svelte:head>

<Shell locale="en" title={data.title} description={data.description} page="journal" journalSlug={data.path.slice("/blog/".length, -1)} customSocialMetadata>
  <main id="main" class="reader container">
    <nav class="wayfinding" aria-label="Journal navigation"><a href="/blog/">All articles</a><a href="https://devantler.tech/blog/rss.xml">Subscribe via RSS</a></nav>
    <header><h1 id="_top">{data.title}</h1><p>{data.description}</p></header>
    {#if data.headings.length}<details class="contents"><summary>On this page</summary><nav aria-label="On this page"><ol>{#each data.headings as heading}<li class:subheading={heading.level > 2}><a href={`#${heading.id}`}>{heading.title}</a></li>{/each}</ol></nav></details>{/if}
    <article aria-labelledby="_top"><div class="sl-markdown-content">{@html data.html}</div></article>
    <p class="modified">Last updated: <time datetime={data.modified}>{new Intl.DateTimeFormat("en", {year:"numeric",month:"short",day:"numeric",timeZone:"UTC"}).format(new Date(data.modified))}</time></p>
  </main>
</Shell>

<style>
  .reader { max-width:960px; padding-block:2rem 4rem; }
  .wayfinding { display:flex; justify-content:space-between; flex-wrap:wrap; gap:1rem; color:var(--muted); font-size:.85rem; }
  header { padding-block:3rem 2rem; }
  h1 { font-size:clamp(2.4rem,5vw,4rem); max-width:22ch; }
  header p { margin-top:1.5rem; max-width:64ch; font-size:1.12rem; color:var(--muted); }
  .contents { padding-block:1rem; border-block:1px solid var(--line); margin-bottom:2.5rem; }
  summary { cursor:pointer; font-weight:600; }
  .contents ol { list-style:none; padding:1rem 0 0; display:grid; gap:.7rem; }
  .contents .subheading { padding-left:1.5rem; font-size:.9rem; }
  article { min-width:0; max-width:76ch; margin-inline:auto; overflow-wrap:anywhere; }
  summary:focus-visible { outline:3px solid var(--accent); outline-offset:5px; }
  article :global(p),article :global(ul),article :global(ol) { margin-block:1.25rem; line-height:1.8; }
  article :global(ul),article :global(ol) { padding-left:1.5rem; }
  article :global(li) { padding-left:.25rem; }
  article :global(.sl-heading-wrapper) { display:flex; gap:.5rem; align-items:center; margin-top:3rem; }
  article :global(h2) { font-size:clamp(1.7rem,3vw,2.2rem); scroll-margin-top:2rem; }
  article :global(h3) { font-size:1.35rem; scroll-margin-top:2rem; }
  article :global(.sl-anchor-link) { flex-shrink:0; width:1rem; opacity:.4; }
  article :global(.sl-anchor-link svg) { width:1rem; height:1rem; }
  article :global(.sr-only) { position:absolute; width:1px; height:1px; overflow:hidden; clip-path:inset(50%); }
  article :global(.sl-blog-cover) { margin:0 0 1.5rem; }
  article :global(img) { max-width:100%; height:auto; }
  article :global(.sl-blog-cover img) { display:block; width:100%; height:auto; border-radius:.5rem; }
  article :global(.sl-blog-metadata) { display:flex; flex-wrap:wrap; gap:1rem 2rem; align-items:center; margin-bottom:2rem; font-size:.85rem; color:var(--muted); }
  article :global(.sl-blog-author) { display:flex; gap:.75rem; align-items:center; text-decoration:none; }
  article :global(.sl-blog-author-picture) { border-radius:50%; width:40px; height:40px; }
  article :global(.sl-blog-author-title) { font-size:.75rem; }
  article :global(.reading-table) { max-width:100%; overflow-x:auto; margin-block:1.5rem; }
  article :global(.reading-table:focus-visible) { outline:3px solid var(--accent); outline-offset:4px; }
  article :global(.reading-diagram) { margin:2rem 0; padding:1rem; border:1px solid var(--line); border-radius:.5rem; }
  article :global(.reading-diagram figcaption) { font-weight:600; }
  article :global(.diagram-canvas) { max-width:100%; overflow-x:auto; padding-block:1rem; }
  article :global(.diagram-canvas:focus-visible) { outline:3px solid var(--accent); outline-offset:3px; }
  article :global(.diagram-canvas svg) { display:block; max-width:none !important; width:auto; min-width:100%; height:auto; }
  article :global(.reading-diagram pre) { max-width:100%; overflow-x:auto; font-size:.8rem; line-height:1.6; }
  article :global(.reading-diagram summary) { cursor:pointer; }
  article :global(.diagram-status:empty) { display:none; }
  article :global(table) { border-collapse:collapse; width:100%; }
  article :global(th),article :global(td) { text-align:left; padding:.65rem; border-bottom:1px solid var(--line); }
  article :global(.expressive-code) { margin-block:1.5rem; min-width:0; }
  article :global(.expressive-code pre) { max-width:100%; overflow-x:auto; }
  article :global(.expressive-code .copy [aria-live]) { position:absolute; right:0; top:100%; padding:.4rem; background:var(--paper); color:var(--ink); font-size:.8rem; max-width:18rem; }
  article :global(.expressive-code .copy [aria-live]:empty) { display:none; }
  article :global(hr) { margin-block:2rem; border:0; border-top:1px solid var(--line); }
  article :global(a) { overflow-wrap:anywhere; }
  .modified { margin-top:2rem; color:var(--muted); font-size:.85rem; }
  article :global(.sl-blog-tags-list) { display:flex; flex-wrap:wrap; list-style:none; padding:0; gap:.5rem 1rem; }
  article :global(.sl-blog-pagination) { border-top:1px solid var(--line); padding-top:2rem; margin-top:2rem; }
  article :global(.sl-blog-pagination .card-grid) { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:2rem; }
  article :global(.sl-blog-pagination a) { text-decoration:none; font-size:1.05rem; font-weight:600; }
  article :global(.sl-blog-pagination svg) { width:1rem; height:1rem; margin-top:.75rem; }
  @media(max-width:600px) { header { padding-top:2rem; } article :global(.sl-blog-pagination .card-grid) { grid-template-columns:1fr; } }
</style>
