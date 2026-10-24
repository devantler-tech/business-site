# Dependency review

Reviewed on 15 October 2026 against the actual executable's compiled Go modules;
the SCS row was re-reviewed on 24 October 2026 for its upstream revision pin.
Packaging verifies this table against the executable before distributing it.
This is an engineering inventory, not Danish legal clearance or a new
license for Devantler Tech's application. The application remains governed by
its repository's own terms. No hosted service or paid enrollment is introduced.

| Compiled module                                           | Version                              | Upstream license                                                                                                                                |
|-----------------------------------------------------------|--------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------|
| github.com/alexedwards/scs/pgxstore                       | v0.0.0-20251002162104-209de6e426de   | [MIT](https://raw.githubusercontent.com/alexedwards/scs/209de6e426de9259665975ce16b91331d228f052/LICENSE)                                       |
| github.com/alexedwards/scs/v2                             | v2.9.1-0.20250927102904-c45a9d145a22 | [MIT](https://raw.githubusercontent.com/alexedwards/scs/c45a9d145a2266eab22fe9ca486b299c1d4220d3/LICENSE)                                       |
| github.com/coreos/go-oidc/v3                              | v3.21.0                              | [Apache-2.0](https://raw.githubusercontent.com/coreos/go-oidc/c914bd380327a5a3a81403774d1a5d5b73772ce7/LICENSE)                                 |
| github.com/go-jose/go-jose/v4                             | v4.1.4                               | [Apache-2.0](https://raw.githubusercontent.com/go-jose/go-jose/0e59876635f3dbf46d7b5e97b52bb75a3f96e7d9/LICENSE)                                |
| github.com/jackc/pgpassfile                               | v1.0.0                               | [MIT](https://raw.githubusercontent.com/jackc/pgpassfile/99d8e8e28945ffceaf75b0299fcb2bb656b8a683/LICENSE)                                      |
| github.com/jackc/pgservicefile                            | v0.0.0-20240606120523-5a60cdf6a761   | [MIT](https://raw.githubusercontent.com/jackc/pgservicefile/5a60cdf6a76120dc3d5152b95f3b5fd8aa7cc9eb/LICENSE)                                   |
| github.com/jackc/pgx/v5                                   | v5.11.0                              | [MIT](https://raw.githubusercontent.com/jackc/pgx/5e583fa7aabfa88b796292f849fc9d7d75ac159d/LICENSE)                                             |
| github.com/jackc/puddle/v2                                | v2.2.2                               | [MIT](https://raw.githubusercontent.com/jackc/puddle/bd09d14bd4018b6d65a9d7770e2f3ddf8b00af1c/LICENSE)                                          |
| github.com/justinas/nosurf                                | v1.2.0                               | [MIT](https://raw.githubusercontent.com/justinas/nosurf/ec9bb776d8e5ba9e906b6eb70428f4e7b009feee/LICENSE)                                       |
| github.com/open-feature/go-sdk                            | v1.19.0                              | [Apache-2.0](https://raw.githubusercontent.com/open-feature/go-sdk/5540b6a49436ba4687cfcc530e570caa6d472e40/LICENSE)                            |
| github.com/open-feature/go-sdk-contrib/providers/from-env | v0.1.6                               | [Apache-2.0](https://raw.githubusercontent.com/open-feature/go-sdk-contrib/7c63f0470c103b3189cc89d8c9b916cdf2ac92d4/providers/from-env/LICENSE) |
| golang.org/x/net                                          | v0.61.0                              | [BSD-3-Clause](https://raw.githubusercontent.com/golang/net/c1d31df183e51b1075a2fd670d6f6f4efb871773/LICENSE)                                   |
| golang.org/x/oauth2                                       | v0.37.0                              | [BSD-3-Clause](https://raw.githubusercontent.com/golang/oauth2/c624b89dadc3221560b7345c090bbe69e90808ee/LICENSE)                                |
| golang.org/x/sync                                         | v0.24.0                              | [BSD-3-Clause](https://raw.githubusercontent.com/golang/sync/36f2d70ecde9857dc4e363242a1369c7645d6770/LICENSE)                                  |
| golang.org/x/text                                         | v0.43.0                              | [BSD-3-Clause](https://raw.githubusercontent.com/golang/text/ac40b0f06c159afb5bfca4d1860f8b4b60dbc160/LICENSE)                                  |

The upstream license files retain their respective copyright/redistribution
conditions. The combined-runtime packager collects LICENSE, NOTICE and PATENTS
texts from every actually linked module directory and the Go toolchain. No server
binary is deployed by the static Pages artifact. Container validation additionally
checks its base image and tooling before distribution. Dependencies downloaded only for upstream
tests/tools are not represented as linked application runtime code here.

The module pins patched Go 1.27.3. CI verifies checksums and runs the actual graph
through pinned govulncheck 1.8.0; a successful scan means no reported reachable
vulnerability, not immunity from future advisories. Database CI uses disposable
PostgreSQL 17.11 with a digest-pinned official image, never a production database.

SCS supplies opaque sessions with its maintained pgx/v5 store, not an application
reimplementation of session cryptography. The application explicitly rotates and
destroys tokens, hashes stored tokens and checks current membership on each request.
SCS is pinned to upstream revision c45a9d1 because no tagged release contains its
memstore fix: the revision creates the default in-memory store's stop channel before
starting its cleanup worker, so the browser can stop that unused worker immediately
without a data race. Move to the first tagged release that contains c45a9d1.
Nosurf 1.2.0 is the upstream-patched version for
[CVE-2025-46721](https://github.com/justinas/nosurf/security/advisories/GHSA-w9hf-35q4-vcjw).
Every state-changing handler requires its token; origin checks alone are insufficient.
No direct Google/Microsoft SDK or paid identity service is added.

The from-env provider was assessed on 13 October 2026. Its maintained JSON
evaluation is process-local and uncached; its minimum SDK version does not
downgrade the application's locked SDK. The application adds a bounded staging
lifetime, not a replacement flag parser. The actual linked notice collector
includes this provider's Apache-2.0 license in distributed packages.
