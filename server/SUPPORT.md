# Customer support access

The Customers page can open a read-only view for a single active member of a
customer workspace. The portal retains the operator’s session and records their
identity. Each view requires a reference in the form `CASE-1234` and the
operator’s confirmation that the client’s request is documented. The reference
points to the private support record for that customer. Record requests
received by email or phone there before opening the workspace.

The view shows access status, whether contact details were saved, and up to
20 recent request timestamps and product categories when the client request
feature is available. It shows whether a proposal is shared when sharing is
available. It reads no free-text request content, contact values, draft proposal
details, billing records or credentials. Customer actions continue to require
the customer’s original session.

The operator audit identifies the actor, case reference, target membership
incarnation, time and result. It records the operator’s confirmation before
disclosing workspace status. The retention job removes support audit entries
after 90 days. The case reference must contain only its case number. Keep the
customer’s instructions and support details in the private support record.

## Production activation

`portal-support` defaults to false and uses `PORTAL_SUPPORT_FLAG` only in the
production identity profile. The fictional client preview has its own boundary.

Before setting the flag, verify that the deployed English and Danish privacy
notices include the portal support section. This feature covers Devantler Tech’s
own portal. Where a separate support activity puts Devantler Tech in the role of
processor, record the controller’s documented instructions under the relevant
Article 28 agreement. A recorded support request is an access condition.

### Basis for the portal’s own support

For portal administration, Devantler Tech determines the purpose and scope of
support access. The legitimate interest is to diagnose account and
workflow problems reported by its customers. Where the issue can be resolved
from instructions or fictional data, use those first. When viewing the actual
workspace state is necessary, use the recorded case and only the status fields
needed to investigate it.

The balancing assessment takes into account the client’s request,
their expectation that an operator may investigate it, the use of timestamps
and status in place of free text, the lack of customer actions, and an auditable
90-day window. The accountable owner confirmed this support policy on
24 November 2026. Review the support audit monthly and reassess the policy if
the information or purpose changes. The public privacy notice explains the
access, audit retention and right to object.

- [GDPR Articles 5, 6, 25, 28 and 32](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)
- [Datatilsynet guidance on access by need](https://www.datatilsynet.dk/regler-og-vejledning/behandlingssikkerhed/katalog-over-foranstaltninger/dataadgang-efter-behov)
- [Datatilsynet guidance on access logging](https://www.datatilsynet.dk/regler-og-vejledning/behandlingssikkerhed/katalog-over-foranstaltninger/logning-af-brugernes-anvendelser-af-personoplysninger)
