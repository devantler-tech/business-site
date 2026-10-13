# 9. Bounded identity staging through the maintained environment provider

Date: 2026-10-13

## Status

Accepted for default-off source delivery; production activation remains gated.

## Context

The unified command needs a concrete, operator-owned provider before it can
exercise staged federated login. Complete broker/database configuration is not
a release decision. A cached true must not survive an invalid value, unavailable
provider, expired authority or shutdown. No extra service is needed for a
disposable, time-bounded staging process.

## Decision

Use OpenFeature's maintained from-env provider v0.1.6 at release commit
`7c63f0470c103b3189cc89d8c9b916cdf2ac92d4` (Apache-2.0), with the existing
Go SDK v1.19.0. Only `portal-identity` maps to operator-private configuration;
work-request, triage and proposal flags remain separate and false-default.
The maintained provider parses its JSON configuration on every evaluation.
The application does not implement a second boolean/variant parser.

Require complete private identity dependencies and an explicit lease of no more
than ten minutes. Check request/process lifetime and the monotonic lease before
and after each resolution. SDK shutdown cancels the lease, including for a
retained client. Reject unhealthy SDK state before and after the identity gate.
Ordinary public-only execution uses the no-op provider and connects to none of
the private dependencies.

Process configuration is immutable from the external operator's perspective.
Revocation and rotation require stopping/restarting the process; extending an
environment setting cannot extend an existing lease. Lease expiry independently
closes existing private access while public pages continue. This is a bounded
staging mechanism, not dynamic production flag management or launch permission.

## Alternatives

Flagd file polling cannot supply immediate rejection of unobserved or failed
file reads. Uncached flagd RPC supplies fresh evaluations, but released v0.7.1
has an open event-channel shutdown concern and adds a service and dependency
graph this stage does not need. Dynamic production control remains a separate
deployment design decision; no unsupported wrapper or upstream patch is copied.

Removing the release gate or treating complete broker variables as activation
would bypass the existing boundary. Direct identity-provider SDKs and a second
portal/queue repository would duplicate the unified product's federation and
portal-owned work programme.

## Consequences and evidence

The operator supplies a finite staging lease and restarts to rotate it; normal
public startup is unchanged. No hosted service, paid enrollment or customer
record is introduced. Tests exercise the actual compiled command and a real
disposable store/TLS broker, plus maintained-provider invitation/session and
revocation controls. Private activation values are runtime inputs, never public
assets, image layers or publication receipts.

Source merge, signed artifact distribution, real Google/Microsoft broker setup,
deployed ingress/isolation/recovery, privacy and paid-order release are distinct
proofs. Business-site #69 owns this source slice; monorepo#3936/#3939 and
business-site #19/#21 remain the wider release gates.
