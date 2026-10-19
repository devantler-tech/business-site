# Private contact renderer messages

`internal/renderwire` is the passive Go message codec for the bounded contact
experiment in [ADR 0014](../docs/adr/0014-private-contact-renderer-experiment.md).
No executable or browser handler consumes it. It starts no process, adds no wait,
changes no route and enables no portal operation.
[`internal/contactrenderer`](CONTACT-RENDERER.md) now consumes the codec in a
bounded developer-only process supervisor. HTTP authority checkpoints, packaged
runtime, comparison and rollback remain #199.

## Framing and bounds

Each frame is a four-byte unsigned big-endian JSON payload length followed by
exactly that many bytes. The **complete frame, including its prefix**, is at most
8 KiB for a request and 64 KiB for a response. Thus the request payload is at most
8,188 bytes and the response payload at most 65,532 bytes. Zero-length payloads,
truncated prefixes/bodies and bytes after the declared frame are rejected.

`RequestFrame` validates the complete presentation projection before returning
any encoded bytes. `ResponseSize` accepts exactly four prefix bytes; a future
pipe reader must call it before allocating or reading the declared body.
`DecodeResponse` accepts exactly one complete frame, not an arbitrary stream or
an incremental fragment. A stream reader must assemble that bounded frame first.
JSON whitespace inside the declared payload is allowed; a second value is not.

The pinned Go toolchain's `encoding/json/v2` implementation supplies JSON syntax,
Unicode, duplicate-name and case-sensitive handling. Response decoding also
rejects unknown members explicitly. No custom JSON parser or dependency is used.
Every failure returns the same `ErrMessage` and an empty result, never the
parser's diagnostic or its partially decoded private output.

## Request projection

The request object has exactly `version` (the number `1`), `id` and `contact`.
`id` is 32 lowercase hexadecimal characters; its fresh, unpredictable generation
belongs to the future supervisor, not this codec.

`contact` has these fields only:

| Field                      | Display contract                                                                                                                    |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| `locale`                   | `en` or `da`                                                                                                                        |
| `name`, `email`, `company` | At most 120, 254 and 160 Unicode code points respectively; empty values remain usable for an initial profile or validation recovery |
| `revision`                 | Canonical nonnegative signed-64-bit decimal **string**, including the exact maximum; never a JavaScript number                      |
| `state`                    | `normal`, `invalid`, `stale` or `verification`                                                                                      |
| `verification`             | `unverified`, `confirmable` or `verified`; presentation only, never proof or write authority                                        |
| `csrf`                     | Nonempty public rendered form nonce, at most 512 code points; never the CSRF cookie or server-side secret                           |
| `text`                     | The closed localized plain-text fields below, each at most 512 code points                                                          |

Display strings must be valid UTF-8 without control characters. The codec does
not reapply email/write validation to recovery text: actual writes remain Go's
responsibility. It preserves text such as `<&>` as text; the future Svelte
component must still escape it in its actual HTML context.

`text` contains `title`, `intro`, `nameLabel`, `emailLabel`, `companyLabel`,
`saveLabel`, `pendingText`, `workspaceLabel`, `verifyLabel`, `verificationHelp`,
`errorText` and `reloadLabel`. The ordinary labels and introductory/pending text
are not blank. `errorText` is empty for `normal` and not blank for recovery states.
The verification label/help must not be blank for `confirmable`. Other optional
strings may be empty but remain bounded and validated.

There are no dynamic destinations, template selectors, issuer/subject, client
identifiers, credentials, raw requests or authorization decisions in this type.
Form/link destinations belong to fixed reviewed components. A rendered public
nonce is not authority: the real Go session, cookie, origin and CSRF checks remain
required independently of any displayed control.

## Response and caller obligations

A response has exactly `version` (number `1`), `id` and `body`, all required and
not null. The ID has the same format as a request ID. The body is a non-blank UTF-8
JSON string. Unknown/duplicate members, case aliases, invalid Unicode, unsupported
versions, null/missing/type-mismatched values and extra JSON values fail closed.

Successful decoding is **not** HTML sanitization, correlation, authorization or
permission to write an HTTP response. The future caller still has to:

- Accept output only from the fixed reviewed renderer artifact and supervise its
  actual process, cancellation, deadline and bounded concurrent work.
- Match each ID to exactly one pending request in the current process generation;
  reject unknown, repeated or late IDs and discard affected pending output.
- Construct only the explicit display projection, keep complete private output
  buffered and revalidate the original durable session, record/client scope,
  expiry, membership and healthy identity/release gates after the render wait.
- Preserve the native Go routes, status, redirect, origin, CSRF, no-store and
  escaping semantics. No private data may enter public build output or caches.

Protocol validation is unconditional; a release flag must never weaken it. The
supervisor's `portal-contact-renderer` OpenFeature gate is false-default, with
lifecycle issue #207 and both-state controls. There is no environment activation
mapping or production HTTP consumer; activation remains a separate decision.

## Exercise the codec

From `server/`, with the exact compiler in `go.mod`:

```sh
GOTOOLCHAIN=local go test -race ./internal/renderwire -count=1 -timeout=30s
GOTOOLCHAIN=local go test ./internal/renderwire -run '^$' \
  -fuzz FuzzDecodeResponse -fuzztime=10s -parallel=2 -timeout=30s
```

These exercise the real codec and malformed/fragmented/limit corpus. They do not
exercise a Node process, Svelte rendering, authenticated browser journey,
independent paired performance trial or actual rollback. The complete normal
module, repository and hosted validation remain mandatory before merge.
