# business-site

The [Devantler Tech website](https://devantler.tech): a one-person software business, public
products, journal and technical notes. English is the default; Home, About and Projects also
support Danish. The static Astro + Starlight application lives in `docs/`.

## Development

Use Node 24 and npm 11. See [contributor instructions](AGENTS.md) and the
[site/editorial guide](docs/README.md).

```sh
npm --prefix docs ci
LC_ALL=C npm --prefix docs run build
npm --prefix docs run dev
```

CI builds the normal production experience and uploads `business-site-preview` for review.
Preview with `npm --prefix docs run preview`; stop the server afterwards.

## Publication

Website source, tests and the complete reusable publisher belong here. The monorepo aggregates
this product at `applications/business-site`, and retains its existing GitHub Pages resource,
`github-pages` environment and `devantler.tech` domain. No DNS or hosting-resource transfer is
required. The monorepo's thin caller supplies the exact reviewed gitlink revision to an immutable
publishing-workflow pin. Source and workflow pins are deliberately separate.

A source merge here is not a live release by itself: adopt its reviewed revision in the monorepo,
wait for the successful Pages deployment, then verify the actual public routes and the published
`publication-source.json` receipt. Roll back by reverting the adopting monorepo pin/caller change
through a reviewed PR and redeploying. Never run a competing publisher or bypass protection.

## Source and asset provenance

The initial source snapshot is from
[monorepo commit 830043b5b273229ec029cf51f41fb3b4b6471c0d](https://github.com/devantler-tech/monorepo/tree/830043b5b273229ec029cf51f41fb3b4b6471c0d/docs).
Original file history remains in that repository. Monorepo ADRs were not imported. The extraction
adds signed commits rather than rewriting historical commits without their signatures.
[Asset provenance](docs/src/assets/PROVENANCE.md) and [illustration prompts](docs/src/assets/editorial/PROMPTS.md)
distinguish generated artwork from real portraits, captures and authored diagrams.

Company/contact completion remains [monorepo#3917](https://github.com/devantler-tech/monorepo/issues/3917).
There is no payment flow or client portal here; `client-portal` is a separate application product.
