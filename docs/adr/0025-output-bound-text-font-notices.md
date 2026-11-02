# Output-bound identity text-font notices

Status: Accepted

## Context

The frozen identity distribution contains 54 text fonts and 84 selected source,
generated and Console embed-source copies. Their embedded attribution records
identify different copyrights, versions and terms. Generated JavaScript package
notices do not cover these copied assets.

## Decision

Extend the existing, otherwise inert font-verifier entrypoint with an explicit
text-font mode implemented in a companion Go file. It uses the existing confined
reader, duplicate-key JSON validator and bounded, CRC-reading Login V1 decoder.
The icon mode retains its own output and checks. Both compile as one local
standard-library command; no generic library, new service or dependency is needed.

Admit a frozen catalogue of exact source identities and expected generated paths.
Inspect complete Windows Unicode BMP English copyright, identity/version and
license records (platform 3, encoding 1, language 0x0409; name IDs 0, 3, 5, 13 and
14). Preserve complete selected strings and their original UTF-16 byte spans.
Unsupported table layouts, incomplete observations, ambiguous records and
excessive input/output fail. This is attribution inspection, not glyph validation
or an inventory of every platform/language record.

Retain the actual per-font copyright and license declarations, the original
frozen Lato OFL source, and the complete applicable OFL/Apache terms. Copyrights
belong to the font records that name them; a shared terms document does not
replace a different font's copyright or create a grant. Keep selected older
Roboto's Apache declaration rather than substituting current font terms.

Before compilation, snapshot the exact selected source/generated font bytes,
decoded Login V1 members, compiler selection, preparation, catalogue, terms and
recipe. After compilation, recompute that snapshot and bind the retained notice
text and inventory to the unchanged completed build receipt and binary.
Completed verification is read-only. Failed or partial capture cannot finalize
a build-success receipt.

Require every selected generated copy and Console compiler embed path and exact
Login V1 member. Reject extra selected source/embedded fonts and coherent
substitution of source and generated copies. These checks establish byte
inclusion, not the visual use of every available font or a shaping result.
The separate native browser journey supplies the default presentation proof.
The 32 existing non-text Console icon-font outputs have a separately declared,
closed byte set so they cannot mask an added text font. Their attribution and
release decision remain under the distribution-wide parent, not this collector.

Package the checked text and inventory with the immutable local image. Retain
the command, catalogue and terms in corresponding-source records, and include
the inventory in the network-denied replay receipt. Ordinary website builds do
not invoke this mode.

## Boundaries

The selected-font increment does not establish complete distribution-wide
license closure, image admission, reproducibility, public source retrieval,
production federation, invitation/MFA acceptance or portal activation. Their
existing acceptance remains under #308, #305 and #302.

## Alternatives

A new font parsing dependency or general asset service adds scope unnecessary
for frozen metadata. A separate copy of the generated-Go/ZIP decoder creates
two security-sensitive implementations. A shared library has no independent
product demand. Keeping this font-specific mode in the maintained entrypoint
reuses the actual parser without moving or weakening its existing controls.
