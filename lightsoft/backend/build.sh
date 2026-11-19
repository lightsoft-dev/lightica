#!/usr/bin/env bash
# Build the production backend image from this checkout (LIGH-190).
#
#   bash lightsoft/backend/build.sh
#
# Tags two names on one image:
#   $IMAGE:<team version>   e.g. 0.11.0-20261119 — one per build, never moved
#   $IMAGE:<release>        e.g. v0.11.0 — the name production runs. The selfhost
#                           compose file uses MULTICA_IMAGE_TAG for backend AND web,
#                           so the fork image has to carry the upstream release tag.
# The image reports the team version as its server version and HEAD as /health's commit.
#
# Prints the team version on stdout; build output goes to stderr.
# Env: IMAGE (default localhost/lightsoft/multica-backend — a local-only name, so a
#      `compose pull` fails instead of fetching someone else's image),
#      RELEASE (default: the newest upstream release merged into HEAD).
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
IMAGE="${IMAGE:-localhost/lightsoft/multica-backend}"
die() { echo "build.sh: $*" >&2; exit 1; }

cd "$root"
# Only these paths go into the image (see Dockerfile).
if [ -n "$(git status --porcelain -- server Dockerfile docker LICENSE NOTICE .dockerignore)" ]; then
  die "uncommitted changes under server/ or the Dockerfile — the image must match a commit"
fi

release="$(lightsoft/version.sh release)"
commit="$(git rev-parse HEAD)"
# shellcheck disable=SC2207
taken=($(docker images --format '{{.Tag}}' "$IMAGE"))
version="$(lightsoft/version.sh team ${taken[@]+"${taken[@]}"})"

echo "building $IMAGE:$version ($release, $(git rev-parse --short HEAD))" >&2
docker build -f Dockerfile \
  --build-arg VERSION="$version" \
  --build-arg COMMIT="$commit" \
  --build-arg DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --label org.opencontainers.image.version="$version" \
  --label org.opencontainers.image.revision="$commit" \
  --label org.opencontainers.image.source=https://github.com/lightsoft-dev/multica \
  --label dev.lightsoft.upstream-release="$release" \
  -t "$IMAGE:$version" -t "$IMAGE:$release" \
  . >&2

printf '%s\n' "$version"
