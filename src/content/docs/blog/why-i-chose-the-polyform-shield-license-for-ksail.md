---
title: "Why I Chose the PolyForm Shield License for KSail"
date: 2026-05-05
authors:
  - devantler
tags:
  - ksail
  - licensing
  - open-source
  - polyform
description: KSail's license changed three times in 18 months. Here's why it went from Apache-2.0 to GPL-3.0 to PolyForm Shield 1.0.0, and why I think more dev tools should consider it.
excerpt: KSail's license changed three times in 18 months. Here's why it went from Apache-2.0 to GPL-3.0 to PolyForm Shield 1.0.0, and why I think more dev tools should consider it.
cover:
  alt: Illustration of a blank agreement and modular software cube within a reuse boundary
  image: ../../../assets/editorial/journal-license.webp
---

If you build a developer tool and release it under a permissive license, anyone can take your work, wrap it in a managed service, and sell it back to your users. You get nothing — not revenue, not attribution, not even a thank-you. This is the licensing problem that every solo maintainer of a useful tool will eventually face.

KSail's license has changed three times since the project started: from Apache-2.0 to GPL-3.0-only to [PolyForm Shield 1.0.0](https://polyformproject.org/licenses/shield/1.0.0). Each change was a reaction to a specific problem. This post explains the reasoning behind each step.

## Starting Point: Apache-2.0

KSail launched under the Apache License 2.0. It's the default choice for most Kubernetes ecosystem tools, and for good reason — it's well-understood, OSI-approved, and imposes minimal restrictions on users.

But Apache-2.0 doesn't protect the maintainer at all. Anyone can:

- Fork the project and build a competing product
- Offer the software as a managed or hosted service
- Market a product as a practical substitute

For a tool like KSail — a CLI that bundles Kubernetes tooling into a single binary — these scenarios are not hypothetical. A cloud provider or platform company could package KSail into a managed offering tomorrow, and there would be nothing I could do about it.

As a solo maintainer investing thousands of hours into the project, this felt like a bad deal.

## The GPL-3.0 Detour

In [PR #4543](https://github.com/devantler-tech/ksail/pull/4543), I switched to the GNU General Public License v3.0. The reasoning was straightforward: GPL's copyleft requirement means anyone distributing modified versions must also release their source code under the same license. This effectively prevents proprietary forks.

But GPL-3.0 created a different problem.

KSail is designed to be both a CLI tool and a Go library. The `pkg/` directory is intentionally public so other projects can import KSail's packages — provisioners, reconcilers, Kubernetes clients, and so on. Under GPL-3.0, any project importing KSail as a library would need to be GPL-licensed too. That copyleft requirement would effectively lock out most commercial users and even many open-source projects that use permissive licenses.

| Requirement | GPL-3.0 |
|---|---|
| Use in any project regardless of license | ❌ Copyleft requires GPL |
| Use as CLI tool or embedded library | ⚠️ Library use requires GPL |
| Prevent commercial competition | ❌ GPL allows commercial use |

The copyleft was solving a problem I didn't have (ensuring source availability — the code was already public) while creating a problem I did have (preventing adoption as a library).

## Finding PolyForm Shield

The [PolyForm Project](https://polyformproject.org/) is a group of licensing lawyers and technologists developing standardized, plain-language software licenses. Their [Shield license](https://polyformproject.org/licenses/shield/1.0.0) is specifically designed for the scenario I was facing:

> [!NOTE]
> Any purpose is a permitted purpose, except for providing any product that competes with the software or any product the licensor or any of its affiliates provides using the software.
>
> — [PolyForm Shield 1.0.0, Noncompete](https://polyformproject.org/licenses/shield/1.0.0)

In [PR #4603](https://github.com/devantler-tech/ksail/pull/4603), I switched KSail from GPL-3.0-only to PolyForm Shield 1.0.0. Here's how the three requirements stack up:

| Requirement | Apache-2.0 | GPL-3.0 | PolyForm Shield |
|---|---|---|---|
| Use in any project regardless of license | ✅ | ❌ Copyleft | ✅ No copyleft |
| Use as CLI tool or embedded library | ✅ | ⚠️ Library use requires GPL | ✅ Any use allowed |
| Prevent commercial competition | ❌ | ❌ GPL allows commercial use | ✅ Non-compete clause |

PolyForm Shield is the only license I found that satisfies all three.

## What the License Actually Allows

The non-compete clause sounds broad, and it is. But the practical impact for most users is negligible. Here's a breakdown:

| Scenario | Allowed? |
|---|---|
| Company uses KSail internally | ✅ Yes |
| Developer uses KSail code in a proprietary project (non-competing) | ✅ Yes |
| Someone forks KSail and builds a competing product | ❌ No |
| Someone offers KSail as a managed/hosted service | ❌ No |
| Someone markets a product as a "practical substitute" for KSail (even free) | ❌ No |

If you're using KSail as a CLI tool or importing its packages into your own project, the license does not restrict you. You don't need to open-source your project, you don't need to use a specific license, and you don't need to ask permission.

The only thing you can't do is build a product that competes with KSail itself.

## The Competition Clause in Detail

The PolyForm Shield license defines competition broadly:

> [!NOTE]
> Goods and services compete even when they provide functionality through different kinds of interfaces or for different technical platforms. Applications can compete with services, libraries with plugins, frameworks with development tools, and so on, even if they're written in different programming languages or for different computer architectures. Goods and services compete even when provided free of charge. If you market a product as a practical substitute for the software or another product, it definitely competes.
>
> — [PolyForm Shield 1.0.0, Competition](https://polyformproject.org/licenses/shield/1.0.0)

This is intentionally wide. A SaaS platform that wraps KSail's functionality competes. A free CLI tool marketed as "a better KSail" competes. A rewrite in Rust that positions itself as a KSail replacement competes.

The license also includes two important safety valves:

1. **New Products**: If you're using KSail in a non-competing product and I later expand KSail to compete with your product, you can continue using the version you had before the overlap — you just can't adopt the newer version.
2. **Discontinued Products**: If I stop developing KSail, the non-compete restriction lifts for whatever line of business I stopped.

These clauses prevent the licensor from weaponizing the non-compete clause.

## Why Not Other Licenses?

I considered several alternatives before settling on PolyForm Shield. Here's why each didn't fit:

| License | Problem for KSail |
|---|---|
| **MIT / Apache-2.0** | No protection against commercial exploitation |
| **GPL-3.0 / AGPL-3.0** | Copyleft blocks library adoption in non-GPL projects |
| **BSL (Business Source License)** | Time-based conversion to open source; doesn't permanently protect the project |
| **SSPL (Server Side Public License)** | Designed for databases/infrastructure; overly broad and controversial |
| **Elastic License 2.0** | Similar intent but tied to Elastic's specific use case |
| **PolyForm Noncommercial** | Too restrictive — blocks legitimate commercial use of KSail as a tool |
| **PolyForm Shield** | ✅ Allows all use except competition |

## The Trade-offs

I want to be honest about what PolyForm Shield costs.

### It's Not OSI-approved

The [Open Source Definition](https://opensource.org/osd) requires that licenses not discriminate against fields of endeavor (clause 6) or restrict other software (clause 9). The non-compete clause violates both. By the OSI definition, KSail is **source-available**, not open source.

In practice, this distinction matters less than it sounds. The code is publicly available on GitHub. Anyone can read it, fork it, modify it, and use it. The only restriction is building a competing product — which is not something most users want to do anyway.

### Community Perception

Some developers have a strong reaction to non-OSI licenses. The terms "source-available" and "non-compete" can trigger skepticism, especially in communities that value the FSF's four freedoms. I understand this. The trade-off I'm making is: slightly smaller potential contributor base in exchange for meaningful protection of the project's sustainability.

### Discoverability

OSI-approved licenses have better tooling support. Package registries, license scanners, and compliance tools all know what Apache-2.0 or GPL-3.0 means. PolyForm Shield 1.0.0 is less recognized, which may cause friction in corporate environments that have license allowlists.

## What This Means for KSail Users

For the vast majority of users, the license change has zero practical impact:

- **CLI users**: No change. Use KSail however you like.
- **Library consumers**: No copyleft requirement. Import `pkg/` into any project with any license.
- **Contributors**: By submitting a PR, you agree your code is licensed under PolyForm Shield 1.0.0. See [CONTRIBUTING.md](https://github.com/devantler-tech/ksail/blob/main/CONTRIBUTING.md).
- **Companies**: Use KSail internally, integrate it into your workflows, build products on top of it — as long as you're not building a competing tool.

For details, see the [Licensing FAQ](https://ksail.devantler.tech/faq/#licensing).

## Closing Thoughts

As I wrote on [LinkedIn](https://www.linkedin.com/posts/nikolai-emil-damm-14a786150_why-is-the-polyform-shield-license-100-share-7457445076651196416-5m8V):

> [!NOTE]
> Why is the PolyForm Shield License 1.0.0 not more popular? It seems like a great license to protect OSS dev tools from being commercialized or wrapped in managed or hosted service. Yes it is not OSI-Approved and it would strictly speaking be "source-available", but it feels like less of a risk than choosing Apache 2.0 or any of the other OSI-Approved licenses that do not limit commercial use.
>
> — [LinkedIn](https://www.linkedin.com/posts/nikolai-emil-damm-14a786150_why-is-the-polyform-shield-license-100-share-7457445076651196416-5m8V)

I think the PolyForm Shield license fills a real gap. For solo maintainers and small teams building developer tools, the choice today is often between "let anyone commercialize your work" (permissive licenses) and "force copyleft on everyone" (GPL/AGPL). PolyForm Shield sits in the middle: use it for anything, just don't compete with it.

If you're building a dev tool and struggling with the same licensing question, take a look at the [PolyForm Shield 1.0.0 license text](https://polyformproject.org/licenses/shield/1.0.0). It's short, plain-language, and might be exactly what you need.

---

_This blog post was written with the assistance of GitHub Copilot. The content reflects my genuine experiences and reasoning; the AI helped structure and articulate it._
