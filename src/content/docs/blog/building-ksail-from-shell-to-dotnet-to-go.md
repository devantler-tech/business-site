---
title: "Building KSail: From Shell Scripts to .NET to Go"
date: 2026-01-07
authors:
  - devantler
tags:
  - ksail
  - go
  - dotnet
  - architecture
  - open-source
description: The journey of building KSail through three major rewrites and what I learned along the way.
excerpt: The journey of building KSail through three major rewrites and what I learned along the way.
cover:
  alt: Illustration of three increasingly refined developer-tool prototypes
  image: ../../../assets/editorial/journal-tool-evolution.webp
---

Building a developer tool is rarely a straight path. [KSail](https://github.com/devantler-tech/ksail) started as a humble shell script in late 2023 and has evolved through three major rewrites to become what it is today: a Go-based CLI that bundles common Kubernetes tooling into a single binary. This post tells the story of that journey.

## The Problem

Setting up and operating Kubernetes clusters is a skill of its own. It often requires juggling multiple CLI tools (kubectl, helm, kind, k3d, flux, argocd, sops), writing bespoke scripts, and dealing with inconsistent developer workflows determined by specific projects.

This complexity slows down development, makes Kubernetes intimidating for newcomers, and makes it difficult to maintain reproducible environments. I wanted a tool that would remove the tooling overhead so developers could focus on their workloads.

## Phase 1: Shell Scripts (December 2023)

The first commit to KSail was on December 31, 2023. It started as a Bash script called `ksail.sh` that wrapped Talos Linux commands to create local Kubernetes clusters.

```bash
# Early KSail was literally just a shell script
./ksail.sh up   # Create a cluster
./ksail.sh down # Destroy it
```

This worked, but shell scripts have significant limitations:

- **No type safety**: Bugs only surface at runtime
- **Limited error handling**: Bash's error handling is notoriously awkward
- **Cross-platform issues**: Scripts that work on macOS might fail on Linux
- **Dependency management**: Users needed every tool installed separately

Within the first week, I added K3d support alongside Talos, and the script was becoming unwieldy. It was clear that a more structured approach was needed.

## Phase 2: The .NET Rewrite (January 2024)

On January 8, 2024 — just over a week after the initial commit — I rewrote KSail as a .NET console application with PR #25: "Rework KSail to .NET Project with wrapped binaries and libraries."

### Why .NET?

At the time, .NET was my primary language. I was comfortable with C#, and .NET 8 had just been released with great cross-platform support. The ecosystem had mature libraries for CLI development (`System.CommandLine`), and I could leverage NuGet for dependency management.

### The Architecture

The .NET version took an interesting approach: it embedded the required binaries (kubectl, kind, k3d, talosctl, etc.) directly into the application and extracted them at runtime. This meant users only needed to install KSail itself — the dependencies came bundled.

```csharp
// Pseudo-code of the .NET approach
public class KindClient
{
    private readonly string _binaryPath;

    public KindClient()
    {
        _binaryPath = ExtractEmbeddedBinary("kind");
    }

    public async Task CreateClusterAsync(string name, string config)
    {
        await ProcessRunner.RunAsync(_binaryPath, $"create cluster --name {name} --config {config}");
    }
}
```

Over 2024, the .NET version grew substantially. I presented it at KCD Denmark 2024 in a talk titled ["KSail - a Kubernetes SDK for local GitOps development and CI"](https://youtu.be/Q-Hfn_-B7p8). By the end of 2024, the project had accumulated 788 commits and supported:

- Multiple distributions (Kind, K3d, Talos)
- GitOps engines (Flux, ArgoCD)
- Secret management with SOPS
- Declarative configuration via `ksail.yaml`
- Mirror registries for avoiding Docker Hub rate limits

### Growing Pains

Despite its functionality, the .NET version had issues:

1. **Binary size**: Bundling all those executables made the final binary quite large
2. **Update complexity**: Every upstream tool update required downloading new binaries, embedding them, and releasing a new version
3. **Platform coverage**: Supporting Linux amd64, Linux arm64, macOS arm64, and Windows meant managing multiple binary variants
4. **Startup time**: Extracting binaries on first run added latency
5. **Ecosystem mismatch**: Most Kubernetes tooling is written in Go, and calling out to external processes felt like fighting the ecosystem

## Phase 3: The Go Rewrite (December 2025)

On December 18, 2025, I merged PR #1448: "BREAKING CHANGE: Migrate ksail from .NET to Go." This wasn't a port — it was a complete rewrite that took advantage of Go's unique position in the Kubernetes ecosystem.

### Why Go?

The Kubernetes ecosystem is built on Go. kubectl, kind, k3d, helm, flux — they're all Go projects. This means:

1. **Import instead of wrap**: Instead of shelling out to external binaries, KSail can import these tools as Go libraries
2. **Single binary**: No embedded executables to extract; everything compiles into one binary
3. **Fast startup**: No extraction step, no JIT compilation
4. **First-class Kubernetes support**: client-go is the reference implementation for Kubernetes clients
5. **Cross-compilation**: Go's cross-compilation is trivial compared to .NET's AOT publishing

### The New Architecture

The Go version embeds tool functionality directly:

```go
// Instead of shelling out, we use the libraries directly
import (
    "sigs.k8s.io/kind/pkg/cluster"
    "helm.sh/helm/v4/pkg/action"
    "github.com/fluxcd/flux2/v2/pkg/bootstrap"
)

func (p *KindProvisioner) Create(ctx context.Context, config *KindConfig) error {
    provider := cluster.NewProvider()
    return provider.Create(config.Name,
        cluster.CreateWithConfigFile(config.Path),
        cluster.CreateWithWaitForReady(config.Timeout),
    )
}
```

The project structure is clean and idiomatic:

```
pkg/
├── apis/          # Configuration types
├── cli/           # Command implementations
├── client/        # Embedded tool clients (kubectl, helm, flux, etc.)
├── di/            # Dependency injection
├── io/            # File and config utilities
├── k8s/           # Kubernetes helpers
├── svc/           # Services (installers, provisioners, reconcilers)
└── utils/         # Shared utilities
```

## Lessons Learned

After three rewrites and over 2,200 commits, here's what I've learned:

### 1. Choose the Right Language for the Ecosystem

The Go rewrite wasn't about Go being "better" than .NET — it was about Go being the right tool for Kubernetes development. When your tool integrates deeply with an ecosystem, speaking its native language matters.

### 2. Wrapping vs. Embedding

The .NET version wrapped external binaries. The Go version embeds libraries. The difference is profound:

- **Wrapping**: You're at the mercy of the tool's CLI interface, which can change between versions
- **Embedding**: You have direct access to the tool's internals, with type safety and proper error handling

### 3. Start Simple, Then Structure

The shell script taught me what the tool needed to do. The .NET version taught me how to structure it properly. The Go version combined those lessons with the right technology choice.

### 4. Don't Fear the Rewrite

Each rewrite made KSail better. The shell script validated the idea. The .NET version proved the concept and found users. The Go version delivered on the original vision of a single, fast, portable binary.

## Where KSail Is Today

The current Go version of KSail provides:

- **One binary** — No external dependencies except Docker
- **Multiple distributions** — Kind, K3d, and Talos
- **Customizable stack** — CNI (Cilium, Calico), CSI, metrics-server, cert-manager
- **GitOps native** — Built-in Flux and ArgoCD support
- **Secret management** — Integrated SOPS encryption
- **Declarative configuration** — Everything as code in `ksail.yaml`

Installation is simple:

```bash
brew install --cask devantler-tech/tap/ksail
# or
go install github.com/devantler-tech/ksail/v5@latest
```

And usage is straightforward:

```bash
ksail cluster init --distribution Kind --cni Cilium --gitops-engine Flux
ksail cluster create
ksail workload apply -k ./k8s
ksail cluster connect  # Opens K9s
```

## What's Next

KSail is actively developed with a focus on:

- **Cloud providers**: Hetzner Cloud support is in progress
- **Enhanced GitOps workflows**: Better reconciliation and debugging tools
- **Enhanced SOPS integration**: More secret management features for e.g. GitOps.

The project is open source under the Apache 2.0 license. You can find:

- **Repository**: [github.com/devantler-tech/ksail](https://github.com/devantler-tech/ksail)
- **Documentation**: [ksail.devantler.tech](https://ksail.devantler.tech)
- **Go Package Docs**: [pkg.go.dev/github.com/devantler-tech/ksail/v5](https://pkg.go.dev/github.com/devantler-tech/ksail/v5)

If you're working with Kubernetes locally, give KSail a try. And if you've been through similar rewrites with your own projects, I'd love to hear your stories.

---

_This blog post was written with the assistance of GitHub Copilot and Claude Opus 4.5. The content reflects my genuine experiences and journey; the AI helped structure and articulate it._
