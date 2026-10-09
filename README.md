# business-site

The [Devantler Tech website](https://devantler.tech): a one-person software business, public
products, journal and technical notes. English is the default; Home, About and Projects also
support Danish. The static Astro + Starlight application uses the repository root:
`src/` for application code and content, `public/` for copied assets, and `scripts/`
for production checks. Contributor documentation and architecture decisions live in `docs/`.

## Development

Use Node 24 and npm 11. See [contributor instructions](AGENTS.md) and the
[site/editorial guide](docs/README.md).

```sh
npm ci
LC_ALL=C npm run build
npm run dev
```

CI builds the normal production experience and uploads `business-site-preview` for review.
Preview with `npm run preview`; stop the server afterwards.

The editorial Journal presentation is behind the default-off
`FEATURE_JOURNAL_PRESENTATION` release flag. Set it to `true` for a review build;
CI checks enabled, explicitly false and entirely unset output. It changes the
Journal's frame and typography, not its article prose, URLs or native browsing controls.
Each post also owns a distinct topic-specific cover in the existing workshop illustration style;
the normal build checks that it is not reused for another post or unrelated site subject.
[The presentation decision](docs/adr/0001-journal-presentation.md) records its scope.
Activation and flag removal are tracked in [#12](https://github.com/devantler-tech/business-site/issues/12)
and require a reviewed source adoption and verified public deployment.

## Publication

The source-owned `publish-site.yaml` builds this repository's exact main revision on
main pushes, manual dispatches and daily public-ranking refreshes. Deployment is
default-off until the repository variable `SITE_PUBLICATION_ENABLED` is exactly
`true`. A main-branch manual dispatch defaults to artifact-only `preview`; it
does not deploy, even when publication is enabled. Production receipts identify
this repository, source SHA, workflow run/attempt and publication mode.

The existing monorepo Pages owner and immutable reusable publisher remain the
live path until the staged ownership transfer in [#25](https://github.com/devantler-tech/business-site/issues/25).
The [cutover and rollback decision](docs/adr/0003-source-owned-publication.md)
requires protected main-only deployment, replacement proof and actual HTTPS
visitor/receipt checks before independently reviewed monorepo cleanup.
A source merge or preview artifact alone is not live delivery.

## Source and asset provenance

The initial source snapshot is from
[monorepo commit 830043b5b273229ec029cf51f41fb3b4b6471c0d](https://github.com/devantler-tech/monorepo/tree/830043b5b273229ec029cf51f41fb3b4b6471c0d/docs).
Original file history remains in that repository. Monorepo ADRs were not imported. The extraction
adds signed commits rather than rewriting historical commits without their signatures.
[Asset provenance](src/assets/PROVENANCE.md) and [illustration prompts](src/assets/editorial/PROMPTS.md)
distinguish generated artwork from real portraits, captures and authored diagrams.

Company/contact completion remains [monorepo#3917](https://github.com/devantler-tech/monorepo/issues/3917).
The unified site's private portal backend, identity, customer isolation and payments
are not deployed by the static Pages publisher.
