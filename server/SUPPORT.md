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

Before setting the flag, the accountable business owner records the support
purpose, confirms the applicable GDPR Article 6 basis, and reviews the audit
retention period and access review cadence. Where Devantler Tech is a processor,
record the controller’s documented instructions under the relevant Article 28
agreement. A recorded support request is an access condition. Determine the
lawful basis separately and explain the access and audit retention in the
customer privacy notice before activation.

### Proposed basis for the portal’s own support

For portal administration, Devantler Tech determines the purpose and scope of
support access. The proposed legitimate interest is to diagnose account and
workflow problems reported by its customers. Where the issue can be resolved
from instructions or fictional data, use those first. When viewing the actual
workspace state is necessary, use the recorded case and only the status fields
needed to investigate it.

The proposed balancing assessment takes into account the client’s request,
their expectation that an operator may investigate it, the use of timestamps
and status in place of free text, the lack of customer actions, and an auditable
90-day window. The accountable owner confirms this assessment, the normal
monthly review of the support audit, and the disclosure before the first use.

### Proposed customer notice

**English:** If you ask us for help with the portal, we may view the status of
your account and workspace to investigate your request. We record which
operator opened the support view, the time, and the case reference for up to
90 days to check that access was appropriate. Our legitimate interests are
resolving reported portal problems and monitoring support access. The privacy
notice explains how to object or contact us about your data.

**Dansk:** Hvis du beder om hjælp til portalen, kan vi se status for din konto
og dit arbejdsområde for at undersøge din henvendelse. Vi registrerer, hvilken
operatør der åbnede supportvisningen, tidspunktet og sagens reference i op til
90 dage for at kontrollere adgangen. Vores berettigede interesser er at løse
de problemer, du melder, og kontrollere adgangen. Privatlivspolitikken forklarer,
hvordan du kan gøre indsigelse eller kontakte os om dine oplysninger.

- [GDPR Articles 5, 6, 25, 28 and 32](https://eur-lex.europa.eu/eli/reg/2016/679/oj/eng)
- [Datatilsynet guidance on access by need](https://www.datatilsynet.dk/regler-og-vejledning/behandlingssikkerhed/katalog-over-foranstaltninger/dataadgang-efter-behov)
- [Datatilsynet guidance on access logging](https://www.datatilsynet.dk/regler-og-vejledning/behandlingssikkerhed/katalog-over-foranstaltninger/logning-af-brugernes-anvendelser-af-personoplysninger)
