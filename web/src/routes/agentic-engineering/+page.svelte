<script lang="ts">
  import JournalReading from "#lib/JournalReading.svelte";
  import type { load } from "./+page.server";
  let { data }: {data:ReturnType<typeof load>} = $props();
</script>
<JournalReading data={data.reading} guides={data.guides} technical />
