<script lang="ts">
  import controllerScript from "../../../server/internal/identity/web/review-collector.mjs?url&no-inline";

  let { locale }: { locale: "en" | "da" } = $props();
  const template = "56278e9abfbbba0bdcd568bc";
  const frame = `https://widget.trustpilot.com/trustboxes/${template}/index.html?templateId=${template}&businessunitId=6b00bb09a7dbf30ad4d33478`;
  const labels = {
    en: {
      link: "Review us on Trustpilot",
      options: "Use the Trustpilot widget",
      disclosure: "Trustpilot A/S receives your IP address and browser details when you load the widget. It uses cookies to measure and test the widget. You can withdraw consent by hiding it.",
      cookies: "Trustpilot’s cookie policy",
      cookiesURL: "https://corporate.trustpilot.com/legal/for-everyone/cookie-policy/may-2025",
      load: "Allow cookies and show widget",
      hide: "Hide widget and withdraw consent",
      frameTitle: "Review Devantler Tech on Trustpilot",
      locale: "en-US",
    },
    da: {
      link: "Anmeld os på Trustpilot",
      options: "Brug Trustpilot-widgetten",
      disclosure: "Trustpilot A/S modtager din IP-adresse og browseroplysninger, når du viser widgetten. Trustpilot bruger cookies til at måle brugen og teste widgetten. Du kan trække dit samtykke tilbage ved at skjule den.",
      cookies: "Trustpilots cookiepolitik",
      cookiesURL: "https://dk.corporate.trustpilot.com/legal/for-everyone/cookie-policy/may-2025",
      load: "Tillad cookies og vis widget",
      hide: "Skjul widget og træk samtykke tilbage",
      frameTitle: "Anmeld Devantler Tech på Trustpilot",
      locale: "da-DK",
    },
  };
  const copy = $derived(labels[locale]);
</script>

<section class="review-collector" aria-labelledby="review-title" data-review-collector>
  <a id="review-title" class="review-link" href="https://www.trustpilot.com/evaluate/devantler.tech" rel="noreferrer">{copy.link}</a>
  <details data-review-options hidden>
    <summary>{copy.options}</summary>
    <p id="review-privacy">{copy.disclosure} <a href={copy.cookiesURL} rel="noreferrer">{copy.cookies}</a></p>
    <button type="button" hidden aria-expanded="false" aria-controls="review-frame" aria-describedby="review-privacy" data-review-toggle data-hide-label={copy.hide}>
      {copy.load}
    </button>
    <div id="review-frame" hidden></div>
  </details>
  <template data-review-frame>
    <iframe
      data-src={`${frame}#locale=${copy.locale}&styleHeight=52px&styleWidth=100%25`}
      title={copy.frameTitle}
      width="340"
      height="52"
      sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox"
      referrerpolicy="no-referrer"
    ></iframe>
  </template>
  <script type="module" src={controllerScript}></script>
</section>

<style>
  .review-collector {
    margin-block-start: 0.6rem;
    max-inline-size: 34rem;
    font-size: 0.85rem;
  }
  .review-link {
    display: inline-flex;
    align-items: center;
    min-block-size: 44px;
    font-weight: 600;
  }
  summary {
    min-block-size: 44px;
    padding-block: 0.7rem;
    color: var(--muted);
    cursor: pointer;
  }
  p {
    color: var(--muted);
    margin-block: 0 0.75rem;
  }
  button {
    min-height: 44px;
    border: 1px solid var(--line);
    border-radius: 0.4rem;
    padding: 0.5rem 0.75rem;
    color: inherit;
    background: var(--canvas);
    font: inherit;
    cursor: pointer;
  }
  a {
    color: var(--accent);
    text-underline-offset: 0.2em;
  }
  .review-collector :global(iframe) {
    display: block;
    max-width: 100%;
    margin-block-start: 1rem;
    border: 0;
    border-radius: 0.4rem;
    background: var(--canvas);
  }
  button[hidden] {
    display: none;
  }
</style>
