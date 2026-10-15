<script>
  import { projects, copy } from "#sample/data.js";
  let { locale = "en" } = $props();
</script>

<h2>{copy[locale].projects}</h2>
<div class="grid">
  {#each projects as project}<article>
      <img src="/work.svg" width="600" height="300" alt="" />
      <h3><a href="https://example.com/">{project}</a></h3>
      <p>{copy[locale].intro}</p>
    </article>{/each}
</div>
