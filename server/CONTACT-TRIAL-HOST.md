# Launch the private contact trial host

`cmd/contact-trial-host` runs the maintained Go identity/contact authority in a
separate process for the private presentation experiment. It is a developer-only
building block, not the complete comparison runner or a deployed portal.

From `server/`, build it with the exact compiler from `go.mod`:

```sh
GOTOOLCHAIN=local go build -o /private/output/contact-trial-host ./cmd/contact-trial-host
```

The controller invokes `contact-trial-host --private-contact-trial`, supplies one
JSON object through a pipe on standard input, and passes its retained TCP listener
as descriptor 3. The command refuses implicit activation, ordinary files as setup,
unknown/duplicate/missing fields, trailing input and setup over 64 KiB. Input must
finish within five seconds. No setup values or raw dependency errors are printed.
After successful assembly, standard output contains only `contact-trial-host ready`.
SIGTERM/interrupt drains the owned server; a ten-minute process lease also bounds
the host. Lease expiry fails rather than becoming a successful trial.

## Controller contract

The closed object has nine string fields:

| Field | Required value |
| --- | --- |
| `mode` | `closed`, `go` or `svelte` |
| `database` | Synthetic `portal_runtime` login with the documented fixture password; numeric loopback port 5432 or 55444; a fresh `portal_trial_` database followed by 32 lowercase hexadecimal digits; only `sslmode=disable` |
| `issuer` | Fixed HTTPS numeric-loopback origin with an explicit port and no path/query |
| `callback` | HTTPS numeric-loopback origin matching the inherited listener, with path `/portal/callback` only |
| `certificate`, `key` | PEM certificate and matching private key for that local application origin |
| `trust` | PEM trust roots for the synthetic TLS broker |
| `node`, `worker` | Empty for `go`/`closed`; fixed absolute packaged executable/worker paths for `svelte` |

Only the controller prepares/migrates the disposable database, issues invitations
and operates the synthetic broker. Runtime startup checks both effective and
session database identity plus `identity.CheckRuntimeRole` before discovery. It
never runs DDL or fixture resets. In particular, ordinary test-reset TRUNCATE grants
are unsuitable for this host. Use the restricted grants in the real process test.

The controller retains its original listening descriptor throughout restarts;
each child owns only a duplicate. It must not accept requests itself. This keeps
one kernel-bound origin without closing a reservation and hoping to rebind it.
Retain the TLS certificate, issuer, database and independent actor cookie jars too.
Changing only `mode` to `go` restores maintained Go presentation over the same
authority and durable data. The host exposes no invitation, role-selection,
migration, broker or collection HTTP endpoint.

An isolated maintained OpenFeature provider enables only identity in `go`,
identity/contact presentation in `svelte`, and neither in `closed`. All other
capabilities remain false-default. Candidate composition owns and closes the
existing bounded supervisor. Broker HTTP transport admits only the configured
numeric-loopback destination, ignores proxy environment settings and follows no
redirects. Normal `cmd/business-site`, public builds and production packaging do
not import this host or acquire a Node dependency.

## Correctness proof versus comparison evidence

The ordinary database suite builds and starts the actual command using a separate
restricted disposable database. It checks closed behavior, the maintained complete
three-actor native-form journey, independent logout, saved session/record/revision
through a same-origin process restart and a genuine forbidden-grant refusal.
Its broker/controller stay outside the child. These are correctness controls,
not paired timing or service-RSS observations. The ordinary suite does not require
Node; actual compiled Svelte behavior remains separately covered by the explicit
renderer controls in [CONTACT-RENDERER.md](CONTACT-RENDERER.md).

Issue #213 still owns immutable source/harness receipts, packaged Node provenance,
the identical maintenance task on both sides, three ordered paired runs, all raw
failures, 100-ms process-tree RSS, clean/cold build observations, browser/holdout
proof and candidate-to-Go recovery using the original actors and saved data.
Freeze/review the complete runner before collecting comparative samples. This
host does not settle the frontend choice, extend the registered window, claim a
budget pass or activate production identity, customer intake or payments.
