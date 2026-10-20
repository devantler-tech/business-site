# Bounded private contact renderer

`internal/contactrenderer` is the developer-only process boundary for
[ADR 0014](../docs/adr/0014-private-contact-renderer-experiment.md). The explicit
`Foundation.DeveloperContactBrowser` composes it with the real contact handler;
the caller owns and closes the supervisor. Ordinary `Foundation.Browser`, the
executable, public site and production runtime package retain Go presentation.
Native Go writes and durable schemas are unchanged. It does not establish adoption,
performance, maintenance improvement or portal launch readiness.

## Ownership and bounds

Construct `New(nodePath, workerPath, flags)` with fixed absolute deployment-owned
regular files and the maintained OpenFeature client. Construction is passive.
There is no shell, PATH lookup, listener or request-selected executable.
Only a healthy SDK evaluation of `portal-contact-renderer`, with default false,
can start work. Absent, wrong-type, error, stale or nil SDK states admit no child.
The gate is checked again before releasing a complete result. The lifecycle task,
issue #207, requires removal or an explicit reviewed decision by 2026-10-26.
This does not extend the experiment's evidence window.

- At most one owned child exists, including during failure and restart/reaping.
- Eight operations include gate waits, pending messages and queued writes; a
  ninth gets `ErrUnavailable` immediately, without an additional waiting queue.
- Every call has a 250 ms total budget including cold start, capped by any earlier
  caller deadline. A provider that ignores cancellation can retain at most those
  eight operations and their capacity until it returns; caller deadlines still
  finish and no expired evaluation may later start a child.
- The existing strict codec bounds complete requests to 8 KiB and responses to
  64 KiB, validates a prefix before allocation, and admits only the closed
  display projection. Each request gets a random 128-bit lowercase-hex ID.
- One writer serializes frames. One reader correlates complete responses to
  current pending IDs. Unknown, repeated, late, malformed, oversized or truncated
  responses invalidate the generation and all its still-pending output.
- Deadline/cancellation after registration, child exit and `Close` invalidate the
  generation, close its anonymous pipes and kill its exact owned process.
  Exactly one waiter reaps it before a new generation is possible.
- Restarts occur only on a later eligible call, at most once per second, never
  through a background retry loop. `Close` is idempotent and waits for reaping.

The child receives only `LC_ALL=C` and `LANG=C`, not host secrets, Node options,
credentials or access tokens. Child stderr is discarded; every enabled failure
returns only `ErrUnavailable`, with no body or private cause. A closed gate
returns `ErrDisabled` for the retained Go presentation, not an enabled-failure
success fallback. `ErrDisabled` applies only before work is admitted; a gate that
closes or becomes unhealthy during rendering discards the body and returns
`ErrUnavailable`. This is resource/lifecycle supervision, **not an OS sandbox**.
Only the fixed reviewed bundle may be configured here.

## Remaining HTTP and deployment obligations

The package returns a buffered body, never permission to send it. The developer
HTTP composition retains the original token, principal/deadline and query client
in Go, never in the projection. It buffers the entire Go-owned private frame,
rechecks displayed confirmation and identity/release gates, then reloads the
original durable session through SCS's context-aware store and codec, preserving
cancellation/deadlines without using the request cache. It rejects absent, expired
or replaced identity/deadline data and scopes final membership/session SQL to the
original client, maintained SCS hashed key and exact decoded byte snapshot.
Read-only revalidation never commits, refreshes or resurrects a session.
Initial renderer opt-out retains Go only after the same fresh authority check;
enabled errors discard all buffered bytes and return generic unavailable.
These are final checkpoints, not atomic wire-time revocation. Go retains redirects,
status, origin, CSRF, no-store, form destinations, sessions and all writes.
Renderer output is not an authority token or a general safe-HTML contract.

There is no environment activation map or production caller. `Check` re-evaluates
release state without starting a child and shares the same eight-operation/
250 ms SDK bound. The comparison
still needs an actual packaged Node executable and fixed compiler artifact,
independent two-client/operator HTTP holdouts, the unchanged preregistered paired
measurement harness, the frozen maintenance task and an actual rollback.
The normal Go container and static publisher are deliberately unchanged.
Issues #199, #142 and #131 remain open until their own outcomes are proved.

## Exercise

Use the exact Go toolchain in `go.mod`:

```sh
GOTOOLCHAIN=local go test -race ./internal/contactrenderer -count=1 -timeout=30s
```

These use real owned child processes for lifecycle and protocol failures,
reversed concurrent replies, shutdown, cancellation, throttling, SDK state and
post-wait revocation, capacity with an ignoring provider, and environment
isolation. They do not need Node or a database.

From the repository root with Node 24, npm 11, both existing dependency locks
installed, the exact Go compiler, and BOTH [disposable database fixture
URLs](README.md#actual-database-tests) already provisioned:

```sh
LC_ALL=C node --test web/tests/private-contact-runtime.test.mjs
```

That test builds the actual Svelte artifact in an owned temporary directory,
exercises its real pipe rejection, and explicitly selects the Go
`rendererintegration` supervisor and authenticated HTTP tests with that exact Node
executable and worker, restricted PostgreSQL and synthetic TLS broker. Missing
artifacts fail rather than skip. Ordinary Go/database tests are not coupled to
ambient Node. Source CI runs the selected integration unconditionally after
database provisioning in its existing Node-24/Go job. No private output is uploaded
as a public artifact. Synthetic client/operator proof is not live federation,
production ingress or authorization to collect customer data.
