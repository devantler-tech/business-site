<script lang="ts">
  import { customerScope } from "../../../src/components/business/customer-scope";
  import { getContext } from "svelte";
  import { businessFrameContext, type BusinessFrame } from "./business-frame";
  const frame = getContext<() => BusinessFrame>(businessFrameContext);
  let { locale, href, label, heading = "h2" }: { locale: "en" | "da"; href?: string; label?: string; heading?: "h2" | "h3" } = $props();
  let t = $derived(customerScope[locale]);
</script>

{#if frame().customerScopeEnabled}<div class="customer-scope" data-customer-scope>
  <svelte:element this={heading}>{t.title}</svelte:element><p>{t.audience}</p><p class="boundary">{t.boundary}</p>
  {#if href && label}<a class="contact-link" {href}>{label}</a>{/if}
</div>{:else if href && label}<a class="contact-link" {href}>{label}</a>{/if}

<style>
  .customer-scope { max-width: 700px; margin-block: 2rem; }
  .customer-scope :global(h2), .customer-scope :global(h3) { font-size: 1.1rem; }
  p { margin-top: .75rem; }
  .boundary { font-size: .85rem; }
  .contact-link { display: inline-flex; align-items: center; min-height: 44px; margin-top: 1.25rem; background: var(--accent); color: var(--accent-ink); border-radius: 6px; padding: .75rem 1.25rem; font-weight: 600; }
  .contact-link:hover { color: var(--accent-ink); background: var(--accent-hover); }
</style>
