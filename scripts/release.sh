#!/usr/bin/env bash
# Two-phase release, because tags must land on main but work happens on a branch.
#
#   scripts/release.sh prepare .:minor observers/tee:patch   # on your branch
#   git commit -am 'release ...' && git push               # yours to make
#   ... merge the PR on github ...
#   scripts/release.sh tag                                   # tags origin/main
#   scripts/release.sh sync                                  # go.work replaces only
#
# prepare: tidy, bump the listed modules, rewrite every intra-repo require to
# the resulting versions, refresh the go.work replaces, record the tags in
# ./.release. Leaves the changes in
# the working tree -- committing and pushing is up to you.
# tag: verify ./.release arrived on origin/main, then tag that commit and push.
set -euo pipefail
ORIG=$PWD                               # tidy.sh cds to the repo root
source "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")/tidy.sh"        # MODS, DIROF, MODDIRS, latest, deps, tidy_all

MAIN=${MAIN:-main}
CMD=${1:-}; shift || true

bump() {  # bump <vX.Y.Z> <major|minor|patch>
  local IFS=. ; read -r x y z <<<"${1#v}"
  case $2 in
    major) echo "v$((x+1)).0.0";;
    minor) echo "v$x.$((y+1)).0";;
    patch) echo "v$x.$y.$((z+1))";;
    *) echo "bad bump kind: $2" >&2; exit 1;;
  esac
}

# go.work substitutes a workspace module's source, but MVS still resolves the
# version named in each require -- so a require on a not-yet-pushed tag breaks
# the whole workspace. A version-specific replace per intra-repo (module,
# version) pair fixes that; "latest"/"*"/bare all-versions forms are all
# rejected in go.work, so the block is regenerated on every prepare.
declare -A RELVER=()                    # dir -> version being released this run
BEGIN_MARK='// --- release.sh: intra-repo replaces (regenerated) ---'
END_MARK='// --- end release.sh ---'

sync_gowork() {
  local d dep v dir tag lines=()
  # a pending .release names versions not yet tagged; prefer those
  if [ -f .release ]; then
    while read -r tag; do
      case "$tag" in */v*) RELVER[${tag%/v*}]=v${tag##*/v};; v*) RELVER[.]=$tag;; esac
    done < .release
  fi
  for d in "${!MODDIRS[@]}"; do
    while read -r dep v; do
      dir=${DIROF[$dep]:-}; [ -n "$dir" ] || continue      # not a workspace module
      dir=${dir#"$PWD"/}
      lines+=("replace $dep $v => $([ "$dir" = "." ] && echo . || echo "./$dir")")
    done < <(sed -n 's|.*\(github\.com/imakiri/witness[a-z/]*\) \(v[^ ]*\).*|\1 \2|p' "$d/go.mod")
  done

  # modules nothing in the workspace requires: pin them at their latest tag so
  # every module has a replace. No tag (or only a prerelease) -> nothing to pin.
  local m t
  for d in "${!MODDIRS[@]}"; do
    m=${MODDIRS[$d]}
    printf '%s\n' "${lines[@]:-}" | grep -q "^replace $m " && continue
    t=${RELVER[$d]:-}                       # being released in this run
    [ -n "$t" ] || t=$(latest "$d")
    # fall back to a prerelease tag (e.g. v1.0.0-dev) -- fine for a replace,
    # which only needs a version key, not a releasable one
    [ -n "$t" ] || t=$(git tag --list "$(tagpfx "$d")v[0-9]*" --sort=-v:refname | head -1)
    [ -n "$t" ] || { echo "go.work: no tag at all for $d, no replace" >&2; continue; }
    lines+=("replace $m ${t##*/} => $([ "$d" = "." ] && echo . || echo "./$d")")
  done

  local kept
  # match by substring: an edited/mangled marker must not orphan a whole block
  kept=$(awk '/release\.sh: intra-repo replaces/{skip=1} skip!=1{print} /end release\.sh/{skip=0}' go.work)
  if [ ${#lines[@]} -eq 0 ]; then
    printf '%s\n' "$kept" > go.work
    echo "go.work: no intra-repo requires"
    return
  fi
  { printf '%s\n\n%s\n' "$kept" "$BEGIN_MARK"
    printf '%s\n' "${lines[@]}" | sort -u
    printf '%s\n' "$END_MARK"; } > go.work
  echo "go.work: $(printf '%s\n' "${lines[@]}" | sort -u | wc -l) replace(s)"
}

prepare() {
  # no args: bump the minor of whatever module you are standing in
  if [ $# -eq 0 ]; then
    set -- "${ORIG#"$PWD"}:minor"
    set -- "${1#/}"
    [ "$1" = ":minor" ] && set -- ".:minor"
  fi
  local -A VER PATHVER
  local d t arg kind dep v tags=()

  for d in "${!MODDIRS[@]}"; do t=$(latest "$d"); VER[$d]=${t#"$(tagpfx "$d")"}; done

  for arg in "$@"; do
    d=${arg%:*}; kind=${arg##*:}
    d=${d#./}; d=${d%/}; [ -z "$d" ] && d=.
    [ -n "${MODDIRS[$d]:-}" ] || { echo "no module at $d" >&2; exit 1; }
    # ponytail: first release of a module starts at v0.1.0 regardless of kind.
    VER[$d]=$([ -n "${VER[$d]}" ] && bump "${VER[$d]}" "$kind" || echo v0.1.0)
    tags+=("$(tagpfx "$d")${VER[$d]}")
    RELVER[$d]=${VER[$d]}
  done

  for d in "${!VER[@]}"; do
    t="$(tagpfx "$d")${VER[$d]}"
    [ -n "${VER[$d]}" ] && git rev-parse -q --verify "refs/tags/$t" >/dev/null &&
      ! git diff --quiet "$t" -- "$d" &&
      echo "note: $d changed since $t, not bumped" >&2
  done || true

  tidy_all

  for d in "${!VER[@]}"; do PATHVER[${MODDIRS[$d]}]=${VER[$d]}; done
  for d in "${!VER[@]}"; do
    for dep in $(deps "$d"); do
      v=${PATHVER[$dep]:-}
      # no release version for this sibling (never tagged, or only a prerelease
      # tag): leave the pin tidy_all settled on rather than blocking the release
      [ -n "$v" ] || continue
      go mod edit -require="$dep@$v" "$d/go.mod"
    done
  done

  sync_gowork

  printf '%s\n' "${tags[@]}" | sort > .release
  echo "prepared: $(tr '\n' ' ' < .release)"
  echo "commit .release and the go.mod/go.sum changes, merge into $MAIN, then: scripts/release.sh tag"
}

tag() {
  git fetch -q origin "$MAIN"
  local sha; sha=$(git rev-parse "origin/$MAIN")
  git show "origin/$MAIN:.release" > /tmp/.release.main 2>/dev/null ||
    { echo "no .release on origin/$MAIN — did the merge land?" >&2; exit 1; }
  diff -q .release /tmp/.release.main >/dev/null ||
    { echo ".release on origin/$MAIN differs from local — merge not landed or superseded" >&2; exit 1; }

  local t new=()
  while read -r t; do
    if git rev-parse -q --verify "refs/tags/$t" >/dev/null; then
      echo "already tagged: $t" >&2
    else
      git tag "$t" "$sha"; new+=("$t")
    fi
  done < .release

  [ ${#new[@]} -eq 0 ] && { echo "nothing to tag"; return; }
  git push origin "${new[@]}"
  echo "tagged $MAIN@${sha:0:8}: ${new[*]}"
}

case "$CMD" in
  prepare) prepare "$@";;
  sync)    sync_gowork;;
  tag)     tag;;
  *) echo "usage: release.sh prepare [<dir>:<kind> ...] | release.sh sync | release.sh tag" >&2; exit 1;;
esac
