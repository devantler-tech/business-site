# 20. Exact public staging hostname

Date: 2026-10-23

## Decision

The public-only deployment package proposes exactly
`business-site.platform.devantler.tech`. This supersedes only ADR 0019's inert
`business-site.example.invalid` destination. The package validator admits the
literal staging name, not arbitrary subdomains, wildcard names or the production
website address. The existing namespace, verified baseline image, runtime
arguments, resource limits, disruption budget and private-input restrictions
are unchanged. The portal remains disabled.

Platform main `540a49287ae9e8c1551458ee6781da1b5587eb5d` provides the namespaced
WebApp graph and a shared wildcard HTTPS listener, but its hostname policy
denies unlisted tenant routes. Its current consumer list does not register this
application. Therefore the name above is a source-owned proposal, not an existing
route approval or a deployment. Do not infer DNS, certificate, KRO-controller,
kubelet or live reachability evidence from the manifest.

## Release and activation boundary

The previous v1.0.1 runtime pair was independently verified under #276 and #102;
it still packages the inert hostname. This change needs its own normal protected
merge, original main-CI receipt, genuine source-owned release and independently
verified signed pair before Platform may consume it. The manual publication
switch remains off outside that bounded release procedure.

Platform registration, exact hostname admission, same-namespace disclosure input
materialization and live public-only staging acceptance require a separate
reviewed change under #102 and monorepo#3939. That change must verify the actual
accepted publisher, paired digests, controller/projection/network behavior and
rollback. Pages remains authoritative at `devantler.tech`; this decision changes
neither DNS nor the production publisher and enables no sign-in, database,
customer intake or payments.

## Alternatives and verification

Keeping the example name leaves the next staging deployment without a usable
destination. Accepting any hostname or the production name would widen the
package's authority before the separate cutover decision. A local-only
`.platform.lan` name would not provide the remotely reachable staging outcome.
None is selected.

Behavioral controls run the real Kustomize renderer and admission command. They
require the exact staging name and refuse the old example, production names,
wildcards, suffix lookalikes, nonliteral spellings and other tenants' names with
no rejected-input disclosure. Existing restricted-image and disabled-portal
checks remain mandatory. These checks prove source/package behavior, not actual
Platform admission or live staging.

## Amendment — portal host, 13 November 2026

The maintainer chose `portal.devantler.tech` as the portal's address (#554). The
public site stays on GitHub Pages at `devantler.tech`. Moving it off Pages is a
separate, later step, and no longer gates the portal.

The package now proposes exactly `portal.devantler.tech`, replacing
`business-site.platform.devantler.tech`. Its validator refuses the old staging
name, the production website address, wildcards and lookalikes.

The runtime gains one option, `--portal-only`, which the package sets:

- `/` redirects to `/portal/`;
- every other public path redirects to the same path on `https://devantler.tech`;
- `/portal…` and `/healthz` are served as before.

The redirects are temporary (302) and not cacheable, so later hosting changes
stay cheap. Unsafe paths still miss rather than redirect, and the portal itself
stays default-off.

Platform route, TLS and the Tenant re-pin are devantler-tech/platform#6177.
external-dns creates the DNS record from the route.
