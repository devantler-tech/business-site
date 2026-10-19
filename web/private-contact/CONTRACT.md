# Private contact component

This developer-only artifact implements the contact presentation selected by
[ADR 0014](../../docs/adr/0014-private-contact-renderer-experiment.md).
The explicit developer browser composition connects the compiled pipe worker
through the [Go supervisor](../../server/CONTACT-RENDERER.md) to the real private
contact handler. Ordinary Go construction and production packages retain Go
presentation. The complete experiment's packaging, measurements and adoption
decision remain in business-site issue #199.

## Input and rendering boundary

`entry.mjs` exports `renderContact(contact)`. Its caller must first validate the
display-only projection using the existing
[renderer protocol](../../server/RENDERER-PROTOCOL.md). The TypeScript shape in
`web/src/lib/private-contact/projection.ts` documents that projection; it is not a
request parser or an authorization check. Never pass a request, identity,
customer-record selector, trusted HTML or a destination supplied by a client.

The actual Svelte component produces the complete contact `<main>` body for the
existing Go-owned private frame. It retains native English/Danish forms,
revisions as decimal strings, field limits, recovery alerts, confirmation
eligibility and fixed local destinations. Svelte escapes text and attributes.
No browser script, hydration, public metadata or head fragment is emitted.
Render failures expose a generic error without the original exception or cause.

## Building and testing

Use Node 24, npm 11 and `LC_ALL=C`, after installing both existing dependency lock files:

```sh
npm ci
npm --prefix web ci
node --test web/tests/private-contact-renderer.test.mjs
# With the exact Go compiler and BOTH provisioned disposable database fixture
# URLs from server/README.md as well:
node --test web/tests/private-contact-runtime.test.mjs
```

`buildContactRenderer()` in `build.mjs` invokes the pinned Vite/Svelte server
compiler and returns `{ directory, entry, worker }`. The passive `entry`
exports the component; the separate `worker` entry serves anonymous stdin/stdout
pipes. Both are compiled in one ordinary Vite build. It creates a new owned temporary
directory outside the website outputs, bundles the server dependencies, and
copies the upstream Svelte, clsx and esm-env license texts into `notices/`.
The caller must remove that returned directory after use. A failed build removes
only its own temporary artifact before rejecting.

The tests compile the real component, check independently supplied literal
projections and failure states, execute a copied bundle outside the checkout,
and exercise actual compiler-failure cleanup. They do not claim authenticated
customer journeys, concurrent HTTP isolation or an operating-system sandbox.

The worker accepts only projections constructed by Go's `RequestFrame`, with
versioned four-byte big-endian framing and the complete 8 KiB request / 64 KiB
response bounds. It is not a general untrusted JSON request parser: Go validates
the closed display projection before serialization and strictly validates each
response. A malformed, truncated or oversized input exits without private output
or diagnostics. Importing the passive component never starts the worker.

The runtime tests exercise actual pipe failures and the Go supervisor with the
real compiled child, including cold start and eight concurrent localized renders.
The separately selected HTTP controls exercise actual native forms, original
durable-session/client revalidation and independent client/operator isolation.
This does not prove production federation/ingress or operating-system sandboxing.
The artifact still requires an existing Node 24 executable; it does not distribute
Node, expose a listener or activate a production portal feature.
Packaged-runtime delivery, measured paired two-client trials and actual rollback
remain prerequisites of #199.
No adoption, performance or maintenance improvement has been established by
this component alone. The frozen maintenance task is deliberately not included.
