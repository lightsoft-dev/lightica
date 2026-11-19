#!/usr/bin/env bash
# Prints the Lightsoft build version: <upstream release>-<YYYYMMDD>[.N]
# (LIGH-191), e.g. 0.11.0-20261119, then 0.11.0-20261119.2 for a second build
# on the same day. The date is Asia/Seoul. Lightica (lightsoft/desktop) and the
# fork backend image (LIGH-190) both take their version from here.
#
#   lightsoft/version.sh                     # nothing published yet
#   lightsoft/version.sh --taken FILE        # FILE: versions already used, one per line
#   docker image ls --format '{{.Tag}}' img | lightsoft/version.sh --taken -
#
# The upstream release is the nearest vX.Y.Z tag on HEAD. lightsoft/dev only
# ever merges upstream release tags, so that is the release it is built on.
#
# Valid semver: the date and N are numeric prerelease identifiers, so versions
# order by release, then date, then N. A plain release (0.11.0) sorts above
# every 0.11.0-<date>; the Lightica feed only carries dated versions.
#
# LIGHTSOFT_VERSION_DATE=YYYYMMDD overrides the date (tests).
set -euo pipefail

die() { printf 'version.sh: %s\n' "$*" >&2; exit 1; }

taken=""
while [ $# -gt 0 ]; do
  case "$1" in
    --taken)
      [ $# -ge 2 ] || die "--taken needs a file (or - for stdin)"
      if [ "$2" = "-" ]; then taken="$(cat)"; else taken="$(cat "$2")"; fi
      shift 2 ;;
    *) die "unknown argument: $1" ;;
  esac
done

root="$(cd "$(dirname "$0")/.." && pwd)"
describe() { git -C "$root" describe --tags --match 'v[0-9]*' --abbrev=0 HEAD 2>/dev/null; }
tag="$(describe || true)"
if [ -z "$tag" ]; then
  # A fork clone may not have the upstream tags yet.
  git -C "$root" fetch -q --no-tags https://github.com/multica-ai/multica.git 'refs/tags/v*:refs/tags/v*' >&2 \
    || die "could not fetch upstream release tags"
  tag="$(describe || true)"
fi
upstream="${tag#v}"
[[ "$upstream" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "nearest release tag '$tag' is not vX.Y.Z"

date="${LIGHTSOFT_VERSION_DATE:-$(TZ=Asia/Seoul date +%Y%m%d)}"
[[ "$date" =~ ^[0-9]{8}$ ]] || die "date '$date' is not YYYYMMDD"

base="$upstream-$date"
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
