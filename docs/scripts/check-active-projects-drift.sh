#!/usr/bin/env bash
#
# check-active-projects-drift.sh
#
# Guards current .github automation inventories and the retained legacy Actions lists.
# Current actions live in .github-public/actions and callable workflows in
# .github-public/.github/workflows; both are checked independently of legacy metadata.
# Guards the hand-maintained Actions / Reusable Workflows lists on the Active
# Projects page (docs/src/content/docs/projects/active.mdx) against silent
# drift from their source-of-truth repo. Both lists are sourced from the
# devantler-tech/actions submodule (the standalone reusable-workflows repo was
# merged into it and archived 2026-07-10 — monorepo#1964): composite actions
# live in its top-level directories, reusable workflows in its
# .github/workflows. The check is entirely network-free: it reads only the
# already-checked-out submodule, never the GitHub API.
#
#   - Actions list       — one bullet per composite action, so it maps 1:1 to
#                          the `*/action.yaml` / `*/action.yml` directories. A bullet *count*
#                          alone cannot catch a rename (or a net-zero add+remove)
#                          — the count stays equal while a bullet silently goes
#                          stale — so we additionally pin the exact directory
#                          names via an inline `actions-dirs: a,b,c` marker in
#                          active.mdx and enforce SET equality between that
#                          marker and the live action directories (plus the
#                          original bullet-count tripwire, so the marker can't be
#                          updated without revisiting the prose bullets).
#   - Reusable Workflows — the site list is *categorical* (CI / CD / Automation
#                          buckets), not one bullet per workflow, so a bullet
#                          count cannot map 1:1. We tripwire on the number of
#                          reusable (`workflow_call`) workflows versus an expected
#                          count declared inline in active.mdx via a
#                          `reusable-workflows-count: N` marker — but a count
#                          alone, exactly like the Actions bullet count, cannot
#                          catch a rename or a net-zero add+remove (the count
#                          stays equal while a category bullet silently goes
#                          stale), so we additionally pin the exact workflow names
#                          via a `reusable-workflows-names: a,b,c` marker and
#                          enforce SET equality between it and the live
#                          `workflow_call` workflows (keeping the count tripwire so
#                          the names marker can't be updated without revisiting the
#                          category bullets). Adding, removing, OR renaming a
#                          workflow therefore forces a human to revisit the
#                          category bullets and update both markers.
#   - Submodules         — the page is a *curated* view of the monorepo's
#                          submodule set (the source-of-truth is .gitmodules):
#                          some submodules are their own H2 product section,
#                          some are grouped under "Self-Hosted Personal Apps",
#                          some are listed on the Templates page instead, and
#                          several are infra/config repos not surfaced as
#                          projects at all. A count or section scan can't model
#                          that curation, so we pin the FULL submodule set via a
#                          `projects-submodules: path=disposition,...` marker and
#                          enforce SET equality between the marker's paths and
#                          the live .gitmodules paths. Adding or removing a
#                          submodule therefore forces a human to record it and
#                          decide whether/how it appears — a brand-new product
#                          can no longer land in the monorepo while silently
#                          going unrepresented on the site. (This check reads
#                          only the tracked .gitmodules file, so it needs no
#                          submodule checkout and stays network-free.)
#   - Templates page     — the four template submodules are dispositioned
#                          `=templates-page` in the submodule marker above
#                          because they are surfaced on the Templates page
#                          (docs/src/content/docs/templates/), not here. That
#                          page's index CardGrid is auto-derived from the
#                          `templates/*` doc collection, so the *list* cannot
#                          drift — but the set of template doc PAGES is itself
#                          hand-maintained, and nothing tied it to the
#                          dispositioned submodule set. A new template repo could
#                          be recorded as `=templates-page` (the submodule check
#                          forces that) yet never get a doc page — silently
#                          vanishing from the site — and a renamed/removed
#                          template page could likewise linger. So we enforce SET
#                          equality between the basenames of the
#                          `=templates-page` submodules and the source repos that
#                          the template doc pages declare in their
#                          `**Repository**: [devantler-tech/<repo>]` line. Adding,
#                          removing, or renaming a template therefore forces both
#                          the doc page and the marker disposition to be updated
#                          in lockstep. (Reads only the marker and the in-repo doc
#                          pages, so it stays network-free.)
#   - Homepage featured  — the homepage intentionally promotes a curated subset
#                          of Active Projects, but both surfaces are hand-written.
#                          We extract the literal LinkCard titles from only the
#                          Featured Projects section and require that set to be a
#                          subset of the linked H2 titles on Active Projects. A
#                          stale or renamed featured card therefore fails CI,
#                          while projects that are active but not featured remain
#                          valid. Dynamic blog LinkCards are outside the section
#                          and cannot enter the comparison.
#
# GITHUB_WORKSPACE names the portfolio checkout. SITE_ROOT selects an extracted
# website checkout; portfolio gitlinks still come from GITHUB_WORKSPACE.
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="${GITHUB_WORKSPACE:-$(cd "$script_dir/../.." && pwd)}"
site_root="${SITE_ROOT:-$repo_root}"

mdx="$site_root/docs/src/content/docs/projects/active.mdx"
homepage="$site_root/docs/src/content/docs/index.mdx"
actions_dir="$repo_root/github/devantler-tech/github-actions/actions"
rw_workflows_dir="$repo_root/github/devantler-tech/github-actions/actions/.github/workflows"
current_actions_dir="$repo_root/github/devantler-tech/.github-public/actions"
current_workflows_dir="$repo_root/github/devantler-tech/.github-public/.github/workflows"
gitmodules="$repo_root/.gitmodules"
templates_dir="$site_root/docs/src/content/docs/templates"
content_dir="$site_root/docs/src/content"
homepage_parity_checker="$script_dir/check-homepage-project-parity.mjs"
public_catalogue="$site_root/docs/src/data/public-products.json"

# Anchor each H2 section on the source repo URL it links to — unique and stable.
actions_anchor="](https://github.com/devantler-tech/actions)"

fail=0

die_missing() {
  echo "::error::$1 not found at '$2' — is the submodule checked out?" >&2
  exit 1
}

[ -f "$mdx" ] || die_missing "active.mdx" "$mdx"
[ -f "$homepage" ] || die_missing "homepage index.mdx" "$homepage"
[ -f "$homepage_parity_checker" ] || die_missing "homepage parity checker" "$homepage_parity_checker"
[ -f "$public_catalogue" ] || die_missing "Public product catalogue" "$public_catalogue"
[ -d "$actions_dir" ] || die_missing "actions submodule" "$actions_dir"
[ -d "$rw_workflows_dir" ] || die_missing "actions submodule workflows directory" "$rw_workflows_dir"
[ -d "$current_actions_dir" ] || die_missing "current automation actions" "$current_actions_dir"
[ -d "$current_workflows_dir" ] || die_missing "current automation workflows" "$current_workflows_dir"
[ -d "$content_dir" ] || die_missing "docs content directory" "$content_dir"

# --- Retired repos: no page may still link to one -----------------------------
#
# The lists above are guarded, so they stay current — but a link somewhere else on
# the site pointing at a repo we retired is the same drift with no tripwire. It
# happened: reusable-workflows was merged into actions and archived (2026-07-10,
# monorepo#1964), active.mdx was updated, and the homepage LinkCard kept pointing
# at the archived repo — sending readers to a read-only shell (monorepo#1813,
# theme 2).
#
# Network-free by design, like the rest of this script: the retired set is pinned
# here rather than queried, so add a repo when it is archived/renamed, together
# with what to link instead.
retired_repo_urls=(
  "https://github.com/devantler-tech/reusable-workflows|https://github.com/devantler-tech/actions/tree/main/.github/workflows"
)

for entry in "${retired_repo_urls[@]}"; do
  retired_url="${entry%%|*}"
  replacement_url="${entry##*|}"

  # Components and data also render public links. Inspect the whole source tree,
  # not just MDX; distinguish no matches from an incomplete read.
  if hits=$(grep -rFlI "$retired_url" "$site_root/docs/src"); then
    while IFS= read -r hit; do
      echo "::error file=${hit#"$repo_root"/}::Retired-repo link: this page links to '${retired_url}', \
which is archived (read-only). Link '${replacement_url}' instead." >&2
    done <<<"$hits"
    fail=1
  else
    scan_status=$?
    if [ "$scan_status" -ne 1 ]; then
      echo "::error::Cannot inspect public source for retired-repo links" >&2
      fail=1
    fi
  fi

  # The catalogue renderer constructs links from repository slugs, so there is
  # no literal URL to grep. Check the URLs that it will actually emit as well.
  if ! node --input-type=module - "$public_catalogue" "$retired_url" "$replacement_url" <<'NODE'
import { readFileSync } from 'node:fs';
try {
  const [cataloguePath, retired, replacement] = process.argv.slice(2);
  const catalogue = JSON.parse(readFileSync(cataloguePath, 'utf8'));
  if (!Array.isArray(catalogue)) throw new Error('Expected an array');
  if (catalogue.some(({ repository }) => `https://github.com/devantler-tech/${repository}` === retired)) {
    console.error(`::error::Retired-repo link: the public catalogue generates '${retired}'. Link '${replacement}' instead.`);
    process.exitCode = 1;
  }
} catch {
  console.error('::error::Cannot inspect public catalogue for retired-repo links');
  process.exitCode = 1;
}
NODE
  then
    fail=1
  fi
done

if [ "$fail" -eq 0 ]; then
  echo "OK: no page links to a retired repo (${#retired_repo_urls[@]} pinned)."
fi

# The homepage/Active Projects relation is semantic MDX, so parse its AST rather
# than guessing at comments, code fences, JSX attributes, or JavaScript strings.
if ! node "$homepage_parity_checker" "$homepage" "$mdx"; then
  fail=1
fi

[ -f "$gitmodules" ] || die_missing ".gitmodules" "$gitmodules"
[ -d "$templates_dir" ] || die_missing "templates docs directory" "$templates_dir"

# Count "- " bullets in the H2 section whose header line contains $1.
count_section_bullets() {
  awk -v anchor="$1" '
    /^## / { insec = (index($0, anchor) > 0) ? 1 : 0; next }
    insec && /^[[:space:]]*- / { n++ }
    END { print n + 0 }
  ' "$mdx"
}

# --- Actions: bullet-count tripwire + directory-name set equality --------------
# Live action directory names (either valid metadata extension), sorted & unique.
actions_live=$(
  find "$actions_dir" -mindepth 2 -maxdepth 2 -type f \( -name action.yaml -o -name action.yml \) \
    | awk -F/ '{ print $(NF - 1) }' | sort -u
)
actions_count=$(printf '%s\n' "$actions_live" | grep -c . || true)
actions_bullets=$(count_section_bullets "$actions_anchor")

# Declared directory names from the inline `actions-dirs: a,b,c` marker.
actions_marker=$(
  grep -oE 'actions-dirs:[[:space:]]*[a-z0-9,_-]+' "$mdx" \
    | sed -E 's/^actions-dirs:[[:space:]]*//' | head -n1 || true
)
actions_declared=$(printf '%s' "$actions_marker" | tr ',' '\n' | sed '/^$/d' | sort -u)

if [ -z "$actions_marker" ]; then
  echo "::error file=docs/src/content/docs/projects/active.mdx::Missing 'actions-dirs: a,b,c' marker \
in the '## ⚡ Actions' section of docs/src/content/docs/projects/active.mdx." >&2
  fail=1
elif [ "$actions_count" -ne "$actions_bullets" ]; then
  echo "::error file=docs/src/content/docs/projects/active.mdx::Actions list drift: \
${actions_count} composite action(s) in the actions submodule but ${actions_bullets} bullet(s) under \
'## ⚡ Actions'. An action was added or removed in devantler-tech/actions but the list was not updated — \
update the '## ⚡ Actions' section of docs/src/content/docs/projects/active.mdx." >&2
  fail=1
elif [ "$actions_declared" != "$actions_live" ]; then
  only_live=$(comm -23 <(printf '%s\n' "$actions_live") <(printf '%s\n' "$actions_declared") | paste -sd, -)
  only_marker=$(comm -13 <(printf '%s\n' "$actions_live") <(printf '%s\n' "$actions_declared") | paste -sd, -)
  echo "::error file=docs/src/content/docs/projects/active.mdx::Actions list drift: the 'actions-dirs' \
marker does not match the action directories in devantler-tech/actions. \
Missing from marker: [${only_live:-none}]. Stale in marker (no longer a real action): [${only_marker:-none}]. \
An action was added, removed, or renamed — update the matching bullet AND the 'actions-dirs' marker in the \
'## ⚡ Actions' section of docs/src/content/docs/projects/active.mdx." >&2
  fail=1
else
  echo "OK: Actions list in sync (${actions_count} actions == ${actions_bullets} bullets, marker set matches)."
fi

# --- Reusable Workflows: count tripwire + workflow-name set equality -----------
# Live reusable (workflow_call) workflow names (basename minus .yaml or .yml), sorted & unique.
reusable_workflows() {
  node --input-type=module - "$1" "$script_dir/../package.json" <<'NODE'
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { resolve, parse } from 'node:path';
import { createRequire } from 'node:module';
const { load } = createRequire(resolve(process.argv[3]))('js-yaml');
const names = new Set();
for (const filename of readdirSync(process.argv[2]).filter((name) => /\.ya?ml$/.test(name))) {
  const file = resolve(process.argv[2], filename);
  if (!statSync(file).isFile()) continue;
  try {
    const events = load(readFileSync(file, 'utf8'))?.on;
    let reusable;
    if (typeof events === 'string') reusable = events === 'workflow_call';
    else if (Array.isArray(events) && events.every((event) => typeof event === 'string')) {
      reusable = events.includes('workflow_call');
    } else if (events && typeof events === 'object' && !Array.isArray(events)) {
      reusable = Object.hasOwn(events, 'workflow_call');
    } else throw new Error('on must be an event name, sequence or mapping');
    if (reusable) names.add(parse(filename).name);
  } catch {
    console.error(`::error file=${file}::Cannot parse workflow triggers`);
    process.exit(1);
  }
}
console.log([...names].sort().join('\n'));
NODE
}
rw_live="$(reusable_workflows "$rw_workflows_dir")"
rw_count=$(printf '%s\n' "$rw_live" | grep -c . || true)
rw_expected=$(grep -oE 'reusable-workflows-count:[[:space:]]*[0-9]+' "$mdx" | grep -oE '[0-9]+' | head -n1 || true)

# Declared workflow names from the inline `reusable-workflows-names: a,b,c` marker.
rw_marker=$(
  grep -oE 'reusable-workflows-names:[[:space:]]*[a-z0-9,_-]+' "$mdx" \
    | sed -E 's/^reusable-workflows-names:[[:space:]]*//' | head -n1 || true
)
rw_declared=$(printf '%s' "$rw_marker" | tr ',' '\n' | sed '/^$/d' | sort -u)

if [ -z "$rw_expected" ]; then
  echo "::error file=docs/src/content/docs/projects/active.mdx::Missing 'reusable-workflows-count: N' \
marker in the '## 🔄 Reusable Workflows' section of docs/src/content/docs/projects/active.mdx." >&2
  fail=1
elif [ -z "$rw_marker" ]; then
  echo "::error file=docs/src/content/docs/projects/active.mdx::Missing 'reusable-workflows-names: a,b,c' \
marker in the '## 🔄 Reusable Workflows' section of docs/src/content/docs/projects/active.mdx." >&2
  fail=1
elif [ "$rw_count" -ne "$rw_expected" ]; then
  echo "::error file=docs/src/content/docs/projects/active.mdx::Reusable Workflows drift: \
${rw_count} reusable (workflow_call) workflow(s) in the actions submodule but the marker \
declares ${rw_expected}. A workflow was added or removed in devantler-tech/actions — review \
the category bullets under '## 🔄 Reusable Workflows' in docs/src/content/docs/projects/active.mdx and \
update the 'reusable-workflows-count' marker to ${rw_count}." >&2
  fail=1
elif [ "$rw_declared" != "$rw_live" ]; then
  only_live=$(comm -23 <(printf '%s\n' "$rw_live") <(printf '%s\n' "$rw_declared") | paste -sd, -)
  only_marker=$(comm -13 <(printf '%s\n' "$rw_live") <(printf '%s\n' "$rw_declared") | paste -sd, -)
  echo "::error file=docs/src/content/docs/projects/active.mdx::Reusable Workflows drift: the \
'reusable-workflows-names' marker does not match the workflow_call workflows in \
devantler-tech/actions. Missing from marker: [${only_live:-none}]. \
Stale in marker (no longer a workflow_call workflow): [${only_marker:-none}]. A workflow was added, \
removed, or renamed — review the category bullets under '## 🔄 Reusable Workflows' AND update the \
'reusable-workflows-names' marker in docs/src/content/docs/projects/active.mdx." >&2
  fail=1
else
  echo "OK: Reusable Workflows in sync (${rw_count} workflow_call workflows == marker ${rw_expected}, name set matches)."
fi

# Current shared automation has its own exact inventories. Both action extensions
# are valid; parse actual top-level workflow events with the same YAML parser.
current_actions_live=$(
  find "$current_actions_dir" -mindepth 2 -maxdepth 2 -type f \( -name action.yaml -o -name action.yml \) \
    | awk -F/ '{ print $(NF - 1) }' | sort -u
)
current_workflows_live="$(reusable_workflows "$current_workflows_dir")"
check_current_inventory() {
  local marker="$1" live="$2" label="$3" declared entries
  entries="$(grep -oE "${marker}:[[:space:]]*[a-z0-9,_-]+" "$mdx" | sed -E "s/^${marker}:[[:space:]]*//" || true)"
  if [ -z "$entries" ]; then
    echo "::error::Missing '$marker' inventory in active.mdx" >&2
    fail=1
    return
  fi
  if [ "$(printf '%s\n' "$entries" | wc -l | tr -d ' ')" -ne 1 ]; then
    echo "::error::$label drift: inventory must occur exactly once" >&2
    fail=1
    return
  fi
  declared="$(printf '%s' "$entries" | tr ',' '\n' | sort -u)"
  if [ "$declared" != "$live" ]; then
    echo "::error::$label drift: update '$marker' to match the pinned devantler-tech/.github inventory" >&2
    fail=1
  else
    echo "OK: $label inventory matches the current automation owner."
  fi
}
check_current_inventory github-automation-actions "$current_actions_live" "Current GitHub Actions"
check_current_inventory github-automation-workflows "$current_workflows_live" "Current reusable workflows"

# --- Submodules: every submodule is consciously represented (or excluded) ------
# Live submodule paths from .gitmodules (a tracked file at the repo root, so no
# submodule contents are needed here), sorted & unique.
submodules_live=$(
  grep -E '^[[:space:]]*path[[:space:]]*=' "$gitmodules" \
    | awk -F'=' '{ gsub(/[[:space:]]/, "", $2); print $2 }' | sort -u
)

# Declared paths from the inline `projects-submodules: path=disposition,...`
# marker — validate every complete entry BEFORE discarding its disposition.
submodules_marker=$(
  sed -nE 's#^.*projects-submodules:[[:space:]]*(.*)$#\1#p' "$mdx" \
    | sed -E 's#[[:space:]]*\*/\}[[:space:]]*$##; s/^[[:space:]]+//; s/[[:space:]]+$//'
)
submodules_valid=1
if [ -n "$submodules_marker" ]; then
  if [ "$(grep -oF 'projects-submodules:' "$mdx" | wc -l | tr -d ' ')" -ne 1 ]; then
    submodules_valid=0
  fi
  while IFS= read -r disposition_entry; do
    if [[ ! "$disposition_entry" =~ ^[a-z0-9._/-]+=(section|grouped|templates-page|infra|omitted)$ ]]; then
      submodules_valid=0
    fi
  done < <(printf '%s\n' "$submodules_marker" | tr ',' '\n')
  if [ "$(printf '%s\n' "$submodules_marker" | tr ',' '\n' | cut -d= -f1 | sort | uniq -d | wc -l | tr -d ' ')" -ne 0 ]; then
    submodules_valid=0
  fi
fi
submodules_declared=$(
  printf '%s' "$submodules_marker" | tr ',' '\n' | sed -E 's/=.*$//; /^[[:space:]]*$/d' | sort -u
)

if [ -z "$submodules_marker" ]; then
  echo "::error file=docs/src/content/docs/projects/active.mdx::Missing \
'projects-submodules: path=disposition,...' marker in \
docs/src/content/docs/projects/active.mdx." >&2
  fail=1
elif [ "$submodules_valid" -ne 1 ]; then
  echo "::error file=docs/src/content/docs/projects/active.mdx::Invalid projects-submodules disposition: use one inventory with unique paths and one of section / grouped / templates-page / infra / omitted for EVERY entry." >&2
  fail=1
elif [ "$submodules_declared" != "$submodules_live" ]; then
  only_live=$(comm -23 <(printf '%s\n' "$submodules_live") <(printf '%s\n' "$submodules_declared") | paste -sd, -)
  only_marker=$(comm -13 <(printf '%s\n' "$submodules_live") <(printf '%s\n' "$submodules_declared") | paste -sd, -)
  echo "::error file=docs/src/content/docs/projects/active.mdx::Submodule drift: the \
'projects-submodules' marker does not match the submodules in .gitmodules. \
Missing from marker: [${only_live:-none}]. Stale in marker (no longer a submodule): [${only_marker:-none}]. \
A submodule was added or removed — record it in the 'projects-submodules' marker with its disposition \
(section / grouped / templates-page / infra / omitted) and, if it should appear as a project, update the page." >&2
  fail=1
else
  sub_n=$(printf '%s\n' "$submodules_live" | grep -c .)
  echo "OK: Submodule representation in sync (${sub_n} submodules all accounted for in the marker)."
fi

# --- Templates page: every templates-page submodule has a doc page (& vice versa) ---
# Basenames of the submodules dispositioned `=templates-page` in the marker above
# (the path is always `templates/<repo>`, so the basename is the repo name).
templates_declared=$(
  printf '%s' "$submodules_marker" | tr ',' '\n' \
    | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//' \
    | grep -E '=templates-page$' \
    | sed -E 's/=templates-page$//; s#^.*/##' | sort -u
)

# Source repos the live template doc pages declare in their `**Repository**:`
# line — one per page under docs/src/content/docs/templates/*.md (the index is
# an .mdx file, so the *.md glob excludes it), sorted & unique.
templates_live=$(
  grep -hoE '^\*\*Repository\*\*:[[:space:]]*\[devantler-tech/[a-z0-9._-]+\]' "$templates_dir"/*.md 2>/dev/null \
    | sed -E 's#.*\[devantler-tech/##; s/\]$//' | sort -u
)

if [ "$templates_declared" != "$templates_live" ]; then
  only_decl=$(comm -23 <(printf '%s\n' "$templates_declared") <(printf '%s\n' "$templates_live") | paste -sd, -)
  only_page=$(comm -13 <(printf '%s\n' "$templates_declared") <(printf '%s\n' "$templates_live") | paste -sd, -)
  echo "::error file=docs/src/content/docs/projects/active.mdx::Templates page drift: the submodules \
dispositioned '=templates-page' in the 'projects-submodules' marker do not match the template doc pages \
under docs/src/content/docs/templates/. Dispositioned templates-page but missing a doc page (or its \
'**Repository**: [devantler-tech/<repo>]' link is wrong): [${only_decl:-none}]. Has a doc page but not \
dispositioned templates-page in the marker: [${only_page:-none}]. Add/remove the template doc page and \
keep the 'projects-submodules' marker disposition in lockstep." >&2
  fail=1
else
  tpl_n=$(printf '%s\n' "$templates_declared" | grep -c .)
  echo "OK: Templates page in sync (${tpl_n} templates-page submodules == ${tpl_n} template doc pages, repo set matches)."
fi

# The public page consumes the bilingual JSON, not the legacy MDX descriptions.
# Bind that rendered source to the curated inventory as well as checking the
# older technical metadata; green legacy checks alone cannot clear a stale shelf.
if ! node --input-type=module - "$public_catalogue" "$mdx" <<'NODE'
import { readFileSync } from 'node:fs';
import assert from 'node:assert/strict';
try {
  const [cataloguePath, inventoryPath] = process.argv.slice(2);
  const catalogue = JSON.parse(readFileSync(cataloguePath, 'utf8'));
  assert.ok(Array.isArray(catalogue) && catalogue.length > 0);
  const actual = catalogue.map(({ repository }) => repository);
  assert.ok(actual.every((name) => typeof name === 'string' && /^[a-z0-9._-]+$/.test(name)));
  assert.equal(new Set(actual).size, actual.length);
  const inventories = [...readFileSync(inventoryPath, 'utf8').matchAll(/public-products:\s*([a-z0-9._,-]+)/g)];
  assert.equal(inventories.length, 1);
  const expected = inventories[0][1].split(',');
  assert.equal(new Set(expected).size, expected.length);
  assert.deepEqual(actual.sort(), expected.sort());
  console.log(`OK: Public product catalogue matches inventory (${actual.length} repositories).`);
} catch (error) {
  console.error(`::error::Public product catalogue drift: update the rendered public-products.json source and its public-products: inventory together. ${error.message}`);
  process.exitCode = 1;
}
NODE
then
  fail=1
fi

exit "$fail"
