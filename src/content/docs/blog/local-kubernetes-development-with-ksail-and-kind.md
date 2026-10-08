---
title: "Local Kubernetes Development with KSail and Kind"
date: 2025-12-15
authors:
  - devantler
tags:
  - kubernetes
  - ksail
  - kind
  - local-development
description: A guide to creating local Kubernetes development clusters using KSail with Kind (vanilla Kubernetes) in Docker.
excerpt: A guide to creating local Kubernetes development clusters using KSail with Kind (vanilla Kubernetes) in Docker.
cover:
  alt: Illustration of a miniature local cluster inside nested workshop boxes
  image: ../../../assets/editorial/journal-kind.webp
---

Getting started with Kubernetes development shouldn't require cloud infrastructure or complex setup procedures. With [Kind](https://kind.sigs.k8s.io/) (Kubernetes in Docker) and [KSail](https://github.com/devantler-tech/ksail), you can have a local cluster running in under a minute. This post shows you how.

## Why Kind + KSail?

**Kind** (Kubernetes in Docker) runs Kubernetes clusters using Docker containers as nodes. It's fast, lightweight, and provides a vanilla Kubernetes experience that closely matches production environments.

**KSail** wraps Kind and other tools into a single binary, giving you one consistent interface for cluster provisioning, GitOps setup, and workload management. Instead of learning multiple CLIs, you use `ksail cluster` and `ksail workload` commands.

Together, they provide:

- **Fast iteration** — Clusters start in under 60 seconds
- **Zero cost** — Everything runs locally on your machine
- **Vanilla Kubernetes** — No distribution-specific quirks to learn
- **Declarative configuration** — Everything as code in `ksail.yaml`

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

KSail's `init` command scaffolds a complete project structure:

```bash
mkdir my-cluster && cd my-cluster
ksail cluster init --distribution Vanilla
```

This creates `ksail.yaml` (cluster configuration), `kind.yaml` (Kind config), and `k8s/` (your Kubernetes manifests).

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

1. Creates Docker containers as Kubernetes nodes
2. Bootstraps the Kubernetes control plane
3. Installs your selected CNI, CSI, and other components
4. Configures your local kubeconfig

The process takes under 60 seconds for a single-node cluster.

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

This removes the Docker containers and cleans up kubeconfig entries.

## What's Next

Explore the [KSail documentation](https://ksail.devantler.tech) for advanced topics including:

- Adding Cilium or Calico as your CNI
- Enabling GitOps with Flux or ArgoCD
- Secret management with SOPS
- Mirror registries to avoid Docker Hub rate limits

If Kind's vanilla Kubernetes feels too basic, check out the posts on [K3s with KSail](/blog/local-kubernetes-development-with-ksail-and-k3d/) for a more batteries-included experience, or [Talos with KSail](/blog/local-kubernetes-development-with-ksail-and-talos/) for an immutable, security-focused distribution.

### Feedback Welcome

KSail is under active development. If you encounter bugs or find missing features, please [open an issue on GitHub](https://github.com/devantler-tech/ksail/issues). Your feedback helps improve the tool for everyone.

---

_This blog post was written with the assistance of GitHub Copilot and Claude Opus 4.5._
