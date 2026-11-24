<script lang="ts">
  import "../../../server/internal/identity/web/trustpilot-link.css";

  let { locale }: { locale: "en" | "da" } = $props();
</script>

<a class="trustpilot-link" href="/trustpilot/" rel="noreferrer" data-sveltekit-reload>
  <span>{locale === "da" ? "Anmeldelser på" : "Reviews on"} <strong>Trustpilot</strong></span>
  <span class="trustpilot-link-arrow" aria-hidden="true">↗</span>
</a>
