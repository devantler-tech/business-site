# Identity build-tool license scope

Copyright (c) 2026 Devantler Tech.

Devantler Tech's authored material in the exact 37 source paths listed in
`scope.json`, and in that manifest's three license documents, is licensed under
the GNU Affero General Public License version 3 only (`AGPL-3.0-only`). The
complete, unmodified license text is in `COPYING`. This grant also applies to
identifiable copies of those files retained in the identity source archive.

The list is an exact file scope. It does not grant a license to a directory,
the whole Go module, the website, other portal application source or the
Agentic Engineer. All other Devantler Tech source retains its existing terms.
Third-party material retains its original licenses, copyrights and exceptions;
this grant covers only material Devantler Tech has authority to license.

The private pnpm source adapter uses the same license for its selected authored
files. Its private package setting remains enabled; no npm publication is implied.
AGPL permits commercial use subject to its conditions. This scoped grant does not
add a prohibition on competing services.

Run `bash scripts/check-zitadel-build-tools-license.sh` to check the closed scope,
literal terms and adapter metadata, and `bash scripts/zitadel-build-tools-license.test.sh`
for its adverse repository controls. These CI checks are outside the grant's
source list and are not part of the native build entrypoints.

Scope verification is not a license audit or image admission. Complete custom-tool
source capture, bootstrap-dependency treatment, durable source availability and
identity image publication remain separate requirements under #387 and #308.
