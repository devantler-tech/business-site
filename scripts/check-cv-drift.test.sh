#!/usr/bin/env bash
#
# Self-test for check-cv-drift.mjs — proves the guard PASSES a consistent About page + CV data
# pair and FAILS on each drift it exists to catch (title, period, organisation line, role count),
# asserting on the guard's own message so every case pins its own branch.
set -Eeuo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
guard="$here/check-cv-drift.mjs"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail=0

run_guard() { # about cv
  node --disable-warning=ExperimentalWarning "$guard" "$1" "$2" 2>&1
}

pass_case() { # name about cv
  if run_guard "$2" "$3" >/dev/null; then
    printf '  ✅ %s — passed as expected\n' "$1"
  else
    printf '  ❌ %s — expected PASS but the guard FAILED\n' "$1"; fail=1
  fi
}

fail_match() { # name about cv expected-substring
  local out
  if out="$(run_guard "$2" "$3")"; then
    printf '  ❌ %s — expected FAIL but the guard PASSED\n' "$1"; fail=1
  elif grep -qF -- "$4" <<<"$out"; then
    printf '  ✅ %s — failed on the expected branch\n' "$1"
  else
    printf '  ❌ %s — FAILED but not on the expected branch (wanted: %s)\n%s\n' "$1" "$4" "$out"; fail=1
  fi
}

# A page with one current role and one earlier role folded into <details>, like the real one.
write_about() { # path title period org
  cat >"$1" <<MDX
---
title: About Me
---

## Professional Experience

### $2 <span style="float:right">$3</span>

_${4}_

Prose about the role.

## Previous Experience

<details>
  <summary><strong>Earlier roles</strong></summary>

### Teaching Assistant <span style="float:right">Sep 2020 — Dec 2020</span>

_University of Southern Denmark, Odense_

</details>

## Skills

### Not a role <span style="float:right">2000</span>
MDX
}

write_cv() { # path [extra-earlier-role]
  cat >"$1" <<TS
export const cv = {
  experience: [
    { title: "Platform Engineer", organisation: "Energinet", location: "Fredericia", engagement: "Consultant", period: "Nov 2024 — Jun 2025" },
  ],
  earlierExperience: [
    { title: "Teaching Assistant", organisation: "University of Southern Denmark", location: "Odense", period: "Sep 2020 — Dec 2020" },
    ${2:-}
  ],
};
TS
}

write_cv "$tmp/cv.ts"

write_about "$tmp/ok.mdx" "Platform Engineer" "Nov 2024 — Jun 2025" "Energinet, Fredericia — Consultant"
pass_case "consistent page and data" "$tmp/ok.mdx" "$tmp/cv.ts"

write_about "$tmp/title.mdx" "Platform Engineer and Facilitator" "Nov 2024 — Jun 2025" "Energinet, Fredericia — Consultant"
fail_match "title drift" "$tmp/title.mdx" "$tmp/cv.ts" 'title "Platform Engineer and Facilitator"'

write_about "$tmp/period.mdx" "Platform Engineer" "Nov 2024 — Now" "Energinet, Fredericia — Consultant"
fail_match "period drift" "$tmp/period.mdx" "$tmp/cv.ts" 'period "Nov 2024 — Now"'

write_about "$tmp/org.mdx" "Platform Engineer" "Nov 2024 — Jun 2025" "Energinet, Fredericia"
fail_match "organisation drift" "$tmp/org.mdx" "$tmp/cv.ts" 'organisation "Energinet, Fredericia"'

write_cv "$tmp/extra.ts" '{ title: "Student Developer", organisation: "Umbraco", location: "Odense", period: "Jul 2022 — Aug 2023" },'
fail_match "role count drift" "$tmp/ok.mdx" "$tmp/extra.ts" "lists 2 roles"

if [ "$fail" -ne 0 ]; then
  echo "check-cv-drift self-test FAILED" >&2
  exit 1
fi
echo "check-cv-drift self-test passed"
