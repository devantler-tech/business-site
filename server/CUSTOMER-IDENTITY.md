# Prepare customer sign-in configuration

The portal uses customer Dex, backed initially by an invitation-only Keycloak
customer realm. See [ADR 0021](../docs/adr/0021-customer-identity-provider.md).
The offline command prepares source inputs only. It creates no services, users,
credentials, DNS, mail, database or portal activation. Normal publication does not
run it or include its output.

From `server/`, using the exact compiler in `go.mod`:

```sh
GOTOOLCHAIN=local go run ./cmd/customer-identity-config
# No output unless preparation is explicitly requested.
GOTOOLCHAIN=local go run ./cmd/customer-identity-config --prepare \
  --dex-issuer=https://login.example.invalid \
  --keycloak-issuer=https://accounts.example.invalid/realms/customers \
  --portal-callback=https://site.example.invalid/portal/callback
```

The fictional endpoints deliberately cannot be used for public deployment. Supply
separately approved canonical HTTPS destinations with three distinct origins;
wildcard callbacks, query/fragment aliases, credentials, IPs and insecure URLs are
refused. The command emits one JSON object containing `keycloakRealm` and
`dexConfig`. Extract those two objects for their native consumers, not the wrapper.
Check the command's exit status: an interrupted or failed write is not a complete
package. No real endpoint is chosen or registered by this command.

## Private inputs and lifecycle

Keycloak imports the `CUSTOMER_KEYCLOAK_CLIENT_SECRET` placeholder into its durable
realm configuration. Dex expands the same variable for its upstream connector;
keep its maintained `DEX_EXPAND_ENV` behavior enabled. Its sole portal client reads
the **different** `CUSTOMER_PORTAL_CLIENT_SECRET` via native `secretEnv`. Project
that latter credential privately into the portal's existing supported secret-file
input; do not give the portal Keycloak administrative or database credentials.
Never commit expanded documents, account records, invite links or secret values.

Dex's separate PostgreSQL store needs `CUSTOMER_DEX_DB_HOST`,
`CUSTOMER_DEX_DB_USER`, `CUSTOMER_DEX_DB_PASSWORD` and a verified database CA at
`/run/customer-dex/database-ca.pem`. Its listener needs a matching certificate and
key at `/run/customer-dex/tls.crt` and `/run/customer-dex/tls.key`. Keycloak needs
its own durable PostgreSQL configuration, hostname/proxy policy, protected
administration and verified mail delivery; these are not fabricated by the preparer.
Missing expansion or private inputs must fail the runtime preflight rather than
fall back to development storage, development mode or unverified TLS.
That deployment preflight is not implemented by this offline command. Keycloak
can retain an unresolved placeholder when an environment variable is absent;
reject absent, empty or unresolved values before import. Use JSON-safe generated
credentials or proper JSON escaping when transporting private values, and verify
the persisted client and Dex agree without displaying either credential.

Realm import is initial provisioning, not continuous reconciliation or a backup.
Startup imports can skip an existing realm. Secret rotation requires updating the
actual persisted client, coordinating Dex, observing new logins and revoking the old
credential. Re-rendering a placeholder or restarting a skipped import is not proof
of rotation. Never overwrite a populated realm to apply a configuration update.

Administrators provision approved users without a pre-shared password or an assumed
verified email. Require a real email address: enabling email verification alone
does not establish that an account has one. Send a bounded required-action link for email verification,
password setup and TOTP enrollment. Verify actual link delivery, expiry, replay,
later MFA enforcement, reset and lost-factor handling before inviting real people.
The initial browser flow requires password and OTP on each upstream login; it
deliberately does not reuse a Keycloak browser SSO cookie.
Do not create customer accounts or send mail from this source-preparation command.
The portal's separate durable invitation and membership must still authorize the
authenticated Dex subject; provisioning a Keycloak user grants no portal access.

## Validation and remaining proof

```sh
GOTOOLCHAIN=local go test -race ./internal/customeridentity ./cmd/customer-identity-config -count=1 -timeout=90s
```

The ordinary module-wide inventory and both complementary test cohorts include
these packages automatically. Tests exercise the command's native payloads,
default-off output, unsafe input refusals and failed output. They do not prove a
running Keycloak/Dex chain, mail, MFA, browser login or controller convergence.
Parent #302 retains those activation requirements. Google, Microsoft, Apple and
GitHub configuration is absent by design from this first slice.
