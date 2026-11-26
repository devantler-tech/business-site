# Private appointment availability

The booking work continues [monorepo#3981](https://github.com/devantler-tech/monorepo/issues/3981)
within the unified portal programme. The internal `booking` package computes candidate time
intervals. It is not registered with the browser or runtime and cannot reserve, confirm or cancel
an appointment. No calendar account, payment route or public booking link is activated.

## Operator policy

The confirmed ordinary window is Monday–Friday, 16:30–19:00 **Europe/Copenhagen**. A meeting
must finish inside its window. A free calendar gap does not open another time. A dated operator
opening replaces that date's ordinary window; a dated closure takes precedence over both.
This supports explicit longer openings without deciding what a full-day product means or costs.

The policy is disabled by default. There are no defaults for duration, slot spacing, lead time,
before/after buffers, weekly meeting-time capacity or cancellation terms. The operator must supply
the policy, including a cancellation-terms version. Optional-duration pointers distinguish an
explicit zero from a missing choice. Durations use whole minutes. The caller must supply the
meeting duration authorized for the particular introduction or accepted fixed quote. Synthetic
test values are not approved business terms. The first consultation is free; paid work uses fixed
quotes, with no hourly rate.

Weekly capacity counts elapsed meeting time within each Copenhagen Monday-to-Monday week,
including commitments outside the displayed search range. Calendar conflicts additionally include
the candidate's before/after buffers. Portal commitments include their own buffers too, so both
appointments' preparation/recovery time is protected. Buffers may extend beyond the customer
window; the meeting itself cannot. A changed policy applies conservatively to existing commitments.

Searches are bounded to 31 elapsed days, with at most 4,096 calendar intervals and 4,096 portal
commitments. Explicit openings and closures are each bounded to 366 dates. These are resource
bounds, not a promise to offer that much work. Missing zone information denies availability;
the package embeds Go's timezone data for restricted runtime images. Boundaries in a missing or
repeated DST hour close that date rather than silently choosing an offset. An explicit window
whose boundaries are unambiguous can span a DST transition; its slots retain distinct UTC instants.

## Availability inputs and privacy

`BusyProvider` is deliberately limited to a bounded interval query returning busy intervals and
coverage/completeness/freshness evidence. It has no event title, location, description, attendee or
account field. The future adapter must consume only explicitly selected calendars and preserve
recurrence, all-day and timezone semantics before attesting complete coverage. Adapter failure
must return a generic unavailable result, never a raw provider response.

`Available` requires a complete calendar snapshot covering the search and candidate buffers,
observed no more than 30 seconds ago and never in the future. It also requires complete durable
commitment coverage: every whole Copenhagen week touched by the search, extended at both ends
by the sum of the before/after buffers. The database query must include meetings overlapping that
coverage, even when they start outside it. A partial page cannot prove remaining weekly capacity.
Intervals are half-open: an event ending at a meeting's unbuffered start does not conflict.
Busy intervals may be unsorted or overlap; calculation preserves the caller's input.

Only the service constructs these inputs. Browser fields cannot attest coverage, completeness,
client identity, duration, eligibility or funding. Candidate slots are advisory: another request
or external event can change availability immediately. Before offering a future HTTP operation,
the service must evaluate its independent false-default OpenFeature gates and current membership.
The policy's enabled field alone grants neither of those authorities.

## Sequential delivery and evidence gates

| Child                                                              | Boundary                                              | Required evidence                                                                                                                                                                         |
|--------------------------------------------------------------------|-------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| [#656](https://github.com/devantler-tech/business-site/issues/656) | Internal policy and availability                      | Focused real policy tests, default-off/incomplete-state refusals and DST/buffer/capacity controls                                                                                         |
| [#657](https://github.com/devantler-tech/business-site/issues/657) | Durable reservations and introductory entitlement     | Real PostgreSQL overlap races, expiry/retry/restart, stable client IDs, one free entitlement, final conflict checks and current paid-service authority |
| [#658](https://github.com/devantler-tech/business-site/issues/658) | Private CalDAV adapter and owned-event reconciliation | Actual library contract tests, recurring/all-day intervals, conditional writes, ambiguous failures and owned-only cancellation/rescheduling; later private iCloud acceptance              |
| [#659](https://github.com/devantler-tech/business-site/issues/659) | Native EN/DA customer/operator journeys               | Session/CSRF/current-membership isolation, honest states, mobile/keyboard/appearance, two synthetic clients, deployment and recovery                                                      |

The selected direction builds the small portal-specific policy and state machine in the existing
Go/PostgreSQL application, reusing calendar protocol libraries through the narrow interface.
A client-bound reservation ID and its append-only revisions are stored by migration v34,
after the shared service contract. The private hold writer serializes changes to the
single booking calendar. It re-reads the latest revision after obtaining the lock and
uses database time to exclude expired holds. Dual attempts for the same time can retain
one provisional hold. The booking service must also apply provider availability,
buffers, weekly capacity and current authority before confirming an appointment.
A second booking application would introduce another identity, business-state and presentation
boundary. No new service, subscription or paid enrollment is selected. The maintained MIT
[go-webdav CalDAV package](https://pkg.go.dev/github.com/emersion/go-webdav/caldav) is the adapter
candidate; its documentation includes bounded recurrence expansion and conditional object writes.
That API inventory is not proof that iCloud supplies every required behavior. The adapter child
must verify the actual protocol behavior before a compatibility claim or dependency addition.

The human privately selects calendars and authorizes the connection/credential setup.
[Apple's supported third-party access and revocation guidance](https://support.apple.com/en-us/121539)
describes authorization and the app-specific-password alternative. An agent must not inspect live
events or request credentials in chat. Real connection, revocation and calendar effects are held
acceptance gates. No calendar credential is created or rotated by this implementation.

Paid confirmation must consume the shared exact-quote acceptance, current verified full funding
and durable confirmation sender-acceptance evidence. Prepared mail, sender acceptance and actual
inbox receipt remain distinct. This work neither implements another sender/funding policy nor
advances the software FIFO queue. Unknown business settings, private calendar authorization,
real payment evidence and deployed user-path proof remain gates on the parent and public activation.
