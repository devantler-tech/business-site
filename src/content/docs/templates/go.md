---
title: 🐹 Go Template (retiring)
description: The Go Template is retiring. Start new Go projects from the Application Template, which lays down a Go stack with one command.
---

The Go Template is retiring and will be archived. Start new Go projects from the [Application Template](/templates/application-template/) instead: its Go stack gives you the same kind of starting point, and it can also make your project a tenant on the devantler-tech platform.

**Repository**: [devantler-tech/application-template](https://github.com/devantler-tech/application-template)

## What's Inside

The [Go stack](/templates/application-template/#go) is a `net/http` server with OpenFeature feature flags, table-driven tests and golangci-lint, checked in CI by the shared Go validator. Releases, linting, code scanning and dependency updates come from the template and stay current through its weekly sync.

## Getting Started

Try the Go stack in a throwaway copy:

```bash
git clone --depth 1 https://github.com/devantler-tech/application-template.git my-project
cd my-project
rm -rf .git                 # the scaffold refuses to run in the template itself
scripts/scaffold.sh go      # add --tenancy to run it on the devantler-tech platform
go test ./...
```

The scaffold sets the module path to `github.com/devantler-tech/<name>`, where the name defaults to the directory name. The [Application Template guide](/templates/application-template/#getting-started) covers creating a real application from it.

## Links

- 🚀 [Application Template](/templates/application-template/)
- 📦 [Application Template on GitHub](https://github.com/devantler-tech/application-template)
- 🐹 [Go Template on GitHub](https://github.com/devantler-tech/go-template) (retiring)
