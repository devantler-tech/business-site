# Customer identity through Dex and self-hosted Keycloak

- Status: Accepted for source preparation; deployment and activation require separate proof.
- Date: 2026-10-23
- Scope: business-site #302 and #303; monorepo #3936 and #3939 remain open.

## Decision

The portal trusts a dedicated **customer Dex** issuer. Dex initially has one OIDC
connector to a dedicated **Keycloak customer realm**. Invited people use local
Keycloak accounts; public registration and social connectors are disabled.
Infrastructure identity, administrators, clients, secrets and stores remain separate.
All business-specific source belongs in this repository, not the monorepo or Platform.

Keycloak supplies maintained account administration, credentials, MFA, recovery,
OIDC and future federation. The portal does not implement these mechanisms. It
continues to own its invitations, current membership, client isolation and operator
permissions independently of a successful login. Identity is the verified Dex
issuer and subject, never email, an email domain or a Keycloak role/group.

## Considered options

| Option | Fit and trade-off |
| --- | --- |
| Keycloak | Selected: local accounts, required actions, MFA, recovery, administrative APIs and federation under Apache-2.0. Requires operating and updating a JVM service and its PostgreSQL store; no resource-efficiency claim is made without measurement. |
| authentik | Viable self-hosted alternative with invitation-gated enrollment flows and OIDC. Its server/worker deployment and separate enterprise feature boundary add no demonstrated benefit for this initial local-account scope. |
| ZITADEL | Viable API-oriented self-hosted or managed identity platform with local accounts and federation. Its AGPL-3.0/commercial licensing and distinct operational lifecycle need a separate adoption review; neither implies a mandatory paid plan for self-hosting. |
| Dex alone | Useful broker, not the selected full local-user administration and recovery system. Its static-password facility is not our customer directory. |
| Portal → Keycloak directly | Fewer components, but removes the stable broker boundary explicitly requested by the maintainer. |
| Hosted IDaaS | A possible future choice, not an initial supplier or subscription dependency. |

Selection is based on the required account lifecycle and standard interfaces, not
stars, market popularity or a claim that one product is universally best. No
license, support contract, paid enrollment or new vendor account is purchased.
Existing hosting is not free: measure capacity, upgrades, monitoring, two isolated
identity stores, backups and restore work, mail delivery and operator time before
deployment. Do not share database owners or tables with the portal or admin identity.

## First-release boundary

The explicit offline preparer emits an empty native realm and a native Dex
configuration; its default emits nothing. It adds no deployment resources, user,
mail, secret value, portal activation setting or public-site asset. A successful
preparation does not prove either upstream service accepts the configuration.
The formats were inspected against Keycloak 26.8.1 and Dex 2.46.0 source; executable
versions/images, provenance and vulnerability review belong to runtime adoption.

Keycloak administrators create only approved invitees with unverified email and
no pre-shared password. Expiring required-action email establishes control of the
address and requires credential setup and authenticator enrollment. Realm actions
default to verification, password setup and TOTP. The selected customer browser
flow requires password and OTP sequentially, without cookie, Kerberos or
conditional-factor alternatives. An account without an OTP must enroll before
authentication completes; enrollment alone is not the ongoing enforcement policy.
Public self-registration,
social/federation imports, implicit flow, password grants and client service accounts
are off. Recovery and re-enrollment require the same verified-mail and operator
controls, not a custom bypass. Validate link expiry/replay and every recovery path
against the actual deployed version before admitting real users.

The customer Dex upstream client accepts only its exact `/callback`. The portal
client accepts only the portal's exact `/portal/callback`. Both hops use code flow
and S256 PKCE, with confidential, independently rotated credentials. Dex receives
no platform scope, administrative role, groups or offline refresh-token grant.
Certificate and verified-email checks remain enabled. Dex identity tokens and
authentication requests expire after five minutes; its signing-key rotation is
six hours. Short identity tokens are
not a substitute for current portal membership checks.

## The cost of the extra hop

Dex preserves the portal's issuer boundary if Keycloak or a future provider changes,
but adds a service, durable state, key rotation and another availability dependency.
Portal logout does not imply universal upstream SSO logout. Disabling a Keycloak
account does not revoke an already-issued Dex token or portal session immediately.
Revoke portal membership/sessions explicitly for immediate denial, then disable the
upstream account and test the resulting behavior. Observe both links, failure modes,
expiry, replay and recovery rather than assuming end-to-end semantics from OIDC.

Retain the Dex issuer, client and `customer-keycloak` connector ID, and protect the
upstream immutable subject. Database recovery and destructive realm re-import can
change identity. Moving a social provider between Keycloak and Dex is not transparent:
it can produce another subject. Require deliberate, authenticated account linking
or a reviewed identity migration; never join by matching email.

## Later social login

Google, Microsoft, Apple and GitHub remain future providers. They may be connected
through Keycloak or directly through Dex after compatibility, consent, enrollment
and identity-linking review. Turning off self-registration does not prevent an
upstream broker from creating accounts through first-login flows. Invitation-only
portal authorization remains mandatory even after such a provider is added.
Apple membership and its form-post/rotating client-secret requirements are deferred,
not prerequisites for the local-account first release.

## Activation evidence

Keep portal access closed until a reviewed source-owned deployment proves isolated
workload permissions, TLS/discovery, signed claims and code exchange, actual invite
delivery and MFA/recovery, account revocation, two-client/operator separation,
restart/restore and privacy/retention behavior. Restrict administrative access and
provide private secret delivery and separate storage recovery. Do not widen tenant
Helm or RBAC authority to bypass missing generic Platform capabilities.

## Primary evidence

- [Keycloak administration](https://www.keycloak.org/docs/latest/server_admin/index.html): administered users, required actions, MFA, recovery and federation.
- [Keycloak runtime](https://www.keycloak.org/server/containers) and [Apache-2.0 license](https://github.com/keycloak/keycloak/blob/26.8.1/LICENSE.txt).
- [authentik invitations](https://docs.goauthentik.io/users-sources/user/invitations/) and [configuration](https://docs.goauthentik.io/install-config/configuration/).
- [ZITADEL self-hosting](https://zitadel.com/docs/self-hosting/deploy/overview) and [licensing](https://help.zitadel.com/zitadel-licensing-faqs).
- [Dex OIDC connector](https://github.com/dexidp/dex/blob/v2.46.0/connector/oidc/oidc.go), [configuration](https://github.com/dexidp/dex/blob/v2.46.0/cmd/dex/config.go) and [SQL storage](https://github.com/dexidp/dex/blob/v2.46.0/storage/sql/config.go).
