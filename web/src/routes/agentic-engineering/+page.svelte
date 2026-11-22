<script lang="ts">
  import RetiredGuide from "#lib/RetiredGuide.svelte";
  import type { load } from "./+page.server";
  let { data }: {data:ReturnType<typeof load>} = $props();
</script>

<RetiredGuide path={data.path} />
