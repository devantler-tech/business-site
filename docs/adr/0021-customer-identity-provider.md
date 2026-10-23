# Customer identity through Dex and self-hosted ZITADEL

- Status: Accepted for source preparation; runtime and activation need separate proof.
- Date: 2026-10-23
- Scope: business-site #302/#303/#305; monorepo #3936/#3939 remain open.

## Decision

The portal trusts dedicated customer Dex, initially connected by OIDC to a dedicated
self-hosted ZITADEL instance. Approved invitees use local accounts. Public user and
organization registration, external and passwordless login are disabled initially.
ZITADEL's invited-user login requires password, MFA and address verification.
Social providers are future work.

Customer and infrastructure issuers, clients, secrets, administration and stores
remain separate. Business-specific source belongs in business-site. The portal owns
current invitations, membership and customer authorization independently of login.
Verified issuer+subject is identity; email, domains and group/role claims never grant
portal authority. No provider-specific SDK or password store belongs in the portal.

## Alternatives and evidence

| Option                          | Assessment                                                                                                                                                                                                                                               |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ZITADEL                         | Selected: maintained customer administration, invitations, recovery, MFA, OIDC and APIs; Go backend with self-hosted/managed options. Requires PostgreSQL and operational lifecycle management. Core licensing is AGPL-3.0 with commercial alternatives. |
| Authelia                        | Lightweight authentication/OIDC gateway; project-reported normal usage below 30 MB. Users live in a file or LDAP backend with no internal registration lifecycle. Additional directory/invitation machinery would be required.                           |
| Keycloak                        | Capable local accounts and federation under Apache-2.0. A JVM service with a documented recommended 2 GB container limit for smaller production deployments; not a measured footprint.                                                                   |
| authentik                       | Viable invitation-oriented alternative; server/worker operations and enterprise feature boundaries offer no demonstrated initial-scope advantage.                                                                                                        |
| Dex alone                       | The selected broker, not a full customer directory/recovery service. Static passwords are not our customer account store.                                                                                                                                |
| Portal directly to the provider | Fewer hops, but removes the broker explicitly requested by the maintainer.                                                                                                                                                                               |
| Hosted IDaaS                    | A future option, not an initial paid supplier dependency.                                                                                                                                                                                                |

Language and popularity alone establish neither security nor longevity. Prefer
standard interfaces, maintained lifecycle APIs, recoverable state and measured
complete-stack resources. ZITADEL's approximately 512 MB core sizing guidance,
Authelia's reported normal usage and Keycloak's recommended limit are different
metrics, not a comparative benchmark. No performance-superiority claim follows.

Use supported embedded Login V1 for current requirements. New v4 instances may
default to V2, so set both instance and application selection explicitly. V2 adds
a separate Next.js service, login-to-API credential and routing. Adopt it for a
concrete need, not by accident; never freeze unsupported releases to save memory.

Deploy ZITADEL independently and unmodified where possible. Review actual core,
login and tooling licenses and retain required notices/source obligations. Vendor
guidance distinguishes API integration from modifying/combining its AGPL code;
this is not legal clearance for future customization or resale. No paid license,
enrollment or new vendor account is purchased here.

## Source and provisioning boundary

The offline preparer emits ZITADEL configuration, setup steps, an application API
request template and Dex configuration. It defaults to no output and is absent
from ordinary builds/publication. Formats target ZITADEL 4.20.0 and Dex 2.46.0;
this does not automatically admit production binaries/images.

Setup explicitly skips first-instance creation. Native merged defaults can
otherwise create an administrator; omitting `Human` is unsafe. Actual bootstrap
requires separately private fresh administrator inputs and verification of persisted
policies. Initial policy belongs in `DefaultInstance` configuration, not setup
steps. Existing instances require supported API updates, not rerender/restart.

The modern application API creates a confidential web client with code grant and
exact Dex callback, returning generated client credentials. Dex reads these private
references; the portal uses a different downstream credential. Source preparation
neither expands values nor creates users, projects or applications.

Dex sends S256 and reads UserInfo with issuer, subject and TLS checks. Its
`insecureSkipEmailVerified=false` requires a boolean claim but accepts a present
`false`; it does not enforce verification. Selected ZITADEL Login V1 enforces
address verification before authentication completes, including after a changed
address. UserInfo can reflect a later email change, so this is not a universal
true-only claim guarantee at Dex. Ordinary portal sign-in requests only `openid`
and uses invited issuer/subject membership. Only explicit contact confirmation
requests email claims and requires a signed `email_verified=true` for the exact
saved address/revision. Never grant contact proof or authority from ordinary login.

The upstream confidential client has no documented enforce-PKCE switch; do not claim
challenge omission is rejected without proof. Downstream Dex independently requires
S256. No password, implicit, administrative or offline portal grant is introduced.
Short tokens never replace current portal membership checks.

## Lifecycle and the extra hop

Native invitations establish email control, password setup and MFA. Never pre-share
customer passwords or assume verified email. Prove link expiry/replay, wrong factors,
subsequent MFA, recovery and deactivation with actual pinned services. Close public
organization creation as well as self-registration.

Dex preserves the portal issuer boundary but adds durable state, key rotation and
an availability dependency. Portal logout is local, not universal SSO logout;
upstream deactivation does not immediately destroy portal sessions. Current portal
membership/session revocation provides immediate denial. Exercise both boundaries.

Preserve the adopted Dex issuer, client, connector ID and upstream subject. Provider
or federation-path changes can change subjects. Migration/linking requires proof
of both identities, never matching email. Future Google/Microsoft/Apple/GitHub
integration also needs enrollment, consent, protocol and privacy review and must
not reopen automatic account creation. Apple membership is not required initially.

## Activation and measurement

Source preparation, native startup, observed browser acceptance, signed publication
and deployed acceptance are separate gates. A fictional native fixture must use
real ZITADEL, Dex and the maintained portal path with trusted TLS and no certificate
warning bypass. Synthetic brokers and rewritten cookies are not that proof. Keep
loopback fixture endpoint adaptations out of the strict production preparer.

Measure ZITADEL including embedded login, Dex, all PostgreSQL processes, portal and
any gateway/mail fixture at warm idle, during login and after repeated journeys.
Record exact versions, platform, workload, sample window and failures. Native RSS
is not a container limit or unique physical RAM: shared PostgreSQL pages can be
counted repeatedly. Include a separate login service if V2 is selected.

Before activation, prove source-owned immutable provenance, restricted workloads,
private credential delivery/rotation, exact TLS/routing, durable restart/restore,
real mail, invitation/MFA/recovery/revocation and two-customer/operator separation.
Resolve generic Platform prerequisites without broadening tenant Helm/RBAC
authority. Activate separately and reversibly only after these pass.

## Primary sources

- [Users and invitations](https://zitadel.com/docs/guides/manage/console/users-overview).
- [Supported Login V1 and V2 adoption](https://zitadel.com/docs/self-hosting/manage/adopt-login-v2).
- [Production requirements](https://zitadel.com/docs/self-hosting/manage/production) and [licensing](https://help.zitadel.com/zitadel-licensing-faqs).
- [Pinned bootstrap](https://github.com/zitadel/zitadel/blob/8afa5dd37a8e723efb9eace8d09fd5c1cccef694/cmd/setup/03.go) and [defaults](https://github.com/zitadel/zitadel/blob/8afa5dd37a8e723efb9eace8d09fd5c1cccef694/cmd/defaults.yaml).
- [Modern application API](https://zitadel.com/docs/reference/api/application/zitadel.application.v2.ApplicationService.CreateApplication).
- [Authelia account backends](https://www.authelia.com/configuration/first-factor/introduction/).
- [Keycloak container guidance](https://www.keycloak.org/server/containers).
- [Pinned Dex connector](https://github.com/dexidp/dex/blob/05a275057112ba93e17db21f600b756f000f1c21/connector/oidc/oidc.go).
- [Pinned native email-verification steps](https://github.com/zitadel/zitadel/blob/8afa5dd37a8e723efb9eace8d09fd5c1cccef694/internal/auth/repository/eventsourcing/eventstore/auth_request.go#L1119) and [current UserInfo email mapping](https://github.com/zitadel/zitadel/blob/8afa5dd37a8e723efb9eace8d09fd5c1cccef694/internal/api/oidc/userinfo.go#L218).
