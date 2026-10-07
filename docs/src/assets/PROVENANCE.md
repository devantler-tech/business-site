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
setup and data research. Related articles share a subject illustration.

These are illustrations, not photographs of company equipment, factual architecture diagrams,
customer examples or product screenshots. The real founder photograph, KSail interface captures
and authored research/process diagrams retain their evidential role. Covers are compressed to
1440-pixel-wide WebP; Astro generates responsive delivery assets.
