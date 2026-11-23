<script lang="ts">
  import "../../../server/internal/identity/web/review-collector.css";
  import controllerScript from "../../../server/internal/identity/web/review-collector.mjs?url&no-inline";

  let { locale }: { locale: "en" | "da" } = $props();
  const template = "56278e9abfbbba0bdcd568bc";
  const frame = `https://widget.trustpilot.com/trustboxes/${template}/index.html?templateId=${template}&businessunitId=6b00bb09a7dbf30ad4d33478`;
  const labels = {
    en: {
      action: "Review us on",
      settings: "Cookie settings",
      title: "Optional cookies",
      purpose: "Trustpilot uses cookies to measure and test its review widget.",
      privacy: "Privacy and cookies",
      privacyURL: "/privacy/",
      deny: "Decline",
      allow: "Allow Trustpilot",
      frameTitle: "Review Devantler Tech on Trustpilot",
      locale: "en-US",
    },
    da: {
      action: "Anmeld os på",
      settings: "Cookieindstillinger",
      title: "Valgfrie cookies",
      purpose: "Trustpilot bruger cookies til at måle og teste sin anmeldelseswidget.",
      privacy: "Privatliv og cookies",
      privacyURL: "/da/privacy/",
      deny: "Afvis",
      allow: "Tillad Trustpilot",
      frameTitle: "Anmeld Devantler Tech på Trustpilot",
      locale: "da-DK",
    },
  };
  const copy = $derived(labels[locale]);
</script>

<div class="review-collector" data-review-collector>
  <a id="review-title" class="review-link" href="https://www.trustpilot.com/evaluate/devantler.tech" rel="noreferrer">{copy.action} <strong>Trustpilot</strong><span aria-hidden="true">↗</span></a>
  <div id="review-frame" hidden></div>
  <button type="button" class="review-settings" data-review-settings aria-controls="review-consent" aria-expanded="false" hidden>{copy.settings}</button>
  <section class="review-consent" id="review-consent" data-review-consent aria-labelledby="review-consent-title" hidden>
    <div>
      <strong id="review-consent-title">{copy.title}</strong>
      <p>{copy.purpose} <a href={copy.privacyURL}>{copy.privacy}</a></p>
    </div>
    <div class="review-consent-actions">
      <button type="button" data-review-decision="deny">{copy.deny}</button>
      <button type="button" data-review-decision="allow">{copy.allow}</button>
    </div>
  </section>
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
</div>
