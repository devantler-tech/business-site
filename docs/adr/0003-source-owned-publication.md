# Source-owned website publication

Status: completed — resource/domain cutover and old monorepo publisher retirement verified
10 October 2026 in [#25](https://github.com/devantler-tech/business-site/issues/25).

## Current ownership

`business-site` owns the Pages resource and `devantler.tech` domain. Its source-owned
`publish-site.yaml` is the active push/manual/scheduled publisher; the enabled source switch and
protected main-only environment still gate deployment. On 12 October, Pages readback showed
workflow publication, the domain and enforced HTTPS, with one deployment branch policy for
`main`. The live receipt matched successful source-owned
[run 38189479066](https://github.com/devantler-tech/business-site/actions/runs/38189479066),
source `70775006afa71b8fbd5144269cdb74676304e3d6`, attempt 1 and publish mode.

[Monorepo #4082](https://github.com/devantler-tech/monorepo/pull/4082) retired its old publisher.
The former reusable `publish-pages.yaml` is retired from the default branch; historical immutable
callers still resolve its reviewed Git revision. Its legacy receipt has a `callerRevision`;
the source-owned receipt instead records `workflowRunId`, `workflowRunAttempt` and
`publicationMode`. Historical caller contracts do not establish current ownership or authorize
a second publisher or another domain transfer. The cutover procedure below records the accepted
migration and rollback constraints; it is not outstanding setup work.

## Context and decision

The business-site repository owns the website and unified portal source. Publication
must not require a monorepo source pin or caller. The static website stays on GitHub
Pages; this does not deploy the private portal backend, identity or payment workflow.

`publish-site.yaml` checks out its own exact main-event SHA, refreshes the complete
public portfolio input, builds production output and records the source SHA and
workflow run/attempt. Main pushes and daily refreshes deploy only when the repository
variable `SITE_PUBLICATION_ENABLED` is exactly `true`. Initially it is absent.
Manual dispatch defaults to `preview`, which uploads the same artifact without any
deployment. Preview and production have separate cancelling concurrency groups.

The protected `github-pages` environment must admit only branch `main`, not tags.
Build authority is contents-read; only the dependent deployment gets Pages-write
and OIDC. The existing immutable reusable publisher remains operational during
the cutover and is retired only after replacement live proof.

## Cutover and rollback

1. Merge the reviewed workflow while the switch is absent. Dispatch main in preview
   mode. Inspect the successful artifact, production EN/DA routes and its exact-source
   receipt; preview is not publication proof.
2. Create the business-site Pages resource with `build_type: workflow` and a
   `github-pages` environment with a custom branch rule for `main` only.
   Read the settings back. Preserve existing protections and the verified domain.
3. Verify no old publication is active. Temporarily disable the monorepo publication
   workflow during this controlled transfer, retaining its code and rollback source.
   Set the source-owned switch to `true`, dispatch main in publish mode, and verify
   a successful source-owned Pages deployment and receipt at the repository Pages URL.
   The staging project URL is not a complete custom-domain route proof: the build's
   canonical origin and root-relative assets remain `https://devantler.tech`.
4. Record both repositories' Pages settings and the exact last-good deployment before
   changing them. Remove only the monorepo custom-domain association, immediately set
   business-site's custom domain to `devantler.tech`, and enforce HTTPS once its
   certificate is ready. DNS is unchanged. If ownership/certificate/live verification
   fails, turn the new switch off, restore the monorepo domain/HTTPS settings, re-enable
   its workflow and redeploy the recorded last-good source. Confirm restored live
   receipt and routes. Never disable a verified live owner permanently without a replacement.
5. Verify business-site owns the domain and successful deployment, and that live HTTPS
   EN/DA home, projects, Journal, legacy routes and the source/run receipt all agree.
   Only then merge independently reviewed monorepo publisher/implementation cleanup.

The one-item delivery-capacity exception approved by the maintainer applies to #25,
not signing, CI, review, protection or production evidence. Both ownership transfer
and cleanup must finish before the migration issue is closed.

References: [GitHub custom Pages workflows](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages),
[custom-domain management](https://docs.github.com/en/pages/configuring-a-custom-domain-for-your-github-pages-site/managing-a-custom-domain-for-your-github-pages-site)
and [Pages API](https://docs.github.com/en/rest/pages/pages).
