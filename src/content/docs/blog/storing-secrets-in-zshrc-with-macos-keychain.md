---
title: Storing Sensitive Values in .zshrc with macOS Keychain
date: 2026-02-12
authors:
  - devantler
tags:
  - macos
  - security
  - zsh
  - developer-experience
description: A quick guide on using macOS Keychain to avoid storing secrets in plaintext in your .zshrc.
excerpt: A quick guide on using macOS Keychain to avoid storing secrets in plaintext in your .zshrc.
cover:
  alt: Illustration of a key stored in an organizer beside a rolled terminal note
  image: ../../../assets/editorial/journal-keychain.webp
---

Storing tokens and passwords directly in `~/.zshrc` means they sit on disk in plaintext. macOS Keychain provides a built-in, encrypted alternative. Here's how to use it.

## Store a Secret

Use the `security` CLI to add a value to your Keychain:

```bash
 security add-generic-password -a "$USER" -s 'my_secret_name' -w 'SECRET_VALUE'
```

> [!TIP]
> Prepend the command with a space (note the leading space above) to prevent it from being saved in your `.zsh_history`.

- `-a "$USER"` — associates the entry with your macOS user account.
- `-s 'my_secret_name'` — a label to identify the secret (e.g., `GITHUB_TOKEN`).
- `-w 'SECRET_VALUE'` — the actual secret value.

## Retrieve a Secret

To retrieve the value later:

```bash
security find-generic-password -a "$USER" -s 'my_secret_name' -w
```

This prints the secret to stdout, making it easy to capture in a variable.

## Use It in .zshrc

Export the secret as an environment variable by adding this to `~/.zshrc`:

```bash
export GITHUB_TOKEN=$(security find-generic-password -a "$USER" -s "GITHUB_TOKEN" -w)
```

## Summary

| Task         | Command                                                         |
| ------------ | --------------------------------------------------------------- |
| **Store**    | `security add-generic-password -a "$USER" -s 'name' -w 'value'` |
| **Retrieve** | `security find-generic-password -a "$USER" -s 'name' -w`        |
| **Update**   | Delete and re-add, or use `-U` flag to update in place          |
| **Delete**   | `security delete-generic-password -a "$USER" -s 'name'`         |

That's it — no more plaintext secrets in your `.zshrc`.
