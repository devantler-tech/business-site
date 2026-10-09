# AGENTS.md — contributor documentation

This directory contains contributor documentation, not the web application.
The root [`AGENTS.md`](../AGENTS.md) governs the application, build and publication.
[`README.md`](README.md) carries the editorial standard and feature-flag how-to.

## Build and validate

Run these before opening a site PR, from the repository root, as
CI runs them (Node 24 with npm 11).

| Check | Command |
|---|---|
| Production build — gates every site PR | `npm ci && npm run build` |
| Project checker fixtures (actual cross-product drift runs in the monorepo) | `bash scripts/check-active-projects-drift.test.sh` |
| Publisher admission and boundaries | `bash scripts/publishing-contract.test.sh && bash scripts/site-publication.test.sh` |
| CV drift | `bash scripts/check-cv-drift.test.sh && node --disable-warning=ExperimentalWarning scripts/check-cv-drift.mjs src/content/docs/about.mdx src/data/cv.ts` |
| Dependency audit (when `package*.json` changes) | `./scripts/audit-dependencies.test.sh && ./scripts/audit-dependencies.sh` |

A browser check (`npm run preview`) must be started in the background with its PID
captured, and killed afterwards even on failure, so port 4321 is freed.

## Where things live

Application paths below are relative to the repository root.

- **Pages and blog posts:** `src/content/docs/`, with posts in `src/content/docs/blog/`.
- **Projects:** edit bilingual `src/data/public-products.json`; set changes also update the
  `public-products:` inventory in `src/content/docs/projects/active.mdx` and refresh GitHub stars.
  That MDX is legacy metadata, not rendered descriptions. Production checks both languages.
  Keep KSail brief and linked to ksail.devantler.tech; never duplicate its docs.
- **CV:** `src/data/cv.ts` is the single source; the PDF is rendered at build time (see the README).
- **Architecture decisions:** every ADR for this repository lives in `adr/`, numbered `NNNN-title.md`.
- **Scripts:** `scripts/`, in bash (the existing `.mjs` checks parse MDX with the site's own
  toolchain) — never Python.

## Content rules

- **Write for the reader.** User-facing pages use a concise, human register that frames each item by
  what the reader gets. Keep the stack names technical readers need to recognise what they are
  getting; cut filler and repetition.
- **Describe what is true now.** State current behaviour and rationale; do not narrate history or
  migrations. Dated records such as ADRs, and migration steps users still need, are exempt.
- **Blog posts are a product.** Follow the README's *Blog editorial standard*: evidence first, an
  outside reader's problem, verified outcomes, a clear next step, and one experiment issue per
  substantive publication or refresh. Never invent users, numbers or first-person experience.
- **Unreleased content ships latent** behind a default-off `astro:env` flag (README → *Feature flags*),
  and each release flag is removed once its content is live.
- **Never hand-edit generated output**, and never edit a blog post during a project-description sync.

The site's recurring maintenance tasks (CI doctor, site QA, content sync, blog stewardship and their
cursors) live in the monorepo's [business-site product card](https://github.com/devantler-tech/monorepo/blob/main/.claude/skills/products/business-site/SKILL.md).
