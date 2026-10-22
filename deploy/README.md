# Public runtime package

This directory prepares the public website for the platform's namespace-bound
WebApp interface. It is not applied or published by any workflow. Pages remains
the public deployment; portal access stays disabled.

From the repository root, with Node 24, installed root dependencies, Kubectl and
Docker available:

```sh
kubectl kustomize deploy | node scripts/check-tenant-package.mjs
node --test scripts/tenant-package.test.mjs
node scripts/tenant-runtime-smoke.test.mjs
```

The last command exercises the committed immutable image. Source CI passes its
newly tested production image as an explicit argument, retaining the rendered
arguments and runtime restrictions. Neither mode verifies a registry signature;
that remains mandatory in the signed publisher's independent receipt check.

The package uses two replicas (the validator permits two or three), a
half-core/256 MiB limit and the real `/healthz` probe, plus an application-owned
disruption budget allowing one unavailable Pod.
These are provisional bounds, not load, availability, actual drain or recovery evidence.
Only gateway ingress to the application port is opened. Cilium's documented
`egress: [{}]` enables egress default deny; this package adds no outbound allow
rule. Other platform policies remain additive, so this is not an overriding deny
of every platform-granted connection. No database, migration, broker or private
activation setting is configured. The checker accepts only the committed,
independently verified baseline image; a same-repository digest alone is insufficient.

The selected disclosure file must be provided separately in the same namespace.
The platform projects it read-only at the fixed mount with mode 0440 and the
application's primary group. No disclosure value belongs in this repository or
image. Tests use fictional values in disposable volumes.

The example.invalid hostname is deliberately inert. It is not admitted by the
current platform's production route policy. Before registration, independently
review the real hostname, its allowlist, selected input materialization and
network/controller behavior. Do not apply this directory to an ambient cluster.

Issue #102 retains the semantic-version release caller, actual signed manifest
artifact and separate platform consumption proof. See
[the source-contract decision](../docs/adr/0019-public-tenant-package.md).
