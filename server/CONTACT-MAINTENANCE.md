# Private presentation maintenance inventory

The ordinary Go suite checks all 98 real private documents: 24 portal, 12 contact,
38 request and 24 triage documents. The retired invoice and credit screens
(#117, #120) left the inventory with their 22 documents: the credits template,
three triage invoice states and two portal error states, in both languages. Both languages, complete frames,
native forms, escaping, accessible associations and declared error states remain
mandatory. The test uses actual page builders and templates with fictional display
values; it does not authenticate a person or prove browser accessibility.

The separate work-review inventory adds eighteen real EN/DA documents in the same
Go frame: absent/current/paused/expired/selected, eligible/ineligible funding review,
bounded terminal history and closed funding gates with revocation retained. Its
explicit form comparisons, exact displayed facts, active section associations and
negative controls live in `work_review_render_test.go`. Shared frame, actual-route
and quality checks cover this fifth template. The remaining original frozen
body references are unchanged; no new renderer output is used to regenerate them.
The frame references were re-recorded once, for the brand (see below).

From `server/`, with the exact compiler in `go.mod`:

```sh
GOTOOLCHAIN=local go test -race ./internal/identity \
  -run '^TestContactMaintenance' -count=1 -timeout=30s -v
```

This inventory is also part of the unconditional full-module race/coverage suite.
The separate mandatory `contactintegration` and `contacttrialintegration` suites
exercise actual TLS, restricted storage, native journeys, session/client authority
and real process restart. None requires a private Node rendering worker.

## Complete document expectations

Each document has a complete parsed frame expectation covering brand, skip link,
language navigation, metadata, stylesheet and script. Main attributes remain exact.
Every contact main additionally matches a complete independently frozen tree,
including its title, introduction, section, forms, messages and local links.
Missing, hidden or inert forms, labels and help cannot satisfy the controls.

`maintenanceMainDigest` retains node types, text, namespaces, ordered attributes
and children. The contact projection ignores only compiler comments, attribute
order and ASCII indentation between block containers, preserving prose and label
boundary spaces. Parsed inputs and rendered evidence are not rewritten. Normal
and task bodies use separate explicit expectations; the task hint is not removed
from its fingerprint.

The non-contact oracle covers 86 documents in explicit state order. There are
84 distinct reference bodies: sharing-invalid and sharing-conflict intentionally
have the same portal main in each language. Controls do not claim to distinguish
those aliases. Each draft's real random submission key must first satisfy its
unique native input, expected state and 26-character RFC 4648 base32 shape before
being represented by the fixed fictional key. All other values and markup remain
exact. Missing, duplicate, malformed and misplaced keys fail.

Refusal controls damage full frames and each structural contact element, remove
non-contact bodies, change headings and substitute genuinely different same-language
states. They exercise the selected complete expectations, not renderer-derived
expected values. The maintained inventory has no compiled-fragment consumer or
fragment-only acceptance grammar.

## Frozen provenance and ordinary private screens

The original immutable reference is `a1be78797e7886e3eb57605792027af6d4811add`.
Its fixture is `portal_links_test.go` blob
`056f8203b2ebd224e1f9f2caed2eb411f9189f0d`; its four non-contact templates are:

| Template      | Git blob                                 |
|---------------|------------------------------------------|
| portal.html   | b2edf7e0cd15385c1d7f15a44bd2253f46ecc84d |
| requests.html | f491986e1d19ebbfd7c838f6777fd638fe09d5a9 |
| triage.html   | fd22f5ee2716adb1a9d2c0cfa5736b1b8c1890f9 |
| credits.html  | dd0840fb0a42a2e82a6f08eaca4a7d0838eb6ed3 |

### Re-recorded frame references (Devantler Tech brand, 2026-11-16)

The frame references were re-recorded once, on purpose, when the portal took the
Devantler Tech brand. Every private page now links the tab icon
(`/portal/assets/icon.png`) in its head. Its header brand link now shows the
theme-matched wordmark images, named by `aria-label="Devantler Tech"`, instead of
the text "Devantler Tech". That changes the head and header of all 98 documents and
nothing inside `main`.

Before recording, the rendered frames of every document were compared with the
previous source's frames. Those two edits were the only differences. The new frame
references were then recorded from the branding change's own rendered output, and
the body references were not touched. This is the single exception to the rule
below. Any later frame change needs its own reviewed record here.

The original tables remain unchanged apart from removing the retired entries.
The separate ordinary invoice table (recorded from signed snapshot
`90ade40aa63a54b62012e89a820277dc56eddb5c`) differed from the original only in the
retired invoice and credit bodies, so it was removed with them; triage now uses
the original references. Missing or malformed references fail without fallback.
Validation never generates its own expected values from the application renderer.

Ordinary request validation retains all 38 states. The 12 new/invalid forms
(three original categories in both languages) use the independently authored
complete main-document expectation in `other_requests_reference_test.go`. It
spells out the four options, selected value, case-by-case assessment, native
form controls, text limits, privacy guidance and return links. The other 26
unchanged bodies retain their original immutable references. No production
renderer or reverse projection produces these expectations. Missing, truncated
or malformed ordinary tables fail closed. Task mode always retains all
original body references and the current frame references, and rejects the
newer ordinary forms.
Mutation controls reject absent/incorrect options, selection, assessment text
and its accessible association; the separate Other-request HTTP tests cover
its receipt and operator views. This does not renew the retired comparison.

## Historical comparison boundary

The test-only `-contact-maintenance-task` option selects the frozen original
expectations, not an application flag or a renewed experiment. Its registered
literal name hint and complete descendants remain checked.

[ADR 0014](../docs/adr/0014-private-contact-renderer-experiment.md),
[ADR 0015](../docs/adr/0015-private-contact-comparator-amendment.md),
[ADR 0016](../docs/adr/0016-contact-maintenance-early-rejection.md) and
[ADR 0017](../docs/adr/0017-retain-go-private-presentation.md) retain the immutable
plans, input identities and narrow candidate rejection. The
[frozen harness procedure](https://github.com/devantler-tech/business-site/blob/ca957e59b5df5d8e8a32a398bdb1acdd77ce9a4a/server/CONTACT-MAINTENANCE.md)
identifies the historical two-file overlay, independent expectation construction
and compiled-body exercise. That archived source, not the maintained ordinary
suite, is the comparison harness.

The offline input and report readers retain their own closed, private evidence
contracts. Neither is a completed collector. Unperformed timing, RSS, clean-build,
holdout and same-state recovery measurements remain unperformed, not successful.
The candidate's source-file-count failure is not a universal framework verdict.
No customer, identity, payment or deployment decision is enabled by this inventory.
