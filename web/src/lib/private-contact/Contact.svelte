<script lang="ts">
  import type { ContactProjection } from "./projection";

  let { contact }: { contact: ContactProjection } = $props();
</script>

<main id="main">
  <div class="intro"><h1>{contact.text.title}</h1><p>{contact.text.intro}</p></div>
  <section aria-label={contact.text.title}>
    {#if contact.state !== "normal"}<p class="form-error" role="alert">{contact.text.errorText}</p>{/if}
    <form class="contact-form" method="post" action="/portal/contact">
      <input type="hidden" name="csrf_token" value={contact.csrf} />
      <input type="hidden" name="lang" value={contact.locale} />
      <input type="hidden" name="revision" value={contact.revision} />
      <label for="contact-name">{contact.text.nameLabel} <span class="field-limit">{contact.locale === "da" ? "(højst 120 tegn)" : "(up to 120 characters)"}</span></label>
      <input id="contact-name" name="name" value={contact.name} autocomplete="name" maxlength="120" required />
      <label for="contact-email">{contact.text.emailLabel} <span class="field-limit">{contact.locale === "da" ? "(højst 254 tegn)" : "(up to 254 characters)"}</span></label>
      <input id="contact-email" name="email" type="email" value={contact.email} autocomplete="email" maxlength="254" aria-describedby="contact-pending" required />
      <p id="contact-pending">{contact.text.pendingText}</p>
      <label for="contact-company">{contact.text.companyLabel} <span class="field-limit">{contact.locale === "da" ? "(højst 160 tegn)" : "(up to 160 characters)"}</span></label>
      <input id="contact-company" name="company" value={contact.company} autocomplete="organization" maxlength="160" />
      <button type="submit">{contact.text.saveLabel}</button>
    </form>
    {#if contact.state === "normal" && contact.verification === "confirmable" && contact.revision !== "0"}
      <form method="post" action="/portal/contact/verify">
        <input type="hidden" name="csrf_token" value={contact.csrf} />
        <input type="hidden" name="lang" value={contact.locale} />
        <input type="hidden" name="revision" value={contact.revision} />
        <p id="contact-verification-help">{contact.text.verificationHelp}</p>
        <button type="submit" aria-describedby="contact-verification-help">{contact.text.verifyLabel}</button>
      </form>
    {/if}
    <a class="back" href={`/portal/contact?lang=${contact.locale}`}>{contact.text.reloadLabel}</a>
  </section>
  <a class="back" href={`/portal/?lang=${contact.locale}`}>{contact.text.workspaceLabel}</a>
</main>
