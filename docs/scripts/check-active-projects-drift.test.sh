#!/usr/bin/env bash
#
# Self-test for check-active-projects-drift.sh — proves the drift guard PASSES a
# fully-consistent fixture and FAILS each individual drift scenario, asserting on
# the guard's actual error so every case pins its OWN branch (a refactor that
# made one check's failure get mis-attributed to another's message would be
# caught here). Covered failure branches: a missing marker for each of the three
# markers that have one (actions-dirs, reusable-workflows-count,
# reusable-workflows-names, projects-submodules), the count tripwire for Actions
# and Reusable Workflows, the set-equality mismatch for all four checks (Actions,
# Reusable Workflows, Submodules, Templates page — both directions for Templates),
# and the hard die_missing guard fired when a required submodule/file is absent
# (distinct from drift: it exits with a "is the submodule checked out?" error
# rather than a drift message). Run in CI so the guard's correctness is
# continuously verified — a refactor that silently weakens any check is caught
# here, not in production drift.
#
# The guard reads its repo root from $GITHUB_WORKSPACE (falling back to the
# checkout when unset), so every case points it at a self-contained fixture tree
# under a tempdir — the test needs no submodule checkout and never touches the
# real repo.
set -Eeuo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
guard="$here/check-active-projects-drift.sh"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail=0

pass_case() { # name root
  if GITHUB_WORKSPACE="$2" bash "$guard" >/dev/null 2>&1; then
    printf '  ✅ %s — passed as expected\n' "$1"
  else
    printf '  ❌ %s — expected PASS but the guard FAILED\n' "$1"; fail=1
  fi
}

# Assert the guard FAILS *and* its combined stdout+stderr contains the expected
# literal marker substring — so the case proves it tripped its own branch, not
# merely that something somewhere failed.
fail_match() { # name root expected-substring
  local out
  if out="$(GITHUB_WORKSPACE="$2" bash "$guard" 2>&1)"; then
    printf '  ❌ %s — expected FAIL but the guard PASSED\n' "$1"; fail=1
  elif grep -qF -- "$3" <<<"$out"; then
    printf '  ✅ %s — failed on the expected branch\n' "$1"
  else
    printf '  ❌ %s — FAILED but not on the expected branch (wanted: %s)\n' "$1" "$3"; fail=1
  fi
}

# Build a fully-consistent fixture repo root at $1: two composite actions, two
# reusable (workflow_call) workflows living in the actions submodule's
# .github/workflows (the standalone reusable-workflows repo is archived —
# monorepo#1964) alongside a non-workflow_call workflow the guard must ignore,
# four submodules (two infra-section, one grouped, one templates-page), one
# matching template doc page, and a homepage whose featured project is present
# on Active Projects — all guarded surfaces agreeing with their live sources.
build_fixture() {
  local root="$1"
  rm -rf "$root"
  local mdx_dir="$root/docs/src/content/docs/projects"
  local tpl_dir="$root/docs/src/content/docs/templates"
  local actions_dir="$root/github/devantler-tech/github-actions/actions"
  local rw_dir="$actions_dir/.github/workflows"
  local current="$root/github/devantler-tech/.github-public"
  mkdir -p "$mdx_dir" "$tpl_dir" "$actions_dir/alpha" "$actions_dir/beta" "$rw_dir"
  mkdir -p "$current/actions/current-alpha" "$current/.github/workflows"
  printf 'name: current-alpha\n' > "$current/actions/current-alpha/action.yml"
  printf 'on: [push, workflow_call]\n' > "$current/.github/workflows/current-ci.yml"
  printf 'on: push\n' > "$current/.github/workflows/ordinary.yaml"
  mkdir -p "$root/docs/src/data"
  printf '[{"repository":"actions"},{"repository":"foo-template"}]\n' > "$root/docs/src/data/public-products.json"

  printf 'name: alpha\n' > "$actions_dir/alpha/action.yaml"
  printf 'name: beta\n'  > "$actions_dir/beta/action.yaml"

  printf 'on:\n  workflow_call:\n' > "$rw_dir/ci-one.yaml"
  printf 'on:\n  workflow_call:\n' > "$rw_dir/cd-two.yaml"
  # The actions repo's own CI lives in the same directory; the guard must only
  # count workflow_call workflows, so this file must NOT enter the set.
  printf 'on:\n  push:\n' > "$rw_dir/repo-ci.yaml"

  cat > "$root/.gitmodules" <<'EOF'
[submodule "actions"]
	path = github/devantler-tech/github-actions/actions
	url = https://github.com/devantler-tech/actions.git
[submodule "reusable-workflows"]
	path = github/devantler-tech/github-actions/reusable-workflows
	url = https://github.com/devantler-tech/reusable-workflows.git
[submodule "bar"]
	path = applications/bar
	url = https://github.com/devantler-tech/bar.git
[submodule "foo-template"]
	path = templates/foo-template
	url = https://github.com/devantler-tech/foo-template.git
EOF

  cat > "$tpl_dir/foo.md" <<'EOF'
---
title: Foo Template
---
**Repository**: [devantler-tech/foo-template](https://github.com/devantler-tech/foo-template)
EOF

  cat > "$mdx_dir/active.mdx" <<'EOF'
---
title: Active Projects
---

<div class="projects-page">

{/* projects-submodules: applications/bar=grouped,github/devantler-tech/github-actions/actions=section,github/devantler-tech/github-actions/reusable-workflows=omitted,templates/foo-template=templates-page */}
{/* public-products: actions,foo-template */}
{/* github-automation-actions: current-alpha */}
{/* github-automation-workflows: current-ci */}

## [⚡ Actions](https://github.com/devantler-tech/actions)

- alpha — first action
- beta — second action

{/* actions-dirs: alpha,beta */}

## [☯️ Platform](https://github.com/devantler-tech/platform)

An active project that is intentionally not featured on the homepage. This
proves the guarded relation is a strict subset rather than set equality.

## 🔄 Reusable Workflows

- CI bucket
- CD bucket

{/* reusable-workflows-count: 2 */}
{/* reusable-workflows-names: cd-two,ci-one */}

</div>
EOF

  cat > "$root/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<CardGrid>
  <Card title="About the portfolio" />
  <LinkCard
    title="⚡ Actions"
    href="https://github.com/devantler-tech/actions"
  />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
}

# Path to the fixture's active.mdx — every drift case mutates this file.
mdx_path() { printf '%s/docs/src/content/docs/projects/active.mdx' "$1"; }

# 0. fully-consistent fixture → pass (the happy path; guards against a guard
#    that fails-closed on a correct page).
good="$tmp/good"; build_fixture "$good"
pass_case "fully-consistent fixture" "$good"

# 1. Actions: marker absent → missing-marker branch.
c="$tmp/actions-missing-marker"; build_fixture "$c"
sed -i.bak '/actions-dirs:/d' "$(mdx_path "$c")"
fail_match "Actions: missing actions-dirs marker" "$c" \
  "Missing 'actions-dirs: a,b,c' marker"

# 2. Actions: a third action dir but the bullets/marker still list two →
#    bullet-count tripwire fires.
c="$tmp/actions-count"; build_fixture "$c"
mkdir -p "$c/github/devantler-tech/github-actions/actions/gamma"
printf 'name: gamma\n' > "$c/github/devantler-tech/github-actions/actions/gamma/action.yaml"
fail_match "Actions: count tripwire (3 dirs, 2 bullets)" "$c" \
  "composite action(s) in the actions submodule but"

# 3. Actions: count matches but a marker name is renamed → set-equality fires.
c="$tmp/actions-set"; build_fixture "$c"
sed -i.bak 's/actions-dirs: alpha,beta/actions-dirs: alpha,gamma/' "$(mdx_path "$c")"
fail_match "Actions: set mismatch (marker renames beta→gamma)" "$c" \
  "the 'actions-dirs' marker does not match"

# 4. Reusable Workflows: count marker absent → missing-marker branch.
c="$tmp/rw-missing-count"; build_fixture "$c"
sed -i.bak '/reusable-workflows-count:/d' "$(mdx_path "$c")"
fail_match "Reusable Workflows: missing reusable-workflows-count marker" "$c" \
  "Missing 'reusable-workflows-count: N'"

# 5. Reusable Workflows: names marker absent (count present) → missing-names branch.
c="$tmp/rw-missing-names"; build_fixture "$c"
sed -i.bak '/reusable-workflows-names:/d' "$(mdx_path "$c")"
fail_match "Reusable Workflows: missing reusable-workflows-names marker" "$c" \
  "Missing 'reusable-workflows-names: a,b,c'"

# 6. Reusable Workflows: count marker disagrees with the live workflow count.
c="$tmp/rw-count"; build_fixture "$c"
sed -i.bak 's/reusable-workflows-count: 2/reusable-workflows-count: 3/' "$(mdx_path "$c")"
fail_match "Reusable Workflows: count tripwire (marker 3, live 2)" "$c" \
  "but the marker declares"

# 7. Reusable Workflows: count matches but a name is renamed → set-equality fires.
c="$tmp/rw-set"; build_fixture "$c"
sed -i.bak 's/reusable-workflows-names: cd-two,ci-one/reusable-workflows-names: cd-three,ci-one/' \
  "$(mdx_path "$c")"
fail_match "Reusable Workflows: set mismatch (marker renames cd-two→cd-three)" "$c" \
  "'reusable-workflows-names' marker does not match"

# 8. Submodules: marker absent → missing-marker branch.
c="$tmp/submodule-missing-marker"; build_fixture "$c"
sed -i.bak '/projects-submodules:/d' "$(mdx_path "$c")"
fail_match "Submodules: missing projects-submodules marker" "$c" \
  "Missing 'projects-submodules: path=disposition,...'"

# 9. Submodules: a new submodule in .gitmodules not recorded in the marker →
#    submodule set-equality fires.
c="$tmp/submodule-set"; build_fixture "$c"
cat >> "$c/.gitmodules" <<'EOF'
[submodule "newthing"]
	path = applications/newthing
	url = https://github.com/devantler-tech/newthing.git
EOF
fail_match "Submodules: unrecorded new submodule" "$c" \
  "Submodule drift: the 'projects-submodules' marker does not match"

# 10. Templates page (forward): a templates-page submodule (recorded in
#     .gitmodules so the submodule check still passes) has no matching template
#     doc page → templates set-equality fires on the "missing a doc page" side.
c="$tmp/templates-missing-page"; build_fixture "$c"
sed -i.bak 's#templates/foo-template=templates-page#templates/foo-template=templates-page,templates/baz-template=templates-page#' \
  "$(mdx_path "$c")"
cat >> "$c/.gitmodules" <<'EOF'
[submodule "baz-template"]
	path = templates/baz-template
	url = https://github.com/devantler-tech/baz-template.git
EOF
fail_match "Templates: templates-page submodule with no doc page" "$c" \
  "link is wrong): [baz-template]"

# 11. Templates page (reverse): a template doc page whose repo is NOT
#     dispositioned =templates-page in the marker (and not a submodule, so the
#     submodule check still passes) → templates set-equality fires on the
#     "has a doc page but not dispositioned" side.
c="$tmp/templates-extra-page"; build_fixture "$c"
cat > "$c/docs/src/content/docs/templates/qux.md" <<'EOF'
---
title: Qux Template
---
**Repository**: [devantler-tech/qux-template](https://github.com/devantler-tech/qux-template)
EOF
fail_match "Templates: doc page not dispositioned templates-page" "$c" \
  "not dispositioned templates-page in the marker: [qux-template]"

# 12. die_missing: a required submodule is absent → hard error (exit 1 with the
#     "is the submodule checked out?" message), distinct from a drift failure.
c="$tmp/missing-submodule"; build_fixture "$c"
rm -rf "$c/github/devantler-tech/github-actions/actions"
fail_match "die_missing: actions submodule not checked out" "$c" \
  "actions submodule not found"

# 13. Retired-repo link: ANY page under docs/src/content linking to an archived
#     repo trips the guard — not just the guarded lists. Uses the homepage,
#     because that is exactly where this drifted in production: active.mdx was
#     updated when reusable-workflows was archived and the homepage LinkCard was
#     not (monorepo#1813, theme 2).
c="$tmp/retired-link"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'FIXTURE'
---
title: Home
---

<LinkCard href="https://github.com/devantler-tech/reusable-workflows" />
FIXTURE
fail_match "retired-repo link on an unguarded page" "$c" \
  "Retired-repo link"

# 14. Homepage featured-project set: a literal card with no matching linked H2
#     on Active Projects must fail. The blog's dynamic LinkCard is deliberately
#     outside the Featured Projects section and must not enter the comparison.
c="$tmp/homepage-featured-project"; build_fixture "$c"
sed -i.bak 's/title="⚡ Actions"/title="👻 Ghost Project"/' \
  "$c/docs/src/content/docs/index.mdx"
fail_match "Homepage: featured project absent from Active Projects" "$c" \
  "Homepage featured-project drift"

# 15. The homepage is now a required source for the cross-page invariant. A
#     missing file must fail closed instead of silently skipping the check.
c="$tmp/homepage-missing"; build_fixture "$c"
rm "$c/docs/src/content/docs/index.mdx"
fail_match "Homepage: source file missing" "$c" \
  "homepage index.mdx not found"

# 16. A rendered LinkCard without a literal title cannot participate in the
#     invariant and must fail closed with the card-shape error.
c="$tmp/homepage-empty-featured"; build_fixture "$c"
sed -i.bak '/title="⚡ Actions"/d' "$c/docs/src/content/docs/index.mdx"
fail_match "Homepage: Featured Projects has no literal card titles" "$c" \
  "Every homepage Featured Projects LinkCard must have exactly one literal title"

# 17. Bracketed text without an inline-link target is not a linked project H2.
#     Removing every target must identify the broken Active Projects source,
#     not masquerade as unrelated stale homepage cards.
c="$tmp/active-projects-empty-linked-headings"; build_fixture "$c"
sed -i.bak -E 's|^## \[([^]]+)\]\([^)]*\).*$|## [\1]|' "$(mdx_path "$c")"
fail_match "Active Projects: no linked project headings" "$c" \
  "No linked H2 project titles found on Active Projects"

# 18. Single quotes are valid MDX literal syntax. A ghost card using them must
#     not disappear merely because another double-quoted card was extracted.
c="$tmp/homepage-single-quoted-title"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<CardGrid>
  <LinkCard title='👻 Ghost Project' href="https://example.invalid/ghost" />
  <LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: single-quoted ghost project is not omitted" "$c" \
  "Homepage featured-project drift"

# 19. Two LinkCards on one line are still separate rendered MDX nodes. Both must
#     enter the comparison, so the Ghost card is caught.
c="$tmp/homepage-compact-cards"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<CardGrid>
  <LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" /> <LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: compact LinkCards fail closed" "$c" \
  "Homepage featured-project drift"

# 20. Attribute order is irrelevant in MDX. A preceding data-title must not
#     impersonate the real Ghost title.
c="$tmp/homepage-data-title"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<CardGrid>
  <LinkCard data-title="⚡ Actions" title='👻 Ghost Project' href="https://example.invalid/ghost" />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: data-title cannot mask the real title" "$c" \
  "Homepage featured-project drift"

# 21. Two cards with the same visible identity are not a valid set. Raw title
#     and LinkCard counts still agree, so only the uniqueness guard catches it.
c="$tmp/homepage-duplicate-title"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<CardGrid>
  <LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
  <LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: duplicate featured title" "$c" \
  "titles must be unique"

# 22. Attribute-looking text inside another quoted value must not mask the real
#     title; the MDX AST identifies the Ghost title semantically.
c="$tmp/homepage-quoted-title-text"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<CardGrid>
  <LinkCard description='See title="⚡ Actions" docs' title="👻 Ghost Project" href="https://example.invalid/ghost" />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: quoted title text cannot mask the real title" "$c" \
  "Homepage featured-project drift"

# 23. A preceding component on the same line is a separate MDX node and cannot
#     donate its title to the following Ghost LinkCard.
c="$tmp/homepage-preceding-card"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<CardGrid>
  <Card title="⚡ Actions" /> <LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: preceding Card cannot mask LinkCard title" "$c" \
  "Homepage featured-project drift"

# 24. An MDX comment is not rendered content. A commented-out card must not
#     satisfy an otherwise-empty Featured Projects section.
c="$tmp/homepage-commented-card"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

{/* <LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" /> */}

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: commented LinkCard is not rendered content" "$c" \
  "No literal LinkCard titles found in the homepage Featured Projects section"

# 25. A fenced example is likewise source text rather than a rendered card. It
#     must not satisfy the required live-card extraction.
c="$tmp/homepage-fenced-card"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

```mdx
<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
```

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: fenced LinkCard is not rendered content" "$c" \
  "No literal LinkCard titles found in the homepage Featured Projects section"

# 26. A closing fence must use at least as many markers as its opener. The
#     triple-backtick line inside this four-backtick example is literal content,
#     so the Actions card remains excluded; the matching closer must then restore
#     parsing so the live Ghost card is caught.
c="$tmp/homepage-long-fence"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

````mdx
```
<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
````

<LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: shorter fence marker does not close a long fence" "$c" \
  "Homepage featured-project drift"

# 27. A linked H2 inside an MDX comment is not a rendered Active Project and
#     cannot satisfy a ghost homepage card.
c="$tmp/active-projects-commented-heading"; build_fixture "$c"
sed -i.bak 's/title="⚡ Actions"/title="👻 Ghost Project"/' \
  "$c/docs/src/content/docs/index.mdx"
cat >>"$(mdx_path "$c")" <<'EOF'

{/*
## [👻 Ghost Project](https://example.invalid/ghost)
*/}
EOF
fail_match "Active Projects: commented heading cannot satisfy homepage" "$c" \
  "Homepage featured-project drift"

# 28. A linked H2 inside a fenced example is likewise non-rendered source and
#     must not enter the independently navigable project-title set.
c="$tmp/active-projects-fenced-heading"; build_fixture "$c"
sed -i.bak 's/title="⚡ Actions"/title="👻 Ghost Project"/' \
  "$c/docs/src/content/docs/index.mdx"
cat >>"$(mdx_path "$c")" <<'EOF'

````mdx
```
## [👻 Ghost Project](https://example.invalid/ghost)
````
EOF
fail_match "Active Projects: fenced heading cannot satisfy homepage" "$c" \
  "Homepage featured-project drift"

# 29. Comment-looking text inside inline code is rendered text, not an MDX
#     comment opener. It must not hide the real Ghost card that follows.
c="$tmp/homepage-inline-code-comment-marker"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
`{/*`
<LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: inline-code comment marker cannot hide a card" "$c" \
  "Homepage featured-project drift"

# 30. CommonMark forbids a backtick in a backtick fence info string, so this is
#     rendered text rather than a fence opener and cannot hide the Ghost card.
c="$tmp/homepage-invalid-backtick-fence"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
```foo`bar
<LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: invalid backtick fence cannot hide a card" "$c" \
  "Homepage featured-project drift"

# 31. An escaped comment opener is rendered text too. Reject the ambiguous
#     source shape rather than entering comment state and hiding the real card.
c="$tmp/homepage-escaped-comment-marker"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
\{/*
<LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: escaped comment marker cannot hide a card" "$c" \
  "Homepage featured-project drift"

# 32. Attribute-looking text inside a multiline template expression is not a
#     literal title attribute. The actual expression-valued title is unsupported
#     by the cross-page invariant and must fail closed.
c="$tmp/homepage-template-string-title"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard
  description={`example
title="⚡ Actions"
`}
  title={"👻 Ghost Project"}
  href="https://example.invalid/ghost"
/>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: template text cannot impersonate literal title" "$c" \
  "Every homepage Featured Projects LinkCard must have exactly one literal title"

# 33. A linked-H2-looking line inside MDX ESM is JavaScript data, not a rendered
#     Active Project section, and cannot satisfy the homepage subset.
c="$tmp/active-projects-template-heading"; build_fixture "$c"
sed -i.bak 's/title="⚡ Actions"/title="👻 Ghost Project"/' \
  "$c/docs/src/content/docs/index.mdx"
cat >>"$(mdx_path "$c")" <<'EOF'

export const fakeProject = `
## [👻 Ghost Project](https://example.invalid/ghost)
`;
EOF
fail_match "Active Projects: JS template heading cannot satisfy homepage" "$c" \
  "Homepage featured-project drift"

# 34. Conditional JSX is runtime-dependent and cannot be enumerated from the
#     static LinkCard nodes. Reject it rather than silently omitting Ghost.
c="$tmp/homepage-conditional-card"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
{showGhost && <LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />}

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: conditional LinkCard fails closed" "$c" \
  "Featured Projects must remain statically inspectable"

# 35. A spread can override a literal title at runtime, so a card using one is
#     not statically safe even when it also declares an accepted title.
c="$tmp/homepage-spread-card"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard title="⚡ Actions" {...projectProps} />

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: spread attributes fail closed" "$c" \
  "Every homepage Featured Projects LinkCard must have exactly one literal title"

# 36. Inline JSX uses mdxJsxTextElement rather than mdxJsxFlowElement. It is
#     still rendered and must enter the comparison.
c="$tmp/homepage-inline-linkcard"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

Featured now: <LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: inline LinkCard enters comparison" "$c" \
  "Homepage featured-project drift"

# 37. The section boundary must be unique, otherwise choosing one silently
#     leaves another rendered Featured Projects section unchecked.
c="$tmp/homepage-duplicate-featured-heading"; build_fixture "$c"
cat >>"$c/docs/src/content/docs/index.mdx" <<'EOF'

## Featured Projects

<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
EOF
fail_match "Homepage: duplicate Featured Projects headings fail closed" "$c" \
  "Expected exactly one rendered '## Featured Projects' heading"

# 38. Malformed MDX must fail at the semantic parser rather than degrading to
#     an incomplete text scan.
c="$tmp/homepage-malformed-mdx"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard title="⚡ Actions"
EOF
fail_match "Homepage: malformed MDX fails closed" "$c" \
  "Unable to parse project metadata as MDX"

# 39. A non-LinkCard JSX element can still render a card through an expression-
#     valued prop. Reject all runtime JSX attributes in the guarded section.
c="$tmp/homepage-expression-attribute-card"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
<CardGrid children={<LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />} />

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: expression-valued JSX attributes fail closed" "$c" \
  "Featured Projects must remain statically inspectable"

# 40. Decoded title text is untrusted workflow-command data. Newlines must be
#     escaped so a title cannot inject a second GitHub Actions command.
c="$tmp/homepage-annotation-injection"; build_fixture "$c"
sed -i.bak 's/title="⚡ Actions"/title="👻 Ghost\&#10;::notice::Injected"/' \
  "$c/docs/src/content/docs/index.mdx"
fail_match "Homepage: annotation data escapes decoded newlines" "$c" \
  "%0A::notice::Injected"

# 41. Parse failures must identify the source file that failed, including the
#     Active Projects side of the comparison.
c="$tmp/active-projects-malformed-mdx"; build_fixture "$c"
cat >>"$(mdx_path "$c")" <<'EOF'

<Broken
EOF
fail_match "Active Projects: malformed MDX reports its own file" "$c" \
  "file=docs/src/content/docs/projects/active.mdx::Unable to parse project metadata as MDX"

# 42. Astro frontmatter is metadata, not rendered Markdown. A linked-H2-looking
#     YAML block scalar cannot satisfy the homepage subset.
c="$tmp/active-projects-frontmatter-heading"; build_fixture "$c"
sed -i.bak '/title: Active Projects/a\
fake: |\
  ## [👻 Ghost Project](https://example.invalid/ghost)' "$(mdx_path "$c")"
sed -i.bak 's/title="⚡ Actions"/title="👻 Ghost Project"/' \
  "$c/docs/src/content/docs/index.mdx"
fail_match "Active Projects: frontmatter heading cannot satisfy homepage" "$c" \
  "Homepage featured-project drift"

# 43. The whole section must not sit beneath a runtime-controlled JSX ancestor.
#     Keeping its H2 top-level makes the section boundary statically meaningful.
c="$tmp/homepage-runtime-wrapper"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

<Conditional show={false}>

## Featured Projects

<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />

</Conditional>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: runtime-wrapped Featured section fails closed" "$c" \
  "Featured Projects heading must remain top-level"

# 44. A nested or blockquoted H2 is rendered content inside the section, not the
#     top-level boundary. It must not truncate inspection before a later card.
c="$tmp/homepage-nested-h2"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

## Featured Projects

<LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />

> ## Note
>
> Featured projects can change over time.

<LinkCard title="👻 Ghost Project" href="https://example.invalid/ghost" />

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: nested H2 cannot truncate Featured section" "$c" \
  "Homepage featured-project drift"

# 45. A linked project H2 beneath a runtime JSX ancestor is not a statically
#     rendered Active Projects section and cannot satisfy homepage parity.
c="$tmp/active-projects-runtime-wrapper"; build_fixture "$c"
cat >>"$(mdx_path "$c")" <<'EOF'

<Conditional show={false}>

## [👻 Ghost Project](https://example.invalid/ghost)

</Conditional>
EOF
sed -i.bak 's/title="⚡ Actions"/title="👻 Ghost Project"/' \
  "$c/docs/src/content/docs/index.mdx"
fail_match "Active Projects: runtime-wrapped H2 cannot satisfy homepage" "$c" \
  "Homepage featured-project drift"

# 46. Runtime content inside a linked Active Project H2 changes its rendered
#     identity. It must not be dropped by literal-text extraction.
c="$tmp/active-projects-computed-title"; build_fixture "$c"
sed -i.bak '/<div class="projects-page">/i\
export const suffix = " Renamed";\
' "$(mdx_path "$c")"
sed -i.bak 's/\[⚡ Actions\]/[⚡ Actions{suffix}]/' "$(mdx_path "$c")"
fail_match "Active Projects: computed linked-H2 title fails closed" "$c" \
  "Linked Active Projects H2 titles must remain literal"

# 47. Attribute-free JSX can still compute rendered link text. Literal-title
#     validation must reject the component itself, not just computed props.
c="$tmp/active-projects-jsx-title"; build_fixture "$c"
sed -i.bak '/<div class="projects-page">/i\
export const Suffix = () => " Renamed";\
' "$(mdx_path "$c")"
sed -i.bak 's/\[⚡ Actions\]/[⚡ Actions<Suffix \/>]/' "$(mdx_path "$c")"
fail_match "Active Projects: JSX inside linked-H2 title fails closed" "$c" \
  "Linked Active Projects H2 titles must remain literal"

# 48. A rendered LinkCard can be imported under another component name. Unknown
#     JSX in the guarded section must fail closed instead of disappearing from
#     the comparison while a different normal LinkCard keeps the set non-empty.
c="$tmp/homepage-aliased-linkcard"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

import { CardGrid, LinkCard, LinkCard as FeaturedLink } from "@astrojs/starlight/components";

## Featured Projects

<CardGrid>
  <LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
  <FeaturedLink title="👻 Ghost Project" href="https://example.invalid/ghost" />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: aliased LinkCard renderer fails closed" "$c" \
  "Unsupported JSX component in Featured Projects"

# 49. A custom component can suppress children without accepting any props.
#     Only the page's known static layout wrapper may contain source headings.
c="$tmp/active-projects-static-custom-wrapper"; build_fixture "$c"
sed -i.bak '/<div class="projects-page">/i\
export const Hidden = () => null;\
' "$(mdx_path "$c")"
cat >>"$(mdx_path "$c")" <<'EOF'

<Hidden>

## [👻 Ghost Project](https://example.invalid/ghost)

</Hidden>
EOF
sed -i.bak 's/title="⚡ Actions"/title="👻 Ghost Project"/' \
  "$c/docs/src/content/docs/index.mdx"
fail_match "Active Projects: custom wrapper cannot satisfy homepage" "$c" \
  "Homepage featured-project drift"

# 50. An allowlisted JSX tag name is not sufficient when an import aliases a
#     different renderer under it. This valid MDX renders Ghost via LinkCard-as-
#     Card while retaining a normal LinkCard that keeps the inspected set nonempty.
c="$tmp/homepage-rebound-linkcard"; build_fixture "$c"
cat >"$c/docs/src/content/docs/index.mdx" <<'EOF'
---
title: Home
---

import { CardGrid, LinkCard, LinkCard as Card } from "@astrojs/starlight/components";

## Featured Projects

<CardGrid>
  <Card title="👻 Ghost Project" href="https://example.invalid/ghost" />
  <LinkCard title="⚡ Actions" href="https://github.com/devantler-tech/actions" />
</CardGrid>

## Latest from the Blog

<LinkCard title={post.data.title} href={`/${post.id}/`} />
EOF
fail_match "Homepage: rebound LinkCard binding fails closed" "$c" \
  "Featured Projects component bindings must use named exports"

c="$tmp/public-products-set"; build_fixture "$c"
printf '[{"repository":"foo-template"}]\n' > "$c/docs/src/data/public-products.json"
fail_match "Public catalogue: removed product cannot leave inventory stale" "$c" "Public product catalogue drift"

c="$tmp/public-products-missing"; build_fixture "$c"
rm "$c/docs/src/data/public-products.json"
fail_match "Public catalogue: missing source fails closed" "$c" "Public product catalogue not found"

c="$tmp/public-products-marker"; build_fixture "$c"
sed -i.bak '/public-products:/d' "$(mdx_path "$c")"
fail_match "Public catalogue: missing inventory fails closed" "$c" "Public product catalogue drift"

c="$tmp/extracted-site"; build_fixture "$c"
mkdir -p "$c/site"
cp -R "$c/docs" "$c/site/docs"
mv "$c/docs" "$c/unused-docs"
if GITHUB_WORKSPACE="$c" SITE_ROOT="$c/site" bash "$guard" >/dev/null 2>&1; then
  printf '  ✅ Extracted site: explicit source root preserves real portfolio joins\n'
else
  printf '  ❌ Extracted site: separate source root was not used\n'; fail=1
fi

c="$tmp/extracted-source-missing"; build_fixture "$c"
if out="$(GITHUB_WORKSPACE="$c" SITE_ROOT="$c/absent-site" bash "$guard" 2>&1)"; then
  printf '  ❌ Extracted site: missing explicit source root silently fell back\n'; fail=1
elif grep -qF "$c/absent-site/docs/src/content/docs/projects/active.mdx" <<<"$out"; then
  printf '  ✅ Extracted site: missing source fails on the selected source root\n'
else
  printf '  ❌ Extracted site: missing source failed on an unrelated root\n'; fail=1
fi

# Both extensions are GitHub workflow files; include reusable .yml files while
# ignoring ordinary CI files regardless of their extension.
c="$tmp/workflow-mixed-extension"; build_fixture "$c"
mv "$c/github/devantler-tech/github-actions/actions/.github/workflows/ci-one.yaml" "$c/github/devantler-tech/github-actions/actions/.github/workflows/ci-one.yml"
pass_case "Reusable Workflows: mixed YAML extensions are counted" "$c"

c="$tmp/workflow-yml-only"; build_fixture "$c"
for name in ci-one cd-two repo-ci; do
  mv "$c/github/devantler-tech/github-actions/actions/.github/workflows/$name.yaml" "$c/github/devantler-tech/github-actions/actions/.github/workflows/$name.yml"
done
pass_case "Reusable Workflows: a .yml-only directory is counted" "$c"

c="$tmp/workflow-yml-added"; build_fixture "$c"
printf 'on:\n  workflow_call:\n' > "$c/github/devantler-tech/github-actions/actions/.github/workflows/new-call.yml"
fail_match "Reusable Workflows: unlisted .yml trips count drift" "$c" "3 reusable (workflow_call) workflow(s)"

c="$tmp/workflow-yml-renamed"; build_fixture "$c"
mv "$c/github/devantler-tech/github-actions/actions/.github/workflows/ci-one.yaml" "$c/github/devantler-tech/github-actions/actions/.github/workflows/renamed.yml"
fail_match "Reusable Workflows: a .yml rename trips set drift" "$c" "'reusable-workflows-names' marker does not match"

c="$tmp/workflow-yml-ordinary"; build_fixture "$c"
printf 'on:\n  push:\n' > "$c/github/devantler-tech/github-actions/actions/.github/workflows/ordinary-ci.yml"
pass_case "Reusable Workflows: ordinary .yml CI is ignored" "$c"

# GitHub accepts scalar, sequence and mapping event declarations. These literal
# fixtures catch omitted reusable workflows without mirroring the parser.
for form in scalar flow-list block-list flow-map quoted-key; do
  c="$tmp/workflow-event-$form"; build_fixture "$c"
  workflow="$c/github/devantler-tech/github-actions/actions/.github/workflows/ci-one.yaml"
  case "$form" in
    scalar) printf 'on: workflow_call\n' > "$workflow" ;;
    flow-list) printf 'on: [push, workflow_call]\n' > "$workflow" ;;
    block-list) printf 'on:\n  - push\n  - workflow_call\n' > "$workflow" ;;
    flow-map) printf 'on: {workflow_call: {}}\n' > "$workflow" ;;
    quoted-key) printf '"on": workflow_call\n' > "$workflow" ;;
  esac
  pass_case "Reusable Workflows: $form event declaration is counted" "$c"
done

c="$tmp/workflow-nested-key"; build_fixture "$c"
printf 'on: push\nenv:\n  workflow_call: false\n' > "$c/github/devantler-tech/github-actions/actions/.github/workflows/ordinary.yml"
pass_case "Reusable Workflows: a key outside on is ignored" "$c"

c="$tmp/workflow-script-text"; build_fixture "$c"
printf 'on: push\njobs:\n  example:\n    steps:\n      - run: |\n          workflow_call: not-an-event\n' > "$c/github/devantler-tech/github-actions/actions/.github/workflows/ordinary.yml"
pass_case "Reusable Workflows: script text is not an event" "$c"

c="$tmp/workflow-unlisted-scalar"; build_fixture "$c"
printf 'on: workflow_call\n' > "$c/github/devantler-tech/github-actions/actions/.github/workflows/new-call.yml"
fail_match "Reusable Workflows: an unlisted scalar event trips drift" "$c" "3 reusable (workflow_call) workflow(s)"

c="$tmp/workflow-malformed-yaml"; build_fixture "$c"
printf 'on: [workflow_call\n' > "$c/github/devantler-tech/github-actions/actions/.github/workflows/ci-one.yaml"
fail_match "Reusable Workflows: malformed YAML fails closed" "$c" "Cannot parse workflow triggers"

c="$tmp/workflow-invalid-events"; build_fixture "$c"
printf 'on: 42\n' > "$c/github/devantler-tech/github-actions/actions/.github/workflows/ci-one.yaml"
fail_match "Reusable Workflows: invalid event shape fails closed" "$c" "Cannot parse workflow triggers"

# Current automation is independent of the legacy comparison: unchanged legacy
# inventories cannot hide an added, removed or renamed maintained building block.
c="$tmp/current-action-added"; build_fixture "$c"
mkdir -p "$c/github/devantler-tech/.github-public/actions/new-action"
printf 'name: new-action\n' > "$c/github/devantler-tech/.github-public/actions/new-action/action.yaml"
fail_match "Current automation: new action trips inventory drift" "$c" "Current GitHub Actions drift"

c="$tmp/current-action-removed"; build_fixture "$c"
rm "$c/github/devantler-tech/.github-public/actions/current-alpha/action.yml"
fail_match "Current automation: removed action trips inventory drift" "$c" "Current GitHub Actions drift"

c="$tmp/current-workflow-renamed"; build_fixture "$c"
mv "$c/github/devantler-tech/.github-public/.github/workflows/current-ci.yml" "$c/github/devantler-tech/.github-public/.github/workflows/renamed.yaml"
fail_match "Current automation: renamed workflow trips inventory drift" "$c" "Current reusable workflows drift"

c="$tmp/current-workflow-added"; build_fixture "$c"
printf 'on: workflow_call\n' > "$c/github/devantler-tech/.github-public/.github/workflows/new-call.yml"
fail_match "Current automation: scalar reusable workflow trips drift" "$c" "Current reusable workflows drift"

c="$tmp/current-workflow-invalid"; build_fixture "$c"
printf 'on: [workflow_call\n' > "$c/github/devantler-tech/.github-public/.github/workflows/current-ci.yml"
fail_match "Current automation: malformed workflow fails closed" "$c" "Cannot parse workflow triggers"

c="$tmp/current-marker-missing"; build_fixture "$c"
sed -i.bak '/github-automation-actions:/d' "$(mdx_path "$c")"
fail_match "Current automation: missing inventory fails closed" "$c" "Missing 'github-automation-actions' inventory"

c="$tmp/current-checkout-missing"; build_fixture "$c"
rm -rf "$c/github/devantler-tech/.github-public"
fail_match "Current automation: missing checkout fails closed" "$c" "current automation actions not found"

if [ "$fail" -ne 0 ]; then
  printf '❌ active-projects drift-guard self-test FAILED\n' >&2
  exit 1
fi
printf '✅ active-projects drift-guard self-test passed (78 cases)\n'
