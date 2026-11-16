# Portal commerce acceptance

This is the execution checklist for [#21](https://github.com/devantler-tech/business-site/issues/21),
not a legal opinion or an activation receipt. Source tests, an HTTP 200, a published
image and a submitted SMTP message each prove only their own step. Record the
actual source revision, release/image, deployment, date and outcome for each gate
in the private acceptance record. Keep customer details, addresses, provider
identifiers and payment evidence out of public issues and logs.

## Before an acceptance run

- Use two isolated synthetic customers and a separate operator. Agent-driven
  orders, provider events and refunds use disposable databases and sandbox/test
  providers only. Real agreements, production invoices, charges and refunds need
  the human operator; this checklist authorizes none of them.
- Rebind current required CI, review, signed publication, successful migration,
  rollout and live health. Platform recovery and TLS must be verified before a
  deployed journey can count. Recheck the exact running revision afterwards.
- Retain the settled scope: invited private and business customers; the current
  approved VAT-inclusive offer; Numina invoices by default; Stripe for card and
  single-use MobilePay, with monthly collection by card. Do not use historical
  issue text to replace [ADR 0030](../docs/adr/0030-numina-first-billing.md).
- Publish the reviewed add-on terms and listing together only when their
  coordinated release gates pass. [#621](https://github.com/devantler-tech/business-site/pull/621)
  remains held until then. Confirm both languages and the one-request limit,
  Priority/Express targets, away clause and per-add-on cancellation.
- Confirm the applicable customer/service category, pre-contract information,
  early-start wording, cancellation/withdrawal/refund rules and privacy/provider
  records with the accountable person. Existing decisions are not reopened by
  this checklist; unresolved legal interpretation is not an engineering verdict.

## Settings prepared for the deployment owner

Use the confined settings projection described in [README](README.md#explicit-production-identity-profile).
Identity and each capability are independent. Omitted values remain false.
The maintained provider's typed values are:

```json
{"defaultVariant":"on","variants":[{"name":"on","value":true}]}
```

```json
{"defaultVariant":"off","variants":[{"name":"off","value":false}]}
```

| Acceptance stage | Explicit settings to assess |
| --- | --- |
| Invited access | `PORTAL_IDENTITY_PROFILE=production`, `PORTAL_IDENTITY_FLAG`; no staging lease |
| Contact confirmation | `PORTAL_CONTACT_VERIFICATION_FLAG` |
| Request/operator proposal | `PORTAL_REQUESTS_FLAG`, `PORTAL_TRIAGE_FLAG`, `PORTAL_PROPOSALS_FLAG` |
| Exact-version customer review | `PORTAL_PROPOSAL_SHARING_FLAG`, `PORTAL_PROPOSAL_ACCEPTANCE_FLAG` |
| Default invoice order | `PORTAL_INVOICE_BILLING_FLAG`; validated Numina configuration, complete terms, contact and SMTP sender |
| Card/MobilePay order | `PORTAL_CARD_BILLING_FLAG`; validated Stripe configuration, invoice tax rate and verified webhook |

These are preparation facts, not instructions to enable production. Preserve
independent work approval and worker execution gates; #651 maps neither. Leave
unassessed capabilities off. Provider secrets remain in their existing private
delivery path; never copy them into test fixtures or acceptance notes.

## Two-customer journey

1. **Access and isolation.** Receive the controlled invitation/sign-in mail and
   inspect its authentication results privately. Complete actual login, MFA,
   logout, recovery and revocation. Reject an uninvited identity and cross-client
   record reads/writes. A synthetic OIDC broker is source evidence only.
2. **Contact and request.** Each customer saves their own contact and submits a
   request. Verify stale contact edits and duplicate request submissions. Customer
   B cannot read customer A's profile, brief, proposal, agreement or payment.
3. **Proposal and acceptance.** The operator generates, reviews, saves and shares
   the quote. The customer reviews the exact scope, deliverables, VAT-inclusive
   prices and monthly terms, then accepts that version. A changed/withdrawn share
   must reject the old review; historical receipts remain unchanged.
4. **Binding order.** Read the complete terms and withdrawal form before the
   payment-obligation button. Exercise new-order refusal for missing contact,
   sender or terms and a stale/forged terms reference. Check that no order,
   launch place, invoice or checkout survives refusal. Exercise the optional,
   initially unchecked early-start choice separately.
5. **Durable confirmation.** Receive the actual order email in the chosen
   language. It must contain the original acceptance/version, scope, deliverables,
   prices/add-ons, collection path, early-start choice, withdrawal deadline and
   full terms/model form with the trader disclosure filled in. Save the received
   copy, change the source/contact in the disposable environment and retry: no
   replacement or duplicate confirmation may be created.
6. **Invoice path.** Against Numina's sandbox, verify one contact/invoice,
   booking/retry behavior, exact currency/amount/period reconciliation and no
   funding for a mismatch. Numina's sandbox cannot send invoice mail; record that
   limitation and leave actual invoice receipt to the human-controlled live gate.
7. **Card path.** Use Stripe test mode for one-time card/MobilePay and a separate
   recurring card checkout. Verify the full VAT invoice, required billing details,
   signed event/customer/order/amount/currency/period binding, duplicate and
   out-of-order events, failed/expired checkout, refund and dispute controls.
   Browser return and `invoice.paid` alone are not payout or bank-credit proof.
8. **Withdrawal and ending.** Exercise the native withdrawal form, received
   withdrawal email, invoice/checkout stop, subscription cancellation and manual
   refund handoff. Exercise early-start completion and proportional settlement
   in controlled source fixtures. End hosting and one monthly add-on online;
   check the final billed month and removal of the add-on's priority tier.
9. **Retention and rights.** In disposable historical fixtures, run the actual
   retention command as the separate migration role. Verify the three-year
   former-client boundary, active/pending/shared-history exclusions and removal
   of portal records/outbox. Keep the separate five-year accounting retention
   and any justified legal hold distinct. Verify the human rights-request path.
10. **Close and hand off.** Restart with the relevant gates off and verify denial.
    Record remaining API/MCP worker authorization, progress and booking acceptance
    with their owners. Payment alone must never approve or start work. Close #21
    only when its applicable source, specialist and real-user-path gates pass.

## Source basis checked 16 November 2026

- **Law:** [Consumer Contracts Act](https://www.retsinformation.dk/eli/lta/2025/1184/pdf),
  sections 8, 12 and 13 cover pre-contract information, the payment-obligation
  order and durable confirmation; the service's withdrawal and cancellation
  rules must be assessed separately.
- **Authority practice:** [the Consumer Ombudsman's published position](https://forbrugerombudsmanden.dk/find-sager/sager/markedsfoeringsloven/sager-efter-markedsfoeringsloven/ehandel/kopi-af-samtykke-paa-et-varigt-medium)
  discusses why a link to website information is not itself a durable copy.
- **Provider capability:** [Stripe MobilePay](https://docs.stripe.com/payments/mobilepay)
  is single-use and does not support recurring payments. Verify the actual
  account's capability and existing approvals before any human-controlled live use.
