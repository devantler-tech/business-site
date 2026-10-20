<script lang="ts">
  import JournalArchive from "#lib/JournalArchive.svelte";
  import type { load } from "./+page.server";
  let { data }: { data:ReturnType<typeof load> } = $props();
</script>

<JournalArchive {data} />
