# Private contact component

This developer-only artifact implements the contact presentation selected by
[ADR 0014](../../docs/adr/0014-private-contact-renderer-experiment.md).
It is not connected to an HTTP handler or a production package. The existing
Go contact page remains the only runtime path. The complete supervised-process
experiment and its measurements remain in business-site issue #199.

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
node --test scripts/private-contact-renderer.test.mjs
```

`buildContactRenderer()` in `build.mjs` invokes the pinned Vite/Svelte server
compiler and returns `{ directory, entry }`. It creates a new owned temporary
directory outside the website outputs, bundles the server dependencies, and
copies the upstream Svelte, clsx and esm-env license texts into `notices/`.
The caller must remove that returned directory after use. A failed build removes
only its own temporary artifact before rejecting.

The tests compile the real component, check independently supplied literal
projections and failure states, execute a copied bundle outside the checkout,
and exercise actual compiler-failure cleanup. They do not claim authenticated
customer journeys, concurrent HTTP isolation or an operating-system sandbox.

The artifact requires an existing Node 24 executable; it does not distribute
Node, provide a worker transport, own a process or enable a portal feature.
Those packaging, supervision, fail-closed activation, post-wait authorization,
two-client trials and rollback requirements remain prerequisites of #199.
No adoption, performance or maintenance improvement has been established by
this component alone. The frozen maintenance task is deliberately not included.
