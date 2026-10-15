<script>
  import { copy } from "#sample/data.js";
  let { locale = "en", portal = false, children } = $props();
  let t = $derived(copy[locale]);
  let home = $derived(locale === "da" ? "/da/" : "/");
</script>

<svelte:head
  ><title>{portal ? t.portal : t.title} — Framework sample</title><meta
    name="description"
    content={t.intro}
  /></svelte:head
>
<a href="#main" class="skip">{locale === "da" ? "Gå til indhold" : "Skip to content"}</a>
<header>
  <nav class="wrap" aria-label={locale === "da" ? "Navigation" : "Navigation"}>
    <a class="brand" href={home}>Framework sample</a><a href={home}>{t.projects}</a><a href={`${home}portal/`}
      >{t.portal}</a
    ><a hreflang={locale === "en" ? "da" : "en"} href={`${locale === "en" ? "/da/" : "/"}${portal ? "portal/" : ""}`}
      >{t.language}</a
    >
  </nav>
</header>
<main id="main" class="wrap">{@render children()}</main>
<footer class="wrap">Fictional framework evaluation. No customers, sign-in or payment processing.</footer>
