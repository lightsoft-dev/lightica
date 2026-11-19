#!/usr/bin/env bash
# Lightsoft team build version: <upstream release>-<YYYYMMDD>[.N] (LIGH-190, LIGH-191).
# One place for the format, shared by the fork backend image and Lightica.
#
#   lightsoft/version.sh release           newest upstream release tag merged into HEAD (vX.Y.Z)
#   lightsoft/version.sh team [taken...]   team version for a build of HEAD today, e.g. 0.11.0-20261119.
#                                           A rebuild on the same day gets .2, .3, ... — pass the versions
#                                           already used (image tags, published files) as arguments.
#
# RELEASE=vX.Y.Z picks the release instead (it must be merged into HEAD).
# The date is Asia/Seoul. Runs against the checkout this script lives in.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
die() { echo "version.sh: $*" >&2; exit 1; }

release() {
  local rel="${RELEASE:-}"
  if [ -z "$rel" ]; then
    rel="$(git -C "$root" tag --merged HEAD --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true)"
    [ -n "$rel" ] || die "no upstream release tag (vX.Y.Z) is merged into HEAD — fetch the tags first"
  fi
  [[ "$rel" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "RELEASE must look like vX.Y.Z, got '$rel'"
  git -C "$root" merge-base --is-ancestor "$rel" HEAD 2>/dev/null || die "$rel is not merged into HEAD"
  printf '%s\n' "$rel"
}

team() {
  local rel base v n=2
  rel="$(release)"
  base="${rel#v}-$(TZ=Asia/Seoul date +%Y%m%d)"
  v="$base"
  while printf '%s\n' "$@" | grep -qxF -- "$v"; do
    v="$base.$n"; n=$((n + 1))
  done
  printf '%s\n' "$v"
}

case "${1:-}" in
  release) release ;;
  team) shift; team "$@" ;;
  *) die "usage: version.sh release | team [taken...]" ;;
esac
