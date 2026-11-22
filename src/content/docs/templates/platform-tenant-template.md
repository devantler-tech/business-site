---
title: 🚀 Application Template
description: One starter for new applications. Pick a Go, .NET, SvelteKit or your own stack, optionally run it on the devantler-tech platform as a tenant, and keep the shared CI/CD current through a weekly sync.
---

One command lays down a **Go**, **.NET**, **SvelteKit** or bring-your-own stack and, if you want it, makes the application a **platform tenant** on the [devantler-tech platform](https://github.com/devantler-tech/platform). A weekly sync keeps the shared plumbing current afterwards.

**Repository**: [devantler-tech/platform-tenant-template](https://github.com/devantler-tech/platform-tenant-template) (being renamed `application-template`)

## What's Inside

`scripts/scaffold.sh <go|dotnet|svelte|none> [--tenancy] [name]` runs once in a new copy of the template and shapes it into your application.

### Go

A Go `net/http` server with [OpenFeature](https://openfeature.dev/) feature flags, table-driven tests and golangci-lint, checked in CI by the shared Go validator.

### .NET

An ASP.NET Core minimal API with OpenFeature feature flags, xUnit tests and warnings-as-errors analyzers, built and tested in CI. Package publishing, such as to NuGet, is not part of the template; add it in your own repository if you need it.

### SvelteKit

A [SvelteKit](https://svelte.dev/docs/kit) app with `adapter-node`, OpenFeature feature flags, Vitest, svelte-check and ESLint.

### Your own stack

Choose `none` to keep a placeholder CI job for the stack you bring yourself.

### Platform tenancy

`--tenancy` adds `deploy/` Kustomize manifests and CI that builds the image and renders `deploy/` before merge. On every `v*` tag, your image and manifests are built, digest-pinned, pushed as an OCI artifact and [cosign](https://docs.sigstore.dev/)-signed. The platform verifies that signature, which names your repository's own release workflow, before it deploys anything.

Every stack serves a placeholder page at `GET /` on port 3000, where a tenant's probes look, so a new tenant deploys before you write any code. Without `--tenancy`, the tenant plumbing is removed and the application gets CI and releases only.

## What the template owns vs. what you own

The weekly [template-sync](https://github.com/AndreasAugustin/actions-template-sync) keeps one copy of each cross-cutting concern current in every application. Where yours needs to differ, change your own file, which the sync never touches:

| Concern | Shared, kept current by the sync | Yours, to diverge |
| --- | --- | --- |
| Releases | `release.yaml`: [semantic-release](https://semantic-release.gitbook.io/) turns Conventional-Commit merges into `vX.Y.Z` tags | `.releaserc` |
| Linting | `lint.yaml` and a shared MegaLinter base configuration | `.mega-linter.yml`, which extends the base |
| Code scanning | `codeql.yaml`: CodeQL for the languages it finds | `.github/codeql/codeql-config.yml` |
| Dependency updates | Renovate for a tenant's `deploy/` images and charts | `.github/dependabot.yml` for packages, Actions and the Dockerfile |
| Agent instructions | `CLAUDE.md` | `AGENTS.md` |

Your application code, `Dockerfile`, `deploy/`, `.github/workflows/ci.yaml`, `README.md` and the other files you own are listed in `.templatesyncignore` (same syntax as `.gitignore`), so the sync never overwrites them.

The template's [reference](https://github.com/devantler-tech/platform-tenant-template/blob/main/docs/REFERENCE.md#what-the-template-owns-vs-what-you-own) has the authoritative file-by-file list.

## Getting Started

Try it in a throwaway copy. You need Git, a POSIX shell and the stack's toolchain; no GitHub token, cloud account or cluster:

```bash
git clone --depth 1 https://github.com/devantler-tech/platform-tenant-template.git demo-app
cd demo-app
rm -rf .git                        # the scaffold refuses to run in the template itself
scripts/scaffold.sh go --tenancy   # or dotnet, svelte or none; drop --tenancy for a plain app
go test ./...
```

The scaffold prints `Scaffolded 'demo-app' (stack: go, platform tenant: yes).` With `--tenancy`, `kubectl kustomize deploy/` then renders the tenant's manifests and `docker build .` builds its image.

To create a real application:

1. **Create its repository from the template.** GitHub's template option is not enabled for this repository yet; [issue 211](https://github.com/devantler-tech/platform-tenant-template/issues/211) tracks it.
2. **Scaffold it.** In the new checkout, run `scripts/scaffold.sh` once with your stack, and `--tenancy` if it will run on the platform. The name defaults to the directory name and must equal the repository name. Review `git status` and `git diff`, then commit.
3. **Make it yours.** Replace the placeholder page with your app, fill in the remaining placeholders in `AGENTS.md`, and replace `README.md` and `CONTRIBUTING.md` with your own.
4. **Protect your files from the weekly sync** by tailoring the supplied `.templatesyncignore`. The sync needs the template's GitHub App credentials.
5. **Register a tenant** on the platform by following [`platform/docs/TENANTS.md`](https://github.com/devantler-tech/platform/blob/main/docs/TENANTS.md).

> **Convention:** a tenant's Deployment container `name` MUST equal the repository name — the release workflow pins the built image digest into the container with that name.

## Links

- 📦 [Template on GitHub](https://github.com/devantler-tech/platform-tenant-template)
- 📖 [Template reference](https://github.com/devantler-tech/platform-tenant-template/blob/main/docs/REFERENCE.md)
- 🛰️ [The devantler-tech platform](https://github.com/devantler-tech/platform)
- 🔄 [Reusable workflows used by this template](https://github.com/devantler-tech/.github/tree/main/.github/workflows)
