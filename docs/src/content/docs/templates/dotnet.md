---
title: 📁 .NET Template
description: A minimal .NET template with CI/CD, GitHub Packages/NuGet publishing, GitHub Code Quality coverage, EditorConfig, and Dependabot.
---

A minimal, batteries-included template for new .NET projects and libraries. Skip the boilerplate — start shipping.

**Repository**: [devantler-tech/dotnet-template](https://github.com/devantler-tech/dotnet-template)

## What's Inside

- **CI/CD** — GitHub Actions workflows for build, test, lint, and release
- **Publishing** — Release libraries to [GitHub Packages](https://docs.github.com/en/packages) and [NuGet](https://www.nuget.org/) automatically
- **Testing** — Test projects with coverage reporting via [GitHub Code Quality](https://docs.github.com/code-security/code-quality)
- **Code style** — `.editorconfig` with opinionated .NET code style
- **Dependency management** — [Dependabot](https://docs.github.com/code-security/dependabot) keeps NuGet packages and Actions up to date
- **Release automation** — Semantic versioning and automated GitHub Releases

## Getting Started

```bash
# Create a new repo from the template
gh repo create my-project --template devantler-tech/dotnet-template --public --clone
cd my-project

# Repoint the `Example` scaffold (.slnx, src/, tests/, README) to your project
# name in one shot — run this first. With no argument it derives a PascalCase
# name from your origin GitHub remote. Review the result with `git diff`.
./scripts/rename-placeholders.sh Widget   # e.g. your project name

# Restore packages
dotnet restore

# Run tests
dotnet test
```

## Links

- 📦 [Template on GitHub](https://github.com/devantler-tech/dotnet-template)
- 🔄 [Reusable workflows used by this template](https://github.com/devantler-tech/actions/tree/main/.github/workflows)
