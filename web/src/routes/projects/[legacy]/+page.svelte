<script lang="ts">
  import redirectScript from "#lib/project-redirect.js?url";
  let { data }: { data: { fallback: string } } = $props();
</script>
<svelte:head>
  <title>Projects have moved | Devantler Tech</title>
  <link rel="canonical" href="https://devantler.tech/projects/" />
  <script src={redirectScript} data-destination="/projects/" data-fallback={data.fallback}></script>
  <noscript><meta http-equiv="refresh" content={`0;url=/projects/${data.fallback}`} /></noscript>
</svelte:head>
<p>The project catalogue has moved. <a href={`/projects/${data.fallback}`}>Continue to Projects</a>.</p>
