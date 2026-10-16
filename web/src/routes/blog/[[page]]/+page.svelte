<script lang="ts">
  import Shell from "#lib/Shell.svelte";
  import navigation from "../../../../../src/components/business/journal-navigation.js?url";
  import avatar from "../../../../../public/author-avatar.png?url";
  import type { Picture } from "vite-imagetools";
  import type { load } from "./+page.server";
  let { data }: { data:ReturnType<typeof load> } = $props();
  const covers = import.meta.glob<Picture>("../../../../../src/assets/editorial/journal-*.webp", { eager:true, import:"default", query:"?enhanced&w=480;800;1200" });
  function cover(name:string) {
    const image = covers[`../../../../../src/assets/editorial/${name}`];
    if (!image) throw new Error(`Journal cover missing from the local image map: ${name}`);
    return image;
  }
</script>

<svelte:head>
  <link rel="alternate" type="application/rss+xml" title="Devantler Tech Journal" href="https://devantler.tech/blog/rss.xml" />
  <script src={navigation}></script>
</svelte:head>

<Shell locale="en" title={data.title} description={data.description} page="journal" journalPage={data.page}>
  <main id="main" class="container journal">
    <nav class="wayfinding" aria-label="Journal navigation"><a href="/blog/">All articles</a><a href="https://devantler.tech/blog/rss.xml">Subscribe via RSS</a></nav>
    <header class="intro"><h1>Journal</h1><p>{data.intro}</p></header>
    <details class="browse"><summary>Browse the Journal</summary><div>
      <a href="/blog/">All posts</a>
      <section class="browse-group" aria-labelledby="recent-posts"><h2 id="recent-posts">Recent posts</h2><ul>{#each data.browse.recent as post}<li><a href={post.href}>{post.title}</a></li>{/each}</ul></section>
      <section class="browse-group" aria-labelledby="journal-tags"><h2 id="journal-tags">Tags</h2><ul>{#each data.browse.tags as tag}<li><a href={tag.href}>{tag.label} ({tag.count})</a></li>{/each}</ul></section>
      <a href="https://devantler.tech/blog/rss.xml">RSS</a>
      <p><a href="https://devantler.tech/blog/authors/nikolai-emil-damm/">Articles by Nikolai Emil Damm</a></p>
      <p>Articles and topic archives are available on the published website.</p>
    </div></details>
    <section class="posts" aria-label="Journal articles">
      {#each data.posts as post, index}
        <article class="sl-blog-preview">
          <header>
            <a class="sl-blog-preview-link" href={post.href}>
              <h2 class="sl-blog-preview-title">{post.title}</h2>
              <enhanced:img src={cover(post.cover)} alt="" sizes="(max-width: 760px) 100vw, 560px" loading={index === 0 ? "eager" : "lazy"} />
            </a>
            <div class="metadata">
              <time class="sl-blog-metadata-date" datetime={post.date}>{post.dateLabel}</time>
              <div class="authors">{#each post.authors as author}<a class="sl-blog-author" href={author.url}><img src={avatar} alt={author.name} width="40" height="40" /><span><span class="author-name">{author.name}</span><span class="author-title">{author.title}</span></span></a>{/each}</div>
            </div>
          </header>
          <div class="sl-blog-preview-excerpt">{@html post.excerpt}</div>
          <footer><ul aria-label="Tags">{#each post.tags as tag}<li><a class="sl-blog-tag" href={tag.href}>{tag.label}</a></li>{/each}</ul></footer>
        </article>
      {/each}
    </section>
    <nav class="journal-pagination" aria-label="Journal pages">
      <div>{#if data.previous}<a href={data.previous} rel="prev">Newer articles</a>{/if}</div>
      <p>Page {data.page} of {data.count}</p>
      <div>{#if data.next}<a href={data.next} rel="next">Older articles</a>{/if}</div>
    </nav>
  </main>
</Shell>

<style>
  .journal { padding-block:2rem 3rem; }
  .wayfinding { display:flex; flex-wrap:wrap; justify-content:space-between; gap:1rem; font-size:.85rem; color:var(--muted); }
  .wayfinding a { text-decoration:none; }
  .intro { padding-block:3.5rem 2.5rem; max-width:850px; }
  .intro h1 { font-size:clamp(2.75rem,6vw,4.5rem); }
  .intro p { font-size:1.12rem; margin-top:1.25rem; max-width:46rem; }
  .browse { border-bottom:1px solid var(--line); margin-bottom:2.75rem; }
  .browse summary { padding-block:.8rem 1.25rem; cursor:pointer; font-weight:600; }
  .browse div { padding-bottom:1.5rem; font-size:.9rem; }
  .browse p { margin-top:.6rem; }
  .browse-group { margin-block:1.5rem; }
  .browse-group h2 { font-size:1rem; margin-bottom:.75rem; }
  .browse-group ul { display:grid; gap:.6rem; }
  .browse-group li { min-width:0; overflow-wrap:anywhere; }
  .browse-group a { display:inline-block; padding-block:.25rem; }
  .browse-group:last-of-type ul { display:flex; }
  summary:focus-visible { outline:3px solid var(--accent); outline-offset:4px; }
  .posts { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:2.5rem; }
  article { min-width:0; border-bottom:1px solid var(--line); padding-bottom:2rem; }
  .sl-blog-preview-link { display:flex; flex-direction:column; gap:1.25rem; text-decoration:none; }
  .sl-blog-preview-title { font-size:clamp(1.5rem,2.6vw,2rem); letter-spacing:-.035em; overflow-wrap:anywhere; }
  .sl-blog-preview-link :global(picture) { order:-1; }
  .sl-blog-preview-link :global(img) { display:block; width:100%; height:auto; aspect-ratio:16/9; object-fit:cover; border-radius:.5rem; }
  article:first-child { grid-column:1/-1; }
  article:first-child .sl-blog-preview-link { display:grid; grid-template-columns:1fr 1fr; gap:2.5rem; align-items:center; }
  article:first-child .sl-blog-preview-title { font-size:clamp(2rem,3.6vw,3rem); }
  article:first-child .sl-blog-preview-link :global(picture) { order:0; }
  .metadata { margin-block:1.25rem; color:var(--muted); font-size:.85rem; }
  .authors { display:flex; flex-wrap:wrap; gap:1rem; margin-top:.75rem; }
  .sl-blog-author { display:flex; gap:.5rem; align-items:center; text-decoration:none; line-height:1.3; }
  .sl-blog-author img { width:2rem; height:2rem; border-radius:50%; border:1px solid var(--line); }
  .author-name,.author-title { display:block; }
  .author-name { font-weight:600; }
  .author-title { margin-top:.2rem; font-size:.75rem; }
  .sl-blog-preview-excerpt { line-height:1.75; color:var(--muted); }
  footer { margin-top:1.25rem; }
  ul { display:flex; flex-wrap:wrap; gap:.5rem; list-style:none; padding:0; margin:0; }
  .sl-blog-tag { display:inline-flex; align-items:center; min-height:32px; padding:.25rem .5rem; border:1px solid var(--line); border-radius:.3rem; background:var(--paper); font-size:.75rem; text-decoration:none; color:var(--muted); }
  .journal-pagination { display:grid; grid-template-columns:1fr auto 1fr; gap:1.5rem; align-items:center; margin-top:3rem; font-size:.9rem; }
  .journal-pagination div:last-child { text-align:right; }
  .journal-pagination a { display:inline-flex; align-items:center; min-height:44px; }
  @media(max-width:760px) {
    .intro { padding-block:2.5rem 2rem; }
    .intro p { font-size:1rem; }
    .posts { grid-template-columns:1fr; gap:2rem; }
    article:first-child { grid-column:auto; }
    article:first-child .sl-blog-preview-link { display:flex; gap:1.25rem; }
    article:first-child .sl-blog-preview-link :global(picture) { order:-1; }
    .journal-pagination { gap:.8rem; font-size:.8rem; }
  }
</style>
