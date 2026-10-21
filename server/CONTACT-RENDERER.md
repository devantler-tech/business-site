# Private contact presentation

Private contact responses use the ordinary Go browser, templates and native forms.
There is no private Svelte component, worker, supervisor, protocol package or
renderer release decision in the maintained runtime.

Go buffers the entire document before headers, then checks the original durable
session, identity, expiry and query client after SDK waits. The maintained SCS
context store and codec supply fresh session bytes; the final SQL binds those
bytes and the original hashed token to current client membership. Reads never
refresh or resurrect sessions. See [contact behavior](CONTACTS.md) and the
[separate Go HTTP integration](README.md#actual-database-tests).

[ADR 0017](../docs/adr/0017-retain-go-private-presentation.md) records the narrow
rejection of the private candidate. The immutable
[experiment implementation and contract](https://github.com/devantler-tech/business-site/blob/ca957e59b5df5d8e8a32a398bdb1acdd77ce9a4a/server/CONTACT-RENDERER.md)
remain historical evidence, not executable instructions for current source.
Unperformed comparative measurements are not passing results. Public Svelte
presentation, production activation and the remaining portal programme are separate.
