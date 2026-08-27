#!/usr/bin/env bash
# Release modules, deriving current versions from git tags.
#
#   scripts/release.sh .:minor observers/tee:patch
#   scripts/release.sh -n .:minor          # dry run
#
# Listed modules get bumped and tagged. Unlisted modules keep their latest
# existing tag and are not re-tagged, but every intra-repo require in the
# workspace is rewritten to the resulting versions.
set -euo pipefail
source "$(dirname "$0")/tidy.sh"        # MODS, DIROF, MODDIRS, latest, deps, tidy_all

DRY=
[ "${1:-}" = "-n" ] && { DRY=1; shift; }
[ $# -gt 0 ] || { echo "usage: release.sh [-n] <dir>:<major|minor|patch> ..." >&2; exit 1; }

bump() {  # bump <vX.Y.Z> <major|minor|patch>
  local IFS=. ; read -r x y z <<<"${1#v}"
  case $2 in
    major) echo "v$((x+1)).0.0";;
    minor) echo "v$x.$((y+1)).0";;
    patch) echo "v$x.$y.$((z+1))";;
    *) echo "bad bump kind: $2" >&2; exit 1;;
  esac
}

declare -A VER PATHVER
for d in "${!MODDIRS[@]}"; do
  t=$(latest "$d")
  VER[$d]=${t#"$(tagpfx "$d")"}
done

tags=()
for arg in "$@"; do
  d=${arg%:*}; kind=${arg##*:}
  d=${d#./}; d=${d%/}; [ -z "$d" ] && d=.
  [ -n "${MODDIRS[$d]:-}" ] || { echo "no module at $d" >&2; exit 1; }
  # ponytail: first release of a module starts at v0.1.0 regardless of kind.
  VER[$d]=$([ -n "${VER[$d]}" ] && bump "${VER[$d]}" "$kind" || echo v0.1.0)
  tags+=("$(tagpfx "$d")${VER[$d]}")
done

for d in "${!VER[@]}"; do
  t="$(tagpfx "$d")${VER[$d]}"
  [ -n "${VER[$d]}" ] && git rev-parse -q --verify "refs/tags/$t" >/dev/null &&
    ! git diff --quiet "$t" -- "$d" &&
    echo "note: $d changed since $t, not bumped" >&2
done || true

tidy_all

for d in "${!VER[@]}"; do
  PATHVER[${MODDIRS[$d]}]=${VER[$d]}
done
for d in "${!VER[@]}"; do
  for dep in $(deps "$d"); do
    v=${PATHVER[$dep]:-}
    [ -n "$v" ] || { echo "$dep (required by $d) has no tag yet — release it too" >&2; exit 1; }
    go mod edit -require="$dep@$v" "$d/go.mod"
  done
done

IFS=$'\n' tags=($(sort <<<"${tags[*]}")); unset IFS
echo "releasing: ${tags[*]}"
[ -n "$DRY" ] && { git diff --stat; exit 0; }

git add -A -- go.mod go.sum '*/go.mod' '*/go.sum'
git diff --cached --quiet || git commit -m "release ${tags[*]}"
for t in "${tags[@]}"; do git tag "$t"; done
git push origin HEAD "${tags[@]}"
