<script lang="ts">
  import { connectReviewFrame } from "../../../server/internal/identity/web/review-collector.mjs";

  let { locale }: { locale: "en" | "da" } = $props();
  let open = $state(false);
  const template = "56278e9abfbbba0bdcd568bc";
  const frame = `https://widget.trustpilot.com/trustboxes/${template}/index.html?templateId=${template}&businessunitId=6b00bb09a7dbf30ad4d33478`;
  const labels = {
    en: {
      title: "Review Devantler Tech",
      prompt: "Have you used our services? Share your experience on Trustpilot.",
      disclosure: "Loading the widget shares your IP address and browser details with Trustpilot, which may use cookies.",
      privacy: "Trustpilot’s privacy policy",
      load: "Load Trustpilot widget",
      hide: "Hide Trustpilot widget",
      link: "Open on Trustpilot",
      frameTitle: "Review Devantler Tech on Trustpilot",
      locale: "en-US",
    },
    da: {
      title: "Anmeld Devantler Tech",
      prompt: "Har du brugt vores ydelser? Del din oplevelse på Trustpilot.",
      disclosure: "Hvis du viser widgetten, modtager Trustpilot din IP-adresse og browseroplysninger og kan bruge cookies.",
      privacy: "Trustpilots privatlivspolitik",
      load: "Vis Trustpilot-widget",
      hide: "Skjul Trustpilot-widget",
      link: "Åbn på Trustpilot",
      frameTitle: "Anmeld Devantler Tech på Trustpilot",
      locale: "da-DK",
    },
  };
  const copy = $derived(labels[locale]);
</script>

<section class="review-collector" aria-labelledby="review-title" data-review-collector>
  <h3 id="review-title">{copy.title}</h3>
  <p>{copy.prompt}</p>
  <p id="review-privacy">{copy.disclosure} <a href="https://legal.trustpilot.com/for-everyone/end-user-privacy-terms" rel="noreferrer">{copy.privacy}</a></p>
  <div class="review-actions">
    <button type="button" aria-expanded={open} aria-controls="review-frame" aria-describedby="review-privacy" onclick={() => { open = !open; }}>
      {open ? copy.hide : copy.load}
    </button>
    <a href="https://www.trustpilot.com/review/devantler.tech" rel="noreferrer">{copy.link}</a>
  </div>
  <div id="review-frame" hidden={!open}>
    {#if open}
      <iframe
        {@attach connectReviewFrame}
        src={`${frame}#locale=${copy.locale}&styleHeight=52px&styleWidth=100%25`}
        title={copy.frameTitle}
        width="340"
        height="52"
        sandbox="allow-scripts allow-same-origin allow-popups allow-popups-to-escape-sandbox"
        referrerpolicy="no-referrer"
      ></iframe>
    {/if}
  </div>
</section>

<style>
  .review-collector {
    margin-block-start: 2rem;
    padding-block-start: 1.5rem;
    border-top: 1px solid var(--line);
  }
  .review-collector p {
    margin-block: 0.5rem;
  }
  .review-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.75rem 1rem;
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
  iframe {
    display: block;
    max-width: 100%;
    margin-block-start: 1rem;
    border: 0;
    border-radius: 0.4rem;
    background: #fff;
  }
  @media (scripting: none) {
    button {
      display: none;
    }
  }
</style>
