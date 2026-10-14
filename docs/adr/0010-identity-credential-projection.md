# Explicit sign-in credential projections

Date: 2026-10-14
Status: Accepted for source implementation; deployment remains separately gated.

## Context

The restricted unified runtime uses a non-root identity and a read-only root.
Kubernetes secret volumes can provide root-owned, matching-group-readable files.
The ordinary private credential reader deliberately rejects group access.

## Decision

Select exactly one ordinary `PORTAL_CLIENT_SECRET_FILE` or explicit
`PORTAL_CLIENT_SECRET_PROJECTION_FILE`. The latter reuses the confined projection
descriptor boundary: root/current-user ownership, optional primary-group read,
no group writes or other-user access, no executable/special/nonregular input,
and bounded reads within the projection directory. It requires a non-root process.
Input selection occurs before any dependency connection and never activates identity.

Keep the ordinary reader and migration input unchanged. Copying credentials into
writable scratch, running the web application as root, or silently admitting group
access on the ordinary input would violate the deployment and privacy boundary.
This is permanent explicit transport configuration, not a new release flag.

## Consequences

The operator can supply a matching-group atomic projection without changing its
ownership or writing a private copy. Rotation requires a deliberate restart.
Actual constructor and compiled-command tests retain closed access, bounded staging
leases, public continuity and generic failure. Synthetic fixtures do not prove
Kubernetes wiring, real identity-provider federation, recovery or customer readiness;
those remain platform#4854 and monorepo#3936/#3939.
