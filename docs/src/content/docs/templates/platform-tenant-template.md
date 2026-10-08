---
title: 🚀 Platform Tenant Template
description: A stack-neutral template for platform tenants on the devantler-tech platform, with signed build → publish → release plumbing kept current via template-sync.
---

A template for **platform tenants** on the [devantler-tech platform](https://github.com/devantler-tech/platform) — an application that runs on the platform from its own repository. Skip the CI/CD boilerplate — bring your own stack and start shipping.

**Repository**: [devantler-tech/platform-tenant-template](https://github.com/devantler-tech/platform-tenant-template)

It is intentionally **stack-neutral**: it carries no application code or language-specific tooling. Bring your own language and framework, and fill in the scaffolding.

## What's Inside

- **Signed supply chain** — On a `v*` tag, your image and `deploy/` manifests are built, digest-pinned, pushed as an OCI artifact, and [cosign](https://docs.sigstore.dev/)-signed. The platform checks that signature before it deploys anything, so only builds from this workflow can reach the cluster.
- **Release automation** — [semantic-release](https://semantic-release.gitbook.io/) turns Conventional-Commit merges to `main` into the `vX.Y.Z` tags that trigger deployment.
- **Stays current** — [template-sync](https://github.com/AndreasAugustin/actions-template-sync) opens a weekly PR keeping the shared CI/CD plumbing up to date across every tenant.
- **Security baseline** — A `zizmor.yml` policy requires GitHub Actions to be pinned, and CI enforces it.

## What the template owns vs. what you own

| Ownership | Files | Notes |
| --- | --- | --- |
| Template-owned | Shared CI/CD plumbing under `.github/workflows/` (`cd.yaml`, `release.yaml`, `template-sync.yaml`, `validate-scaffold.yaml`, `sync-labels.yaml`), `scripts/rename-placeholders.sh`, `CLAUDE.md`, `zizmor.yml` | Overwritten by `template-sync` |
| You own | App code, `Dockerfile`, `deploy/` manifests, `.github/CODEOWNERS`, `.github/workflows/ci.yaml`, `.github/dependabot.yml`, `AGENTS.md`, `.claude/skills/maintain/SKILL.md`, `README.md`, `.releaserc`, `.gitignore`, `LICENSE`, `.templatesyncignore` | Declare in `.templatesyncignore` (same syntax as `.gitignore`), using these full paths |

See the template's [README](https://github.com/devantler-tech/platform-tenant-template#what-the-template-owns-vs-what-you-own) for the authoritative file-by-file list.

## Getting Started

```bash
# Create a new private repo from the template
gh repo create devantler-tech/my-tenant --template devantler-tech/platform-tenant-template --private --clone
cd my-tenant

# Run this FIRST. It renames the `app` and `REPLACE_ME` placeholders in deploy/
# to your tenant name — defaulting to the repo directory name, or pass your own.
# Doing it by hand is easy to get half-wrong. Delete the helper once you're set up.
scripts/rename-placeholders.sh        # or: scripts/rename-placeholders.sh my-tenant

# Replace the rest of the scaffolding with your app (code, Dockerfile, ci.yaml),
# then validate locally:
kubectl kustomize deploy/             # manifests build
actionlint .github/workflows/*        # workflows parse
```

Then register the tenant on the platform by following [`platform/docs/TENANTS.md`](https://github.com/devantler-tech/platform/blob/main/docs/TENANTS.md).

> **Convention:** the Deployment's container `name` MUST equal the repository name — `publish-app` pins the built image digest into the container with that name.

## Links

- 📦 [Template on GitHub](https://github.com/devantler-tech/platform-tenant-template)
- 🛰️ [The devantler-tech platform](https://github.com/devantler-tech/platform)
- 🔄 [Reusable workflows used by this template](https://github.com/devantler-tech/actions/tree/main/.github/workflows)
