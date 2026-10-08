---
title: "GitOps Without the Git Server: OCI Registries as a Flux Source with KSail"
date: 2026-03-25
authors:
  - devantler
tags:
  - kubernetes
  - gitops
  - flux
  - oci
  - ksail
description: Most local Flux setups point at a Git repo. OCI registries are cleaner — here's a full walkthrough using KSail's local registry and workload push command, plus how to extend the same workflow to GHCR for CI.
excerpt: Most local Flux setups point at a Git repo. OCI registries are cleaner — here's a full walkthrough using KSail's local registry and workload push command, plus how to extend the same workflow to GHCR for CI.
cover:
  alt: Illustration of connected local and cloud infrastructure
  image: ../../../assets/editorial/cloud-fleet.webp
---

The standard advice for running Flux locally is to point it at a Git repository. In practice that means either pushing to a remote repo and waiting for Flux to poll it, maintaining a local Git server, or wrestling with Flux's lack of support for local file paths. None of these are great for a tight development loop.

Flux has supported OCI repositories as sources since v2.0.0, and this is a much cleaner approach for local development. Instead of a Git repo, Flux watches an OCI artifact in a container registry. You push your manifests as a versioned artifact, Flux pulls and applies it. KSail takes care of the registry for you — when you create a local cluster with a GitOps engine, KSail automatically provisions a local Docker-based OCI registry alongside the cluster. No external accounts, no tokens, no network dependency. The workflow after editing manifests comes down to two commands.

Here's a complete walkthrough.

## Prerequisites

You need Docker installed and running. Verify with:

```bash
docker ps
```

KSail itself only needs Docker — it bundles kubectl, helm, flux, kustomize, kind, and more into a single binary.

```bash
brew install --cask devantler-tech/tap/ksail
```

That's it for local development. No registry accounts or tokens needed — KSail provisions a local registry automatically.

## Setting Up a Cluster with Flux

```bash
mkdir my-cluster && cd my-cluster
ksail cluster init \
  --name dev \
  --distribution Vanilla \
  --gitops-engine Flux
```

This scaffolds:

- `ksail.yaml` — KSail configuration
- `kind.yaml` — Kind cluster config (usable directly with `kind` if you ever want to bypass KSail)
- `k8s/kustomization.yaml` — root Kustomization manifest
- Flux HelmRelease and OCI source configs

Create the cluster:

```bash
ksail cluster create
```

During `cluster create`, KSail:

1. Provisions a local OCI registry as a Docker container
2. Creates the Kind cluster and attaches it to the registry
3. Bootstraps Flux and configures it to watch the local registry as an OCI source

By the time the command finishes, Flux is running and watching the local registry. No manual registry configuration needed.

## Pushing Manifests

Add something to `k8s/` — a namespace, a Deployment, whatever you're working on:

```yaml
# k8s/my-app/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
        - name: my-app
          image: nginx:stable-alpine
          ports:
            - containerPort: 80
```

Push and reconcile:

```bash
ksail workload push
ksail workload reconcile
```

`workload push` packages your `k8s/` directory as an OCI artifact and pushes it to the local registry. It auto-detects the registry from the running cluster — no flags or configuration needed. `workload reconcile` triggers Flux to pull the latest artifact and waits for all Kustomizations to report Ready.

The round trip from `workload push` to reconciliation complete is typically under 10 seconds since everything stays on localhost.

## How Registry and Tag Resolution Works

When you run `ksail workload push`, the registry is resolved using this priority chain:

1. An explicit `oci://` ref on the command line: `ksail workload push oci://localhost:5050/k8s:v1.2.3`
2. CLI flag or environment variable (`--registry` / `KSAIL_REGISTRY`)
3. `spec.cluster.localRegistry` in `ksail.yaml`
4. Cluster GitOps resources (FluxInstance or ArgoCD Application)
5. Auto-detection from Docker containers matching the cluster name

For local development, option 5 handles everything — you don't need to configure anything.

The tag follows a similar priority:

1. Tag from the `oci://` ref on the command line
2. `spec.workload.tag` in `ksail.yaml`
3. Tag embedded in the registry URL from config
4. The default: `dev`

Locally the `dev` tag is convenient — every push overwrites the same tag, and Flux detects changes via digest rather than tag, so reconciliation still fires correctly.

## Extending to GHCR for CI

The same OCI workflow translates directly to CI pipelines by pointing at an external registry like GitHub Container Registry (GHCR). This gives you immutable versioned artifacts tied to specific commits.

Override the registry at the command line to tie the artifact to a commit SHA:

```bash
ksail workload push oci://ghcr.io/YOUR_ORG/my-manifests:${{ github.sha }}
```

Each commit produces an immutable artifact. Flux picks up changes on its regular polling interval — `workload reconcile` is not needed in CI since it requires cluster access (kubeconfig) which is typically unavailable on GitHub Actions runners. If a deployment goes wrong you can point Flux at an earlier digest and it will reconcile back to a known-good state automatically.

The `workload push` command includes retry logic specifically for GHCR (added in v5.67.0), which handles the transient authentication errors that GHCR occasionally returns under load. For pipelines that push during high-traffic windows this meaningfully improves reliability.

A minimal GitHub Actions workflow step:

```yaml
- name: Push manifests to GHCR
  run: |
    echo "${{ secrets.GITHUB_TOKEN }}" | \
      docker login ghcr.io -u ${{ github.actor }} --password-stdin
    ksail workload push \
      oci://ghcr.io/${{ github.repository_owner }}/my-cluster-manifests:${{ github.sha }}
  env:
    KSAIL_CLUSTER_NAME: staging
```

The `KSAIL_CLUSTER_NAME` environment variable tells KSail which cluster configuration to load when you have multiple clusters defined.

For cloud clusters (e.g., Hetzner) where there's no local Docker daemon, an external registry is required. Use the `--local-registry` flag during `cluster init` to configure it:

```bash
ksail cluster init \
  --distribution Talos \
  --provider Hetzner \
  --gitops-engine Flux \
  --local-registry '${GITHUB_USER}:${GITHUB_TOKEN}@ghcr.io/your-org/your-cluster'
```

The `--local-registry` flag accepts the format `[user:pass@]host[:port][/path]`. Environment variable placeholders like `${GITHUB_TOKEN}` are expanded at runtime, keeping credentials out of your config files.

## What About `workload watch`?

For purely local iteration where you don't want to think about pushing at all, `ksail workload watch` is even lower friction:

```bash
ksail workload watch --path ./k8s
```

This watches the `k8s/` directory and applies changes directly with kubectl, bypassing the OCI push step entirely. If Flux is running it will also trigger selective Kustomization reconciliation on affected CRs automatically.

The OCI push workflow is the better choice when:

- You want local and CI behavior to be identical
- You're testing Flux OCI source configuration itself
- You need immutable versioned artifacts (each push gets a content digest)
- You're sharing manifests across multiple clusters or environments

`workload watch` is better when:

- You're iterating fast and CI parity doesn't matter right now
- The push/reconcile round trip feels slow for your current task

Both workflows coexist without conflict. It's a per-session choice.

## A Note on Secret Management

Regardless of whether you use a local registry or GHCR, secrets shouldn't be in your manifests in plain text. KSail has built-in SOPS integration (`ksail cipher encrypt`) for secret management — encrypt secrets before they land in `k8s/`.

If you use GHCR, packages inherit from your repository's visibility by default. For anything sensitive, set the package visibility explicitly in GitHub's package settings, or use a private repository.

---

KSail is open source under Apache-2.0. Full documentation including the OCI push workflow, Flux source configuration, and the composite GitHub Action for CI is at [ksail.devantler.tech](https://ksail.devantler.tech).

---

_This blog post was written with the assistance of AI. The content reflects genuine experiences and opinions; the AI helped structure and articulate them._
