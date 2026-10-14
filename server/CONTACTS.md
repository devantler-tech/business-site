# Confirm a saved contact email

An invited client can explicitly ask the configured sign-in provider to confirm
the email already saved in their private contact profile. This latent capability
requires both `portal-identity` and independently false-default
`portal-contact-verification` through the injected OpenFeature SDK. Missing, false,
wrong-type, failed or unhealthy evaluations keep confirmation closed. The existing
environment provider maps identity only; it enables no contact-confirmation gate.

## Client path and proof

Save the contact profile, then choose **Confirm with your sign-in provider**.
The protected native form carries an expected revision, not an email, client ID,
role or verification claim. The server snapshots the saved client/address/revision
with the current issuer and subject in an expiring, single-use database attempt.
Only this flow requests `openid email` and the email claims in the signed ID Token;
ordinary sign-in still requests only `openid` and never confirms a contact.

The actual callback consumes the contact-purpose attempt before the maintained
code exchange, PKCE and token verification. Both consumers filter the stored
purpose; a browser-session marker only selects which consumer to invoke.
Signature, issuer, audience, authorized party, nonce and expiry remain mandatory.
Confirmation additionally requires the same issuer/subject and a literal boolean
`email_verified=true` with an exact email match. No case folding, replacement,
email identity matching or account linking occurs. Missing, malformed, false or
different claims leave the address unconfirmed and offer a localized recovery path.

The standard verified-email claim describes the provider's assertion of control
at its verification time. A fresh nonce-bound assertion is not a delivered-mail
challenge, proof of present deliverability or proof of fresh interactive login.
See [OpenID Connect standard claims](https://openid.net/specs/openid-connect-core-1_0.html#StandardClaims).
An address different from the provider-confirmed email remains unverified; another
verification method requires a separate delivery/data-processing decision.

## Persistence and authority

The separate migration role applies checksummed v8 after unchanged v1–v7.
Existing attempts retain ordinary sign-in purpose. The runtime table privilege
set is unchanged; no new service, dependency, table ownership or DDL is granted.
Keep confirmation off until every serving private runtime supports v8. Once a
contact has a confirmation timestamp, an old v7 runtime cannot clear that timestamp
on an edit and the provenance constraint refuses its write. Rolling back to that
runtime after confirmation is therefore not a transparent edit-compatible rollback.
Current client membership is locked while the exact contact address/revision is
compared. Success records a confirmation timestamp and advances the revision.
Flags, SDK readiness, token/session expiry and cancellation are checked after
database and SDK waits before commit. Concurrent/replayed callbacks cannot
confirm twice. These are authorization checkpoints, not atomic wire-time
revocation or the ability to retract a response already received.

Any contact-profile edit clears confirmation and its timestamp. A recovered
unsaved draft cannot inherit confirmation from a different saved value.
The optional control is evaluated before the private read's final authorization
checkpoint; rendering introduces no later provider wait over private data.
Confirmed email never changes sign-in identity, assigns operator access, approves
an agreement/payment or grants worker execution authority.

## Validation and release

Use the [actual disposable database suite](README.md#actual-database-tests), including
the real TLS broker/callback path, purpose/replay controls, historical upgrade,
contact changes, current client access and late SQL/SDK checkpoint failures.
The rendered-link contract covers enabled, confirmed and recovery forms in EN/DA.
Native browser checks use only the bounded synthetic visual fixture, not real
Google/Microsoft accounts or customer data.

Business-site#93 reviews the temporary gate by 14 November 2026 after actual rollout
evidence. A source merge, signed runtime image and public Pages receipt do not
deploy private handlers or authorize customer collection. Production broker and
connector claim semantics, two-client/operator isolation, privacy/retention,
recovery and deployment remain open under monorepo#3936/#3939 and business-site#19/#21.
