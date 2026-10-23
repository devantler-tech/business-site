# Output-bound Login V1 icon inventory

Status: Accepted

## Context

The customer identity distribution needs the four selected Login V1 icon fonts,
their ten actual vector inputs and complete redistribution notices. A source
folder or successful compiler does not establish what the generated login archive
contains. The frozen font's clipboard differs from the checked-in solid clipboard.

## Decision

Use an explicit, default-inert repository-local Go verifier with only standard
packages, independent of the portal server module and its newer Go requirement.
It reads one bounded snapshot of the prepared source, preparation receipt,
generated-output manifest and notice inputs. It parses the generated Go as data,
requiring the known single-literal login registration, and inspects the decoded
ZIP without importing, executing or extracting generated code.
Prepared-source reads stay beneath the selected build root, including parent-link
checks. Compiler-control comments are rejected; the native builder also requires
Go's actual dependency selection to include that exact login registration file.

Admit only the frozen four font byte identities and ten codepoint/vector bindings.
Compare all WOFF tables with the TTF and the EOT suffix with that exact TTF.
Compare the SVG-font glyphs with their actual SVG vectors through a closed path
normalizer: absolute M/L/C/Z, reflected S controls, x multiplied by 32 and y
mapped to 960 minus 32y, expressed in integer eighth-units. This establishes
SVG-font/vector geometry, not a decoded TTF-outline or shaping implementation.
The exact input hashes are checked before this normalization, so rounding cannot
admit an arbitrary near-match replacement.

Retain the complete immutable Icons8 license and notices, select its MIT option,
and supply the actual clipboard vector with a truthful modification record.
The inventory records both retained-source and immutable-upstream identities.
It is emitted only after all selected inputs, decoded members and wrapper checks
pass. Duplicate, malformed, partial or changed observations fail without a success
inventory. CI consumes this verifier in the real Linux build and retains its
small diagnostic inventory, never the unadmitted service or generated assets.

## Boundaries

The IcoMoon project/settings and bit-for-bit regeneration remain unknown. The
inventory is one distribution input, not blanket legal clearance, an image
release, a runtime-resource measurement or permission to activate the portal.
Other fonts, Angular and Go notices, corresponding-source packaging and actual
restricted login/runtime acceptance remain separate work under issue #308.

## Alternatives

A general font library adds dependency and maintenance scope not needed for these
immutable bytes. Hash-only inspection cannot establish per-member embedding or
the actual clipboard mapping. Compiling generated Go to inspect its data would
execute evidence and is unnecessary; the standard Go parser and ZIP reader suffice.
