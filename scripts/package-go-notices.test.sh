#!/usr/bin/env bash
set -euo pipefail
repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
scratch="$(mktemp -d)"
trap 'rm -rf -- "$scratch"' EXIT
mkdir -p "$scratch/bin"
printf '%s\n' '#!/usr/bin/env bash' 'set -eu' 'case "$*" in' \
  '  "env GOROOT") printenv GO_NOTICES_FIXTURE_ROOT ;;' \
  '  "env GOVERSION") printf "%s\n" go1.27.2 ;;' \
  '  *) exit 17 ;;' 'esac' > "$scratch/bin/go"
chmod +x "$scratch/bin/go"

fail() { printf 'Go notices: FAIL — %s\n' "$*" >&2; exit 1; }
prepare() {
  rm -rf -- "$scratch/case"
  mkdir -p "$scratch/case/output" "$scratch/case/$1"
  fixture_root="$scratch/case/$1"
  printf 'go1.27.2\ntime fixture\n' > "$fixture_root/VERSION"
  printf 'fictional Go license\n' > "$fixture_root/LICENSE"
  printf 'fictional Go patent notice\n' > "$fixture_root/PATENTS"
}
run_copy() {
  env PATH="$scratch/bin:$PATH" GO_NOTICES_FIXTURE_ROOT="$fixture_root" \
    bash "$repo/scripts/package-go-notices.sh" "$scratch/case/output" > "$scratch/result" 2>&1
}
accept() {
  run_copy || fail "$1 rejected"
  printf 'fictional Go license\n' | cmp -s - "$scratch/case/output/Go-LICENSE" || fail "$1 license differs"
  printf 'fictional Go patent notice\n' | cmp -s - "$scratch/case/output/Go-PATENTS" || fail "$1 patent notice differs"
}
reject() {
  if run_copy; then fail "$1 accepted"; fi
  [[ ! -e "$scratch/case/output/Go-LICENSE" && ! -e "$scratch/case/output/Go-PATENTS" ]] ||
    fail "$1 copied incomplete notices"
}

prepare official/go
accept 'official distribution'

prepare Cellar/go/1.27.2/libexec
mv "$fixture_root/LICENSE" "${fixture_root%/libexec}/LICENSE"
accept 'split Homebrew distribution'

prepare Cellar/go/1.27.2_1/libexec
mv "$fixture_root/LICENSE" "${fixture_root%/libexec}/LICENSE"
accept 'Homebrew formula revision'

prepare Cellar/go@1.27/1.27.2/libexec
mv "$fixture_root/LICENSE" "${fixture_root%/libexec}/LICENSE"
accept 'versioned Homebrew formula'

prepare official/go
rm -- "$fixture_root/LICENSE"
reject 'missing license'

prepare official/go
rm -- "$fixture_root/PATENTS"
reject 'missing patent notice'

prepare official/go
: > "$fixture_root/LICENSE"
reject 'empty license'

prepare official/go
printf 'go1.26.1\n' > "$fixture_root/VERSION"
reject 'compiler and distribution version mismatch'

prepare unrelated/1.27.2/libexec
mv "$fixture_root/LICENSE" "${fixture_root%/libexec}/LICENSE"
reject 'unrelated parent license'

prepare Cellar/go/1.26.1/libexec
mv "$fixture_root/LICENSE" "${fixture_root%/libexec}/LICENSE"
reject 'different formula version'

prepare Cellar/go@1.26/1.27.2/libexec
mv "$fixture_root/LICENSE" "${fixture_root%/libexec}/LICENSE"
reject 'different versioned formula'

prepare official/go
rm -- "$fixture_root/VERSION"
reject 'missing distribution version'

printf '%s\n' 'Go notices: 4 supported layouts and 8 incomplete/mismatched distributions verified'
