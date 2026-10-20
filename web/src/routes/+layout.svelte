<script lang="ts">
  import "../theme.css";
  import type { Snippet } from "svelte";
  import { setContext } from "svelte";
  import { businessFrameContext, type BusinessFrame } from "#lib/business-frame.ts";
  let { children, data }: { children: Snippet; data: { businessFrame: BusinessFrame } } = $props();
  setContext(businessFrameContext, () => data.businessFrame);
</script>

{@render children()}
