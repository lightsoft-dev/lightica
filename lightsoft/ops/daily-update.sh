#!/usr/bin/env bash
# Lightsoft — daily update of the self-hosted Multica on the Mac Studio (LIGH-99).
#
#   1. Production   upgrade to the newest upstream *release* (pinned MULTICA_IMAGE_TAG),
#                   after a verified pg_dump. Rolls the images back if the new
#                   release does not come up healthy (the DB is NOT restored
#                   automatically — the backup path is reported instead).
#   2. Fork mirror  fast-forward lightsoft-dev/multica `main` to upstream `main`,
#                   and push upstream's release tags (v*) the fork does not have yet.
#   3. Team branch  merge the release production runs into `lightsoft/dev`, so the
#                   team client is never ahead of the production API.
#   4. Team client  rebuild/restart compose project `multica-team-web` when
#                   `lightsoft/dev` moved (or the container is not running).
#   5. Report       comment on the tracking issue when something changed; open an
#                   issue for the project agent when something failed.
#
# Installed copy: ~/.multica/ops/daily-update.sh (run by launchd, see
# lightsoft/ops/README.md). Config: ~/.multica/ops/config.env.
# DRY_RUN=1 prints what would change without touching anything.
set -uo pipefail

export PATH="/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin"
# Never act with an agent run's identity, even if started from one.
for v in $(env | grep -oE '^MULTICA_[A-Z_]+' || true); do unset "$v"; done

OPS_HOME="${OPS_HOME:-$HOME/.multica/ops}"
# shellcheck disable=SC1091
[ -f "$OPS_HOME/config.env" ] && . "$OPS_HOME/config.env"

SERVER_DIR="${SERVER_DIR:-$HOME/.multica/server}"
COMPOSE_FILE="$SERVER_DIR/docker-compose.selfhost.yml"
REPO_DIR="${REPO_DIR:-$OPS_HOME/multica-custom}"
BACKUP_DIR="${BACKUP_DIR:-$HOME/.multica/backups}"
KEEP_BACKUPS="${KEEP_BACKUPS:-14}"
TEAM_BRANCH="${TEAM_BRANCH:-lightsoft/dev}"
UPSTREAM_REPO="${UPSTREAM_REPO:-multica-ai/multica}"
API="${API:-http://127.0.0.1:8080}"
TEAM_WEB="${TEAM_WEB:-http://127.0.0.1:13000}"
REPORT_ISSUE="${REPORT_ISSUE:-}"
FAILURE_PROJECT="${FAILURE_PROJECT:-}"
FAILURE_ASSIGNEE_ID="${FAILURE_ASSIGNEE_ID:-}"
DRY_RUN="${DRY_RUN:-0}"

LOG_DIR="$OPS_HOME/logs"
STATE_DIR="$OPS_HOME/state"
mkdir -p "$LOG_DIR" "$STATE_DIR" "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"
TS="$(date +%Y%m%d-%H%M%S)"
LOG="$LOG_DIR/daily-update-$(date +%Y%m%d).log"
exec >>"$LOG" 2>&1
cd "$OPS_HOME" || exit 1

CHANGES=()
FAILURES=()
log()     { printf '%s %s\n' "$(date '+%F %T')" "$*" >&2; }  # stderr: keeps $(...) captures clean
changed() { log "CHANGED: $*"; CHANGES+=("$*"); }
failed()  { log "FAILED: $*"; FAILURES+=("$*"); }

if ! mkdir "$STATE_DIR/lock" 2>/dev/null; then
  log "another run holds $STATE_DIR/lock — exiting"
  exit 0
fi
trap 'rmdir "$STATE_DIR/lock" 2>/dev/null' EXIT

log "=== daily update start (dry_run=$DRY_RUN) ==="

env_get() { grep -E "^$1=" "$SERVER_DIR/.env" | tail -1 | cut -d= -f2-; }
env_set() { sed -i '' "s|^$1=.*|$1=$2|" "$SERVER_DIR/.env"; }
compose() { docker compose -f "$COMPOSE_FILE" "$@"; }
health_commit() {
  curl -sf --max-time 3 "$API/health" 2>/dev/null \
    | python3 -c 'import json,sys; print(json.load(sys.stdin).get("commit",""))' 2>/dev/null
}
wait_backend() {  # $1 = expected commit (empty: any healthy)
  local want=$1 got i
  for i in $(seq 1 150); do
    got="$(health_commit)"
    if [ -n "$got" ] && { [ -z "$want" ] || [ "$got" = "$want" ]; }; then return 0; fi
    sleep 2
  done
  return 1
}

# ------------------------------------------------------------------ repo ----
prepare_repo() {
  if [ ! -d "$REPO_DIR/.git" ]; then
    log "cloning ops checkout into $REPO_DIR"
    git clone -q https://github.com/lightsoft-dev/multica.git "$REPO_DIR" || return 1
    git -C "$REPO_DIR" remote add upstream "https://github.com/$UPSTREAM_REPO.git"
  fi
  git -C "$REPO_DIR" fetch -q --prune origin \
    && git -C "$REPO_DIR" fetch -q --prune --tags upstream
}

# ------------------------------------------------------------ production ----
latest_release() {
  curl -sf --max-time 20 "https://api.github.com/repos/$UPSTREAM_REPO/releases/latest" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["tag_name"])' 2>/dev/null
}
newer_than() {  # is $1 a newer version than $2?
  [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -1)" = "$1" ]
}

backup_db() {  # $1 = label
  local out="$BACKUP_DIR/multica-$1-$TS.dump"
  docker exec multica-postgres-1 sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc' > "$out" || return 1
  chmod 600 "$out"
  local tables
  tables="$(docker run --rm -v "$BACKUP_DIR:/b:ro" pgvector/pgvector:pg17 pg_restore -l "/b/$(basename "$out")" 2>/dev/null | grep -c 'TABLE DATA' || true)"
  [ "${tables:-0}" -gt 0 ] || { log "backup $out looks empty"; return 1; }
  cp -p "$SERVER_DIR/.env" "$BACKUP_DIR/server.env-$1-$TS" && chmod 600 "$BACKUP_DIR/server.env-$1-$TS"
  log "backup ok: $out ($tables tables)"
  printf '%s' "$out"
}

prune_backups() {
  # shellcheck disable=SC2012
  ls -1t "$BACKUP_DIR"/multica-*.dump 2>/dev/null | tail -n +"$((KEEP_BACKUPS + 1))" | while read -r f; do
    rm -f "$f"; log "pruned old backup $f"
  done
}

prune_old_images() {  # keep only $1 (current) and $2 (previous, the rollback target)
  local img tag
  for img in multica-backend multica-web; do
    docker images --format '{{.Tag}}' "ghcr.io/multica-ai/$img" | while read -r tag; do
      case "$tag" in
        "$1"|"$2") ;;
        *) docker rmi "ghcr.io/multica-ai/$img:$tag" >/dev/null 2>&1 && log "removed old image $img:$tag" ;;
      esac
    done
  done
}

update_production() {
  local cur new want backup
  cur="$(env_get MULTICA_IMAGE_TAG)"
  new="$(latest_release)"
  if [ -z "$new" ]; then failed "production: could not read the latest upstream release"; return; fi
  log "production: running $cur, latest release $new"
  if [ "$cur" != latest ] && ! newer_than "$new" "$cur"; then log "production: up to date"; return; fi
  for img in multica-backend multica-web; do
    if ! docker manifest inspect "ghcr.io/multica-ai/$img:$new" >/dev/null 2>&1; then
      log "production: ghcr.io/multica-ai/$img:$new not published yet — retry tomorrow"; return
    fi
  done
  want="$(git -C "$REPO_DIR" rev-list -n1 "$new" 2>/dev/null)"
  if [ "$DRY_RUN" = 1 ]; then log "dry-run: would upgrade production $cur -> $new ($want)"; return; fi

  backup="$(backup_db "$cur")" || { failed "production: DB backup failed — upgrade to $new skipped"; return; }
  env_set MULTICA_IMAGE_TAG "$new"
  if ! compose pull -q backend frontend; then
    env_set MULTICA_IMAGE_TAG "$cur"; failed "production: pulling $new failed — stayed on $cur"; return
  fi
  compose up -d --no-deps backend frontend
  if wait_backend "$want"; then
    changed "운영 Multica $cur → $new 업그레이드 (DB 백업: \`$backup\`)"
    prune_backups
    prune_old_images "$new" "$cur"
  else
    log "production: $new did not become healthy — rolling images back to $cur"
    env_set MULTICA_IMAGE_TAG "$cur"
    compose up -d --no-deps backend frontend
    if wait_backend ""; then
      failed "운영 $new 업그레이드 실패 → 이미지를 $cur 로 롤백함. 마이그레이션이 적용됐을 수 있으니 확인 필요 (DB 백업: \`$backup\`)"
    else
      failed "운영 $new 업그레이드 실패, $cur 롤백 후에도 backend가 healthy 아님 — 즉시 확인 필요 (DB 백업: \`$backup\`)"
    fi
  fi
}

# ------------------------------------------------------------------ fork ----
mirror_fork_main() {
  cd "$REPO_DIR" || return
  if [ "$(git rev-parse origin/main)" = "$(git rev-parse upstream/main)" ]; then
    log "fork main: up to date"; cd "$OPS_HOME"; return
  fi
  if git merge-base --is-ancestor origin/main upstream/main; then
    if [ "$DRY_RUN" = 1 ]; then
      log "dry-run: would fast-forward fork main to $(git rev-parse --short upstream/main)"
    elif git push -q origin upstream/main:refs/heads/main; then
      changed "포크 main → upstream main $(git rev-parse --short upstream/main) (fast-forward)"
    else
      failed "fork main: push failed"
    fi
  else
    failed "포크 main이 upstream과 갈라짐 (fast-forward 불가) — 수동 확인 필요"
  fi
  cd "$OPS_HOME"
}

# Release tags let a fork-only checkout merge the release production runs
# (`git merge v0.11.0`) without adding the upstream remote. Only tags the fork
# lacks are pushed; a tag already on the fork is never moved or forced.
mirror_release_tags() {
  local missing t tags=() remote
  cd "$REPO_DIR" || return
  if ! remote="$(git ls-remote --tags --refs origin 'refs/tags/v*')"; then
    failed "fork tags: could not list the fork's tags"; cd "$OPS_HOME"; return
  fi
  missing="$(comm -23 <(git tag -l 'v*' | sort) <(printf '%s\n' "$remote" | sed -n 's#.*refs/tags/##p' | sort))"
  while IFS= read -r t; do [ -n "$t" ] && tags+=("refs/tags/$t"); done <<<"$missing"
  if [ ${#tags[@]} -eq 0 ]; then log "fork tags: up to date"; cd "$OPS_HOME"; return; fi
  if [ "$DRY_RUN" = 1 ]; then
    log "dry-run: would push ${#tags[@]} release tag(s) to the fork"
  elif git push -q origin "${tags[@]}"; then
    changed "포크에 upstream 릴리스 태그 ${#tags[@]}개 push (최신 $(printf '%s\n' "$missing" | sort -V | tail -1))"
  else
    failed "fork tags: push failed"
  fi
  cd "$OPS_HOME"
}

# ----------------------------------------------------------- team branch ----
sync_team_branch() {
  local prod
  prod="$(env_get MULTICA_IMAGE_TAG)"
  cd "$REPO_DIR" || return
  git checkout -q -B "$TEAM_BRANCH" "origin/$TEAM_BRANCH" && git reset -q --hard "origin/$TEAM_BRANCH" && git clean -qfd
  if git merge-base --is-ancestor "$prod" HEAD; then
    log "team branch: already contains $prod"; cd "$OPS_HOME"; return
  fi
  if [ "$DRY_RUN" = 1 ]; then log "dry-run: would merge $prod into $TEAM_BRANCH"; cd "$OPS_HOME"; return; fi
  if git merge -q --no-ff --no-edit -m "chore(lightsoft): sync with upstream $prod" "$prod"; then
    if git push -q origin "$TEAM_BRANCH"; then
      changed "$TEAM_BRANCH 에 upstream $prod merge"
    else
      failed "team branch: push failed"
    fi
  else
    git merge --abort
    failed "$TEAM_BRANCH 에 $prod merge 중 충돌 — 수동 merge 필요"
  fi
  cd "$OPS_HOME"
}

# ----------------------------------------------------------- team client ----
update_team_client() {
  local head built running
  cd "$REPO_DIR" || return
  head="$(git rev-parse --short HEAD)"
  built="$(cat "$STATE_DIR/team-web.built" 2>/dev/null || true)"
  running="$(docker inspect -f '{{.State.Running}}' multica-team-web-web-1 2>/dev/null || echo false)"
  if [ "$head" = "$built" ] && [ "$running" = true ]; then
    log "team client: up to date ($head)"; cd "$OPS_HOME"; return
  fi
  if [ "$DRY_RUN" = 1 ]; then log "dry-run: would rebuild team client at $head"; cd "$OPS_HOME"; return; fi
  if TEAM_WEB_VERSION="lightsoft-dev-$head" docker compose -f lightsoft/docker-compose.team-web.yml up -d --build; then
    local i code=000
    for i in $(seq 1 60); do
      code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$TEAM_WEB/" || true)"
      [ "$code" = 200 ] && break; sleep 2
    done
    if [ "$code" = 200 ]; then
      echo "$head" > "$STATE_DIR/team-web.built"
      changed "팀 클라이언트(:3444) 재빌드 → $TEAM_BRANCH $head"
      docker image prune -f >/dev/null 2>&1 || true
    else
      failed "팀 클라이언트 재빌드 후 응답 없음 (HTTP $code)"
    fi
  else
    failed "팀 클라이언트 빌드 실패"
  fi
  cd "$OPS_HOME"
}

# ---------------------------------------------------------------- report ----
report() {
  local body="$STATE_DIR/report-$TS.md" prod
  prod="$(env_get MULTICA_IMAGE_TAG)"
  {
    if [ ${#FAILURES[@]} -gt 0 ]; then echo "## ⚠️ 운영 Multica 일일 업데이트 — 실패 ($(date '+%F %H:%M'))"; else echo "## 운영 Multica 일일 업데이트 ($(date '+%F %H:%M'))"; fi
    echo
    for c in ${CHANGES[@]+"${CHANGES[@]}"}; do echo "- ✅ $c"; done
    for f in ${FAILURES[@]+"${FAILURES[@]}"}; do echo "- ❌ $f"; done
    echo
    echo "현재 운영: \`$prod\` · 로그: \`$LOG\`"
  } > "$body"
  if [ ${#FAILURES[@]} -gt 0 ] && [ -n "$FAILURE_PROJECT" ]; then
    local args=(issue create --title "운영 Multica 일일 업데이트 실패 ($(date +%F))" --project "$FAILURE_PROJECT" --description-file "$body" --priority high)
    [ -n "$FAILURE_ASSIGNEE_ID" ] && args+=(--assignee-id "$FAILURE_ASSIGNEE_ID" --status todo)
    multica "${args[@]}" --output table || osascript -e 'display notification "운영 Multica 일일 업데이트 실패 — ~/.multica/ops/logs 확인" with title "Multica"' || true
  elif [ ${#CHANGES[@]} -gt 0 ] && [ -n "$REPORT_ISSUE" ]; then
    multica issue comment add "$REPORT_ISSUE" --content-file "$body" --output table || true
  fi
  rm -f "$body"
}

if prepare_repo; then
  update_production
  mirror_fork_main
  mirror_release_tags
  sync_team_branch
  update_team_client
else
  failed "ops checkout ($REPO_DIR) fetch/clone failed"
fi
[ "$DRY_RUN" = 1 ] || report
log "=== done: ${#CHANGES[@]} changed, ${#FAILURES[@]} failed ==="
[ ${#FAILURES[@]} -eq 0 ]
