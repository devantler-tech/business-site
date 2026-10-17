#!/usr/bin/env bash
# Copy complete notices from the selected Go distribution, never another installed version.
set -euo pipefail
fail() { printf '%s\n' 'Selected Go distribution lacks complete, version-bound notices.' >&2; exit 1; }
[[ "$#" == 1 && -d "$1" && ! -L "$1" ]] || fail
destination="$1"
go_root="$(go env GOROOT)" || fail
compiler_version="$(go env GOVERSION)" || fail
[[ "$go_root" == /* && "$compiler_version" =~ ^go[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail
go_root="$(cd -- "$go_root" && pwd -P)" || fail
distribution_version=""
IFS= read -r distribution_version < "$go_root/VERSION" || fail
[[ "$distribution_version" == "$compiler_version" ]] || fail

license="$go_root/LICENSE"
patents="$go_root/PATENTS"
if [[ ! -e "$license" && ! -L "$license" ]]; then
  # Homebrew moves LICENSE to the formula prefix but leaves PATENTS in libexec.
  # Confine this one fallback to the same formula version that supplied GOROOT.
  [[ "${go_root##*/}" == libexec ]] || fail
  prefix="${go_root%/*}"
  formula_directory="${prefix%/*}"
  cellar="${formula_directory%/*}"
  formula="${formula_directory##*/}"
  version="${compiler_version#go}"
  minor="${version%.*}"
  [[ "${cellar##*/}" == Cellar && ( "$formula" == go || "$formula" == "go@$minor" ) ]] || fail
  formula_version="${prefix##*/}"
  case "$formula_version" in
    "$version") ;;
    "$version"_*) [[ "${formula_version#"$version"_}" =~ ^[0-9]+$ ]] || fail ;;
    *) fail ;;
  esac
  license="$prefix/LICENSE"
fi

# Validate both before copying either: incomplete distributions never appear complete.
for notice in "$license" "$patents"; do
  [[ -f "$notice" && -r "$notice" && -s "$notice" ]] || fail
done
cp -- "$license" "$destination/Go-LICENSE"
cp -- "$patents" "$destination/Go-PATENTS"
