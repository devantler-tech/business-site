---
title: 📁 .NET Template (retiring)
description: The .NET Template is retiring. Start new .NET projects from the Application Template, which lays down an ASP.NET Core stack with one command.
---

The .NET Template is retiring and will be archived. Start new .NET projects from the [Application Template](/templates/application-template/) instead: its .NET stack gives you a tested ASP.NET Core starting point, and it can also make your project a tenant on the devantler-tech platform.

**Repository**: [devantler-tech/application-template](https://github.com/devantler-tech/application-template)

## What's Inside

The [.NET stack](/templates/application-template/#net) is an ASP.NET Core minimal API with OpenFeature feature flags, xUnit tests and warnings-as-errors analyzers, built and tested in CI. Releases, linting, code scanning and dependency updates come from the template and stay current through its weekly sync.

Package publishing, such as to NuGet, is not part of the Application Template; add it in your own repository if your library needs it.

## Getting Started

Try the .NET stack in a throwaway copy. You need the .NET SDK that its `global.json` names:

```bash
git clone --depth 1 https://github.com/devantler-tech/application-template.git my-project
cd my-project
rm -rf .git                 # the scaffold refuses to run in the template itself
scripts/scaffold.sh dotnet  # add --tenancy to run it on the devantler-tech platform
dotnet test
```

The scaffold names the solution, projects and namespace after the PascalCase form of the name, which defaults to the directory name. The [Application Template guide](/templates/application-template/#getting-started) covers creating a real application from it.

## Links

- 🚀 [Application Template](/templates/application-template/)
- 📦 [Application Template on GitHub](https://github.com/devantler-tech/application-template)
- 📁 [.NET Template on GitHub](https://github.com/devantler-tech/dotnet-template) (retiring)
