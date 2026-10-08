# Journal presentation within the business site

Status: accepted for the default-off presentation slice in #11. Activation and release-flag retirement are tracked in #12.

## Context

The business homepage uses a wide, calm layout with local Inter typography and green/charcoal themes. The Journal currently inherits documentation navigation and decorative article treatments. Visitors should recognise the same business while still being able to explore technical writing.

## Decision

Keep the Astro/Starlight Blog content pipeline, native previews, article rendering, tag/author archives, pagination, search, RSS and existing URLs. Override only Starlight's PageFrame and PageTitle on Journal routes when FEATURE_JOURNAL_PRESENTATION is enabled. The disabled path forwards the original slots to the stock components. Technical guides always take that stock path.

The enabled frame moves the original sidebar into a native details disclosure, retains search and adds an RSS link. The introduction establishes the publication before the archive. A wide lead article introduces a two-column archive; reading pages use a narrower text measure. All CSS is scoped to the enabled Journal frame.

Palette: charcoal canvas #080d0b, deep-green paper #101a15, soft-white ink #eef6f0, green accent #39ff14; light-mode canvas #f6faf7 and forest ink #152b1c. These are existing BusinessTheme tokens, not a second theme. Typography uses the existing self-hosted Studio Inter regular/semibold; technical code retains its existing monospace.

```text
Business navigation / theme / language
Journal introduction
Browse the Journal [expand]                       RSS
Lead story: title + introduction | unique cover
Story + cover                    | Story + cover
Native older/newer pagination
Business footer
```

Design principles: clear hierarchy; restrained green accents; generous space around images and readable text; native controls and honest content. This deliberately removes the dense fixed sidebar and oversized green card headings observed in the existing Journal. The maintainer's follow-up in #13 adds a distinct conceptual cover for every post in the existing workshop style and separates unrelated site subjects. Article prose, real screenshots, authored diagrams and the founder portrait retain their roles. No invented claims, article rewrites, customer details or framework migration are introduced.

## Consequences and validation

Build enabled, explicitly false and entirely unset states. Check emitted routes and retained articles/feed destinations, then exercise native browsing disclosure, pagination, tags, titles, code copying, keyboard order, System/Light/Dark and 390px rendering in the browser. Compare disabled Journal/guide behaviour separately. A reviewed source merge does not prove publisher adoption or live delivery; those receipts remain required before #12 removes the flag.

References: [Starlight component overrides](https://starlight.astro.build/guides/overriding-components/), [Starlight Blog data](https://starlight-blog-docs.vercel.app/guides/blog-data/).
