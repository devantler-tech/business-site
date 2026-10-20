# Private bank-credit evidence

An operator can record observed transfers against an existing external invoice.
This is a manual evidence ledger, not a connection to a bank, payment processing,
accounting certification or permission to start client work. The accepted proposal,
invoice amount and client binding remain unchanged. Clients and agents have no
credit read/write endpoint.

## Record, reconcile and correct

With all invoice prerequisites and the independent false-default
`portal-credit-reconciliation` decision enabled, open an operator request, expand
its invoice and choose **Review bank-credit evidence**. Record a stable source
namespace, transfer reference, observed DKK amount, evidence state and a short
evidence note. Do not include account numbers or supporting bank documents.

The namespace and reference each permit 1–64 ASCII letters, digits, dots, slashes,
hyphens or underscores, starting with a letter or digit. They are exact and
case-sensitive: the operator must consistently use the bank's actual stable
identity, not a newly invented alias for a retry. The database globally reserves
each pair to one immutable invoice/request/client. A transfer cannot be split
between invoices or reassigned after invalidation. This prevents repeated use of
the same recorded identity; it does not detect aliases or prove external funds.

Amounts are exact positive DKK minor units, with at most two decimal places and
no thousands grouping, up to DKK 10,000,000 per transfer revision. Confirmed means
the operator has recorded evidence of credited funds. Unverified or disputed
evidence contributes no confirmed funds and prevents a fully-credited result.
Only the latest revision of each transfer counts: zero confirmed funds, partial,
exactly full and excess are distinct. A full result still grants no work-start,
monthly entitlement, refund or agent execution authority. Future work eligibility
must independently require the current agreement and its other release gates.

Use **Add correction** to append a replacement observation or **Invalidated**
with an empty amount to remove the transfer from the confirmed total. Supply a
reason; earlier evidence, actor and database timestamp remain intact. Each invoice
allows 32 transfer identities and each transfer allows 16 revisions. Revision 16
is reserved for invalidation so the positive-credit edit bound cannot trap an
incorrect balance. An invalidated identity stays reserved forever. Once that last
slot is used, a separately reviewed reconciliation procedure is required; this
release does not silently reset history or move funds elsewhere. Review these
bounds and operational correction capacity before activation.

Identical retries return their original receipt only for the same actor and
content. Changed content, another actor, a stale revision or a competing invoice
conflicts without overwriting. Historical invoices remain recordable after a
re-quote or withdrawal: money already observed still belongs to its original
invoice. The page labels whether that invoice's acceptance is current/historical;
historical evidence never authorizes the replacement scope.

## Storage and release

Migration v11 follows unchanged v1–v10 and adds globally unique transfer identity
and append-only revision tables with composite invoice/client foreign keys. The
shared request lock serializes corrections, proposals and consent changes; the
database unique identity arbitrates competing requests. Both tables need
SELECT/INSERT only. Direct or assumable mutation, delegated grants, triggers,
references, maintenance and ownership are refused by the runtime role check.
Private reads materialize complete bounded history and fail unavailable on a
storage error, never a misleading zero. Current operator membership, SDK decisions,
expiry and cancellation are checked around actual reads and writes. Native POST,
CSRF/origin checks, non-cacheable responses and escaped EN/DA output remain.

The gate has no production environment mapping. Missing, false, wrong-type or
erroneous decisions deny. Review retirement in
[#120](https://github.com/devantler-tech/business-site/issues/120) by 15 November
2026. Merged source and synthetic TLS/browser tests do not activate financial
evidence collection. Deployment, real identity, privacy/retention and legal/consumer
prerequisites remain separate; no real bank/customer data is used by fixtures.
