<script lang="ts">
  import "../theme.css";
  import type { Snippet } from "svelte";
  let { children }: { children: Snippet } = $props();
</script>

{@render children()}
