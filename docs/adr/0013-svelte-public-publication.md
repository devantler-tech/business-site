# Svelte public publication with protected artifact recovery

## Decision

The existing source-owned Pages publisher selects the complete Svelte public artifact by default
for main pushes, daily ranking refreshes and manual publication. Manual dispatch still defaults to
artifact-only preview. Explicit `frontend=astro` retains a reviewed recovery option; it does not
bypass repository, main-ref, event, publication-switch or protected-environment checks. Locally,
`FEATURE_SVELTE_FRONTEND` remains false by default so both rendering paths remain testable.

The production-profile Go package explicitly selects Svelte through the same validated build and
keeps its established `runtime-production-dist/` output and image/archive/signature chain. The
ordinary Astro package/container and separate default-profile Svelte package remain comparison
controls. None deploys or activates private portal routes. Identity, company disclosure, prices,
customer scope, offer copy and VAT keep their own configuration and release prerequisites.

The input-bound Astro reading/reference bridge is retained. Removing it, the comparison path or
the temporary frontend gate requires #125's separate review; public activation is not retirement.

## Freeze before activation

Issue #193 records the comparison baseline: source
`3dff578a32e907bf7bebc5153f78c71945b379e4`, successful publication run `39167475865`, build job
`117793335808`, deployment job `117793541843` and Pages artifact `12151097971` with service digest
`sha256:5030f7da73aff03ab6d078cdbafd09778c999ce37fe3885e10a64252aff7b6b3`.
The retained baseline is Astro, company identity on, other unreleased offer/customer/VAT flags off.
The Pages resource remains source-owned with canonical domain `devantler.tech`, HTTPS and a
main-only protected environment. No domain transfer or monorepo publisher is involved.

## Exercise rollback, then restore

This is an operator procedure with production impact, not a local build or an automatic fallback.
First require the intended release's successful protected deployment, current live source/run
receipt and complete visitor proof. Ensure no other publication is active and that the frozen
artifact still exists and is unexpired. Pages artifacts have a short retention period; the frozen
one expires on 19 October 2026. Expiry requires freezing a new verified baseline, never rebuilding
old source and calling its mutable ranking input the original artifact.

Use GitHub's supported **single-job rerun** on the frozen successful deployment job, not its build
or the whole workflow. The pinned deploy action reads its own workflow run's retained artifact.
Original source SHA/ref and main-only environment protection remain in effect. Verify that only
the deployment reran, then verify live Astro output and the original artifact's publication receipt.
That receipt identifies the **build attempt**; a deployment-only rerun must not rewrite it or claim
a fresh build. Record the new deployment attempt separately on #193.

Restore using a single-job rerun of the intended Svelte release's successful deployment job,
after rechecking its retained artifact and absence of competing publication. Verify that deployment
and the intended original build receipt, EN/DA journeys, local resources, PDFs and discovery all
agree. Do not close #193 or #130 until both actual rollback and restoration have been observed.
If the artifact is unavailable or the protected rerun refuses it, stop; use a separately reviewed
source revert/restoration instead of weakening protections or adding an arbitrary-artifact deployer.

References: [GitHub job reruns](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/re-run-workflows-and-jobs?tool=cli)
and [the pinned deployment's artifact selection](https://github.com/actions/deploy-pages/blob/368f82528645a54fb793d4d04e342629a3f51346/src/internal/api-client.js).
