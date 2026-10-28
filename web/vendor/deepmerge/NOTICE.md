# Temporary frontend build dependency correction

This is the source of deepmerge 4.3.1, with a local correction for
[GHSA-gcqx-w48m-w2h4](https://github.com/advisories/GHSA-gcqx-w48m-w2h4),
tracked in business-site #398. It is used by the Svelte build configuration,
not shipped as a portal authentication or payment implementation.

The complete upstream MIT terms are retained verbatim in `LICENSE`. Upstream
material keeps those terms. Locally authored modifications retain the repository's
existing terms; this directory introduces no additional own-code license grant.
The package is private and is not published to a registry.

## Upstream identity

- Source release: `TehShrike/deepmerge` at
  `5b87756a5671635679001cbac72aa42f23472c81` (4.3.1).
- Registry package: `https://registry.npmjs.org/deepmerge/-/deepmerge-4.3.1.tgz`.
- Complete package integrity:
  `sha512-3sUqbMEc77XqpdNO7FRyRog+eW3ph+GYCbj+rK+uYyRMuwsVy0rMiVtPn+QJlKFvWP/1PYpapqYn0Me2knFn+A==`.
- Original complete `index.js` SHA-256:
  `12440c84957c1990ea2d5e33dc0091722dc1dc1585c6d20e0c780e568ffa9d30`.
- Complete upstream license SHA-256:
  `6cfc4687cb2f2d86f4a77e6b526290d3878e5e512f3fec2f4cb36a9cb36f798b`.

## Local modification, 28 October 2026

The original CommonJS source is named `index.cjs` to make its format explicit.
The repository's native formatting is applied to the modified source.
Both object-copy paths reject `__proto__`, `constructor` and `prototype` by key
name. Values are defined as ordinary own properties without invoking a prototype
setter. Array, symbol, custom-merge and cloning behavior otherwise stays with the
upstream implementation. The unmodified source's `is-mergeable-object` dependency
is selected at its original exact 1.1.0 version rather than hand-editing the
upstream generated bundle.

Native npm file-package resolution and a root override select this private
package for the real Svelte consumer. Its local identity is deliberately distinct
from the registry release: the registry audit alone cannot establish its safety.
The normal frontend test command resolves from the actual Svelte plugin, checks
the installed source and complete terms, and exercises the formerly failing
prototype cases as well as normal configuration merging. The full registry audit
remains enabled.

The retained Svelte comparison fixture uses this same package source and runs
the same controls through its own installed Svelte consumer. It keeps a separate
native lockfile; a passing public frontend does not clear that comparison graph.

Remove this temporary package and override once a supported upstream release
corrects the finding and passes those same consumer-level controls and the
complete frontend release checks. Do not replace it with an audit exception or
rename an unpatched copy to make a scanner silent.
