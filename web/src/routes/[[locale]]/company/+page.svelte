<script lang="ts">
  import Shell from "#lib/Shell.svelte";
  import type { load } from "./+page.server";
  let { data }: { data: ReturnType<typeof load> } = $props();
  let t = $derived(data.copy);
  // The server hook substitutes escaped text in HTML only. Never return the
  // address from load: even csr=false prerenders load results as __data.json.
  const lines = ["__BUSINESS_ADDRESS_LINE_1__", "__BUSINESS_ADDRESS_LINE_2__"];
</script>

<Shell locale={data.locale} title={t.title} description={t.description} page="company">
  <main id="main" class="container company-page">
    <h1>{t.title}</h1><p class="intro">{t.intro}</p>
    <dl data-company-disclosure>
      <div><dt>{t.name}</dt><dd>{data.company.name}</dd></div>
      <div><dt>CVR</dt><dd>{data.company.cvr}</dd></div>
      <div><dt>{t.registration}</dt><dd>{t.type}</dd></div>
      <div><dt>{t.owner}</dt><dd>{data.company.owner}</dd></div>
      <div><dt>{t.address}</dt><dd><address data-company-address data-pagefind-ignore>{#each lines as line}<span>{line}</span>{/each}</address></dd></div>
      <div><dt>{t.email}</dt><dd><a href={`mailto:${data.company.email}`}>{data.company.email}</a></dd></div>
    </dl>
    <p class="contact">{t.contact}</p>
  </main>
</Shell>

<style>
  .company-page { padding-block: 4rem; max-width: 960px; }
  h1 { font-size: clamp(2.3rem, 5vw, 4rem); overflow-wrap: anywhere; }
  .intro { margin-top: 1.5rem; max-width: 650px; font-size: 1.15rem; }
  dl { margin-block: 2.5rem; border-top: 1px solid var(--line); }
  dl > div { display: grid; grid-template-columns: minmax(180px, 1fr) minmax(0, 2fr); gap: 1.5rem; padding-block: 1.25rem; border-bottom: 1px solid var(--line); }
  dt { color: var(--muted); } dd { margin: 0; overflow-wrap: anywhere; }
  address { font-style: normal; } address span { display: block; }
  a { display: inline-flex; align-items: center; min-height: 44px; color: var(--accent); text-underline-offset: .2em; }
  .contact { max-width: 650px; }
  @media (max-width: 600px) { .company-page { padding-block: 2.5rem; } dl > div { grid-template-columns: 1fr; gap: .5rem; } }
</style>
