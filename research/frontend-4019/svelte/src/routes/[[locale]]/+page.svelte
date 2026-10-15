<script>
  import Frame from "#lib/Frame.svelte";
  import Projects from "#lib/Projects.svelte";
  import { copy } from "#sample/data.js";
  let { data } = $props();
</script>

<Frame locale={data.locale}
  ><h1>{copy[data.locale].title}</h1>
  <p class="intro">{copy[data.locale].intro}</p>
  <Projects locale={data.locale} /></Frame
>
