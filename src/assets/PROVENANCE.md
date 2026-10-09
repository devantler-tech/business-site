# Asset provenance — `docs/src/assets`

## `matrix.png`

Committed splash-hero background for `docs/src/content/docs/index.mdx`.

- **Origin:** generated once by `docs/scripts/generate-hero.py` (Pillow / `PIL`), a manual
  offline generator that was never wired into CI.
- **Removed generator:** `d847f3b` / [#2232](https://github.com/devantler-tech/monorepo/pull/2232)
  (`chore: remove generate-hero.py, the last Python file in the repo`) — portfolio scripting is
  bash or Go only.
- **Status:** the PNG is the durable asset. Do not reintroduce a Python generator; if the hero
  must be regenerated, rewrite the tool in Go (or replace the image by hand) and update this note.

Closes the acceptance criteria of [#2176](https://github.com/devantler-tech/monorepo/issues/2176).

## `agentic-engineering-process.png`

Inline diagram for the Agentic Engineering blog post. The Agentic Engineering
documentation page renders the editable Mermaid source.

- **Origin:** rendered from the Mermaid `block-beta` definition embedded in
  `docs/src/content/docs/agentic-engineering.mdx`.
- **Rendering:** light background, three category boxes, and a numbered serpentine activity flow;
  exported at 3808×2142 (16:9).
- **Status:** the MDX Mermaid definition is the editable source. Regenerate the PNG from that source
  whenever the process or diagram changes, and update both in the same change.

## `editorial/*.webp`

Conceptual journal and project cover illustrations generated with the built-in image-generation
tool on 2026-10-07. The complete prompt set is in [editorial/PROMPTS.md](editorial/PROMPTS.md).
They share a charcoal/green workshop palette and use subject-specific metaphors: workflows,
local clusters, cloud infrastructure, assistant dialogue, software craft, ownership, developer
setup and data research. The original generic series is retained for its site subjects.

The topic-specific `editorial/journal-*.webp` covers and `data-product-research.webp` were generated
with the built-in image tool on 2026-10-09, matching the original workshop series as a style
reference. Every current Journal post owns one distinct cover; no cover is repurposed for an
unrelated business-site subject. The older data-product implementation has a separate illustration
from the broader data-space research feature. Repeated views of one article, translated views of
one project and responsive delivery sizes use their subject's same image. The complete prompts,
including the targeted removal of unintended emblems, are recorded in `editorial/JOURNAL-PROMPTS.md`.
`scripts/check-editorial-assets.mjs` compares decoded image pixels as well as paths, so renaming or
losslessly re-encoding an identical cover cannot make it unique.

These are illustrations, not photographs of company equipment, factual architecture diagrams,
customer examples or product screenshots. The real founder photograph, KSail interface captures
and authored research/process diagrams retain their evidential role. Covers are compressed to
1440-pixel-wide WebP; Astro generates responsive delivery assets.

## `ascoaching-homepage.jpg`

Real desktop screenshot of the public [AS Coaching og Vaner homepage](https://ascoachingogvaner.dk/),
captured in the browser on 2026-10-07. It shows the public opening section and portrait, not an
invented design or a generated illustration. Astro emits compact responsive WebP thumbnails;
the original JPEG is linked for visitors who choose the larger view. Family-work examples are
not presented as paid client commissions.

## `ksail-desktop.png`

Real, unmodified native-window screenshot of the installed KSail macOS desktop application,
captured on 2026-10-09. It shows the Assistant start screen with no conversation, customer data,
cluster identifiers or credentials. No assistant request, cluster provisioning or plugin
installation was performed for the capture. This is an interface screenshot, not proof of an
operating customer environment or an agent completing work. Astro generates the delivery WebP.

## `wedding-guest-demo.jpg`

Real browser capture of Wedding App's signed-in local guest demo, captured on 2026-10-07
from the reviewed application source `dcf337b61ed342be615240b474c29894e729bf9e`.
Its documented database-free demo login was used, not a production guest account.
Local-only display substitutions removed the guest/couple names, event date, venue/address,
countdown values and venue background before capture. No guest list, gallery photographs,
schedule, invitation code or private URL appears in the image. These substitutions are not
application changes and are not deployed. The card identifies this as an anonymized demo;
Astro emits responsive thumbnails and links the original anonymous capture for enlargement.
