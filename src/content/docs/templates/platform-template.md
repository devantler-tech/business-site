---
title: ☸️ Platform Template
description: A batteries-included Kubernetes platform (Flux GitOps + KSail + Talos) you instantiate from a template and bootstrap onto Hetzner Cloud, fully unattended.
---

A starting point for your own homelab or small-team Kubernetes platform. Create a repository from the template, point it at your accounts, and you get a working cluster with the usual groundwork — networking, certificates, secrets, SSO, security, storage, databases, observability, backups and autoscaling — already wired together.

It runs on [Talos Linux](https://www.talos.dev/), provisioned by [KSail](https://github.com/devantler-tech/ksail) and managed by [Flux](https://fluxcd.io/) GitOps: locally on Docker for development, on [Hetzner Cloud](https://www.hetzner.com/cloud) for production. Every change is validated in CI before it reaches the cluster. It is a generic, self-bootstrapping version of the [devantler-tech platform](https://github.com/devantler-tech/platform).

**Repository**: [devantler-tech/platform-template](https://github.com/devantler-tech/platform-template)

## What's Inside

Everything below is reconciled onto the cluster by Flux — you don't wire any of it up yourself. Local and CI (Docker) get the full set; the Hetzner/prod overlay drops a few controllers to save resources.

- **GitOps & config** — Flux Operator applies every change you commit; Reloader restarts workloads when their config changes.
- **Networking** — Cilium carries traffic and terminates ingress (Gateway API), CoreDNS resolves in-cluster names, external-dns keeps your Cloudflare records pointed at the cluster, and the Hetzner CCM provisions cloud load balancers in prod.
- **Certificates** — cert-manager issues and renews TLS certificates automatically, trust-manager distributes CA bundles, and a Cloudflare Origin CA issuer covers Cloudflare-fronted domains.
- **Secrets** — OpenBao holds them, External Secrets pulls them into the cluster at runtime, and SOPS + Age keep the seed secrets encrypted in Git.
- **Identity / SSO** — Dex is the single sign-on front door; oauth2-proxy puts a login in front of apps that have none of their own.
- **Policy & runtime security** — Kyverno blocks non-compliant workloads before they start, Kubescape scores posture and flags vulnerabilities, and Tetragon enforces at runtime.
- **Storage** — Longhorn provides replicated block and shared volumes; CloudNativePG runs PostgreSQL with failover and backups.
- **Autoscaling** — Cluster Autoscaler adds and removes nodes, the Vertical Pod Autoscaler right-sizes resource requests, and KEDA scales workloads on demand, including straight from HTTP traffic.
- **Observability** — Prometheus, Grafana and Alertmanager for metrics, dashboards and alerts; Loki for logs; Grafana Alloy to collect it all; OpenCost to see what it costs.
- **Backup / DR** — Velero backs up cluster state and CloudNativePG ships database backups to S3-compatible storage (Cloudflare R2 in prod).
- **Virtualization** — KubeVirt and CDI run VM workloads alongside containers (local/CI only).
- **Demo apps** — Homepage, Headlamp and whoami, so the platform comes up with something to look at.

To run **your own** application on the platform, add it as a platform tenant from its own repository — see the [Platform Tenant Template](/templates/platform-tenant-template/).

## Getting Started

The **Bootstrap workflow** takes you from a fresh copy of the template to a running Hetzner cluster without further input.

```bash
# Create a new repo from the template
gh repo create my-platform --template devantler-tech/platform-template --private --clone
```

Then, in your new repository:

1. **Install a GitHub App** (or fine-grained PAT) with **Contents, Secrets, Environments and Actions** write access, exposed to the workflow as the `APP_ID` variable and `APP_PRIVATE_KEY` secret. The bootstrap writes credentials back as `prod` environment secrets, which the default `GITHUB_TOKEN` is not allowed to do.
2. **Set your variables and secrets** — variables `DOMAIN`, `CLOUDFLARE_ZONE`, `CLOUDFLARE_ACCOUNT_ID`, `ADMIN_EMAIL`, `HETZNER_LOCATION`; secrets `HCLOUD_TOKEN`, `GHCR_TOKEN`, `CLOUDFLARE_API_TOKEN`, and so on. Leave `SOPS_AGE_KEY`, `KUBE_CONFIG` and `TALOS_CONFIG` alone; the bootstrap generates those for you.
3. **Run the Bootstrap workflow** (Actions → 🌱 Bootstrap → *Run workflow*, choose `prod`, type `yes`). It creates the Talos cluster, saves the resulting credentials as `prod` secrets, points your Cloudflare DNS at the new load balancer, and commits the encrypted result back to your repository.

Full prerequisites, configuration tables, verification, teardown and troubleshooting live in the template's [`docs/BOOTSTRAP.md`](https://github.com/devantler-tech/platform-template/blob/main/docs/BOOTSTRAP.md).

### Local development

You don't need Hetzner or the Bootstrap workflow to develop locally — the local cluster runs entirely on Docker via KSail, using Talos with the Docker provider. With [Docker](https://docs.docker.com/get-docker/) and [KSail](https://github.com/devantler-tech/ksail) installed:

```bash
ksail cluster create
ksail workload push
ksail workload reconcile
```

## Links

- 📦 [Template on GitHub](https://github.com/devantler-tech/platform-template)
- 🚀 [Bootstrap guide](https://github.com/devantler-tech/platform-template/blob/main/docs/BOOTSTRAP.md)
- 🛰️ [The devantler-tech platform](https://github.com/devantler-tech/platform) (the upstream this template is derived from)
- 🚀 [Platform Tenant Template](/templates/platform-tenant-template/) (run your own app on the platform)
