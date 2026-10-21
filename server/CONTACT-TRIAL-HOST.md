# Launch the private contact trial host

`cmd/contact-trial-host` runs the maintained Go identity/contact authority in a
separate disposable process for TLS, isolation and restart regression checks.
It supports only Go presentation and closed identity. It is not a comparison
collector or a deployed portal.

From `server/`, build it with the exact compiler from `go.mod`:

```sh
GOTOOLCHAIN=local go build -o /private/output/contact-trial-host ./cmd/contact-trial-host
```

The controller invokes `contact-trial-host --private-contact-trial`, supplies one
JSON object through a pipe on standard input, and passes its retained TCP listener
as descriptor 3. The command refuses implicit activation, ordinary files as setup,
unknown/duplicate/missing fields, trailing input and setup over 64 KiB. Input must
finish within five seconds. No setup values or raw dependency errors are printed.
The matching TLS key pair must identify the callback hostname, be currently
valid and permit server authentication before startup can announce readiness.
The controller pins this local application certificate; broker trust is validated
separately and system trust roots are not consulted for this local setup check.
After successful assembly, standard output contains only `contact-trial-host ready`.
SIGTERM/interrupt drains the owned server; a ten-minute process lease also bounds
the host. Lease expiry fails rather than becoming a successful trial.

## Controller contract

The closed object has seven required string fields. Unknown fields, including
the retired `node` and `worker` fields even when empty, are refused.

| Field                | Required value                                                                                                                                                                                                   |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `mode`               | `closed` or `go`; every other mode is refused                                                                                                                                                                    |
| `database`           | Synthetic `portal_runtime` login with the documented fixture password; numeric loopback port 5432 or 55444; a fresh `portal_trial_` database followed by 32 lowercase hexadecimal digits; only `sslmode=disable` |
| `issuer`             | Fixed HTTPS numeric-loopback origin with an explicit port and no path/query                                                                                                                                      |
| `callback`           | HTTPS numeric-loopback origin matching the inherited listener, with path `/portal/callback` only                                                                                                                 |
| `certificate`, `key` | PEM certificate and matching private key for that local application origin                                                                                                                                       |
| `trust`              | PEM trust roots for the synthetic TLS broker                                                                                                                                                                     |

Only the controller prepares/migrates the disposable database, issues invitations
and operates the synthetic broker. Runtime startup checks both effective and
session database identity plus `identity.CheckRuntimeRole` before discovery. It
never runs DDL or fixture resets. In particular, ordinary test-reset TRUNCATE grants
are unsuitable for this host. Use the restricted grants in the real process test.

The controller retains its original listening descriptor throughout restarts;
each child owns only a duplicate. It must not accept requests itself. This keeps
one kernel-bound origin without closing a reservation and hoping to rebind it.
Retain the TLS certificate, issuer, database and independent actor cookie jars too.
Restarting `go` retains the same authority and durable data. The host exposes no invitation, role-selection,
migration, broker or collection HTTP endpoint.

An isolated maintained OpenFeature provider enables only identity in `go`
and keeps it false in `closed`. All other capabilities remain false-default.
This host constructs only the ordinary Go browser; it accepts no renderer
executable or worker. Broker HTTP transport admits only the configured
numeric-loopback destination, ignores proxy environment settings and follows no
redirects. Normal `cmd/business-site`, public builds and production packaging do
not import this host or acquire a Node dependency.

## Correctness proof versus comparison evidence

The explicit Go integration suite builds and starts the actual command using a separate
restricted disposable database. It checks closed behavior, the maintained complete
three-actor native-form journey, independent logout, saved session/record/revision
through a same-origin process restart and a genuine forbidden-grant refusal.
Its broker/controller stay outside the child. Actual-command refusal controls
cover regular-file/stalled setup, invalid/wildcard/mismatched listeners and
incorrect broker trust. A parseable application certificate for an unrelated
hostname, an expired/not-yet-valid leaf, and a client-auth-only leaf must each be
refused before readiness; the valid certificate remains the positive control
for the complete authenticated journey. These are correctness
controls, not paired timing or service-RSS observations. From `server/`, with the
documented disposable fixture,
run `go test -race -tags=contacttrialintegration ./internal/identity -run
'^TestContactTrialHost' -count=1 -timeout=90s`. Source CI requires this dedicated
step after provisioning, keeping its expensive process startup outside the ordinary
package's unchanged 90-second budget without omitting any controls. Neither this
Go integration nor the ordinary suite requires Node. Actual-command refusals also
cover the retired candidate mode and legacy executable fields. Separate renderer
HTTP and authority controls remain under [CONTACT-RENDERER.md](CONTACT-RENDERER.md);
they do not use this Go-only process host.

[ADR 0017](../docs/adr/0017-retain-go-private-presentation.md) records the private
candidate's rejection on its mandatory maintenance objective. The frozen plans,
input identities and offline evidence remain historical records; the unperformed
paired timing, resource and candidate-recovery measurements are not passing results.
This Go-only regression host neither renews that experiment nor activates production
identity, customer intake or payments. Issue #207 tracks the remaining
developer-renderer retirement.
