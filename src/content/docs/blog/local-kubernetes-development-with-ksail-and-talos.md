---
title: "Local Kubernetes Development with KSail and Talos"
date: 2025-12-27
authors:
  - devantler
tags:
  - kubernetes
  - ksail
  - talos
  - local-development
description: A guide to creating local Kubernetes development clusters using KSail with Talos Linux in Docker.
excerpt: A guide to creating local Kubernetes development clusters using KSail with Talos Linux in Docker.
cover:
  alt: Illustration of a small cluster on a developer’s workbench
  image: ../../../assets/editorial/kubernetes-workshop.webp
---

[Talos Linux](https://www.talos.dev/) is a minimal, immutable operating system designed specifically for Kubernetes. While it's often used in production environments, you can also run Talos locally in Docker for development. Combined with [KSail](https://github.com/devantler-tech/ksail), you get a security-focused local development experience. This post shows you how.

## Why Talos + KSail?

**Talos Linux** takes a radically different approach to running Kubernetes. There's no SSH, no shell, no package manager — just the Talos API and Kubernetes. The entire OS is immutable and managed declaratively via configuration files.

This approach provides:

- **Minimal attack surface** — No shell means no shell exploits
- **No configuration drift** — Immutable OS prevents unauthorized changes
- **Declarative everything** — OS configuration is versioned and reproducible
- **Production parity** — Your local environment matches production Talos clusters

**KSail** wraps Talos tooling into a single binary, handling cluster provisioning, GitOps setup, and workload management through one consistent interface.

## Prerequisites

You need Docker installed and running. Verify with:

```bash
docker ps
```

If this command works, you're ready to go.

## Step 1: Install KSail

KSail is distributed as a single binary. Install via Homebrew:

```bash
brew install --cask devantler-tech/tap/ksail
```

Or with Go:

```bash
go install github.com/devantler-tech/ksail/v5@latest
```

Verify the installation:

```bash
ksail --version
```

## Step 2: Scaffold Your Cluster Project

KSail's `init` command scaffolds a complete project structure. For a Talos cluster with Cilium CNI:

```bash
mkdir my-cluster && cd my-cluster
ksail cluster init --distribution Talos --cni Cilium
```

This creates `ksail.yaml` (cluster configuration), `talos/` (Talos configs), and `k8s/` (your Kubernetes manifests).

> [!NOTE]
> Talos doesn't include a default CNI, so you should specify one. Cilium is recommended for its eBPF-based networking and observability features.

For all available flags and configuration options, see the [KSail documentation](https://ksail.devantler.tech):

- [CLI flags reference](https://ksail.devantler.tech/configuration/cli-flags/cluster/cluster-init) — All `cluster init` options
- [ksail.yaml reference](https://ksail.devantler.tech/configuration/declarative-configuration) — Configuration file schema
- [Features overview](https://ksail.devantler.tech/features) — CNI, CSI, GitOps, and more

## Step 3: Create the Cluster

Create and start your cluster:

```bash
ksail cluster create
```

This command:

1. Creates Docker containers running Talos Linux
2. Bootstraps the Talos control plane
3. Initializes etcd and the Kubernetes API server
4. Installs your selected CNI, CSI, and other components
5. Configures your local kubeconfig and talosconfig

The process takes 1-2 minutes. Talos clusters take slightly longer than Kind or K3d because of the additional bootstrap steps for the immutable OS.

## Step 4: Working with Your Cluster

Once your cluster is running, KSail provides commands for common operations:

```bash
ksail cluster info      # Show cluster status
ksail cluster list      # List all KSail-managed clusters
ksail cluster connect   # Open K9s for interactive management
ksail cluster stop      # Stop the cluster
ksail cluster start     # Start a stopped cluster
```

Your kubeconfig is automatically configured, so standard `kubectl` commands work too.

For the full command reference, see [Cluster Commands](https://ksail.devantler.tech/configuration/cli-flags/cluster/cluster-root).

## Step 5: Deploying Workloads

KSail wraps kubectl and GitOps operations under the `workload` command:

```bash
ksail workload apply -k ./k8s    # Apply manifests (kubectl workflow)
ksail workload push              # Push to GitOps source
ksail workload reconcile         # Trigger GitOps reconciliation
```

For the full workload command reference, see [Workload Commands](https://ksail.devantler.tech/configuration/cli-flags/workload/workload-root).

## Cleaning Up

When you're done:

```bash
ksail cluster delete
```

This removes the Docker containers and cleans up kubeconfig and talosconfig entries.

## What's Next

Explore the [KSail documentation](https://ksail.devantler.tech) for advanced topics including:

- Multi-node Talos clusters for testing HA scenarios
- Enabling GitOps with Flux or ArgoCD
- Secret management with SOPS
- Mirror registries to avoid Docker Hub rate limits

Once you're comfortable with Talos locally, you can deploy to cloud infrastructure. See [Creating Development Clusters on Hetzner with KSail and Talos](/blog/creating-development-kubernetes-clusters-on-hetzner-with-ksail-and-talos/) for a guide on running Talos in the cloud.

For simpler local setups, check out [Kind with KSail](/blog/local-kubernetes-development-with-ksail-and-kind/) for vanilla Kubernetes or [K3s with KSail](/blog/local-kubernetes-development-with-ksail-and-k3d/) for a batteries-included experience.

### Feedback Welcome

KSail is under active development. If you encounter bugs or find missing features, please [open an issue on GitHub](https://github.com/devantler-tech/ksail/issues). Your feedback helps improve the tool for everyone.

---

_This blog post was written with the assistance of GitHub Copilot and Claude Opus 4.5._
