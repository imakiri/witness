#!/usr/bin/env bash
# go mod tidy every module in the workspace.
#
#   scripts/tidy.sh
#
# Intra-repo requires are pinned at versions that may not exist on the proxy
# yet, so each module is tidied with directory replaces for its siblings; the
# replaces are dropped afterwards and the requires restored to the latest tag
# of each sibling (or their previous value if there is no tag).
#
# Also sourced by scripts/release.sh for the helpers below.
set -euo pipefail
cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/.."

MODS=$(sed -n '/^use (/,/^)/p' go.work | sed -n 's|^[[:space:]]*\(\.[^ ]*\)$|\1|p' | sed 's|^\./||')
tagpfx() { [ "$1" = "." ] && echo "" || echo "$1/"; }       # "." is the root module

latest() {  # latest semver tag for module dir, empty if none
  git tag --list "$(tagpfx "$1")v[0-9]*" --sort=-v:refname |
    { grep -E "^$(tagpfx "$1")v[0-9]+\.[0-9]+\.[0-9]+$" || true; } | head -1
}

deps() {  # intra-repo requires of module dir $1, excluding itself
  local self; self=$(sed -n 's/^module //p' "$1/go.mod")
  sed -n 's|.*\(github\.com/imakiri/witness[a-z/]*\).*|\1|p' "$1/go.mod" |
    sort -u | grep -v "^$self$" || true
}

declare -A DIROF MODDIRS
for d in $MODS; do
  [ -f "$d/go.mod" ] || continue
  case "$d" in examples|examples/*|testenv) continue;; esac             # examples and the demo stack are never released
  grep -q '^module github.com/imakiri/witness' "$d/go.mod" || continue
  MODDIRS[$d]=$(sed -n 's/^module //p' "$d/go.mod")
  DIROF[${MODDIRS[$d]}]=$PWD/$d
done

tidy_all() {
  local d self dep want
  for d in "${!MODDIRS[@]}"; do
    self=${MODDIRS[$d]}
    declare -A had=()
    for dep in $(deps "$d"); do
      had[$dep]=$(sed -n "s|.*$dep \(v[^ ]*\).*|\1|p" "$d/go.mod" | head -1)
    done
    for dep in "${!DIROF[@]}"; do
      [ "$dep" = "$self" ] || go mod edit -replace="$dep=${DIROF[$dep]}" "$d/go.mod"
    done
    ( cd "$d" && GOWORK=off go mod tidy )
    for dep in "${!DIROF[@]}"; do
      [ "$dep" = "$self" ] || go mod edit -dropreplace="$dep" "$d/go.mod"
    done
    # restore a real version: previous pin, else the sibling's latest tag
    for dep in $(deps "$d"); do
      want=${had[$dep]:-}
      # the replace makes tidy write this placeholder; it is not a real version
      [ "$want" = "v0.0.0-00010101000000-000000000000" ] && want=
      [ -n "$want" ] || want=$(latest "${DIROF[$dep]#"$PWD"/}" | sed 's|.*/||')
      [ -n "$want" ] && go mod edit -require="$dep@$want" "$d/go.mod"
    done
    unset had
  done
}

# ponytail: go.sum gets no entries for sibling modules (a directory replace
# needs none). Consumers verify against their own go.sum and in-repo builds go
# through go.work, so this only affects GOWORK=off builds of a single module.

if [ "${BASH_SOURCE[0]}" = "$0" ]; then tidy_all; echo tidied; fi
