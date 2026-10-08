# AGENTS.md — business-site

Devantler Tech's public English/Danish business website, Projects catalogue, journal and
supporting technical pages. This file is canonical; the monorepo's reviewed
[shared engineering contract](https://github.com/devantler-tech/monorepo/blob/main/AGENTS.md)
also applies. Use isolated worktrees, signed commits, issue-backed draft PRs and current-head
review/CI/user-path evidence. Never use an admin merge bypass or request Copilot review.

## Layout and ownership

- `docs/` contains the Astro + Starlight application, public assets and production contracts.
- `docs/README.md` contains the editorial, localization, catalogue and feature-flag standards.
- `.github/workflows/ci.yaml` validates source and uploads a downloadable production preview.
- `.github/workflows/publish-pages.yaml` owns the complete callable publisher. The monorepo
  retains the existing GitHub Pages resource/domain and calls an immutable workflow pin with
  its reviewed `applications/business-site` gitlink SHA. This repository has no independent
  push publisher, cluster tenant, contact backend, checkout or paid subscription system.
- New architecture decisions belong only in `docs/adr/`. Existing monorepo decisions remain
  in their original repository; the source snapshot and asset provenance are linked in the README.

## Maintenance

Use Node 24, npm 11 and `LC_ALL=C`. From this repository's root:

```sh
npm --prefix docs ci
npm --prefix docs run build
bash docs/scripts/npm-toolchain.test.sh
bash docs/scripts/audit-dependencies.test.sh
bash docs/scripts/publishing-contract.test.sh
bash docs/scripts/check-active-projects-drift.test.sh
bash docs/scripts/check-cv-drift.test.sh
node docs/scripts/check-cv-drift.mjs docs/src/content/docs/about.mdx docs/src/data/cv.ts
```

Audit dependency changes with `docs/scripts/audit-dependencies.sh` from `docs/`. The real
cross-product drift check runs in the monorepo against its pinned site and Actions products;
standalone CI exercises the checker's hermetic fixtures and the actual CV. Do not substitute
fixtures for the aggregator's cross-product validation or weaken a failing check.

Exercise English/Danish Home, About and Projects, journal and technical navigation, old
project redirects/bookmarks, the CV, keyboard disclosures, mobile layouts and synchronized
System/Light/Dark appearance before promotion. Stop any local preview server after checking it.
`CI - Required Checks` aggregates every applicable CI job with `always()`. Merge is squash,
pinned to the reviewed head, after the repository's ordered review lanes and complete readiness
preflight; source publication is a separate reviewed monorepo pin adoption plus live readback.

## Content boundaries

Describe an honest one-person business. Do not invent company/contact/customer facts, reveal a
private address, claim statutory compliance, or add purchases/subscriptions. Registration/contact
follow-up [monorepo#3917](https://github.com/devantler-tech/monorepo/issues/3917) remains open.
Keep the introductory guides at DKK 2,995 plus optional hosting 99/month for a website, 7,995
plus 299/month for a small app, and 4,995 plus 199/month for a service; extra scope/costs require
an agreed quote. Preserve real portrait/screenshots/diagrams and generated-art provenance.

Scripts are Bash or Go, never Python. Stage explicit paths; never discard another session's work.
