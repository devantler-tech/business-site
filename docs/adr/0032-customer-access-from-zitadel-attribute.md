# 0032: Grant client access from an administrator-set ZITADEL attribute

- Status: Proposed, 2026-11-16, pending the maintainer's confirmation
- Amends: the server boundary "a token's email/groups or browser fields cannot assign a
  role" ([server/AGENTS.md](../../server/AGENTS.md)), for the client role only
- Scope: #635, with devantler-tech/platform#6410; part of #302

## Context

The maintainer wants to invite customers from his phone's browser as an administrator
of the customer identity provider, without a terminal. Until now, only the trusted
`--portal-invite` command, run inside a web pod, could bind a customer's exact Dex
identity to a workspace. The server rule above kept any token claim from assigning a role.

## Decision

- **Who sets it.** An administrator sets the ZITADEL user metadata `portal_workspace` to
  the customer's workspace. Users cannot set or delete their own metadata.
- **How it reaches the portal.** A ZITADEL Action in the Customers organization emits it
  only for a workspace the organization has declared; customer Dex maps it to the `groups`
  claim. The portal reads `groups` only from Dex's verified ID token, never from UserInfo.
- **First sign-in.** With no membership and no pending invitation, exactly one value in the
  workspace format admits the identity as that workspace's **client**, under the per-identity
  lock. A pending invitation still wins.
- **Every later sign-in.** A client membership signs in only while the token names its
  workspace. Removing or changing the attribute, removing the workspace or deactivating the
  user refuses the next sign-in. The 30-minute absolute session lifetime bounds an open one.
- **What it never does.** It never grants operator, never moves a membership to another
  workspace, never revives a revoked one, and never uses email. Operators come only from
  `--portal-invite`.

## Consequences

- The ZITADEL Customers administrator role now grants client access. Protect it like the
  production cluster: GitHub sign-in with MFA, and as few administrators as possible.
- A wrong workspace typed into the attribute grants that workspace. The Action refuses
  unknown workspaces, and the console shows the attribute's status.
- The rule in [server/AGENTS.md](../../server/AGENTS.md) now names this one exception.
