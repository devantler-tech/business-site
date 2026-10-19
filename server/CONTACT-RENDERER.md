# Bounded private contact renderer

`internal/contactrenderer` is the developer-only process boundary for
[ADR 0014](../docs/adr/0014-private-contact-renderer-experiment.md), delivered by
issue #206. It does not change an HTTP handler, durable data, native Go writes,
the public site or the production runtime package. It does not establish adoption,
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

The package returns a buffered body, never permission to send it. Its future
HTTP caller must revalidate the **original** durable session, original client and
record scope, identity expiry, membership, and healthy identity/release gates
after waiting. A newly valid replacement identity cannot authorize an older
render. Go continues to own redirects, status, origin, CSRF, no-store, form
destinations, sessions and all writes. Renderer output is not an authority token.

There is no environment activation map or production caller. The comparison
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
installed, and the exact Go compiler:

```sh
LC_ALL=C node --test web/tests/private-contact-runtime.test.mjs
```

That test builds the actual Svelte artifact in an owned temporary directory,
exercises its real pipe rejection, and explicitly selects the Go
`rendererintegration` test with that exact Node executable and worker. Missing
artifacts fail rather than skip. Ordinary Go/database tests are not coupled to
ambient Node. Source CI runs the selected integration unconditionally in its
existing Node-24/Go job. No private output is uploaded as a public artifact.
