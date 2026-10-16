<script lang="ts">
  import Shell from "#lib/Shell.svelte";
  import type { load } from "./+page.server";
  let { data }: { data: ReturnType<typeof load> } = $props();
</script>

<Shell locale={data.locale} title={data.about.title} description={data.about.description}>
  <main id="main" class="container supporting-page">
    <div class="opening">
      <h1>{data.about.title}</h1>
      <p>{data.about.intro}</p>
    </div>
    <section class="founder" aria-labelledby="founder-title">
      <enhanced:img
        src="../../../../../src/assets/profile.jpg?w=320;600"
        alt="Nikolai Emil Damm"
        sizes="(max-width: 760px) 320px, 320px"
        fetchpriority="high"
      />
      <div>
        <p class="role">{data.about.founderRole}</p>
        <h2 id="founder-title">Nikolai Emil Damm</h2>
        <p>{data.about.founderText}</p>
        <p>{data.about.founderNote}</p>
      </div>
    </section>
    <section aria-labelledby="approach-title">
      <h2 id="approach-title">{data.about.approachTitle}</h2>
      <div class="principles">
        {#each data.about.principles as item}<article>
            <h3>{item.title}</h3>
            <p>{item.text}</p>
          </article>{/each}
      </div>
    </section>
    <section class="background" aria-labelledby="background-title">
      <h2 id="background-title">{data.about.backgroundTitle}</h2>
      <p>{data.about.backgroundText}</p>
      <div class="links">
        <a href="https://devantler.tech/pdfs/nikolai-emil-damm-cv.pdf">{data.about.cv}</a><a
          href="https://devantler.tech/blog/">{data.locale === "da" ? "Læs journalen (engelsk)" : "Read the Journal"}</a
        >
      </div>
    </section>
    <a class="contact-link" href={`${data.locale === "da" ? "/da/" : "/"}#contact`}
      >{data.about.contact}</a
    >
  </main>
</Shell>

<style>
  .supporting-page {
    padding-block: 4rem;
  }
  .opening {
    max-width: 820px;
    padding-bottom: 3rem;
  }
  h1 {
    font-size: clamp(2.3rem, 5vw, 4rem);
  }
  .opening p {
    font-size: 1.2rem;
    margin-top: 1.5rem;
    max-width: 650px;
  }
  h2 {
    font-size: clamp(1.6rem, 3vw, 2.2rem);
  }
  h3 {
    font-size: 1.2rem;
  }
  section {
    padding-block: 2.5rem;
    border-top: 1px solid var(--line);
  }
  .founder {
    display: grid;
    grid-template-columns: 320px minmax(0, 1fr);
    gap: 4rem;
    align-items: center;
  }
  .founder :global(img) {
    width: 100%;
    height: auto;
    aspect-ratio: 1;
    object-fit: cover;
    object-position: center 35%;
    border-radius: 8px;
    border: 1px solid var(--line);
  }
  .founder p:not(.role) {
    margin-top: 1.2rem;
    max-width: 660px;
  }
  .role {
    color: var(--accent);
    font-size: 0.85rem;
    margin-bottom: 0.5rem;
  }
  .principles {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 2.5rem;
    margin-top: 2rem;
  }
  .principles p {
    margin-top: 1rem;
    font-size: 0.95rem;
  }
  .background > p {
    margin-top: 1.2rem;
    max-width: 760px;
  }
  .links {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem 2rem;
    margin-top: 1.25rem;
  }
  a {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    color: var(--accent);
    text-underline-offset: 0.2em;
  }
  .contact-link {
    background: var(--accent);
    color: var(--accent-ink);
    border-radius: 6px;
    padding: 0.75rem 1.25rem;
    font-weight: 600;
  }
  .contact-link:hover {
    color: var(--accent-ink);
    background: var(--accent-hover);
  }
  @media (max-width: 760px) {
    .supporting-page {
      padding-block: 2.5rem;
    }
    .founder,
    .principles {
      grid-template-columns: 1fr;
      gap: 1.5rem;
    }
    .founder :global(img) {
      max-width: 320px;
    }
    .opening {
      padding-bottom: 2rem;
    }
    section {
      padding-block: 2rem;
    }
  }
</style>
