<script lang="ts">
  import redirectScript from "#lib/project-redirect.js?url";
  import { retiredGuides, retiredGuideCanonical, type RetiredGuidePath } from "../retired-guides.ts";
  let { path }: { path: RetiredGuidePath } = $props();
  let guide = $derived(retiredGuides[path]);
</script>

<svelte:head>
  <title>This page has moved | Devantler Tech</title>
  <meta name="robots" content="noindex" />
  <link rel="canonical" href={retiredGuideCanonical(path)} />
  <script src={redirectScript} data-destination={guide.destination} data-fallback={guide.fallback}></script>
  <noscript><meta http-equiv="refresh" content={`0;url=${guide.destination}${guide.fallback}`} /></noscript>
</svelte:head>

<p>This page has moved. <a href={`${guide.destination}${guide.fallback}`}>Continue to {guide.name}</a>.</p>
