---
title: "Creating Kubernetes Clusters on Hetzner with KSail and Talos"
date: 2026-01-13
authors:
  - devantler
tags:
  - kubernetes
  - ksail
  - talos
  - hetzner
  - cloud
description: A step-by-step guide to creating Talos Linux Kubernetes clusters on Hetzner Cloud using KSail.
excerpt: A step-by-step guide to creating Talos Linux Kubernetes clusters on Hetzner Cloud using KSail.
cover:
  alt: Illustration of a remote server cluster reached by a cable across folded terrain
  image: ../../../assets/editorial/journal-hetzner.webp
---

Setting up Kubernetes environments doesn't have to be expensive or complicated. With [Hetzner Cloud](https://www.hetzner.com/cloud/)'s affordable pricing, [Talos Linux](https://www.talos.dev/)'s security-focused immutable OS, and [KSail](https://github.com/devantler-tech/ksail)'s unified tooling, you can have a cluster running in minutes. This post walks through the complete setup.

## Why Hetzner + Talos + KSail?

**Hetzner Cloud** offers some of the most affordable cloud VPS servers in the industry. A capable `cx23` server (2 vCPU, 4GB RAM) costs around €3.74/month — significantly cheaper than equivalent offerings from AWS, GCP, or Azure.

**Talos Linux** is a minimal, immutable operating system designed specifically for Kubernetes. There's no SSH, no shell, no package manager — just the Talos API and Kubernetes. This dramatically reduces the attack surface and eliminates configuration drift.

**KSail** brings it all together with a single binary that handles cluster provisioning, GitOps setup, and workload management. Instead of juggling `hcloud`, `talosctl`, `kubectl`, `helm`, and `flux` commands, you use one consistent interface.

## Step 1: Create a Hetzner Account

If you don't already have a Hetzner account:

1. Go to [Hetzner Cloud Console](https://console.hetzner.cloud/)
2. Click "Register" and complete the signup process
3. Verify your email and complete any required identity verification

Hetzner has documentation for getting started: [Account Getting Started Guide](https://docs.hetzner.com/general/billing-and-account-management/account-getting-started/).

## Step 2: Create a Hetzner Project

Projects in Hetzner Cloud are organizational units that contain your resources (servers, networks, load balancers, etc.). Each project has its own API tokens and billing.

1. Log into the [Hetzner Cloud Console](https://console.hetzner.cloud/)
2. Click "New Project" in the sidebar
3. Name your project (e.g., "kubernetes-dev" or "my-homelab")
4. Click "Create"

## Step 3: Generate an API Token

KSail needs an API token to provision and manage Hetzner Cloud resources.

1. In your project, go to **Security** → **API Tokens**
2. Click **Generate API Token**
3. Name the token (e.g., "ksail-cluster-management")
4. Select **Read & Write** permissions — KSail needs to create and delete servers
5. Click **Generate API Token**
6. **Copy the token immediately** — you won't be able to see it again!

For more details, see Hetzner's [Generating API Token Guide](https://docs.hetzner.com/cloud/api/getting-started/generating-api-token/).

## Step 4: Export the API Token

KSail reads the Hetzner API token from the `HCLOUD_TOKEN` environment variable. Add it to your shell configuration:

```bash
# For the current session
export HCLOUD_TOKEN="your-api-token-here"

# To persist across sessions, add to your shell config
echo 'export HCLOUD_TOKEN="your-api-token-here"' >> ~/.zshrc  # or ~/.bashrc
source ~/.zshrc
```

You can verify the token is set:

```bash
echo $HCLOUD_TOKEN | head -c 10  # Should show first 10 characters
```

## Step 5: Install KSail

KSail is distributed as a single binary. The easiest installation method is via Homebrew:

```bash
brew install --cask devantler-tech/tap/ksail
```

Alternatively, if you have Go installed:

```bash
go install github.com/devantler-tech/ksail/v5@latest
```

Verify the installation:

```bash
ksail --version
```

## Step 6: Scaffold Your Cluster Project

KSail's `init` command scaffolds a complete project structure. For a basic Talos cluster on Hetzner with Cilium CNI:

```bash
mkdir my-cluster && cd my-cluster
ksail cluster init --distribution Talos --provider Hetzner --cni Cilium
```

This creates `ksail.yaml` (cluster configuration), `talos/` (Talos configs), and `k8s/` (your Kubernetes manifests).

**For GitOps workflows**, add a GitOps engine and external registry:

```bash
ksail cluster init \
  --distribution Talos \
  --provider Hetzner \
  --cni Cilium \
  --gitops-engine Flux \
  --local-registry '${GITHUB_USER}:${GITHUB_TOKEN}@ghcr.io/your-org/your-cluster'
```

This configures Flux to sync manifests from GitHub Container Registry. See [Step 9: Deploying Workloads](#step-9-deploying-workloads) for the full GitOps workflow.

For all available flags and configuration options, see the [KSail documentation](https://ksail.devantler.tech):

- [CLI flags reference](https://ksail.devantler.tech/configuration/cli-flags/cluster/cluster-init) — All `cluster init` options
- [ksail.yaml reference](https://ksail.devantler.tech/configuration/declarative-configuration) — Configuration file schema
- [Features overview](https://ksail.devantler.tech/features) — CNI, CSI, GitOps, and more

## Step 7: Create the Cluster

With your configuration ready, create the cluster:

```bash
ksail cluster create
```

This command:

1. Creates servers in Hetzner Cloud
2. Configures the private network
3. Bootstraps Talos Linux on each node
4. Initializes the Kubernetes cluster
5. Installs your selected CNI, CSI, and other components
6. Configures your local kubeconfig
7. Configures your local talosconfig

The process takes 3-5 minutes depending on your cluster size.

You can watch the progress as KSail outputs status updates for each stage.

## Step 8: Working with Your Cluster

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

## Step 9: Deploying Workloads

KSail wraps kubectl and GitOps operations under the `workload` command. For cloud clusters like Hetzner, you have two main options for deploying workloads.

### Option A: Direct kubectl Workflow

The simplest approach applies manifests directly to the cluster:

```bash
ksail workload apply -k ./k8s    # Apply Kustomize manifests
ksail workload get pods          # Check pod status
ksail workload logs deployment/my-app  # View logs
```

This works well for quick iterations but doesn't provide GitOps benefits like drift detection and automatic reconciliation.

### Option B: GitOps with External Registry

For cloud clusters without a local Docker registry, you can use an external OCI registry like GitHub Container Registry (ghcr.io). This enables full GitOps workflows with Flux or ArgoCD.

**Step 1: Create a GitHub Personal Access Token**

1. Go to GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Create a token with `write:packages` and `read:packages` scopes
3. Export the credentials:

```bash
export GITHUB_USER="your-username"
export GITHUB_TOKEN="ghp_your-token-here"
```

**Step 2: Initialize with GitOps and External Registry**

When scaffolding your cluster, specify the GitOps engine and external registry:

```bash
ksail cluster init \
  --distribution Talos \
  --provider Hetzner \
  --cni Cilium \
  --gitops-engine Flux \
  --local-registry '${GITHUB_USER}:${GITHUB_TOKEN}@ghcr.io/your-org/your-cluster'
```

The `--local-registry` flag accepts the format `[user:pass@]host[:port][/path]`. Environment variable placeholders like `${GITHUB_TOKEN}` are expanded at runtime, keeping credentials out of your config files.

**Step 3: Create the Cluster**

```bash
ksail cluster create
```

This installs Flux and configures it to sync from your external registry.

**Step 4: Push and Reconcile Workloads**

```bash
# Package manifests and push to registry
ksail workload push

# Trigger GitOps reconciliation
ksail workload reconcile
```

The `push` command packages your `k8s/` directory as an OCI artifact and pushes it to ghcr.io. Flux then pulls and applies the manifests automatically.

> [!TIP]
> You can also set the registry via environment variable: `KSAIL_REGISTRY='ghcr.io/org/repo' ksail workload push`

For the full workload command reference, see [Workload Commands](https://ksail.devantler.tech/configuration/cli-flags/workload/workload-root).

## Cleaning Up

When you're done with the cluster:

```bash
ksail cluster delete
```

This removes:

- All Hetzner Cloud servers
- The private network
- Placement groups
- Local kubeconfig entries
- Local talosconfig entries

> [!WARNING]
> This is destructive and cannot be undone. All data in the cluster will be lost.

## Cost Considerations

A minimal development setup (1 control plane) using a `cx23` server costs under €4/month.

For a more robust setup (3 control planes + 2 workers) using `cx23` servers:

| Component              | Monthly Cost      |
| ---------------------- | ----------------- |
| 3x cx23 control planes | €11.22            |
| 2x cx23 workers        | €7.48             |
| Private network        | Free              |
| **Total**              | **~€18.70/month** |

This is remarkably affordable for a Kubernetes development cluster.

## What's Next

Explore the [KSail documentation](https://ksail.devantler.tech) for advanced topics including secret management with SOPS, mirror registries, and GitOps workflows.

### Planned: Production-Grade Features

Integration with [Hetzner Cloud Controller Manager](https://github.com/hetznercloud/hcloud-cloud-controller-manager) and [Hetzner Cloud CSI Driver](https://github.com/hetznercloud/csi-driver) is planned for future releases. This will enable:

- **Cloud Load Balancers**: Automatic provisioning of Hetzner Load Balancers for Kubernetes Services of type `LoadBalancer`
- **Persistent Storage**: Dynamic provisioning of Hetzner Cloud Volumes for PersistentVolumeClaims

### Feedback Welcome

This is the first iteration of Hetzner Cloud support in KSail. If you encounter bugs or find missing features, please [open an issue on GitHub](https://github.com/devantler-tech/ksail/issues). Your feedback helps improve the tool for everyone.

### A Note on Cloud Provider Support

Testing and maintaining cloud provider integrations comes with ongoing infrastructure costs. Hetzner is supported because I use it for my own homelab and want to manage it via KSail. Additional cloud providers (AWS, GCP, Azure, etc.) will not be added without sponsorship to cover testing costs.

If you'd like to see your preferred cloud provider supported, consider [sponsoring the project on GitHub](https://github.com/sponsors/devantler).

---

_This blog post was written with the assistance of GitHub Copilot and Claude Opus 4.5. The content is based on real-world experience running Talos development clusters on Hetzner Cloud._
