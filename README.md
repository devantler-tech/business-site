# Devantler Tech website

The source for [devantler.tech](https://devantler.tech): an independent software business's
website, public project catalogue, Journal and technical guides. Visit the published site to
explore the work; use this repository to improve its content or presentation. Home, About and
Projects are available in English and Danish. The Journal, CV and detailed technical guides
are in English.

The public site is an Astro + Starlight static application. A separate, default-off Go portal
foundation is implemented under [`server/`](server/README.md), but is not deployed by GitHub
Pages. Its local browser sign-in tests do not establish production authentication, customer
intake or payments. You do not need a database, cloud account or publishing credentials to
work on the static site.

The separate [SvelteKit frontend candidate](web/README.md) currently implements English/Danish
Home, About and Projects pages for migration evaluation. It is default-off, built separately and not published.
The existing Astro application remains the live public source.

## Run the site locally

Install Git, Bash, Node 24, npm 11 and `jq` (the build runs JSON-based fixture checks).
From the directory where you want your checkout:

```sh
git clone https://github.com/devantler-tech/business-site.git
cd business-site
npm ci
npm run dev -- --host 127.0.0.1
```

Open the local address printed by Astro (normally `http://127.0.0.1:4321/`). Check `/da/` for
Danish and `/blog/` for the Journal. Stop the server with Ctrl+C. This starts only a local
development server; it does not publish the website or enable the private portal.

From the repository root, build and inspect the static output with:

```sh
LC_ALL=C npm run build
npm run preview -- --host 127.0.0.1
```

A successful build creates `dist/` and passes the embedded source and visitor-page checks.
Use the preview address printed by Astro, then stop it with Ctrl+C. Do not edit `dist/`.
The Journal frame is permanent; rollback requires a reviewed source revert and publication.
The curated client-facing portfolio is enabled by default and retains its explicit-false
build-time rollback. Other previews and their prerequisites are explained in the
[site reference](docs/README.md#feature-flags-build-time).

## Contribute or find the right guide

Start with [CONTRIBUTING.md](CONTRIBUTING.md) for a safe first change, the complete local check
sequence and submission requirements. The [site reference](docs/README.md) covers editorial
standards, bilingual content, the project catalogue, CV and build-time previews.
[Architecture decisions](docs/adr/) explain the site's design; [AGENTS.md](AGENTS.md) is the
canonical repository contract. Backend contributors must also follow the
[portal runtime guide](server/README.md) and its own instructions.

## Publication is separate from a source change

This repository's [`publish-site.yaml`](.github/workflows/publish-site.yaml) owns GitHub Pages
publication on main pushes, admitted manual runs and daily ranking refreshes. Production
publication requires `SITE_PUBLICATION_ENABLED=true` and the protected main-only deployment
environment. Manual runs default to artifact-only `preview`, even when publication is enabled.
The old reusable publisher is retained for its legacy immutable callers, not as the live owner.

A source merge or preview artifact is not publication evidence. Operators check the successful
deployment and the live [`publication-source.json`](https://devantler.tech/publication-source.json)
receipt against its source revision and workflow run, then check the visitor paths. See the
[publication and rollback decision](docs/adr/0003-source-owned-publication.md). The static
publisher does not deploy the portal backend, authentication or payments.

## Source, assets and reuse

The initial source snapshot came from
[monorepo commit 830043b5b273229ec029cf51f41fb3b4b6471c0d](https://github.com/devantler-tech/monorepo/tree/830043b5b273229ec029cf51f41fb3b4b6471c0d/docs);
original history remains there. [Asset provenance](src/assets/PROVENANCE.md) and
[illustration prompts](src/assets/editorial/PROMPTS.md) distinguish generated illustrations
from real photographs, captures and authored diagrams. This repository has no application-source
license grant; public source and dependency notices do not grant unrestricted reuse of its
code, content or assets. Check the applicable terms before reusing them.
