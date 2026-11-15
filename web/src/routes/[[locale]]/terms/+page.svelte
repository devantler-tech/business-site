<script lang="ts">
  import LegalPage from "#lib/LegalPage.svelte";
  import type { load } from "./+page.server";
  let { data }: { data: ReturnType<typeof load> } = $props();
</script>

<LegalPage locale={data.locale} kind={data.kind} copy={data.copy} />
