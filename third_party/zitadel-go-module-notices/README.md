# Selected Go dependency notices

The frozen catalogue binds 231 actual dependency identities to their complete
root and eleven nested notice components. Its twelve selected source entries
also retain seventeen literal attribution/origin spans. The Linux collector
checks whole-file hashes, actual package/GoFiles membership, complete terms
references and all external input bytes before retaining any output. Ordinary
website builds and no-argument invocations remain inactive.

## Source relationships

| Selected material | Retained relationship |
|---|---|
| TOML field handling and builder buffers | Original Go adaptation/copyright blocks, module MIT terms and the already retained complete Go BSD terms |
| golang-lru `internal/list.go` | Original Go header and the actual retained `simplelru/LICENSE_list`, preserving the distinction between source and notice locations |
| chi radix tree | Armon Dadgar's origin/modification block and original go-radix MIT terms |
| rs/cors sorted set | jub0bs origin line and original MIT terms; the upstream author submitted the adaptation |
| MinIO CRC combination | Complete Justin Ruggles/zlib/Klaus Post origin/modification block and conservative historical zlib reference headers |
| Afero matching and directory helpers | Complete original headers/adaptation comments, module Apache terms and complete Go BSD terms |
| Unicode bidi core and bracket handling | Actual Go headers/origin comments, full Unicode V3 and the reviewed reference rights/credits blocks |
| Public Suffix List | Selected Go/source identity, full MPL 2.0, historical list, and exact embed/generator bindings |

The complete Go BSD text is byte-equal to the selected compiler's LICENSE:
SHA-256 `911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad`.
Cross-component references reuse those verified bytes while retaining every
origin identity. Other inspected adaptations whose rights/conditions are already
covered do not become additional notices merely because a year or header differs.
Their source comments remain part of corresponding source.

## External provenance and limits

The catalogue records each retained file's exact bytes/hash and original URL.
The fixed local files are never fetched or interpreted by the collector.

- [go-radix MIT](https://github.com/armon/go-radix/blob/59b4e6d2b27a4609e6c9507b02b3488ecd323c2b/LICENSE)
  and [jub0bs/cors MIT](https://github.com/jub0bs/cors/blob/516cd2b996f6a495ba93fbc5914f0b27edc844ce/LICENSE)
  retain complete original terms. The latter's author submitted the rs/cors
  adaptation in [4c32059b](https://github.com/rs/cors/commit/4c32059b2756926619f6bf70281b91be7b5dddb2).
  These references do not identify an exact copied source revision where the
  downstream comment does not specify one.
- The two [historical zlib headers](https://github.com/madler/zlib/tree/50893291621658f355bc5b4d450a8d06a563053d)
  preserve the full `zlib.h` notice and CRC origin header. Complete zlib.h source
  SHA-256 is `726b0569915917b967f87f3f08a1eec039101bf9dcc29d61c0b2b0b8f271b58d`;
  crc32.c is `3f3daca510273dda74eaf381c46d2551e3a3506891ae063ab451b87744a7a36c`.
  Neither the precise zlib ancestor nor intervening Go-port revision is encoded
  in MinIO's comment. The reference is conservative; its source-distribution
  condition is not a mandatory binary acknowledgement.
- [Unicode terms](https://www.unicode.org/copyright.html) include Public/PROGRAMS
  software absent a material-specific exception. The complete
  [Unicode V3 licence](https://www.unicode.org/license.txt) is retained alongside
  the rights/credits from the reviewed 2013
  [BidiReference](https://www.unicode.org/Public/PROGRAMS/BidiReferenceJava/archive-2013-09-16/BidiReference.java)
  and [BidiPBAReference](https://www.unicode.org/Public/PROGRAMS/BidiReferenceJava/archive-2013-09-16/BidiPBAReference.java).
  Complete reference-source hashes are respectively
  `8784ac7a4357852593d0cbf7d8cfc3b133097abbb0c7d18e83703fe0fbcfa872`
  and `2610c48f86a4ccdb945174de9c30497df46ada02674bf6c3adaf9f3996fa6e8e`.
  The exact Java ancestor is unspecified. These are current publisher permission
  terms and reviewed archive rights, not a reconstruction of a 2013 website.
  No full Java source is retained as notice output.
- The complete [PSL at d6c92f1](https://github.com/publicsuffix/list/blob/d6c92f1bbb7433e5db7b8405c25d4035fb8ff376/public_suffix_list.dat)
  retains its original header/comments alongside full MPL 2.0. Static decoding
  matched 10,133 rules and 10,533 nodes including ICANN/private, wildcard and
  exception semantics. The catalogue checks the actual selected list/table,
  three embeds and ignored generator. It checks the complete historical list
  as a corresponding-source input but emits only its reference, not its data.
  Generator execution and byte-identical optimized-table regeneration are not
  established by that semantic correspondence.

## Distribution boundary

`go-module-notices.txt` contains the assessed notice spans and complete external
terms, with checked inventory offsets and source identities. Whole Go/Java
source, the PSL list, embeds and generator are not diagnostic payloads. The
historical list remains in this source repository for corresponding-source
packaging. Complete recipient retrieval and clean replay remain under #343/#308;
retention alone is not full licence closure, image admission, signed publication,
daemon/resource acceptance or portal activation.
