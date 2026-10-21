# Historical private-renderer protocol

The maintained portal uses Go presentation and has no private-renderer message
codec or worker consumer. Do not configure a Node worker or renderer gate.

The immutable [reviewed protocol](https://github.com/devantler-tech/business-site/blob/ca957e59b5df5d8e8a32a398bdb1acdd77ce9a4a/server/RENDERER-PROTOCOL.md)
and [ADR 0014](../docs/adr/0014-private-contact-renderer-experiment.md) preserve
its original framing, limits and display-only boundary.
[ADR 0017](../docs/adr/0017-retain-go-private-presentation.md) records the rejected
candidate decision; it does not establish any unperformed comparison result.
Current session/client authorization and complete response buffering remain Go's
responsibility. See [contact presentation](CONTACT-RENDERER.md).
