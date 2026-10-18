# One verified public artifact for publication and hosting

Status: integration prerequisite #194 for the separately reviewed cutover #193.
Extends [0011](0011-isolated-svelte-frontend.md); does not activate its candidate.

## Decision

Keep `npm run build` and canonical output `dist/` for all consumers. Resolve the existing
framework-native `FEATURE_SVELTE_FRONTEND` setting with Vite's production environment loader and
the same strict parser as the renderer. False/unset retains Astro. Explicit true builds the
incumbent reading reference, then Svelte, and validates the actual independent artifacts before
replacing generated output. The reference invokes the explicit Astro builder, not selected
publication. Both pinned dependency installs are now required for the common build.

All previous incumbent checks remain. Selection compares every actual HTML route and local
HTML/CSS dependency, retains full content/download/feed/discovery checks and refuses symlinks,
private output and copied publication receipts. It carries only canonical CNAME and the two inert
identity-off redirects from the reference; it never copies a missing content page or asset tree
over Svelte. Svelte owns a non-hydrated bilingual-link 404 document. A failed selected build removes
its generated `dist/` rather than leaving the reference looking like successful selected output.
Older presentation variants absent from Svelte fail closed; company/offer/customer/VAT/profile
settings retain their separately validated behavior. None authorizes customer intake.

After the native adapter replaces its output, it captures a private complete path/digest inventory.
A fresh caller-generated nonce binds that inventory to the actual selected build. Independent
selection rejects extra, missing, changed and older-build files; neither inventory nor nonce enters
public output. Descriptor-bound no-follow reads and directory identity checks protect the snapshot
from replaced entries. Resource link relations are matched as case-insensitive token lists.

## Consumer boundaries

The source-owned publisher permits selected Svelte only for an artifact-only manual main preview.
Its existing source/run receipt, immutable checkout, protected production environment and separate
preview concurrency remain. Production push/manual/schedule builds explicitly select Astro.
Svelte production dispatch is rejected before checkout; this is not a production switch.

The Go packager's optional third `--svelte` argument explicitly selects the same path and writes a
separate candidate package. Its defaults remain pinned to Astro despite ambient/dotenv settings.
Compiled binary identity, public digest, notices, clean-source release requirement and actual
portal-off serving tests remain unchanged. No new Go authority or deployment is introduced.

## Exit

Issue #193 still owns production activation, complete live EN/DA journeys and an executed protected
rollback/restore. #130 remains open until those outcomes are verified; #125 still owns retirement
only afterward. This increment does not remove the input-bound reading bridge, retire Astro,
activate private routes or change legal/company/payment facts, prices, DNS or hosting services.

## References

- [Astro CLI and build output](https://docs.astro.build/en/reference/cli-reference/)
- [Vite environment loading](https://vite.dev/guide/env-and-mode.html)
- [GitHub manual workflow inputs](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#onworkflow_dispatchinputs)
