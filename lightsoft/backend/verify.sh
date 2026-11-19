#!/usr/bin/env bash
# Boot a fork backend image on a throwaway Postgres and run the API smoke test
# (LIGH-190). Nothing here touches the production stack or its DB.
#
#   bash lightsoft/backend/verify.sh [tag]      # tag defaults to the release HEAD is on, e.g. v0.11.0
#
# Env: IMAGE (default localhost/lightsoft/multica-backend), VERIFY_PG_PORT (5434),
#      VERIFY_BACKEND_PORT (18080), KEEP=1 leaves the stack running for a look.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
here="$root/lightsoft/backend"
export IMAGE="${IMAGE:-localhost/lightsoft/multica-backend}"
export TAG="${1:-$(cd "$root" && lightsoft/version.sh release)}"
export VERIFY_PG_PORT="${VERIFY_PG_PORT:-5434}" VERIFY_BACKEND_PORT="${VERIFY_BACKEND_PORT:-18080}"
compose() { docker compose -f "$here/docker-compose.verify.yml" "$@"; }

ref="$IMAGE:$TAG"
docker image inspect "$ref" >/dev/null || { echo "verify.sh: no local image $ref — run lightsoft/backend/build.sh first" >&2; exit 1; }
commit="$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$ref")"
version="$(docker image inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$ref")"
echo "verifying $ref ($version, ${commit:0:9})"

[ "${KEEP:-0}" = 1 ] || trap 'compose down -v --remove-orphans >/dev/null 2>&1' EXIT
compose up -d --wait postgres
compose up -d backend
api="http://127.0.0.1:$VERIFY_BACKEND_PORT"
for _ in $(seq 1 90); do
  curl -sf --max-time 2 "$api/health" >/dev/null 2>&1 && break
  sleep 2
done
curl -sf --max-time 2 "$api/health" >/dev/null || { compose logs --tail 50 backend; echo "verify.sh: backend did not come up" >&2; exit 1; }

python3 -I "$here/smoke.py" "$api" "$commit" "$version"
