<script lang="ts">
  import Shell from "./Shell.svelte";
  import type { LegalKind, LegalPage } from "../../../src/components/business/legal-copy";
  let { locale, kind, copy }: { locale: "en" | "da"; kind: LegalKind; copy: LegalPage } = $props();
</script>

<Shell {locale} title={copy.title} description={copy.description} page={kind}>
  <main id="main" class="container legal-page" data-legal-page={kind}>
    <h1>{copy.title}</h1>
    <p class="intro">{copy.intro}</p>
    {#each copy.sections as section}
      <section>
        <h2>{section.title}</h2>
        {#each section.blocks as block}
          {#if typeof block === "string"}<p>{block}</p>
          {:else if "list" in block}<ul>{#each block.list as item}<li>{item}</li>{/each}</ul>
          {:else}<ul class="links" role="list">{#each block.links as link}<li><a href={link.href}>{link.label}</a></li>{/each}</ul>{/if}
        {/each}
      </section>
    {/each}
    <p class="updated">{copy.updated}</p>
  </main>
</Shell>

<style>
  .legal-page { padding-block: 4rem; max-width: 760px; }
  h1 { font-size: clamp(2.3rem, 5vw, 4rem); overflow-wrap: anywhere; }
  .intro { margin-top: 1.5rem; font-size: 1.15rem; }
  section { margin-top: 2.5rem; padding-top: 1.5rem; border-top: 1px solid var(--line); }
  h2 { font-size: 1.35rem; overflow-wrap: anywhere; }
  section > * + * { margin-top: 1rem; }
  p, li { overflow-wrap: anywhere; }
  li + li { margin-top: .4rem; }
  ul { padding-left: 1.25rem; }
  ul.links { list-style: none; padding-left: 0; }
  a { display: inline-flex; align-items: center; min-height: 44px; color: var(--accent); text-underline-offset: .2em; }
  .updated { margin-top: 3rem; color: var(--muted); }
  @media (max-width: 600px) { .legal-page { padding-block: 2.5rem; } }
</style>
