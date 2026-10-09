---
title: "Local Kubernetes Development with KSail and K3d"
date: 2025-12-20
authors:
  - devantler
tags:
  - kubernetes
  - ksail
  - k3s
  - k3d
  - local-development
description: A guide to creating local Kubernetes development clusters using KSail with K3d (K3s in Docker).
excerpt: A guide to creating local Kubernetes development clusters using KSail with K3d (K3s in Docker).
cover:
  alt: Illustration of three compact server blocks on a lightweight development rail
  image: ../../../assets/editorial/journal-k3d.webp
---

K3s is Rancher's lightweight Kubernetes distribution, and when combined with [K3d](https://k3d.io/) and [KSail](https://github.com/devantler-tech/ksail), you get a fast, batteries-included local development experience. This post shows you how to get started.

## Why K3d + KSail?

**K3s** is a lightweight, certified Kubernetes distribution designed for resource-constrained environments. It comes with sensible defaults and includes components like Traefik ingress, local-path storage provisioner, and metrics-server out of the box.

**K3d** wraps K3s to run it in Docker containers, making it perfect for local development.

**KSail** ties it all together with a single binary that handles cluster provisioning, GitOps setup, and workload management. You get one consistent interface regardless of the underlying distribution.

Together, they provide:

- **Fast startup** — Clusters ready in about 30 seconds
- **Batteries included** — Storage, ingress, and metrics built in
- **Low resource usage** — K3s is optimized for minimal overhead
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
ksail cluster init --distribution K3s
```

This creates `ksail.yaml` (cluster configuration), `k3d.yaml` (K3d config), and `k8s/` (your Kubernetes manifests).

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

1. Creates Docker containers as K3s nodes
2. Bootstraps the K3s control plane with built-in components
3. Installs any additional components you configured (Cilium, Flux, etc.)
4. Configures your local kubeconfig

The process takes about 30 seconds for a single-node cluster.

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

- Swapping the default CNI for Cilium or Calico
- Enabling GitOps with Flux or ArgoCD
- Secret management with SOPS
- Mirror registries to avoid Docker Hub rate limits

If you want a vanilla Kubernetes experience, check out the post on [Kind with KSail](/blog/local-kubernetes-development-with-ksail-and-kind/). For an immutable, security-focused distribution, see [Talos with KSail](/blog/local-kubernetes-development-with-ksail-and-talos/).

### Feedback Welcome

KSail is under active development. If you encounter bugs or find missing features, please [open an issue on GitHub](https://github.com/devantler-tech/ksail/issues). Your feedback helps improve the tool for everyone.

---

_This blog post was written with the assistance of GitHub Copilot and Claude Opus 4.5._
