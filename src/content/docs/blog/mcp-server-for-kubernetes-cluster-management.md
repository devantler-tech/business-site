---
title: "I Built an MCP Server into My Kubernetes CLI"
date: 2026-04-15
authors:
  - devantler
tags:
  - kubernetes
  - ai
  - mcp
  - devtools
  - golang
description: KSail now ships with a built-in MCP server that lets Claude, Cursor, and other AI assistants manage Kubernetes clusters directly. Here's how it works and what you can do with it.
excerpt: KSail now ships with a built-in MCP server that lets Claude, Cursor, and other AI assistants manage Kubernetes clusters directly. Here's how it works and what you can do with it.
cover:
  alt: Illustration of a terminal and a dialogue connected to infrastructure
  image: ../../../assets/editorial/agent-dialogue.webp
---

There's a growing list of tools adopting the Model Context Protocol — the standard that lets AI assistants call external capabilities as structured tools rather than guessing from context. Most of the MCP servers I've seen are wrappers around databases, APIs, or SaaS products.

I added one to a Kubernetes CLI.

[KSail](https://github.com/devantler-tech/ksail) is a tool I've been building for a while — it bundles kubectl, helm, flux, argocd, kind, k3d, talos, and vcluster into one binary and wraps them with a consistent CLI. The idea is that you shouldn't need seven tools configured and version-matched just to spin up a local cluster and deploy some workloads. `ksail cluster create` handles Kind, K3d, Talos, or VCluster with the same workflow.

Running `ksail mcp` starts a local MCP server that exposes all of that to any MCP-compatible client.

## What It Actually Exposes

The server surfaces five tool groups:

- **cluster_read** — list clusters, get cluster info, inspect configuration
- **cluster_write** — create, update, delete, start, stop, backup, restore
- **workload_read** — get, describe, logs, explain, validate manifests
- **workload_write** — apply, delete, scale, rollout, reconcile GitOps, push to registry
- **cipher_write** — encrypt and decrypt secrets with SOPS/Age

That's roughly 30 underlying CLI commands organized into read/write pairs. The read tools are lower-risk; the write tools are the ones that actually change cluster state.

## Connecting It to Claude Desktop

```json
{
  "mcpServers": {
    "ksail": {
      "command": "ksail",
      "args": ["mcp"]
    }
  }
}
```

Add that to `claude_desktop_config.json`, restart Claude Desktop, and it gains access to the full KSail tool surface. You can do things like:

- "Create a K3s cluster called dev with Cilium CNI and Flux GitOps"
- "Show me what's deployed in the workload namespace"
- "Scale the nginx deployment to 3 replicas"
- "Encrypt this secret file before I commit it"

For Cursor, the setup is similar — add the MCP server in settings and point it at the `ksail mcp` command.

## How the Tools Are Generated

This is the part I find genuinely interesting to think about.

KSail's CLI is built with [Cobra](https://github.com/spf13/cobra). Every command — `ksail cluster create`, `ksail workload apply`, `ksail cipher encrypt` — is defined as a Cobra command with flags, arguments, and descriptions.

Rather than manually writing MCP tool definitions for each command, I built a code generator that walks the Cobra command tree and auto-generates the tool definitions. The generator reads each command's description, flags, and argument patterns, and produces the corresponding MCP tool schema.

The consolidation step is where it gets interesting. Exposing 30 individual tools to an AI assistant is noisy — you end up with too many choices and the model spends time deciding which narrow tool applies rather than doing the work. Instead, parent commands annotated with `ai.toolgen.consolidate` aggregate their subcommands into a single tool that accepts a subcommand name as an argument. The `ai.toolgen.permission` annotation then splits that into read and write variants.

The result: adding a new CLI command under a consolidated parent automatically makes it available as an MCP tool. No manual registration.

## KSail Is Now in the MCP Registry

The server is registered in the [MCP Registry](https://registry.modelcontextprotocol.io/?q=ksail) as `io.github.devantler-tech/ksail`. This means MCP clients that support registry-based discovery can find and configure it without manual setup.

## The Practical Trade-Off

The write tools (cluster_write, workload_write, cipher_write) are the ones that require thought. Giving an AI assistant the ability to `ksail cluster delete` or `ksail workload apply` against a running cluster is genuinely useful for routine tasks — and genuinely risky if the model misunderstands the context.

The read/write split exists for this reason. If you're using an assistant primarily for inspection and debugging, you can expose only the read tools. If you're using it for fully autonomous cluster setup (a documented cluster-as-code pattern KSail supports), you give it write access and let it run.

KSail also has a built-in `ksail chat` command with three modes — Interactive (write ops require approval), Plan (no execution, description only), and Autopilot (full autonomous execution) — that gives you the same control in a terminal TUI rather than an external AI client.

## Where to Find It

```bash
# Install
brew install --cask devantler-tech/tap/ksail

# Start the MCP server
ksail mcp
```

Source: [github.com/devantler-tech/ksail](https://github.com/devantler-tech/ksail)

The MCP server is part of the main binary — no separate installation. If you're already using KSail, `ksail mcp` is already there.
