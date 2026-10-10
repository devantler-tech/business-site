# Dependency review

Reviewed on 10 October 2026 against the locked, actually compiled Go module
graph. This is an engineering inventory, not Danish legal clearance or a new
license for Devantler Tech's application. The application remains governed by
its repository's own terms. No hosted service or paid enrollment is introduced.

| Compiled module | Version | Upstream license |
| --- | --- | --- |
| coreos/go-oidc/v3 | 3.21.0 | [Apache-2.0](https://github.com/coreos/go-oidc/blob/v3.21.0/LICENSE) |
| jackc/pgx/v5 | 5.11.0 | [MIT](https://github.com/jackc/pgx/blob/v5.11.0/LICENSE) |
| open-feature/go-sdk | 1.19.0 | [Apache-2.0](https://github.com/open-feature/go-sdk/blob/v1.19.0/LICENSE) |
| go-jose/go-jose/v4 | 4.1.4 | [Apache-2.0](https://github.com/go-jose/go-jose/blob/v4.1.4/LICENSE) |
| jackc/pgpassfile | 1.0.0 | [MIT](https://github.com/jackc/pgpassfile/blob/v1.0.0/LICENSE) |
| jackc/pgservicefile | 0.0.0-20240606120523-5a60cdf6a761 | [MIT](https://github.com/jackc/pgservicefile/blob/5a60cdf6a761/LICENSE) |
| jackc/puddle/v2 | 2.2.2 | [MIT](https://github.com/jackc/puddle/blob/v2.2.2/LICENSE) |
| golang.org/x/oauth2 | 0.36.0 | [BSD-3-Clause](https://github.com/golang/oauth2/blob/v0.36.0/LICENSE) |
| golang.org/x/sync | 0.23.0 | [BSD-3-Clause](https://github.com/golang/sync/blob/v0.23.0/LICENSE) |
| golang.org/x/text | 0.42.0 | [BSD-3-Clause](https://github.com/golang/text/blob/v0.42.0/LICENSE) |

The upstream license files retain their respective copyright/redistribution
conditions. Before shipping a server binary/container, package all applicable
license and NOTICE texts with that artifact and recheck its full compiled graph,
base image and tooling. No server binary is distributed by this latent-library
PR or by the static Pages artifact. Dependencies downloaded only for upstream
tests/tools are not represented as linked application runtime code here.

The module pins patched Go 1.27.2. CI verifies checksums and runs the actual graph
through pinned govulncheck 1.8.0; a successful scan means no reported reachable
vulnerability, not immunity from future advisories. Database CI uses disposable
PostgreSQL 17.11 with a digest-pinned official image, never a production database.
