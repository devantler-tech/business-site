<script lang="ts">
  import "../../../server/internal/identity/web/trustpilot-link.css";

  let { locale }: { locale: "en" | "da" } = $props();
</script>

<a class="trustpilot-link" href="https://www.trustpilot.com/review/devantler.tech" rel="noreferrer">
  <span>{locale === "da" ? "Anmeldelser på" : "Reviews on"} <strong>Trustpilot</strong></span>
  <span class="trustpilot-link-arrow" aria-hidden="true">↗</span>
</a>
