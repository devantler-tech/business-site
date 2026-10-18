# Contributing to the website

Useful first contributions include a clear wording correction, a broken-link fix, an accessibility
improvement or an accurate English/Danish project description. Discuss the problem in an
[issue](https://github.com/devantler-tech/business-site/issues) before substantial work so the
maintainer can agree on scope. Follow the inherited
[Code of Conduct](https://github.com/devantler-tech/.github/blob/main/.github/CODE_OF_CONDUCT.md).

Do not put vulnerabilities, credentials, personal data or private operational details in a public
issue, PR or commit. Follow the inherited
[security policy](https://github.com/devantler-tech/.github/blob/main/.github/SECURITY.md): use this
repository's private reporting option when available; otherwise request a private channel without
disclosing sensitive details and wait before sending them.

## Set up your own copy

Use Git, Bash, Node 24, npm 11, `jq` and Mike Farah's `yq` v4. The two YAML/JSON tools are needed
by the documented fixture checks; Python-based tools also named `yq` are not interchangeable.
These checks install repository dependencies, but do not require
publishing credentials, a cluster or a production database. Fork the repository on GitHub, then
replace `YOUR-GITHUB-LOGIN` with your own login. From your chosen checkout directory:

```sh
git clone https://github.com/YOUR-GITHUB-LOGIN/business-site.git
cd business-site
git worktree add -b docs/my-first-change ../business-site-first-change HEAD
cd ../business-site-first-change
npm ci
npm --prefix web ci
```

Read [AGENTS.md](AGENTS.md), the canonical contract, before editing. Always make changes in your
own isolated worktree, as above; do not share another session's physical checkout or discard its
edits. In the steps below, "repository root" means the root of that worktree.

## Make one focused change

Application paths are relative to the repository root:

- Business-page translations and offer copy live under `src/components/business/`; preserve both
  languages and do not invent prices, promises, client outcomes or company facts.
- Supporting pages and Journal posts live in `src/content/docs/`. This is the Starlight content
  collection, not the contributor-documentation directory. Follow the
  [editorial standard](docs/README.md#blog-editorial-standard) before changing a post.
- Catalogue descriptions belong in `src/data/public-products.json`. Changes to the set also need
  its legacy inventory and dated stars snapshot; follow the
  [catalogue procedure](docs/README.md#public-product-catalogue). Its refresh needs `gh` and `jq`
  with read-only GitHub access, unlike ordinary local content work.
- CV data belongs in `src/data/cv.ts`; keep its documented roster and update date consistent.
- The isolated SvelteKit candidate lives in `web/`; its [guide](web/README.md) describes
  installation, enabled/disabled checks and the current migration boundary.
- Contributor reference and architecture decisions live in `docs/`. Do not hand-edit generated
  `dist/`, the CV PDF, generated snapshots or dependency lockfiles; follow the documented generator.

For a first change, correct a small paragraph or link in an existing page, preserving its MDX
frontmatter and asset attribution. Preview the affected page with
`npm run dev -- --host 127.0.0.1`, then stop the server with Ctrl+C. New or unreleased UI/content
uses the existing default-off release-flag procedure; a documentation change is not activation
permission. Do not introduce confidential examples or use real residential addresses in tests.

## Validate from the repository root

After both pinned installs above, run the shared baseline exactly as follows:

```sh
export LC_ALL=C
npm run build
bash scripts/npm-toolchain.test.sh
bash scripts/dependency-overrides.test.sh
bash scripts/audit-dependencies.test.sh
bash scripts/publishing-contract.test.sh
bash scripts/site-publication.test.sh
bash scripts/check-active-projects-drift.test.sh
bash scripts/check-cv-drift.test.sh
node scripts/check-cv-drift.mjs src/content/docs/about.mdx src/data/cv.ts
git diff --check
```

The build must finish successfully, emit `dist/` and pass the actual source/visitor checks. The
Bash fixtures and CV check must exit zero. The cross-product drift fixtures are local tests;
the real check against other products runs in the monorepo, not in a standalone clone.

Preview the built result with `npm run preview -- --host 127.0.0.1`. Check English/Danish Home,
About and Projects, Journal and technical navigation, existing project redirects/bookmarks,
the CV, keyboard disclosures, mobile layout and synchronized System/Light/Dark appearance.
Check your changed page too. Stop the preview with Ctrl+C. Automated builds do not replace
these observed user paths.

CI additionally checks enabled, explicitly false and entirely unset presentation states and the
portal/package boundary. For dependency changes, follow the clean-install guidance in the
[site reference](docs/README.md#develop) and run `scripts/audit-dependencies.sh`; failures are
not waived. The separate Go module has exact Go, disposable PostgreSQL and packaging requirements
in [server/README.md](server/README.md). Read [server/AGENTS.md](server/AGENTS.md) before backend
changes; never substitute a production database or enable private routes for a documentation test.

## Submit for review

Set up commit signing before committing. Stage only the files you deliberately changed, then
make a signed commit; for example, for a README-only change:

```sh
git add README.md
git commit -S -m "docs: clarify local website setup"
```

Push your branch to your fork and open a **draft** PR against `main` using GitHub's supplied
template. Use a Conventional Commit title, explain the problem and reader-facing outcome, and
link the issue. Record the checks and user paths actually tried, including failures or unavailable
checks, without implying deployment. Disclose AI assistance when used; Agentic Engineer artifacts
follow the repository's disclosure convention. There is no new source license grant from this guide.

Address review feedback and keep evidence tied to the current head. Promotion requires the
repository's complete readiness and review gates; maintainers squash-merge the reviewed head
after required CI succeeds. Do not bypass a gate or request Copilot review. Publishing and portal
activation are separately authorized operator work, not a prerequisite for your first contribution.
