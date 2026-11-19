#!/usr/bin/env bash
# Lightsoft team build version: <upstream release>-<YYYYMMDD>[.N] (LIGH-190, LIGH-191),
# e.g. 0.11.0-20261119, then 0.11.0-20261119.2 for a second build on the same
# day. One place for the format, shared by the fork backend image
# (lightsoft/backend) and Lightica (lightsoft/desktop).
#
#   lightsoft/version.sh                     team version; nothing used yet today
#   lightsoft/version.sh --taken FILE        FILE: versions already used, one per line (- = stdin)
#   docker image ls --format '{{.Tag}}' img | lightsoft/version.sh --taken -
#   lightsoft/version.sh --release           the upstream release itself, e.g. v0.11.0
#
# The upstream release is the newest vX.Y.Z tag merged into HEAD: lightsoft/dev
# only ever merges upstream release tags, so that is the release it is built on.
# RELEASE=vX.Y.Z picks it instead (it must be merged into HEAD) — the daily
# update names the release it just merged.
#
# Valid semver: the date and N are numeric prerelease identifiers, so versions
# order by release, then date, then N. A plain release (0.11.0) sorts above
# every 0.11.0-<date>; the Lightica feed only carries dated versions.
#
# The date is Asia/Seoul; LIGHTSOFT_VERSION_DATE=YYYYMMDD overrides it (tests).
set -euo pipefail

die() { printf 'version.sh: %s\n' "$*" >&2; exit 1; }

mode=team
taken=""
while [ $# -gt 0 ]; do
  case "$1" in
    --taken)
      [ $# -ge 2 ] || die "--taken needs a file (or - for stdin)"
      if [ "$2" = "-" ]; then taken="$(cat)"; else taken="$(cat "$2")"; fi
      shift 2 ;;
    --release) mode=release; shift ;;
    *) die "unknown argument: $1" ;;
  esac
done

root="$(cd "$(dirname "$0")/.." && pwd)"
newest_release() {
  git -C "$root" tag --merged HEAD --list 'v*' | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1 || true
}

rel="${RELEASE:-}"
if [ -z "$rel" ]; then
  rel="$(newest_release)"
  if [ -z "$rel" ]; then
    # A fork clone may not have the upstream tags yet.
    git -C "$root" fetch -q --no-tags https://github.com/multica-ai/multica.git 'refs/tags/v*:refs/tags/v*' >&2 \
      || die "could not fetch upstream release tags"
    rel="$(newest_release)"
  fi
  [ -n "$rel" ] || die "no upstream release tag (vX.Y.Z) is merged into HEAD"
fi
[[ "$rel" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "release must look like vX.Y.Z, got '$rel'"
git -C "$root" merge-base --is-ancestor "$rel" HEAD 2>/dev/null || die "$rel is not merged into HEAD"

if [ "$mode" = release ]; then
  echo "$rel"
  exit 0
fi

date="${LIGHTSOFT_VERSION_DATE:-$(TZ=Asia/Seoul date +%Y%m%d)}"
[[ "$date" =~ ^[0-9]{8}$ ]] || die "date '$date' is not YYYYMMDD"

base="${rel#v}-$date"
# Highest N already used for this base; the bare base counts as 1.
max=0
while IFS= read -r v; do
  v="${v#v}"
  v="${v//[[:space:]]/}"
  if [ "$v" = "$base" ]; then
    [ "$max" -ge 1 ] || max=1
  elif [[ "$v" == "$base".* ]]; then
    n="${v#"$base".}"
    if [[ "$n" =~ ^[0-9]+$ ]] && [ "$n" -gt "$max" ]; then max="$n"; fi
  fi
done <<< "$taken"

if [ "$max" -eq 0 ]; then
  echo "$base"
else
  echo "$base.$((max + 1))"
fi
