<script>
  import Frame from "#lib/Frame.svelte";
  import Projects from "#lib/Projects.svelte";
  import { copy, amountPattern } from "#sample/data.js";
  let { data, form } = $props();
  let t = $derived(copy[data.locale]);
</script>

<Frame locale={data.locale} portal
  ><h1>{t.portal}</h1>
  <p>Fictional invoice form; no transfer or accounting action.</p>
  {#if form?.invalid}<p class="error" role="alert">{t.invalid}</p>{/if}{#if form?.ok}<p class="result" role="status">
      {t.result}: {form.amount} DKK
    </p>{/if}
  <form method="post">
    <label for="amount">{t.amount} (DKK)</label><input
      id="amount"
      name="amount"
      type="text"
      inputmode="decimal"
      pattern={amountPattern}
      required
    /><button>{t.save}</button>
  </form>
  <Projects locale={data.locale} /></Frame
>
