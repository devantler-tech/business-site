<script lang="ts">
  import JournalArchive from "#lib/JournalArchive.svelte";
  import JournalReading from "#lib/JournalReading.svelte";
  import type { load } from "./+page.server";
  let { data }: { data:ReturnType<typeof load> } = $props();
</script>

{#if data.kind === "reading"}<JournalReading data={data.reading} />{:else}<JournalArchive {data} />{/if}
